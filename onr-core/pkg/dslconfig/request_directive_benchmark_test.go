package dslconfig

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslmeta"
)

// Request-side directive benchmarks, ordered by directive frequency across
// production provider configs (set_path/match > json_del > json_replace >
// json_set). Each op benchmarks a light (~200B) and a large (~33KB) request
// body where payload size affects the work done.
//
// ApplyJSONOps mutates the root in place, so benchmarks that measure a
// destructive op (del) rebuild the input each iteration and exclude the
// rebuild cost via timer control; idempotent ops (set/replace to a constant)
// reuse one root.

func benchmarkChatRoot(extraBytes int) map[string]any {
	content := "Summarize the benefits of using Next Router."
	if extraBytes > 0 {
		para := "China is one of the most geographically diverse countries. "
		content = strings.Repeat(para, extraBytes/len(para)+1)[:extraBytes]
	}
	return map[string]any{
		"model":  "gpt-4o-mini",
		"stream": false,
		"messages": []any{
			map[string]any{"role": "system", "content": "You are a helpful assistant."},
			map[string]any{"role": "user", "content": content},
		},
		"max_tokens":       500,
		"stream_options":   map[string]any{"include_usage": true},
		"inference_geo":    "us",
		"speed":            "fast",
		"reasoning_effort": "low",
	}
}

func benchmarkApplyJSONOps(b *testing.B, ops []JSONOp, extraBytes int, rebuildEachIter bool) {
	b.Helper()
	meta := &dslmeta.Meta{API: "chat.completions"}
	root := benchmarkChatRoot(extraBytes)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if rebuildEachIter {
			b.StopTimer()
			root = benchmarkChatRoot(extraBytes)
			b.StartTimer()
		}
		if _, err := ApplyJSONOps(meta, root, ops); err != nil {
			b.Fatalf("ApplyJSONOps: %v", err)
		}
	}
}

func BenchmarkJSONDel_Light(b *testing.B) {
	ops := []JSONOp{
		{Op: jsonOpDel, Path: "$.inference_geo"},
		{Op: jsonOpDel, Path: "$.speed"},
		{Op: jsonOpDel, Path: "$.reasoning_effort"},
	}
	benchmarkApplyJSONOps(b, ops, 0, true)
}

func BenchmarkJSONDel_Large(b *testing.B) {
	ops := []JSONOp{
		{Op: jsonOpDel, Path: "$.inference_geo"},
		{Op: jsonOpDel, Path: "$.speed"},
		{Op: jsonOpDel, Path: "$.reasoning_effort"},
	}
	benchmarkApplyJSONOps(b, ops, 33000, true)
}

func BenchmarkJSONSet_Light(b *testing.B) {
	ops := []JSONOp{{Op: jsonOpSet, Path: "$.stream_options.include_usage", ValueExpr: "true"}}
	benchmarkApplyJSONOps(b, ops, 0, false)
}

func BenchmarkJSONSet_Large(b *testing.B) {
	ops := []JSONOp{{Op: jsonOpSet, Path: "$.stream_options.include_usage", ValueExpr: "true"}}
	benchmarkApplyJSONOps(b, ops, 33000, false)
}

func BenchmarkJSONReplace_Light(b *testing.B) {
	ops := []JSONOp{{Op: jsonOpReplace, Path: "$.model", ValueExpr: `"gpt-4o-mini-mapped"`}}
	benchmarkApplyJSONOps(b, ops, 0, false)
}

func BenchmarkJSONReplace_Large(b *testing.B) {
	ops := []JSONOp{{Op: jsonOpReplace, Path: "$.model", ValueExpr: `"gpt-4o-mini-mapped"`}}
	benchmarkApplyJSONOps(b, ops, 33000, false)
}

// BenchmarkRoutingApply covers the match + set_path directives (the most
// frequent pair: every request resolves upstream path through them).
func BenchmarkRoutingApply(b *testing.B) {
	routing := &ProviderRouting{
		Matches: []RoutingMatch{
			{API: "claude.messages", SetPath: `"/v1/messages"`},
			{API: "embeddings", SetPath: `"/v1/embeddings"`},
			{API: "chat.completions", SetPath: `"/v1/chat/completions"`},
		},
	}
	meta := &dslmeta.Meta{API: "chat.completions"}

	b.ReportAllocs()
	for b.Loop() {
		if err := routing.Apply(meta); err != nil {
			b.Fatalf("routing.Apply: %v", err)
		}
	}
}

func BenchmarkRoutingApply_SetPathExpr(b *testing.B) {
	routing := &ProviderRouting{
		Matches: []RoutingMatch{
			{API: "chat.completions", SetPath: `"/v1beta/models/" + $model + ":generateContent"`},
		},
	}
	meta := &dslmeta.Meta{API: "chat.completions", DSLModelMapped: "gemini-2.5-flash"}

	b.ReportAllocs()
	for b.Loop() {
		if err := routing.Apply(meta); err != nil {
			b.Fatalf("routing.Apply: %v", err)
		}
	}
}

// sink prevents dead-code elimination of marshal results.
var benchmarkSink []byte

// BenchmarkMarshalRoot_Large isolates the re-marshal cost that any changed
// json op incurs on a large body (the dominant term of request-side memory
// amplification observed in relay profiling).
func BenchmarkMarshalRoot_Large(b *testing.B) {
	root := benchmarkChatRoot(33000)

	b.ReportAllocs()
	for b.Loop() {
		out, err := json.Marshal(root)
		if err != nil {
			b.Fatalf("marshal: %v", err)
		}
		benchmarkSink = out
	}
}
