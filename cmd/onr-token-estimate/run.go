package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslconfig"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/streamtext"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/usageestimate"
)

type options struct {
	file           string
	api            string
	route          string
	model          string
	allowTruncated bool
	debugID        string
	debugPreview   int
}

type dumpEntry struct {
	ID       any      `json:"id"`
	Request  dumpSide `json:"request"`
	Response dumpSide `json:"response"`
}

type dumpSide struct {
	Body dumpBody `json:"body"`
}

type dumpBody struct {
	Format    string          `json:"format"`
	Size      int             `json:"size"`
	Truncated bool            `json:"truncated"`
	Content   json.RawMessage `json:"content"`
	Events    []dumpSSEEvent  `json:"events"`
}

type dumpSSEEvent struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type estimateRow struct {
	Status       string
	Index        string
	ID           string
	Stage        string
	InputActual  string
	InputEst     string
	InputDelta   string
	OutputActual string
	OutputEst    string
	OutputDelta  string
	Reason       string
}

func run(args []string, stdout, stderr io.Writer) int {
	opts, err := parseOptions(args, stderr)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "error: "+err.Error())
		return 2
	}
	api, err := resolveAPI(opts.api, opts.route)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "error: "+err.Error())
		return 2
	}

	entries, err := readDumpEntries(opts.file)
	if err != nil {
		_, _ = fmt.Fprintln(stderr, "error: "+err.Error())
		return 1
	}

	cfg := &usageestimate.Config{}
	usageestimate.ApplyDefaults(cfg)
	processed := 0
	skipped := 0
	rows := make([]estimateRow, 0, len(entries))
	for i, entry := range entries {
		if shouldDebugEntry(opts.debugID, entry.ID) {
			printDebugEntry(stdout, api, opts.debugPreview, i, entry)
		}
		in, err := buildEstimateInput(entry, api, opts.model, opts.allowTruncated)
		if err != nil {
			rows = append(rows, skippedRow(i, entry.ID, err.Error()))
			skipped++
			continue
		}
		actual := in.UpstreamUsage
		if !hasCompleteUsage(actual) {
			rows = append(rows, skippedRow(i, entry.ID, "incomplete upstream usage"))
			skipped++
			continue
		}

		estimateIn := in
		estimateIn.UpstreamUsage = nil
		estimated := usageestimate.Estimate(cfg, estimateIn)
		if estimated.Usage == nil {
			rows = append(rows, skippedRow(i, entry.ID, "estimate unavailable"))
			skipped++
			continue
		}

		processed++
		rows = append(rows, estimatedRow(i, entry.ID, estimated.Stage, actual, estimated.Usage))
	}
	printRows(stdout, rows)
	_, _ = fmt.Fprintf(stdout, "summary entries=%d estimated=%d skipped=%d\n", len(entries), processed, skipped)
	return 0
}

func skippedRow(index int, id any, reason string) estimateRow {
	return estimateRow{
		Status: "skipped",
		Index:  fmt.Sprintf("%d", index),
		ID:     fmt.Sprint(id),
		Reason: reason,
	}
}

func estimatedRow(index int, id any, stage string, actual, estimated *dslconfig.Usage) estimateRow {
	return estimateRow{
		Status:       "estimated",
		Index:        fmt.Sprintf("%d", index),
		ID:           fmt.Sprint(id),
		Stage:        stage,
		InputActual:  fmt.Sprintf("%d", actual.InputTokens),
		InputEst:     fmt.Sprintf("%d", estimated.InputTokens),
		InputDelta:   fmt.Sprintf("%+.2f%%", percentDelta(estimated.InputTokens, actual.InputTokens)),
		OutputActual: fmt.Sprintf("%d", actual.OutputTokens),
		OutputEst:    fmt.Sprintf("%d", estimated.OutputTokens),
		OutputDelta:  fmt.Sprintf("%+.2f%%", percentDelta(estimated.OutputTokens, actual.OutputTokens)),
	}
}

func printRows(out io.Writer, rows []estimateRow) {
	headers := []string{"status", "idx", "id", "stage", "in.actual", "in.est", "in.delta", "out.actual", "out.est", "out.delta", "reason"}
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		values := rowValues(row)
		for i, value := range values {
			if len(value) > widths[i] {
				widths[i] = len(value)
			}
		}
	}

	printTableLine(out, headers, widths)
	separators := make([]string, len(headers))
	for i, w := range widths {
		separators[i] = strings.Repeat("-", w)
	}
	printTableLine(out, separators, widths)
	for _, row := range rows {
		printTableLine(out, rowValues(row), widths)
	}
}

