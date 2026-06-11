package balancequery

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslconfig"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslmeta"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/httpclient/httpclienttest"
)

func TestQueryCustom(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/balance" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if _, err := w.Write([]byte(`{"data":{"total":12.5,"used":3.25}}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer srv.Close()

	result, err := Query(context.Background(), Params{
		Provider: "test",
		File: dslconfig.ProviderFile{
			Routing: dslconfig.ProviderRouting{BaseURLExpr: `"` + srv.URL + `"`},
			Balance: dslconfig.ProviderBalance{
				Defaults: dslconfig.BalanceQueryConfig{
					Mode:        "custom",
					Path:        "/balance",
					BalancePath: "$.data.total",
					UsedPath:    "$.data.used",
				},
			},
		},
		Meta:   &dslmeta.Meta{API: "chat.completions"},
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("query custom failed: %v", err)
	}
	if result.Balance != 12.5 {
		t.Fatalf("balance got %.2f, want %.2f", result.Balance, 12.5)
	}
	if result.Used == nil || *result.Used != 3.25 {
		t.Fatalf("used got %+v, want 3.25", result.Used)
	}
	if result.Unit != "USD" {
		t.Fatalf("unit got %q, want USD", result.Unit)
	}
}

func TestQueryDebugOutput(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, err := w.Write([]byte(`{"data":{"total":9.5}}`)); err != nil {
			t.Fatalf("write response: %v", err)
		}
	}))
	defer srv.Close()

	var debug bytes.Buffer
	_, err := Query(context.Background(), Params{
		Provider: "test",
		File: dslconfig.ProviderFile{
			Routing: dslconfig.ProviderRouting{BaseURLExpr: `"` + srv.URL + `"`},
			Balance: dslconfig.ProviderBalance{
				Defaults: dslconfig.BalanceQueryConfig{
					Mode:        "custom",
					Path:        "/balance",
					BalancePath: "$.data.total",
				},
			},
		},
		Meta:     &dslmeta.Meta{API: "chat.completions"},
		APIKey:   "sk-test",
		DebugOut: &debug,
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	out := debug.String()
	if !strings.Contains(out, "debug upstream_response") {
		t.Fatalf("debug output missing prefix: %q", out)
	}
	if !strings.Contains(out, `"data":{"total":9.5}`) {
		t.Fatalf("debug output missing body: %q", out)
	}
}

func TestQueryOpenAI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/dashboard/billing/subscription":
			_, _ = w.Write([]byte(`{"has_payment_method":true,"hard_limit_usd":100}`))
		case "/v1/dashboard/billing/usage":
			_, _ = w.Write([]byte(`{"total_usage":1234}`))
		default:
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	result, err := Query(context.Background(), Params{
		Provider: "openai",
		File: dslconfig.ProviderFile{
			Routing: dslconfig.ProviderRouting{BaseURLExpr: `"` + srv.URL + `"`},
			Balance: dslconfig.ProviderBalance{
				Defaults: dslconfig.BalanceQueryConfig{
					Mode: "openai",
					Unit: "USD",
				},
			},
		},
		Meta:   &dslmeta.Meta{API: "chat.completions"},
		APIKey: "sk-test",
	})
	if err != nil {
		t.Fatalf("query openai failed: %v", err)
	}
	if result.Balance != 87.66 {
		t.Fatalf("balance got %.2f, want %.2f", result.Balance, 87.66)
	}
	if result.Used == nil || *result.Used != 12.34 {
		t.Fatalf("used got %+v, want 12.34", result.Used)
	}
}

func TestQuery_UsesInjectedHTTPClient(t *testing.T) {
	fakeClient := httpclienttest.NewFakeDoer(t,
		httpclienttest.NewStringResponse(http.StatusOK, `{"data":{"total":42}}`),
	)

	result, err := Query(context.Background(), Params{
		Provider: "custom",
		File: dslconfig.ProviderFile{
			Balance: dslconfig.ProviderBalance{
				Defaults: dslconfig.BalanceQueryConfig{
					Mode:        "custom",
					Path:        "/v1/balance",
					BalancePath: "$.data.total",
				},
			},
		},
		Meta:       &dslmeta.Meta{API: "chat.completions"},
		APIKey:     "sk-test",
		BaseURL:    "https://billing.example",
		HTTPClient: fakeClient,
	})
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}
	if result.Balance != 42 {
		t.Fatalf("balance=%v", result.Balance)
	}
	reqs := fakeClient.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests=%d", len(reqs))
	}
	if reqs[0].URL.String() != "https://billing.example/v1/balance" {
		t.Fatalf("unexpected request url: %s", reqs[0].URL.String())
	}
	if reqs[0].Method != http.MethodGet {
		t.Fatalf("unexpected method: %s", reqs[0].Method)
	}
}
