package dslruntime

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRenderRoutePath_ReplacesRegistryPlaceholders(t *testing.T) {
	got := RenderRoutePath(
		"/v1beta/models/gemini-2.5-flash:generateContent",
		"/v1/projects/{credential.project_id}/locations/{channel.location}/publishers/google/models/{model}:generateContent",
		RoutePathContext{
			Model:               "gemini-2.5-flash",
			CredentialProjectID: "vertex-project",
			ChannelLocation:     "us-central1",
		},
	)
	want := "/v1/projects/vertex-project/locations/us-central1/publishers/google/models/gemini-2.5-flash:generateContent"
	if got != want {
		t.Fatalf("path=%q want=%q", got, want)
	}
}

func TestRenderRoutePath_EvaluatesTemplateExpression(t *testing.T) {
	got := RenderRoutePath(
		"/v1beta/models/gemini-2.5-flash:streamGenerateContent?alt=sse",
		`template("/v1/projects/${credential.project_id}/locations/${channel.location}/models/${request.model_mapped}:streamGenerateContent")`,
		RoutePathContext{
			Model:               "gemini-2.5-flash",
			ModelMapped:         "gemini-2.5-pro",
			CredentialProjectID: "vertex-project",
			ChannelLocation:     "global",
		},
	)
	want := "/v1/projects/vertex-project/locations/global/models/gemini-2.5-pro:streamGenerateContent?alt=sse"
	if got != want {
		t.Fatalf("path=%q want=%q", got, want)
	}
}

func TestBuildAuthHeaders_GoogleServiceAccountOAuthBearer(t *testing.T) {
	var tokenCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tokenCalls++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("grant_type=%q", got)
		}
		if strings.TrimSpace(r.Form.Get("assertion")) == "" {
			t.Fatalf("missing assertion")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"ya29.test","expires_in":3600,"token_type":"Bearer"}`))
	}))
	defer srv.Close()

	hdr, err := BuildAuthHeaders(t.Context(), AuthHeaderInput{
		Auth: AuthConfig{
			Type:  "oauth_bearer",
			Mode:  "google_service_account_file",
			Scope: "https://www.googleapis.com/auth/cloud-platform",
		},
		Key:                            testServiceAccountCredentialJSON(t, srv.URL),
		HTTPClient:                     srv.Client(),
		CacheKeyPrefix:                 "test",
		IncludeGoogleUserProjectHeader: true,
	})
	if err != nil {
		t.Fatalf("BuildAuthHeaders: %v", err)
	}
	if got := hdr.Get("Authorization"); got != "Bearer ya29.test" {
		t.Fatalf("Authorization=%q", got)
	}
	if got := hdr.Get("x-goog-user-project"); got != "vertex-project" {
		t.Fatalf("x-goog-user-project=%q", got)
	}
	if tokenCalls != 1 {
		t.Fatalf("tokenCalls=%d", tokenCalls)
	}
}

func TestBuildAuthHeaders_GoogleServiceAccountRequiresExplicitScope(t *testing.T) {
	_, err := BuildAuthHeaders(t.Context(), AuthHeaderInput{
		Auth: AuthConfig{
			Type: "oauth_bearer",
			Mode: "google_service_account_file",
		},
		Key: testServiceAccountCredentialJSON(t, "https://oauth2.googleapis.com/token"),
	})
	if err == nil || !strings.Contains(err.Error(), "oauth_scope is required") {
		t.Fatalf("err=%v, want explicit scope error", err)
	}
}

func TestGoogleServiceAccountProjectID(t *testing.T) {
	got, err := GoogleServiceAccountProjectID(testServiceAccountCredentialJSON(t, "https://oauth2.googleapis.com/token"))
	if err != nil {
		t.Fatalf("GoogleServiceAccountProjectID: %v", err)
	}
	if got != "vertex-project" {
		t.Fatalf("project_id=%q", got)
	}
}

func testServiceAccountCredentialJSON(t *testing.T, tokenURI string) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("GenerateKey: %v", err)
	}
	der, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatalf("MarshalPKCS8PrivateKey: %v", err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der})
	raw, err := json.Marshal(map[string]string{
		"type":           "service_account",
		"project_id":     "vertex-project",
		"private_key_id": "kid-1",
		"private_key":    string(pemBytes),
		"client_email":   "svc@vertex-project.iam.gserviceaccount.com",
		"token_uri":      tokenURI,
	})
	if err != nil {
		t.Fatalf("Marshal credential: %v", err)
	}
	return string(raw)
}