func rowValues(row estimateRow) []string {
	return []string{
		row.Status,
		row.Index,
		row.ID,
		row.Stage,
		row.InputActual,
		row.InputEst,
		row.InputDelta,
		row.OutputActual,
		row.OutputEst,
		row.OutputDelta,
		row.Reason,
	}
}

func printTableLine(out io.Writer, values []string, widths []int) {
	for i, value := range values {
		if i > 0 {
			_, _ = fmt.Fprint(out, "  ")
		}
		if isNumericColumn(i) {
			_, _ = fmt.Fprintf(out, "%*s", widths[i], value)
			continue
		}
		_, _ = fmt.Fprintf(out, "%-*s", widths[i], value)
	}
	_, _ = fmt.Fprintln(out)
}

func isNumericColumn(index int) bool {
	switch index {
	case 1, 4, 5, 6, 7, 8, 9:
		return true
	default:
		return false
	}
}

func parseOptions(args []string, stderr io.Writer) (options, error) {
	opts := options{}
	fs := flag.NewFlagSet("onr-token-estimate", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.StringVar(&opts.file, "file", "", "path to dump json file")
	fs.StringVar(&opts.file, "f", "", "path to dump json file (alias of --file)")
	fs.StringVar(&opts.api, "api", "", "usage estimate API name")
	fs.StringVar(&opts.route, "route", "", "route alias for API name")
	fs.StringVar(&opts.model, "model", "", "model name")
	fs.StringVar(&opts.model, "m", "", "model name (alias of --model)")
	fs.BoolVar(&opts.allowTruncated, "allow-truncated", false, "allow truncated dump bodies")
	fs.StringVar(&opts.debugID, "debug-id", "", "dump id to print extracted output text for")
	fs.IntVar(&opts.debugPreview, "debug-preview", 800, "max characters to print for --debug-id")
	if err := fs.Parse(args); err != nil {
		return options{}, err
	}
	if fs.NArg() > 0 {
		return options{}, errors.New("unexpected positional arguments")
	}
	if strings.TrimSpace(opts.file) == "" {
		return options{}, errors.New("missing --file")
	}
	if strings.TrimSpace(opts.model) == "" {
		return options{}, errors.New("missing --model")
	}
	return opts, nil
}

func shouldDebugEntry(debugID string, id any) bool {
	debugID = strings.TrimSpace(debugID)
	if debugID == "" {
		return false
	}
	return fmt.Sprint(id) == debugID
}

func printDebugEntry(out io.Writer, api string, preview int, index int, entry dumpEntry) {
	text, err := extractResponseDebugText(api, entry.Response.Body)
	if err != nil {
		_, _ = fmt.Fprintf(out, "debug dump id=%v index=%d error=%q\n", entry.ID, index, err.Error())
		return
	}
	_, _ = fmt.Fprintf(out, "debug dump id=%v index=%d extracted_output_chars=%d\n", entry.ID, index, len([]rune(text)))
	previewText := truncateRunes(text, preview)
	if previewText == "" {
		_, _ = fmt.Fprintln(out, "debug output preview: <empty>")
		return
	}
	_, _ = fmt.Fprintf(out, "debug output preview:\n%s\n", previewText)
}

func extractResponseDebugText(api string, body dumpBody) (string, error) {
	switch strings.ToLower(strings.TrimSpace(body.Format)) {
	case "sse":
		return extractSSEDeltaText(api, body.Events)
	case "json":
		if len(bytes.TrimSpace(body.Content)) == 0 {
			return "", nil
		}
		return string(body.Content), nil
	case "", "empty":
		return "", nil
	default:
		return "", fmt.Errorf("unsupported response format %q", body.Format)
	}
}

func truncateRunes(s string, limit int) string {
	if limit <= 0 {
		return s
	}
	runes := []rune(s)
	if len(runes) <= limit {
		return s
	}
	return string(runes[:limit]) + "\n...<truncated>"
}

func resolveAPI(apiFlag, routeFlag string) (string, error) {
	api := strings.TrimSpace(apiFlag)
	route := strings.ToLower(strings.TrimSpace(routeFlag))
	if api != "" && route == "" {
		return api, nil
	}
	if route == "" {
		return "", errors.New("missing --api or --route")
	}

	mapped, ok := map[string]string{
		"chat.completions":               "chat.completions",
		"openai-chat":                    "chat.completions",
		"openai-chat-completions":        "chat.completions",
		"responses":                      "responses",
		"openai-responses":               "responses",
		"claude.messages":                "claude.messages",
		"anthropic-messages":             "claude.messages",
		"claude-messages":                "claude.messages",
		"embeddings":                     "embeddings",
		"gemini.generatecontent":         "gemini.generateContent",
		"gemini-generate-content":        "gemini.generateContent",
		"gemini.streamgeneratecontent":   "gemini.streamGenerateContent",
		"gemini-stream-generate-content": "gemini.streamGenerateContent",
	}[route]
	if !ok {
		return "", fmt.Errorf("unknown route %q", routeFlag)
	}
	if api != "" && strings.ToLower(api) != strings.ToLower(mapped) {
		return "", fmt.Errorf("--api %q conflicts with --route %q (%s)", api, routeFlag, mapped)
	}
	return mapped, nil
}

func readDumpEntries(path string) ([]dumpEntry, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, errors.New("dump file path is empty")
	}
	// #nosec G304 -- CLI reads a user-provided local dump path.
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read dump file: %w", err)
	}
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return nil, errors.New("dump file is empty")
	}

	if b[0] == '[' {
		var entries []dumpEntry
		if err := json.Unmarshal(b, &entries); err != nil {
			entries, fallbackErr := readLooseJSONArrayEntries(b)
			if fallbackErr != nil {
				return nil, fmt.Errorf("parse dump json array: %w", err)
			}
			return entries, nil
		}
		if len(entries) == 0 {
			return nil, errors.New("dump file has no entries")
		}
		return entries, nil
	}

	dec := json.NewDecoder(bytes.NewReader(b))
	var entries []dumpEntry
	for {
		var entry dumpEntry
		if err := dec.Decode(&entry); err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return nil, fmt.Errorf("parse dump json stream: %w", err)
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, errors.New("dump file has no entries")
	}
	return entries, nil
}

