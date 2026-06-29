package dsllang_test

import (
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dsllang"
)

func TestProviderNameFromURI(t *testing.T) {
	got := dsllang.ProviderNameFromURI("file:///tmp/openai.conf")
	if got != "openai" {
		t.Fatalf("expected openai, got %q", got)
	}
	got = dsllang.ProviderNameFromURI("/tmp/anthropic.conf")
	if got != "anthropic" {
		t.Fatalf("expected anthropic, got %q", got)
	}
}

func TestMaxHelper(t *testing.T) {
	if dsllang.Max(1, 2) != 2 {
		t.Fatalf("expected dsllang.Max(1,2)=2")
	}
	if dsllang.Max(5, 3) != 5 {
		t.Fatalf("expected dsllang.Max(5,3)=5")
	}
}
