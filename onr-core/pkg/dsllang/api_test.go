package dsllang

import (
	"strings"
	"testing"
)

func TestCollectDiagnosticsAndSemanticTokens(t *testing.T) {
	text := `
syntax "next-router/0.1";

provider_bad "openai" {
}
`
	diags := CollectDiagnostics("file:///tmp/providers/openai.conf", text)
	if len(diags) == 0 {
		t.Fatalf("expected diagnostics for invalid directive")
	}

	legend := CollectSemanticTokenLegend()
	if len(legend.TokenTypes) == 0 {
		t.Fatalf("expected semantic token legend")
	}
	tokens := CollectSemanticTokens(strings.Replace(text, "provider_bad", "provider", 1))
	if len(tokens.Data) == 0 {
		t.Fatalf("expected semantic token data")
	}
}

func TestFormatText(t *testing.T) {
	got := FormatText(`provider "openai" { defaults { auth { auth_bearer; } } }`, FormatOptions{
		TabSize:      2,
		InsertSpaces: true,
	})
	if !strings.Contains(got, "\n  defaults {") {
		t.Fatalf("unexpected formatted text:\n%s", got)
	}
}