func readLooseJSONArrayEntries(b []byte) ([]dumpEntry, error) {
	dec := json.NewDecoder(bytes.NewReader(b))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '[' {
		return nil, errors.New("not a json array")
	}

	var entries []dumpEntry
	for dec.More() {
		var entry dumpEntry
		if err := dec.Decode(&entry); err != nil {
			return nil, err
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, errors.New("dump file has no entries")
	}
	return entries, nil
}

func buildEstimateInput(entry dumpEntry, api, model string, allowTruncated bool) (usageestimate.Input, error) {
	req, _, err := extractDumpBody("request", entry.Request.Body, api, allowTruncated)
	if err != nil {
		return usageestimate.Input{}, err
	}
	resp, stream, err := extractDumpBody("response", entry.Response.Body, api, allowTruncated)
	if err != nil {
		return usageestimate.Input{}, err
	}
	return usageestimate.Input{
		API:           strings.TrimSpace(api),
		Model:         strings.TrimSpace(model),
		UpstreamUsage: extractUpstreamUsage(entry),
		RequestBody:   req,
		ResponseBody:  resp,
		StreamTail:    stream,
	}, nil
}

func extractDumpBody(label string, body dumpBody, api string, allowTruncated bool) (jsonBody []byte, sseBody []byte, err error) {
	if body.Truncated && !allowTruncated {
		return nil, nil, fmt.Errorf("%s body is truncated", label)
	}
	switch strings.ToLower(strings.TrimSpace(body.Format)) {
	case "", "empty":
		return nil, nil, nil
	case "json":
		if len(bytes.TrimSpace(body.Content)) == 0 {
			return nil, nil, fmt.Errorf("%s json body content is empty", label)
		}
		if !json.Valid(body.Content) {
			return nil, nil, fmt.Errorf("%s json body content is invalid", label)
		}
		return append([]byte(nil), body.Content...), nil, nil
	case "sse":
		text, err := extractSSEDeltaText(api, body.Events)
		if err != nil {
			return nil, nil, fmt.Errorf("%s sse body: %w", label, err)
		}
		if strings.TrimSpace(text) == "" {
			return nil, nil, nil
		}
		b, err := buildResponseBodyFromText(api, text)
		if err != nil {
			return nil, nil, fmt.Errorf("%s sse body: %w", label, err)
		}
		return b, nil, nil
	default:
		return nil, nil, fmt.Errorf("%s body has unsupported format %q", label, body.Format)
	}
}

func extractSSEDeltaText(api string, events []dumpSSEEvent) (string, error) {
	var out strings.Builder
	for i, ev := range events {
		if len(bytes.TrimSpace(ev.Data)) == 0 {
			return "", fmt.Errorf("event %d has empty data", i)
		}
		if text := streamtext.ExtractDeltaText(api, ev.Data); text != "" {
			out.WriteString(text)
		}
	}
	return out.String(), nil
}

func buildResponseBodyFromText(api, text string) ([]byte, error) {
	switch strings.ToLower(strings.TrimSpace(api)) {
	case "chat.completions":
		return json.Marshal(map[string]any{
			"choices": []any{
				map[string]any{
					"message": map[string]any{"content": text},
				},
			},
		})
	case "claude.messages":
		return json.Marshal(map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": text},
			},
		})
	case "gemini.generatecontent", "gemini.streamgeneratecontent":
		return json.Marshal(map[string]any{
			"candidates": []any{
				map[string]any{
					"content": map[string]any{
						"parts": []any{
							map[string]any{"text": text},
						},
					},
				},
			},
		})
	default:
		return json.Marshal(map[string]any{
			"output": []any{
				map[string]any{
					"type": "message",
					"content": []any{
						map[string]any{"type": "output_text", "text": text},
					},
				},
			},
		})
	}
}

