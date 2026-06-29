package dsllang_test

import (
	"strings"
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dsllang"
)

func TestDiagnosticsUnknownDirective(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    request {\n      req_map openai_chat_to_anthropic_messages;\n      bad_cmd foo;\n    }\n  }\n}\n"
	diags := dsllang.AnalyzeSyntax(text)
	if len(diags) == 0 {
		t.Fatalf("expected diagnostics, got none")
	}
	ok := false
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown directive") {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("expected unknown directive diagnostic, got: %+v", diags)
	}
}

func TestDiagnosticsWrongPhaseDirective(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    response {\n      req_map openai_chat_to_anthropic_messages;\n    }\n  }\n}\n"
	diags := dsllang.AnalyzeSyntax(text)
	if len(diags) == 0 {
		t.Fatalf("expected diagnostics, got none")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "not allowed in response block") && strings.Contains(d.Message, "quick fix: move it into request") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected wrong-phase directive diagnostic, got: %+v", diags)
	}
}

func TestDiagnosticsMissingBrace(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    request {\n      req_map openai_chat_to_anthropic_messages;\n  }\n}\n"
	diags := dsllang.AnalyzeSyntax(text)
	if len(diags) == 0 {
		t.Fatalf("expected diagnostics, got none")
	}
	ok := false
	for _, d := range diags {
		if strings.Contains(d.Message, "missing closing '}'") {
			ok = true
			break
		}
	}
	if !ok {
		t.Fatalf("expected missing brace diagnostic, got: %+v", diags)
	}
}

func TestDiagnosticsTopLevelSyntaxDirective(t *testing.T) {
	text := "syntax \"next-router/0.1\";\nprovider \"x\" {\n  defaults {\n    upstream_config {\n      base_url = \"https://example.com\";\n    }\n  }\n}\n"
	diags := dsllang.AnalyzeSyntax(text)
	for _, d := range diags {
		if strings.Contains(d.Message, "unknown top-level directive: syntax") {
			t.Fatalf("syntax directive should be accepted, got diagnostics: %+v", diags)
		}
	}
}

func TestSemanticDiagnosticsUnsupportedReqMapMode(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    upstream_config {\n      base_url = \"https://example.com\";\n    }\n    request {\n      req_map not_a_real_mapper;\n    }\n  }\n}\n"
	diags := dsllang.CollectDiagnostics("file:///tmp/x.conf", text)
	if len(diags) == 0 {
		t.Fatalf("expected diagnostics, got none")
	}
	found := false
	for _, d := range diags {
		if strings.Contains(d.Message, "unsupported req_map mode") {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected semantic diagnostic for unsupported req_map mode, got: %+v", diags)
	}
}

func TestSemanticDiagnosticsIgnoreDynamicUsageExtractMode(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    upstream_config {\n      base_url = \"https://example.com\";\n    }\n    metrics {\n      usage_extract local_usage_mode;\n    }\n  }\n}\n"
	diags := dsllang.CollectDiagnostics("file:///tmp/x.conf", text)
	for _, d := range diags {
		if strings.Contains(d.Message, "unsupported usage_extract mode") {
			t.Fatalf("did not expect usage_extract mode diagnostic, got: %+v", diags)
		}
	}
}

func TestSemanticDiagnosticsIgnoreDynamicFinishReasonMode(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    upstream_config {\n      base_url = \"https://example.com\";\n    }\n    metrics {\n      finish_reason_extract local_finish_reason_mode;\n    }\n  }\n}\n"
	diags := dsllang.CollectDiagnostics("file:///tmp/x.conf", text)
	for _, d := range diags {
		if strings.Contains(d.Message, "unsupported finish_reason_extract mode") {
			t.Fatalf("did not expect finish_reason_extract mode diagnostic, got: %+v", diags)
		}
	}
}

func TestSemanticDiagnosticsMultipleModeErrors(t *testing.T) {
	text := "provider \"x\" {\n  defaults {\n    request { req_map bad_req_mode; }\n    response { resp_map bad_resp_mode; }\n    upstream_config { base_url = \"https://example.com\"; }\n  }\n}\n"
	diags := dsllang.CollectDiagnostics("file:///tmp/x.conf", text)
	if len(diags) < 2 {
		t.Fatalf("expected at least 2 diagnostics, got: %+v", diags)
	}
	reqErr := false
	respErr := false
	for _, d := range diags {
		if strings.Contains(d.Message, "unsupported req_map mode") {
			reqErr = true
		}
		if strings.Contains(d.Message, "unsupported resp_map mode") {
			respErr = true
		}
	}
	if !reqErr || !respErr {
		t.Fatalf("expected both req_map and resp_map semantic diagnostics, got: %+v", diags)
	}
}
