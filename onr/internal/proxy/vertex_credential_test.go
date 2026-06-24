package proxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildProxyCtxVertexCredentialFileMetadata(t *testing.T) {
	credentialFile := writeVertexCredentialFile(t, `{
  "type": "service_account",
  "project_id": "vertex-proj",
  "client_email": "svc@vertex-proj.iam.gserviceaccount.com",
  "private_key": "redacted"
}`)
	c := newMockE2EClient(t, map[string]string{
		"vertex.conf": providerConfVertexCredentialMetadata(),
	})
	gc, _ := newGinJSONRequest(t, []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`))

	ctx, err := c.buildProxyCtx(gc, "vertex", ProviderKey{
		Name:           "vertex-sa",
		CredentialFile: credentialFile,
		Location:       "us-central1",
	}, "chat.completions", false)
	if err != nil {
		t.Fatalf("buildProxyCtx: %v", err)
	}
	if ctx.meta.CredentialFile != credentialFile {
		t.Fatalf("CredentialFile=%q", ctx.meta.CredentialFile)
	}
	if ctx.meta.CredentialProjectID != "vertex-proj" {
		t.Fatalf("CredentialProjectID=%q", ctx.meta.CredentialProjectID)
	}
	if ctx.meta.ChannelLocation != "us-central1" {
		t.Fatalf("ChannelLocation=%q", ctx.meta.ChannelLocation)
	}
	if ctx.meta.BaseURL != "https://aiplatform.googleapis.com" {
		t.Fatalf("BaseURL=%q", ctx.meta.BaseURL)
	}
	wantPath := "/v1/projects/vertex-proj/locations/us-central1/publishers/google/models/gemini-2.5-flash:generateContent"
	if ctx.meta.RequestURLPath != wantPath {
		t.Fatalf("RequestURLPath=%q want %q", ctx.meta.RequestURLPath, wantPath)
	}
}

func TestBuildProxyCtxVertexBaseURLOverride(t *testing.T) {
	credentialFile := writeVertexCredentialFile(t, `{
  "type": "service_account",
  "project_id": "vertex-proj",
  "client_email": "svc@vertex-proj.iam.gserviceaccount.com",
  "private_key": "redacted"
}`)
	c := newMockE2EClient(t, map[string]string{
		"vertex.conf": providerConfVertexCredentialMetadata(),
	})
	gc, _ := newGinJSONRequest(t, []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`))

	ctx, err := c.buildProxyCtx(gc, "vertex", ProviderKey{
		Name:            "vertex-sa",
		CredentialFile:  credentialFile,
		Location:        "us-central1",
		BaseURLOverride: "https://us-central1-aiplatform.googleapis.com",
	}, "chat.completions", false)
	if err != nil {
		t.Fatalf("buildProxyCtx: %v", err)
	}
	if ctx.meta.BaseURL != "https://us-central1-aiplatform.googleapis.com" {
		t.Fatalf("BaseURL=%q", ctx.meta.BaseURL)
	}
}

func TestBuildProxyCtxVertexCredentialFileMissingProjectIDNoLeak(t *testing.T) {
	credentialFile := writeVertexCredentialFile(t, `{
  "type": "service_account",
  "client_email": "svc@vertex-proj.iam.gserviceaccount.com",
  "private_key": "secret-private-key"
}`)
	c := newMockE2EClient(t, map[string]string{
		"vertex.conf": providerConfVertexCredentialMetadata(),
	})
	gc, _ := newGinJSONRequest(t, []byte(`{"model":"gemini-2.5-flash","messages":[{"role":"user","content":"hi"}]}`))

	_, err := c.buildProxyCtx(gc, "vertex", ProviderKey{
		Name:           "vertex-sa",
		CredentialFile: credentialFile,
	}, "chat.completions", false)
	if err == nil {
		t.Fatalf("expected missing project_id error")
	}
	if !strings.Contains(err.Error(), "missing project_id") {
		t.Fatalf("unexpected error: %v", err)
	}
	if strings.Contains(err.Error(), "secret-private-key") || strings.Contains(err.Error(), "svc@vertex-proj") {
		t.Fatalf("error leaked credential content: %v", err)
	}
}

func writeVertexCredentialFile(t *testing.T, raw string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "vertex-sa.json")
	if err := os.WriteFile(path, []byte(raw), 0o600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	return path
}

func providerConfVertexCredentialMetadata() string {
	return fmt.Sprintf(`syntax "next-router/0.1";

provider "vertex" {
  defaults {
    upstream_config {
      base_url = %q;
    }
    response {
      resp_passthrough;
    }
  }

  match api = "chat.completions" stream = false {
    upstream {
      set_path template("/v1/projects/${credential.project_id}/locations/${channel.location}/publishers/google/models/${request.model_mapped}:generateContent");
    }
  }
}
`, "https://aiplatform.googleapis.com")
}
