package requesttransform

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslconfig"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslmeta"
)

// Full request-transform pipeline benchmarks. Apply is on every relay request
// path (config-file API type); req_map modes additionally cover the protocol
// conversion pipelines (openai chat <-> anthropic/gemini). Light (~200B) and
// large (~33KB) request bodies bracket the payload range seen in relay perf
// baselines.

func benchmarkChatBody(extraBytes int) ([]byte, map[string]any) {
	content := "Summarize the benefits of using Next Router."
	if extraBytes > 0 {
		para := "China is one of the most geographically diverse countries. "
		content = strings.Repeat(para, extraBytes/len(para)+1)[:extraBytes]
	}
	root := map[string]any{
		"model":  "gpt-4o-mini",
		"stream": false,
		"messages": []any{
			map[string]any{"role": "system", "content": "You are a helpful assistant."},
			map[string]any{"role": "user", "content": content},
		},
		"max_tokens": 500,
	}
	body, err := json.Marshal(root)
	if err != nil {
		panic(err)
	}
	return body, root
}

func parseRoot(body []byte) map[string]any {
	var root map[string]any
	if err := json.Unmarshal(body, &root); err != nil {
		panic(err)
	}
	return root
}

// benchmarkApply runs Apply with a fresh parsed root each iteration
// (Apply mutates the root in place); parsing cost is excluded via timer control.
func benchmarkApply(b *testing.B, t *dslconfig.RequestTransform, extraBytes int, mappedModel string) {
	b.Helper()
	body, _ := benchmarkChatBody(extraBytes)

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		root := parseRoot(body)
		meta := &dslmeta.Meta{API: "chat.completions", DSLModelMapped: mappedModel}
		b.StartTimer()
		if _, err := Apply(meta, "application/json", body, root, t, ApplyOptions{}); err != nil {
			b.Fatalf("Apply: %v", err)
		}
	}
}

// Passthrough: no ops, no model change — the cheapest path (body reused as-is).
func BenchmarkApply_Passthrough_Light(b *testing.B) {
	benchmarkApply(b, &dslconfig.RequestTransform{}, 0, "")
}

func BenchmarkApply_Passthrough_Large(b *testing.B) {
	benchmarkApply(b, &dslconfig.RequestTransform{}, 33000, "")
}

// ModelRemap: only the model field changes — triggers a full re-marshal of the
// body (the dominant request-side memory amplification term in relay profiling).
func BenchmarkApply_ModelRemap_Light(b *testing.B) {
	benchmarkApply(b, &dslconfig.RequestTransform{}, 0, "gpt-4o-mini-mapped")
}

func BenchmarkApply_ModelRemap_Large(b *testing.B) {
	benchmarkApply(b, &dslconfig.RequestTransform{}, 33000, "gpt-4o-mini-mapped")
}

// JSONOps: model remap + a realistic op mix (set stream option, delete two
// unsupported fields) — the common non-req_map provider patch path.
func benchmarkJSONOpsTransform() *dslconfig.RequestTransform {
	return &dslconfig.RequestTransform{
		JSONOps: []dslconfig.JSONOp{
			{Op: "json_set", Path: "$.stream_options.include_usage", ValueExpr: "true"},
			{Op: "json_del", Path: "$.inference_geo"},
			{Op: "json_del", Path: "$.speed"},
		},
	}
}

func BenchmarkApply_JSONOps_Light(b *testing.B) {
	benchmarkApply(b, benchmarkJSONOpsTransform(), 0, "gpt-4o-mini-mapped")
}

func BenchmarkApply_JSONOps_Large(b *testing.B) {
	benchmarkApply(b, benchmarkJSONOpsTransform(), 33000, "gpt-4o-mini-mapped")
}

// ReqMap: protocol conversion pipelines (typed parse -> map -> re-marshal).
func BenchmarkApply_ReqMapOpenAIToAnthropic_Light(b *testing.B) {
	benchmarkApply(b, &dslconfig.RequestTransform{ReqMapMode: "openai_chat_to_anthropic_messages"}, 0, "")
}

func BenchmarkApply_ReqMapOpenAIToAnthropic_Large(b *testing.B) {
	benchmarkApply(b, &dslconfig.RequestTransform{ReqMapMode: "openai_chat_to_anthropic_messages"}, 33000, "")
}

func BenchmarkApply_ReqMapOpenAIToGemini_Light(b *testing.B) {
	benchmarkApply(b, &dslconfig.RequestTransform{ReqMapMode: "openai_chat_to_gemini_generate_content"}, 0, "")
}

func BenchmarkApply_ReqMapOpenAIToGemini_Large(b *testing.B) {
	benchmarkApply(b, &dslconfig.RequestTransform{ReqMapMode: "openai_chat_to_gemini_generate_content"}, 33000, "")
}