func extractUpstreamUsage(entry dumpEntry) *dslconfig.Usage {
	usage := &dslconfig.Usage{}
	mergeUsageFromBody(usage, entry.Response.Body)
	normalizeUsage(usage)
	if usage.InputTokens == 0 && usage.OutputTokens == 0 && usage.PromptTokens == 0 &&
		usage.CompletionTokens == 0 && usage.TotalTokens == 0 {
		return nil
	}
	return usage
}

func mergeUsageFromBody(out *dslconfig.Usage, body dumpBody) {
	switch strings.ToLower(strings.TrimSpace(body.Format)) {
	case "json":
		var obj any
		if json.Unmarshal(body.Content, &obj) == nil {
			mergeUsageFromValue(out, obj)
		}
	case "sse":
		for _, ev := range body.Events {
			var obj any
			if json.Unmarshal(ev.Data, &obj) == nil {
				mergeUsageFromValue(out, obj)
			}
		}
	}
}

func mergeUsageFromValue(out *dslconfig.Usage, v any) {
	switch t := v.(type) {
	case map[string]any:
		if hasUsageTokenFields(t) {
			mergeUsageMap(out, t)
		}
		for k, vv := range t {
			if strings.EqualFold(k, "usage") {
				if m, ok := vv.(map[string]any); ok {
					mergeUsageMap(out, m)
				}
			}
			mergeUsageFromValue(out, vv)
		}
	case []any:
		for _, it := range t {
			mergeUsageFromValue(out, it)
		}
	}
}

func hasUsageTokenFields(m map[string]any) bool {
	for _, key := range []string{"input_tokens", "output_tokens", "prompt_tokens", "completion_tokens", "total_tokens"} {
		if _, ok := m[key]; ok {
			return true
		}
	}
	return false
}

func mergeUsageMap(out *dslconfig.Usage, m map[string]any) {
	setMax(&out.InputTokens, intField(m, "input_tokens"))
	setMax(&out.OutputTokens, intField(m, "output_tokens"))
	setMax(&out.PromptTokens, intField(m, "prompt_tokens"))
	setMax(&out.CompletionTokens, intField(m, "completion_tokens"))
	setMax(&out.TotalTokens, intField(m, "total_tokens"))
}

func intField(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch t := v.(type) {
	case float64:
		if t > 0 {
			return int(t)
		}
	case int:
		if t > 0 {
			return t
		}
	case json.Number:
		n, err := t.Int64()
		if err == nil && n > 0 {
			return int(n)
		}
	}
	return 0
}

func setMax(dst *int, v int) {
	if v > *dst {
		*dst = v
	}
}

func normalizeUsage(u *dslconfig.Usage) {
	if u.InputTokens == 0 && u.PromptTokens > 0 {
		u.InputTokens = u.PromptTokens
	}
	if u.OutputTokens == 0 && u.CompletionTokens > 0 {
		u.OutputTokens = u.CompletionTokens
	}
	if u.PromptTokens == 0 && u.InputTokens > 0 {
		u.PromptTokens = u.InputTokens
	}
	if u.CompletionTokens == 0 && u.OutputTokens > 0 {
		u.CompletionTokens = u.OutputTokens
	}
	if u.TotalTokens == 0 && (u.InputTokens > 0 || u.OutputTokens > 0) {
		u.TotalTokens = u.InputTokens + u.OutputTokens
	}
}

func hasCompleteUsage(u *dslconfig.Usage) bool {
	return u != nil && u.InputTokens > 0 && u.OutputTokens > 0 && u.TotalTokens > 0
}

func percentDelta(estimated, actual int) float64 {
	if actual == 0 {
		return 0
	}
	return float64(estimated-actual) * 100 / float64(actual)
}
