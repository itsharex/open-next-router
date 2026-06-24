package dslconfig

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslmeta"
)

func TestProviderHeadersEffective_OAuthMerge(t *testing.T) {
	t.Parallel()

	boolPtr := func(v bool) *bool { return &v }
	h := ProviderHeaders{
		Defaults: PhaseHeaders{
			OAuth: OAuthConfig{
				Mode:             oauthModeOpenAI,
				RefreshTokenExpr: exprChannelKey,
			},
		},
		Matches: []MatchHeaders{
			{
				API:    "chat.completions",
				Stream: boolPtr(false),
				Headers: PhaseHeaders{
					OAuth: OAuthConfig{
						TokenURLExpr: `"https://token.example.com"`,
						Form: []OAuthFormField{
							{Key: "extra", ValueExpr: `"1"`},
						},
					},
				},
			},
		},
	}

	phase, ok := h.Effective(&dslmeta.Meta{API: "chat.completions", IsStream: false, APIKey: "rk"})
	if !ok {
		t.Fatalf("Effective should match")
	}
	if got := strings.ToLower(strings.TrimSpace(phase.OAuth.Mode)); got != oauthModeOpenAI {
		t.Fatalf("mode=%q want=%q", got, oauthModeOpenAI)
	}
	resolved, rok := phase.OAuth.Resolve(&dslmeta.Meta{API: "chat.completions", APIKey: "rk"})
	if !rok {
		t.Fatalf("resolve oauth should succeed")
	}
	if got := strings.TrimSpace(resolved.TokenURL); got != "https://token.example.com" {
		t.Fatalf("token_url=%q", got)
	}
	if got := strings.TrimSpace(resolved.Form["extra"]); got != "1" {
		t.Fatalf("form extra=%q", got)
	}
}

func TestValidateProviderFile_OAuthUnknownMode(t *testing.T) {
	t.Parallel()

	path := writeProviderFile(t, "openai.conf", `
provider "openai" {
  defaults {
    upstream_config { base_url = "https://api.openai.com"; }
    auth {
      oauth_mode "unknown_mode";
      auth_oauth_bearer;
    }
  }
}
`)
	_, err := ValidateProviderFile(path)
	if err == nil || !strings.Contains(err.Error(), "unsupported oauth_mode") {
		t.Fatalf("expected unsupported oauth_mode error, got: %v", err)
	}
}

func TestValidateProviderFile_OAuthCustomRequiresTokenURLAndForm(t *testing.T) {
	t.Parallel()

	path := writeProviderFile(t, "openai.conf", `
provider "openai" {
  defaults {
    upstream_config { base_url = "https://api.openai.com"; }
    auth {
      oauth_mode custom;
      auth_oauth_bearer;
    }
  }
}
`)
	_, err := ValidateProviderFile(path)
	if err == nil || !strings.Contains(err.Error(), "oauth_token_url is required in custom mode") {
		t.Fatalf("expected custom mode token_url error, got: %v", err)
	}
}

func TestValidateProviderFile_OAuthBuiltinOK(t *testing.T) {
	t.Parallel()

	path := writeProviderFile(t, "openai.conf", `
provider "openai" {
  defaults {
    upstream_config { base_url = "https://api.openai.com"; }
    auth {
      oauth_mode openai;
      oauth_refresh_token $channel.key;
      auth_oauth_bearer;
    }
  }
}
`)
	if _, err := ValidateProviderFile(path); err != nil {
		t.Fatalf("validate err=%v", err)
	}
}

func TestValidateProviderFile_OAuthGoogleServiceAccountOK(t *testing.T) {
	t.Parallel()

	path := writeProviderFile(t, "vertex.conf", `
provider "vertex" {
  defaults {
    upstream_config { base_url = "https://aiplatform.googleapis.com"; }
    auth {
      oauth_mode google_service_account_file;
      oauth_scope "https://www.googleapis.com/auth/cloud-platform";
      auth_oauth_bearer;
    }
  }
}
`)
	if _, err := ValidateProviderFile(path); err != nil {
		t.Fatalf("validate err=%v", err)
	}
}

