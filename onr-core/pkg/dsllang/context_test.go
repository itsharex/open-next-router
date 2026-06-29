package dsllang_test

import (
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dsllang"
)

func TestCurrentBlockStack_Branches(t *testing.T) {
	text := "provider \"x\" {\n  match api = \"chat.completions\" stream = true {\n    response {\n      s\n    }\n  }\n}\n"
	stack := dsllang.CurrentBlockStack(text, dsllang.Position{Line: 3, Character: 6})
	if len(stack) < 3 {
		t.Fatalf("expected nested stack, got: %+v", stack)
	}
	if stack[1] != "match" {
		t.Fatalf("expected second stack element to stay match, got: %+v", stack)
	}

	unknown := dsllang.CurrentBlockStack("{\n", dsllang.Position{Line: 0, Character: 1})
	if len(unknown) != 1 || unknown[0] != "unknown" {
		t.Fatalf("expected unknown stack for bare '{', got: %+v", unknown)
	}
}

func TestBlockShapeFromDSLSpec(t *testing.T) {
	if !dsllang.BlockAllowsChildBlock("top", "models_mode") {
		t.Fatalf("expected top-level models_mode to be a block")
	}
	if !dsllang.BlockDirectiveNeedsHeader("top", "models_mode") {
		t.Fatalf("expected top-level models_mode block to consume a header")
	}
	if dsllang.BlockAllowsChildBlock("models", "models_mode") {
		t.Fatalf("did not expect models.models_mode to be a block")
	}
	if dsllang.BlockDirectiveNeedsHeader("models", "models_mode") {
		t.Fatalf("did not expect models.models_mode to consume a block header")
	}
	if !dsllang.BlockAllowsChildBlock("request", "after_req_map") {
		t.Fatalf("expected request.after_req_map to be a block")
	}
	if dsllang.BlockDirectiveNeedsHeader("request", "after_req_map") {
		t.Fatalf("did not expect request.after_req_map to consume a header")
	}
}
