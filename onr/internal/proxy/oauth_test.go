package proxy

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestProxyOAuth_CustomMode_Cache(t *testing.T) {
	var tokenCalls atomic.Int32
	var upstreamCalls atomic.Int32

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-1",
				"expires_in":   3600,
				"token_type":   "Bearer",
			})
		case "/v1/chat/completions":
			upstreamCalls.Add(1)
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer tok-1" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mock.Close)

	c := newMockE2EClient(t, map[string]string{"openai.conf": providerConfOAuthCustom(mock.URL)})
	body := mustReadTestData(t, "fixtures/chat_nonstream_request.json")

	for i := 0; i < 2; i++ {
		gc, _ := newGinJSONRequest(t, body)
		res, err := c.ProxyJSON(gc, "openai", ProviderKey{Name: "oauth-key", Value: "rk"}, "chat.completions", false)
		if err != nil {
			t.Fatalf("proxy err: %v", err)
		}
		if res == nil || res.Status != http.StatusOK {
			t.Fatalf("unexpected result: %#v", res)
		}
	}

	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls=%d want=1", got)
	}
	if got := upstreamCalls.Load(); got != 2 {
		t.Fatalf("upstream calls=%d want=2", got)
	}
}

func TestProxyOAuth_401RetryInvalidate(t *testing.T) {
	var tokenCalls atomic.Int32
	var upstreamCalls atomic.Int32
	var firstUnauthorized atomic.Bool

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			n := tokenCalls.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": fmt.Sprintf("tok-%d", n),
				"expires_in":   3600,
			})
		case "/v1/chat/completions":
			upstreamCalls.Add(1)
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got == "Bearer tok-1" && !firstUnauthorized.Swap(true) {
				w.WriteHeader(http.StatusUnauthorized)
				_, _ = w.Write([]byte(`{"error":"expired"}`))
				return
			}
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer tok-2" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mock.Close)

	c := newMockE2EClient(t, map[string]string{"openai.conf": providerConfOAuthCustom(mock.URL)})
	body := mustReadTestData(t, "fixtures/chat_nonstream_request.json")
	gc, _ := newGinJSONRequest(t, body)
	res, err := c.ProxyJSON(gc, "openai", ProviderKey{Name: "oauth-key", Value: "rk"}, "chat.completions", false)
	if err != nil {
		t.Fatalf("proxy err: %v", err)
	}
	if res == nil || res.Status != http.StatusOK {
		t.Fatalf("unexpected result: %#v", res)
	}

	if got := tokenCalls.Load(); got != 2 {
		t.Fatalf("token calls=%d want=2", got)
	}
	if got := upstreamCalls.Load(); got != 2 {
		t.Fatalf("upstream calls=%d want=2", got)
	}
}

func TestProxyOAuth_GoogleServiceAccountFile(t *testing.T) {
	var tokenCalls atomic.Int32
	var upstreamCalls atomic.Int32

	mock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/oauth/token":
			tokenCalls.Add(1)
			if err := r.ParseForm(); err != nil {
				t.Fatalf("ParseForm: %v", err)
			}
			if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
				t.Fatalf("grant_type=%q", got)
			}
			if assertion := strings.TrimSpace(r.Form.Get("assertion")); assertion == "" {
				t.Fatalf("assertion is empty")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token": "tok-1",
				"expires_in":   3600,
				"token_type":   "Bearer",
			})
		case "/v1/chat/completions":
			upstreamCalls.Add(1)
			if got := strings.TrimSpace(r.Header.Get("Authorization")); got != "Bearer tok-1" {
				t.Fatalf("Authorization=%q", got)
			}
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(mock.Close)

	credentialFile := writeOAuthServiceAccountCredentialFile(t, mock.URL+"/oauth/token")
	c := newMockE2EClient(t, map[string]string{"openai.conf": providerConfOAuthGoogleServiceAccount(mock.URL)})
	body := mustReadTestData(t, "fixtures/chat_nonstream_request.json")

	gc, _ := newGinJSONRequest(t, body)
	res, err := c.ProxyJSON(gc, "openai", ProviderKey{Name: "vertex-sa", CredentialFile: credentialFile}, "chat.completions", false)
	if err != nil {
		t.Fatalf("proxy err: %v", err)
	}
	if res == nil || res.Status != http.StatusOK {
		t.Fatalf("unexpected result: %#v", res)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token calls=%d want=1", got)
	}
	if got := upstreamCalls.Load(); got != 1 {
		t.Fatalf("upstream calls=%d want=1", got)
	}
}

func providerConfOAuthCustom(baseURL string) string {
	return fmt.Sprintf(`syntax "next-router/0.1";

provider "openai" {
  defaults {
    upstream_config {
      base_url = %q;
    }
    auth {
      oauth_mode custom;
      oauth_token_url %q;
      oauth_content_type form;
      oauth_form "grant_type" "refresh_token";
      oauth_form "refresh_token" $channel.key;
      oauth_token_path "$.access_token";
      oauth_expires_in_path "$.expires_in";
      auth_oauth_bearer;
    }
    response {
      resp_passthrough;
    }
    metrics {
      usage_fact input token path="$.usage.prompt_tokens";
      usage_fact output token path="$.usage.completion_tokens";
      usage_fact cache_read token path="$.usage.prompt_tokens_details.cached_tokens";
      finish_reason_path "$.choices[*].finish_reason";
    }
  }
  match api = "chat.completions" stream = false {
    upstream {
      set_path "/v1/chat/completions";
    }
  }
}
`, baseURL, baseURL+"/oauth/token")
}

func providerConfOAuthGoogleServiceAccount(baseURL string) string {
	return fmt.Sprintf(`syntax "next-router/0.1";

provider "openai" {
  defaults {
    upstream_config {
      base_url = %q;
    }
    auth {
      oauth_mode google_service_account_file;
      oauth_scope "https://www.googleapis.com/auth/cloud-platform";
      auth_oauth_bearer;
    }
    response {
      resp_passthrough;
    }
    metrics {
      usage_fact input token path="$.usage.prompt_tokens";
      usage_fact output token path="$.usage.completion_tokens";
      usage_fact cache_read token path="$.usage.prompt_tokens_details.cached_tokens";
      finish_reason_path "$.choices[*].finish_reason";
    }
  }
  match api = "chat.completions" stream = false {
    upstream {
      set_path "/v1/chat/completions";
    }
  }
}
`, baseURL)
}

func writeOAuthServiceAccountCredentialFile(t *testing.T, tokenURI string) string {
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
