package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolveAPI(t *testing.T) {
	got, err := resolveAPI("", "openai-chat")
	if err != nil {
		t.Fatalf("resolveAPI failed: %v", err)
	}
	if got != "chat.completions" {
		t.Fatalf("api=%q want chat.completions", got)
	}

	if _, err := resolveAPI("responses", "openai-chat"); err == nil {
		t.Fatalf("expected conflicting api and route to fail")
	}
}

func TestBuildEstimateInputJSON(t *testing.T) {
	entry := dumpEntry{
		ID: 1,
		Request: dumpSide{Body: dumpBody{
			Format:  "json",
			Content: []byte(`{"messages":[{"role":"user","content":"hello"}]}`),
		}},
		Response: dumpSide{Body: dumpBody{
			Format:  "json",
			Content: []byte(`{"choices":[{"message":{"content":"hi"}}],"usage":{"prompt_tokens":3,"completion_tokens":2,"total_tokens":5}}`),
		}},
	}

	in, err := buildEstimateInput(entry, "chat.completions", "gpt-4o-mini", false)
	if err != nil {
		t.Fatalf("buildEstimateInput failed: %v", err)
	}
	if string(in.RequestBody) != `{"messages":[{"role":"user","content":"hello"}]}` {
		t.Fatalf("unexpected request body: %s", in.RequestBody)
	}
	if len(in.ResponseBody) == 0 || len(in.StreamTail) != 0 {
		t.Fatalf("unexpected response body/stream sizes: response=%d stream=%d", len(in.ResponseBody), len(in.StreamTail))
	}
	if in.UpstreamUsage == nil {
		t.Fatalf("expected upstream usage")
	}
	if in.UpstreamUsage.InputTokens != 3 || in.UpstreamUsage.OutputTokens != 2 || in.UpstreamUsage.TotalTokens != 5 {
		t.Fatalf("unexpected usage: %+v", *in.UpstreamUsage)
	}
}

func TestBuildEstimateInputSSE(t *testing.T) {
	entry := dumpEntry{
		ID:      2,
		Request: dumpSide{Body: dumpBody{Format: "empty"}},
		Response: dumpSide{Body: dumpBody{
			Format: "sse",
			Events: []dumpSSEEvent{
				{Event: "content_block_delta", Data: []byte(`{"delta":{"text":"hello "}}`)},
				{Event: "content_block_delta", Data: []byte(`{"delta":{"text":"world"}}`)},
				{Event: "message_start", Data: []byte(`{"message":{"usage":{"input_tokens":7}}}`)},
				{Data: []byte(`{"usage":{"output_tokens":4}}`)},
				{Data: []byte(`"[DONE]"`)},
			},
		}},
	}

	in, err := buildEstimateInput(entry, "claude.messages", "claude-sonnet-4-5", false)
	if err != nil {
		t.Fatalf("buildEstimateInput failed: %v", err)
	}
	if len(in.StreamTail) != 0 {
		t.Fatalf("unexpected stream tail: %q", string(in.StreamTail))
	}
	resp := string(in.ResponseBody)
	if !strings.Contains(resp, "hello world") {
		t.Fatalf("unexpected response body: %q", resp)
	}
	if in.UpstreamUsage == nil {
		t.Fatalf("expected upstream usage")
	}
	if in.UpstreamUsage.InputTokens != 7 || in.UpstreamUsage.OutputTokens != 4 || in.UpstreamUsage.TotalTokens != 11 {
		t.Fatalf("unexpected usage: %+v", *in.UpstreamUsage)
	}
}

func TestBuildEstimateInputRejectsTruncated(t *testing.T) {
	entry := dumpEntry{
		Request: dumpSide{Body: dumpBody{Format: "json", Truncated: true, Content: []byte(`{"ping":"pong"}`)}},
	}
	if _, err := buildEstimateInput(entry, "responses", "gpt-5-mini", false); err == nil {
		t.Fatalf("expected truncated body to fail")
	}
}

func TestRunParsesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dump.json")
	if err := os.WriteFile(path, []byte(`{
  "id": 1,
  "request": {"body": {"format": "json", "size": 15, "truncated": false, "content": {"ping": "pong"}}},
  "response": {"body": {"format": "json", "size": 43, "truncated": false, "content": {"ok": true, "usage": {"input_tokens": 1, "output_tokens": 2}}}}
}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--file", path, "--route", "openai-responses", "--model", "gpt-5-mini"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stderr=%s", code, stderr.String())
	}
	if !strings.Contains(stdout.String(), "status") ||
		!strings.Contains(stdout.String(), "in.actual") ||
		!strings.Contains(stdout.String(), "estimated") ||
		!strings.Contains(stdout.String(), "summary entries=1 estimated=1 skipped=0") ||
		strings.Contains(stdout.String(), "total: actual=") {
		t.Fatalf("unexpected stdout: %s", stdout.String())
	}
}

func TestRunParsesMultipleEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dump.json")
	if err := os.WriteFile(path, []byte(`[
  {
    "id": 1,
    "request": {"body": {"format": "json", "size": 15, "truncated": false, "content": {"ping": "pong"}}},
    "response": {"body": {"format": "json", "size": 43, "truncated": false, "content": {"ok": true, "usage": {"input_tokens": 1, "output_tokens": 2}}}}
  },
  {
    "id": 2,
    "request": {"body": {"format": "empty", "size": 0, "truncated": false}},
    "response": {"body": {"format": "sse", "size": 71, "truncated": false, "events": [
      {"event": "response.created", "data": {"type": "response.created"}},
      {"data": "[DONE]"}
    ]}}
  }
]`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--file", path, "--route", "openai-responses", "--model", "gpt-5-mini"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "estimated") ||
		!strings.Contains(out, "skipped") ||
		!strings.Contains(out, "incomplete upstream usage") ||
		!strings.Contains(out, "summary entries=2 estimated=1 skipped=1") ||
		strings.Contains(out, "total: actual=") {
		t.Fatalf("unexpected stdout: %s", out)
	}
}

func TestRunParsesJSONStreamEntries(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dump.log")
	if err := os.WriteFile(path, []byte(`{
  "id": 1,
  "request": {"body": {"format": "json", "size": 15, "truncated": false, "content": {"ping": "pong"}}},
  "response": {"body": {"format": "json", "size": 43, "truncated": false, "content": {"ok": true, "usage": {"input_tokens": 1, "output_tokens": 2}}}}
}
{
  "id": 2,
  "request": {"body": {"format": "empty", "size": 0, "truncated": false}},
  "response": {"body": {"format": "json", "size": 11, "truncated": false, "content": {"ok": true}}}
}
`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--file", path, "--route", "openai-responses", "--model", "gpt-5-mini"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "estimated") ||
		!strings.Contains(out, "skipped") ||
		!strings.Contains(out, "incomplete upstream usage") ||
		!strings.Contains(out, "summary entries=2 estimated=1 skipped=1") ||
		strings.Contains(out, "total: actual=") {
		t.Fatalf("unexpected stdout: %s", out)
	}
}

func TestRunDebugIDPrintsExtractedOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "dump.json")
	if err := os.WriteFile(path, []byte(`{
  "id": 7,
  "request": {"body": {"format": "json", "size": 15, "truncated": false, "content": {"input": "hello"}}},
  "response": {"body": {"format": "sse", "size": 100, "truncated": false, "events": [
    {"event": "response.output_text.delta", "data": {"type": "response.output_text.delta", "delta": "hello debug"}},
    {"event": "response.completed", "data": {"type": "response.completed", "response": {"usage": {"input_tokens": 1, "output_tokens": 2}}}}
  ]}}
}`), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	var stdout, stderr bytes.Buffer
	code := run([]string{"--file", path, "--api", "responses", "--model", "gpt-5-mini", "--debug-id", "7"}, &stdout, &stderr)
	if code != 0 {
		t.Fatalf("run code=%d stderr=%s", code, stderr.String())
	}
	out := stdout.String()
	if !strings.Contains(out, "debug dump id=7 index=0 extracted_output_chars=11") ||
		!strings.Contains(out, "hello debug") ||
		!strings.Contains(out, "summary entries=1 estimated=1 skipped=0") {
		t.Fatalf("unexpected stdout: %s", out)
	}
}