func TestOAuthGoogleServiceAccountResolve(t *testing.T) {
	t.Parallel()

	cfg := OAuthConfig{
		Mode:      oauthModeGoogleSA,
		ScopeExpr: `"https://www.googleapis.com/auth/cloud-platform"`,
	}
	meta := &dslmeta.Meta{
		CredentialJSON: `{"project_id":"proj-1","client_email":"svc@example.com","private_key":"redacted","private_key_id":"kid-1","token_uri":"https://token.example.com"}`,
	}
	resolved, ok := cfg.Resolve(meta)
	if !ok {
		t.Fatalf("resolve oauth should succeed")
	}
	if resolved.Mode != oauthModeGoogleSA {
		t.Fatalf("mode=%q", resolved.Mode)
	}
	if resolved.Scope != "https://www.googleapis.com/auth/cloud-platform" {
		t.Fatalf("scope=%q", resolved.Scope)
	}
	if resolved.ServiceAccountCredentialJSON == "" {
		t.Fatalf("expected credential json")
	}
	if _, exists := resolved.Form["refresh_token"]; exists {
		t.Fatalf("service account mode should not use refresh_token form")
	}
}

func TestValidateProviderFile_GoogleServiceAccountRequiresScope(t *testing.T) {
	t.Parallel()

	path := writeProviderFile(t, "vertex.conf", `
provider "vertex" {
  defaults {
    upstream_config { base_url = "https://aiplatform.googleapis.com"; }
    auth {
      oauth_mode google_service_account_file;
      auth_oauth_bearer;
    }
  }
}
`)
	_, err := ValidateProviderFile(path)
	if err == nil || !strings.Contains(err.Error(), "oauth_scope is required") {
		t.Fatalf("ValidateProviderFile err=%v, want oauth_scope required", err)
	}
}

func TestResolvedOAuthConfigCacheIdentity_GoogleServiceAccountNoCrossUse(t *testing.T) {
	t.Parallel()

	base := ResolvedOAuthConfig{
		Mode:                         oauthModeGoogleSA,
		Method:                       "POST",
		ContentType:                  "form",
		TokenURL:                     "https://oauth2.googleapis.com/token",
		TokenPath:                    "$.access_token",
		ExpiresInPath:                "$.expires_in",
		TokenTypePath:                "$.token_type",
		TimeoutMs:                    5000,
		RefreshSkewSec:               300,
		FallbackTTLSec:               1800,
		Scope:                        "scope-a",
		ServiceAccountCredentialJSON: `{"project_id":"proj-1","client_email":"svc@example.com","private_key":"redacted-a","private_key_id":"kid-1","token_uri":"https://token.example.com/a"}`,
	}
	same := base
	if base.CacheIdentity() != same.CacheIdentity() {
		t.Fatalf("same service account config should have stable cache identity")
	}

	otherCredential := base
	otherCredential.ServiceAccountCredentialJSON = `{"project_id":"proj-2","client_email":"svc2@example.com","private_key":"redacted-b","private_key_id":"kid-2","token_uri":"https://token.example.com/a"}`
	if base.CacheIdentity() == otherCredential.CacheIdentity() {
		t.Fatalf("cache identity should change when credential content changes")
	}

	otherScope := base
	otherScope.Scope = "scope-b"
	if base.CacheIdentity() == otherScope.CacheIdentity() {
		t.Fatalf("cache identity should change when scope changes")
	}

	otherTokenURI := base
	otherTokenURI.ServiceAccountCredentialJSON = `{"project_id":"proj-1","client_email":"svc@example.com","private_key":"redacted-a","private_key_id":"kid-1","token_uri":"https://token.example.com/b"}`
	if base.CacheIdentity() == otherTokenURI.CacheIdentity() {
		t.Fatalf("cache identity should change when token_uri changes")
	}
	if strings.Contains(base.CacheIdentity(), "redacted") || strings.Contains(base.CacheIdentity(), "svc@example.com") {
		t.Fatalf("cache identity should not contain credential material: %q", base.CacheIdentity())
	}
}

func writeProviderFile(t *testing.T, name string, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write provider file: %v", err)
	}
	return path
}
