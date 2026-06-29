package dsllang_test

import (
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dsllang"
)

func TestSemanticTokensFull_EncodesData(t *testing.T) {
	text := "provider \"openai\" {\n  defaults {\n    request {\n      req_map openai_chat_to_openai_responses;\n    }\n  }\n}\n"
	res := dsllang.CollectSemanticTokens(text)
	if len(res.Data) == 0 {
		t.Fatalf("expected semantic token data")
	}
	if len(res.Data)%5 != 0 {
		t.Fatalf("semantic token data should be groups of 5, got len=%d", len(res.Data))
	}
}

func TestSemanticTokensFull_ClassifiesContextualModeValue(t *testing.T) {
	text := `provider "x" { defaults { models { models_mode openai; } } }`
	legend := dsllang.CollectSemanticTokenLegend()
	res := dsllang.CollectSemanticTokens(text)
	toks := decodeSemanticTokenTypes(res.Data, legend)

	openaiStart := len(`provider "x" { defaults { models { models_mode `)
	if got := toks[semanticTokenKey{line: 0, start: openaiStart}]; got != "enumMember" {
		t.Fatalf("models.models_mode value token type=%q want enumMember; tokens=%+v", got, toks)
	}
}

func TestSemanticTokensFull_ClassifiesSingleQuotedProviderName(t *testing.T) {
	text := "provider 'gemini' {\n  metrics { usage_fact input token path='$.usage.prompt_tokens'; }\n}\n"
	legend := dsllang.CollectSemanticTokenLegend()
	res := dsllang.CollectSemanticTokens(text)
	toks := decodeSemanticTokenTypes(res.Data, legend)

	geminiStart := len("provider ")
	if got := toks[semanticTokenKey{line: 0, start: geminiStart}]; got != "namespace" {
		t.Fatalf("provider name token type=%q want namespace; tokens=%+v", got, toks)
	}
}

type semanticTokenKey struct {
	line  int
	start int
}

func decodeSemanticTokenTypes(data []uint32, legend dsllang.SemanticTokenLegend) map[semanticTokenKey]string {
	out := map[semanticTokenKey]string{}
	line := 0
	start := 0
	for i := 0; i+4 < len(data); i += 5 {
		line += int(data[i])
		if data[i] == 0 {
			start += int(data[i+1])
		} else {
			start = int(data[i+1])
		}
		tokenType := int(data[i+3])
		typeName := ""
		if tokenType >= 0 && tokenType < len(legend.TokenTypes) {
			typeName = legend.TokenTypes[tokenType]
		}
		out[semanticTokenKey{line: line, start: start}] = typeName
	}
	return out
}
