package dslquery

import (
	"bytes"
	"context"
	"net/http"
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslconfig"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslmeta"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/httpclient/httpclienttest"
)

func TestBuildRequestURL(t *testing.T) {
	got, err := BuildRequestURL("https://example.test/base/", "", "v1/models", "models")
	if err != nil {
		t.Fatalf("BuildRequestURL: %v", err)
	}
	if got != "https://example.test/base/v1/models" {
		t.Fatalf("url=%q", got)
	}

	got, err = BuildRequestURL("https://example.test", "/default", "", "balance")
	if err != nil {
		t.Fatalf("BuildRequestURL default: %v", err)
	}
	if got != "https://example.test/default" {
		t.Fatalf("default url=%q", got)
	}

	got, err = BuildRequestURL("https://example.test", "", "https://upstream.test/models", "models")
	if err != nil {
		t.Fatalf("BuildRequestURL absolute: %v", err)
	}
	if got != "https://upstream.test/models" {
		t.Fatalf("absolute url=%q", got)
	}
}

func TestBuildRequestURL_Errors(t *testing.T) {
	if _, err := BuildRequestURL("https://example.test", "", "", "models"); err == nil || err.Error() != "models path is empty" {
		t.Fatalf("empty path error=%v", err)
	}
	if _, err := BuildRequestURL("", "", "/v1/models", "models"); err == nil || err.Error() != "models baseURL is empty" {
		t.Fatalf("empty baseURL error=%v", err)
	}
}

func TestResolveBaseURLFromExpr(t *testing.T) {
	if got := ResolveBaseURLFromExpr(`" https://example.test "`); got != "https://example.test" {
		t.Fatalf("quoted baseURL=%q", got)
	}
	if got := ResolveBaseURLFromExpr("https://example.test"); got != "https://example.test" {
		t.Fatalf("raw baseURL=%q", got)
	}
}

func TestApplyHeaderOps(t *testing.T) {
	h := http.Header{"X-Remove": {"1"}}
	ApplyHeaderOps(h, []dslconfig.HeaderOp{
		{Op: "header_set", NameExpr: `"Authorization"`, ValueExpr: `concat("Bearer ", $channel.key)`},
		{Op: "header_del", NameExpr: `"X-Remove"`},
	}, &dslmeta.Meta{APIKey: "sk-test"})

	if got := h.Get("Authorization"); got != "Bearer sk-test" {
		t.Fatalf("Authorization=%q", got)
	}
	if got := h.Get("X-Remove"); got != "" {
		t.Fatalf("X-Remove=%q", got)
	}
}

func TestGetResponseBody(t *testing.T) {
	client := httpclienttest.NewFakeDoer(t,
		httpclienttest.NewStringResponse(http.StatusOK, `{"ok":true}`),
	)
	headers := http.Header{"X-Test": {"1"}}
	var debug bytes.Buffer

	body, err := GetResponseBody(context.Background(), client, http.MethodGet, "https://example.test/models", headers, &debug)
	if err != nil {
		t.Fatalf("GetResponseBody: %v", err)
	}
	if string(body) != `{"ok":true}` {
		t.Fatalf("body=%s", body)
	}
	reqs := client.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests=%d", len(reqs))
	}
	if got := reqs[0].Header.Get("X-Test"); got != "1" {
		t.Fatalf("X-Test=%q", got)
	}
	if !bytes.Contains(debug.Bytes(), []byte("debug upstream_response")) {
		t.Fatalf("debug output=%q", debug.String())
	}
}

func TestGetResponseBody_NonOK(t *testing.T) {
	client := httpclienttest.NewFakeDoer(t,
		httpclienttest.NewStringResponse(http.StatusUnauthorized, `{"error":"bad key"}`),
	)

	_, err := GetResponseBody(context.Background(), client, http.MethodGet, "https://example.test/models", nil, nil)
	if err == nil {
		t.Fatalf("expected error")
	}
	want := `request GET https://example.test/models failed: status=401 body={"error":"bad key"}`
	if err.Error() != want {
		t.Fatalf("error=%q want=%q", err.Error(), want)
	}
}
