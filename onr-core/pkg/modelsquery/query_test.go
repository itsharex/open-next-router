package modelsquery

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslconfig"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslmeta"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/httpclient/httpclienttest"
)

func TestQuery_CustomModelsConfig(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/models" {
			t.Fatalf("path=%q", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("auth header=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4.1"}]}`))
	}))
	defer srv.Close()

	pf := dslconfig.ProviderFile{
		Routing: dslconfig.ProviderRouting{
			BaseURLExpr: `"` + srv.URL + `"`,
		},
		Headers: dslconfig.ProviderHeaders{
			Defaults: dslconfig.PhaseHeaders{
				Auth: []dslconfig.HeaderOp{
					{
						Op:        "header_set",
						NameExpr:  `"Authorization"`,
						ValueExpr: `concat("Bearer ", $channel.key)`,
					},
				},
			},
		},
		Models: dslconfig.ProviderModels{
			Defaults: dslconfig.ModelsQueryConfig{
				Mode:    "custom",
				Method:  "GET",
				Path:    "/v1/models",
				IDPaths: []string{"$.data[*].id"},
			},
		},
	}

	result, err := Query(context.Background(), Params{
		Provider: "openai",
		File:     pf,
		Meta: &dslmeta.Meta{
			API: "chat.completions",
		},
		APIKey: "test-key",
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result == nil {
		t.Fatalf("Query returned nil result")
	}
	if len(result.IDs) != 2 || result.IDs[0] != "gpt-4o-mini" || result.IDs[1] != "gpt-4.1" {
		t.Fatalf("ids=%v", result.IDs)
	}
}

func TestQuery_UsesInjectedHTTPClient(t *testing.T) {
	fakeClient := httpclienttest.NewFakeDoer(t,
		httpclienttest.NewStringResponse(http.StatusOK, `{"data":[{"id":"fake-model"}]}`),
	)

	pf := dslconfig.ProviderFile{
		Headers: dslconfig.ProviderHeaders{
			Defaults: dslconfig.PhaseHeaders{
				Auth: []dslconfig.HeaderOp{
					{
						Op:        "header_set",
						NameExpr:  `"Authorization"`,
						ValueExpr: `concat("Bearer ", $channel.key)`,
					},
				},
			},
		},
		Models: dslconfig.ProviderModels{
			Defaults: dslconfig.ModelsQueryConfig{
				Mode:    "custom",
				Method:  "GET",
				Path:    "/v1/models",
				IDPaths: []string{"$.data[*].id"},
			},
		},
	}

	result, err := Query(context.Background(), Params{
		Provider:   "openai",
		File:       pf,
		Meta:       &dslmeta.Meta{API: "chat.completions"},
		APIKey:     "sk-test",
		BaseURL:    "https://example.test",
		HTTPClient: fakeClient,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if result == nil {
		t.Fatalf("Query returned nil result")
	}
	if len(result.IDs) != 1 || result.IDs[0] != "fake-model" {
		t.Fatalf("ids=%v", result.IDs)
	}
	reqs := fakeClient.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests=%d", len(reqs))
	}
	if reqs[0].URL.String() != "https://example.test/v1/models" {
		t.Fatalf("unexpected request url: %s", reqs[0].URL.String())
	}
	if reqs[0].Header.Get("Authorization") != "Bearer sk-test" {
		t.Fatalf("unexpected Authorization header: %s", reqs[0].Header.Get("Authorization"))
	}
}

func TestQuery_EvaluatesTemplateModelsPath(t *testing.T) {
	fakeClient := httpclienttest.NewFakeDoer(t,
		httpclienttest.NewStringResponse(http.StatusOK, `{"publisherModels":[{"name":"publishers/google/models/gemini-2.5-flash"}]}`),
	)

	pf := dslconfig.ProviderFile{
		Models: dslconfig.ProviderModels{
			Defaults: dslconfig.ModelsQueryConfig{
				Mode:    "custom",
				Method:  "GET",
				Path:    "/v1beta1/publishers/google/models",
				IDPaths: []string{"$.publisherModels[*].name"},
				IDRegex: `^publishers/google/models/(.+)$`,
				Headers: []dslconfig.HeaderOp{
					{
						Op:        "header_set",
						NameExpr:  `"x-goog-user-project"`,
						ValueExpr: `template("${credential.project_id}")`,
					},
				},
			},
		},
	}

	result, err := Query(context.Background(), Params{
		Provider: "vertex",
		File:     pf,
		Meta: &dslmeta.Meta{
			CredentialProjectID: "vertex-project",
			ChannelLocation:     "us-central1",
		},
		BaseURL:    "https://aiplatform.googleapis.com",
		HTTPClient: fakeClient,
	})
	if err != nil {
		t.Fatalf("Query: %v", err)
	}
	if len(result.IDs) != 1 || result.IDs[0] != "gemini-2.5-flash" {
		t.Fatalf("ids=%v", result.IDs)
	}
	reqs := fakeClient.Requests()
	if len(reqs) != 1 {
		t.Fatalf("requests=%d", len(reqs))
	}
	wantURL := "https://aiplatform.googleapis.com/v1beta1/publishers/google/models"
	if got := reqs[0].URL.String(); got != wantURL {
		t.Fatalf("request url=%q want=%q", got, wantURL)
	}
	if got := reqs[0].Header.Get("x-goog-user-project"); got != "vertex-project" {
		t.Fatalf("x-goog-user-project=%q", got)
	}
}
