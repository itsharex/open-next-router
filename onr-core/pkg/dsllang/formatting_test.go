package dsllang_test

import (
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dsllang"
)

func TestFormatDocument_IndentWithSpaces(t *testing.T) {
	in := "provider \"x\" {\ndefaults {\nrequest {\nreq_map openai_chat_to_openai_responses;\n}\n}\n}\n"
	want := "provider \"x\" {\n  defaults {\n    request {\n      req_map openai_chat_to_openai_responses;\n    }\n  }\n}\n"
	got := dsllang.FormatText(in, dsllang.FormatOptions{TabSize: 2, InsertSpaces: true})
	if got != want {
		t.Fatalf("unexpected formatting output\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestFormatDocument_IgnoresBracesInStringAndComments(t *testing.T) {
	in := "provider \"x\" {\ndefaults {\nrequest {\nset_header \"x\" \"{text}\"; # } in comment\n}\n}\n}\n"
	want := "provider \"x\" {\n  defaults {\n    request {\n      set_header \"x\" \"{text}\"; # } in comment\n    }\n  }\n}\n"
	got := dsllang.FormatText(in, dsllang.FormatOptions{TabSize: 2, InsertSpaces: true})
	if got != want {
		t.Fatalf("unexpected formatting output\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestFormatDocument_ExpandsCompactBlockStatements(t *testing.T) {
	in := "provider \"x\" {\n  defaults {\n    response {resp_passthrough;}\n  }\n}\n"
	want := "provider \"x\" {\n  defaults {\n    response {\n      resp_passthrough;\n    }\n  }\n}\n"
	got := dsllang.FormatText(in, dsllang.FormatOptions{TabSize: 2, InsertSpaces: true})
	if got != want {
		t.Fatalf("unexpected compact block expansion\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestFormatDocument_ExpandsCompactMatch(t *testing.T) {
	in := "provider \"x\" {\nmatch api = \"chat.completions\" {response {resp_passthrough;}}\n}\n"
	want := "provider \"x\" {\n  match api = \"chat.completions\" {\n    response {\n      resp_passthrough;\n    }\n  }\n}\n"
	got := dsllang.FormatText(in, dsllang.FormatOptions{TabSize: 2, InsertSpaces: true})
	if got != want {
		t.Fatalf("unexpected compact match expansion\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestFormatDocument_UsesTabsWhenInsertSpacesFalse(t *testing.T) {
	in := "provider \"x\" {\ndefaults {\nrequest {\nreq_map openai_chat_to_openai_responses;\n}\n}\n}\n"
	want := "provider \"x\" {\n\tdefaults {\n\t\trequest {\n\t\t\treq_map openai_chat_to_openai_responses;\n\t\t}\n\t}\n}\n"
	got := dsllang.FormatText(in, dsllang.FormatOptions{InsertSpaces: false})
	if got != want {
		t.Fatalf("unexpected tab formatting output\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestEndPosition(t *testing.T) {
	p := dsllang.EndPosition("a\nbc\n")
	if p.Line != 2 || p.Character != 0 {
		t.Fatalf("unexpected end position: %+v", p)
	}
}
