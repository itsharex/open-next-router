package cli

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateModelsGetFlags(t *testing.T) {
	if err := validateModelsGetFlags(modelsGetOptions{provider: "openai"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := validateModelsGetFlags(modelsGetOptions{}); err == nil {
		t.Fatalf("expected missing provider error")
	}
	if err := validateModelsGetFlags(modelsGetOptions{provider: "openai", allProviders: true}); err == nil {
		t.Fatalf("expected mutually exclusive flags error")
	}
}

func TestRunModelsGetWithOptions_VertexServiceAccount(t *testing.T) {
	var tokenCalls int
	var modelsCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls++
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Fatalf("grant_type=%q", got)
			}
			if got := strings.TrimSpace(r.Form.Get("assertion")); got == "" {
				t.Fatalf("missing assertion")
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"access_token":"vertex-token","expires_in":3600,"token_type":"Bearer"}`))
		case "/v1/projects/proj-1/locations/us-central1/models":
			modelsCalls++
			if got := r.Header.Get("Authorization"); got != "Bearer vertex-token" {
				t.Fatalf("Authorization=%q", got)
			}
			if got := r.Header.Get("x-goog-user-project"); got != "proj-1" {
				t.Fatalf("x-goog-user-project=%q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"models":[{"name":"projects/proj-1/locations/us-central1/models/tuned-gemini"}]}`))
		default:
			t.Fatalf("unexpected path=%q", r.URL.Path)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	providersDir := filepath.Join(dir, "providers")
	if err := os.MkdirAll(providersDir, 0o750); err != nil {
		t.Fatalf("mkdir providers: %v", err)
	}
	conf := `
syntax "next-router/0.1";

provider "vertex" {
  defaults {
    upstream_config {
      base_url = "` + srv.URL + `";
    }
    auth {
      oauth_mode google_service_account_file;
      oauth_scope "https://www.googleapis.com/auth/cloud-platform";
      auth_oauth_bearer;
    }
    request {
      set_header "x-goog-user-project" template("${credential.project_id}");
    }
    models {
      models_mode custom;
      path template("/v1/projects/${credential.project_id}/locations/${channel.location}/models");
      id_path "$.models[*].name";
      id_regex "^projects/[^/]+/locations/[^/]+/models/(.+)$";
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(providersDir, "vertex.conf"), []byte(conf), 0o600); err != nil {
		t.Fatalf("write provider conf: %v", err)
	}
	credentialFile := writeTestServiceAccountCredentialFile(t, srv.URL+"/oauth/token")
	keysPath := filepath.Join(dir, "keys.yaml")
	keysYAML := `
providers:
  vertex:
    keys:
      - name: vertex-sa
        credential_file: "` + credentialFile + `"
        location: "us-central1"
`
	if err := os.WriteFile(keysPath, []byte(keysYAML), 0o600); err != nil {
		t.Fatalf("write keys: %v", err)
	}
	cfgPath := filepath.Join(dir, "onr.yaml")
	cfgYAML := "auth:\n  api_key: \"x\"\nkeys:\n  file: \"" + keysPath + "\"\nproviders:\n  dir: \"" + providersDir + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := runModelsGetWithOptions(modelsGetOptions{
		cfgPath:  cfgPath,
		provider: "vertex",
	})
	_ = w.Close()
	os.Stdout = old
	if runErr != nil {
		t.Fatalf("runModelsGetWithOptions: %v", runErr)
	}
	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if got := strings.TrimSpace(string(body)); got != "tuned-gemini" {
		t.Fatalf("stdout=%q", got)
	}
	if tokenCalls != 1 {
		t.Fatalf("tokenCalls=%d", tokenCalls)
	}
	if modelsCalls != 1 {
		t.Fatalf("modelsCalls=%d", modelsCalls)
	}
}

func writeTestServiceAccountCredentialFile(t *testing.T, tokenURI string) string {
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
		"project_id":     "proj-1",
		"private_key_id": "kid-1",
		"private_key":    string(pemBytes),
		"client_email":   "svc@proj-1.iam.gserviceaccount.com",
		"token_uri":      tokenURI,
	})
	if err != nil {
		t.Fatalf("Marshal credential: %v", err)
	}
	path := filepath.Join(t.TempDir(), "sa.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatalf("write credential: %v", err)
	}
	return path
}

func TestRunModelsGetWithOptions_SingleProvider(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.URL.Path; got != "/v1/models" {
			t.Fatalf("path=%q", got)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("auth=%q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":[{"id":"gpt-4o-mini"},{"id":"gpt-4.1"}]}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	providersDir := filepath.Join(dir, "providers")
	if err := os.MkdirAll(providersDir, 0o750); err != nil {
		t.Fatalf("mkdir providers: %v", err)
	}
	conf := `
syntax "next-router/0.1";

provider "openai" {
  defaults {
    upstream_config {
      base_url = "` + srv.URL + `";
    }
    auth {
      auth_bearer;
    }
    models {
      models_mode openai;
    }
  }
}
`
	if err := os.WriteFile(filepath.Join(providersDir, "openai.conf"), []byte(conf), 0o600); err != nil {
		t.Fatalf("write provider conf: %v", err)
	}
	onrConf := `
syntax "next-router/0.1";

models_mode "openai" {}
`
	if err := os.WriteFile(filepath.Join(dir, "onr.conf"), []byte(onrConf), 0o600); err != nil {
		t.Fatalf("write onr.conf: %v", err)
	}
	keysPath := filepath.Join(dir, "keys.yaml")
	keysYAML := `
providers:
  openai:
    keys:
      - name: default
        value: test-key
`
	if err := os.WriteFile(keysPath, []byte(keysYAML), 0o600); err != nil {
		t.Fatalf("write keys: %v", err)
	}
	cfgPath := filepath.Join(dir, "onr.yaml")
	cfgYAML := "auth:\n  api_key: \"x\"\nkeys:\n  file: \"" + keysPath + "\"\nproviders:\n  dir: \"" + providersDir + "\"\n"
	if err := os.WriteFile(cfgPath, []byte(cfgYAML), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	runErr := runModelsGetWithOptions(modelsGetOptions{
		cfgPath:  cfgPath,
		provider: "openai",
	})
	_ = w.Close()
	os.Stdout = old
	if runErr != nil {
		t.Fatalf("runModelsGetWithOptions: %v", runErr)
	}

	body, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	out := strings.TrimSpace(string(body))
	if out != "gpt-4.1\ngpt-4o-mini" {
		t.Fatalf("stdout=%q", out)
	}
}
