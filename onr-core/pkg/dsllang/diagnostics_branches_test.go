package dsllang_test

import (
	"github.com/r9s-ai/open-next-router/onr-core/pkg/dsllang"
	"strings"
	"testing"
)

func TestDiagnostics_MatchMissingLBrace(t *testing.T) {
	text := "provider \"x\" {\n  match api = \"chat.completions\"\n"
	diags := dsllang.AnalyzeSyntax(text)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "expected '{' for match block") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected missing match block brace diagnostic, got: %+v", diags)
	}
}

func TestDiagnostics_UnknownDirectiveWithBlock(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    weird {\n      a;\n    }\n  }\n}\n"
	diags := dsllang.AnalyzeSyntax(text)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown directive in defaults block: weird") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected unknown directive diagnostic, got: %+v", diags)
	}
}

func TestDiagnostics_MissingSemicolonBeforeRBrace(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    balance {\n      balance_unit USD\n    }\n  }\n}\n"
	diags := dsllang.AnalyzeSyntax(text)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "expected ';' after balance_unit") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected missing semicolon diagnostic, got: %+v", diags)
	}
}

func TestDiagnostics_MissingSemicolonAtEOF(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    request {\n      req_map openai_chat_to_openai_responses\n"
	diags := dsllang.AnalyzeSyntax(text)
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "expected ';' after req_map") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected missing semicolon at EOF diagnostic, got: %+v", diags)
	}
}

func TestDiagnostics_BalanceExpressionDirective_NoFalseUnknown(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    balance {\n      balance_mode custom;\n      path \"/v1/credits\";\n      balance_expr = $.data.total_credits - $.data.total_usage;\n      used_path \"$.data.total_usage\";\n      balance_unit USD;\n    }\n  }\n}\n"
	diags := dsllang.AnalyzeSyntax(text)
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown directive in balance block: data.total_credits") {
			t.Fatalf("unexpected false unknown-directive diagnostic: %+v", diags)
		}
		if strings.Contains(d.Message, "expected '{' after balance") {
			t.Fatalf("unexpected false block diagnostic for balance expression: %+v", diags)
		}
	}
}

func TestDiagnostics_MetadataBlock_NoFalseBlockDiagnostic(t *testing.T) {
	text := "syntax \"next-router/0.1\";\n\nprovider \"azure\" {\n  metadata {\n    provider_family azure-openai;\n    signal_profile azure-openai;\n  }\n}\n"
	diags := dsllang.AnalyzeSyntax(text)
	for _, d := range diags {
		if strings.Contains(d.Message, "metadata does not use") ||
			strings.Contains(d.Message, "unknown directive in provider block: metadata") ||
			strings.Contains(d.Message, "unknown directive in metadata block") {
			t.Fatalf("unexpected metadata diagnostic: %+v", diags)
		}
	}
}
