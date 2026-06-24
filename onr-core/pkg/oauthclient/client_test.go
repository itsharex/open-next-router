package oauthclient

import (
	"context"
	"crypto"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestClient_GetToken_CacheAndPersist(t *testing.T) {
	t.Parallel()

	var tokenCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		n := tokenCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-" + strconvI32(n),
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	c := New(srv.Client(), true, filepath.Join(dir, "oauth"))
	in := AcquireInput{
		CacheKey:      "k1",
		TokenURL:      srv.URL + "/oauth/token",
		Method:        http.MethodPost,
		ContentType:   "form",
		Form:          map[string]string{"grant_type": "refresh_token", "refresh_token": "rk"},
		TokenPath:     "$.access_token",
		ExpiresInPath: "$.expires_in",
		TokenTypePath: "$.token_type",
		Timeout:       3 * time.Second,
		RefreshSkew:   1 * time.Second,
		FallbackTTL:   30 * time.Minute,
	}

	tok1, err := c.GetToken(context.Background(), in)
	if err != nil {
		t.Fatalf("first get token err=%v", err)
	}
	tok2, err := c.GetToken(context.Background(), in)
	if err != nil {
		t.Fatalf("second get token err=%v", err)
	}
	if tok1.AccessToken != tok2.AccessToken {
		t.Fatalf("cache should return same token: %q vs %q", tok1.AccessToken, tok2.AccessToken)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token endpoint calls=%d want=1", got)
	}

	c2 := New(srv.Client(), true, filepath.Join(dir, "oauth"))
	tok3, err := c2.GetToken(context.Background(), in)
	if err != nil {
		t.Fatalf("third get token err=%v", err)
	}
	if tok3.AccessToken != tok1.AccessToken {
		t.Fatalf("persisted token mismatch: %q vs %q", tok3.AccessToken, tok1.AccessToken)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("persisted token should avoid endpoint call, got=%d", got)
	}
}

func TestClient_Invalidate(t *testing.T) {
	t.Parallel()

	var tokenCalls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/oauth/token" {
			http.NotFound(w, r)
			return
		}
		n := tokenCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "tok-" + strconvI32(n),
			"expires_in":   3600,
		})
	}))
	t.Cleanup(srv.Close)

	c := New(srv.Client(), false, "")
	in := AcquireInput{
		CacheKey:      "k2",
		TokenURL:      srv.URL + "/oauth/token",
		Method:        http.MethodPost,
		ContentType:   "form",
		Form:          map[string]string{"grant_type": "refresh_token", "refresh_token": "rk"},
		TokenPath:     "$.access_token",
		ExpiresInPath: "$.expires_in",
		Timeout:       3 * time.Second,
		RefreshSkew:   time.Second,
		FallbackTTL:   30 * time.Minute,
	}

	if _, err := c.GetToken(context.Background(), in); err != nil {
		t.Fatalf("first get err=%v", err)
	}
	c.Invalidate("k2")
	if _, err := c.GetToken(context.Background(), in); err != nil {
		t.Fatalf("second get err=%v", err)
	}
	if got := tokenCalls.Load(); got != 2 {
		t.Fatalf("calls=%d want=2", got)
	}
}

func TestParseServiceAccountCredential(t *testing.T) {
	t.Parallel()

	raw, _ := testServiceAccountJSON(t, "https://oauth2.googleapis.com/token", "proj-1")
	cred, err := parseServiceAccountCredential([]byte(raw))
	if err != nil {
		t.Fatalf("parseServiceAccountCredential: %v", err)
	}
	if cred.ProjectID != "proj-1" {
		t.Fatalf("project_id=%q", cred.ProjectID)
	}
	if cred.ClientEmail != "svc@example.iam.gserviceaccount.com" {
		t.Fatalf("client_email=%q", cred.ClientEmail)
	}
	if cred.PrivateKeyID != "kid-1" {
		t.Fatalf("private_key_id=%q", cred.PrivateKeyID)
	}
}

func TestParseServiceAccountCredentialMissingPrivateKeyIDAllowed(t *testing.T) {
	t.Parallel()

	cred, err := parseServiceAccountCredential([]byte(`{"project_id":"proj","client_email":"svc@example.com","private_key":"x"}`))
	if err != nil {
		t.Fatalf("parse without private_key_id: %v", err)
	}
	if cred.PrivateKeyID != "" {
		t.Fatalf("private_key_id=%q want empty", cred.PrivateKeyID)
	}
}

func TestParseServiceAccountCredentialMissingPrivateKeyNoLeak(t *testing.T) {
	t.Parallel()

	_, err := parseServiceAccountCredential([]byte(`{"project_id":"proj","client_email":"svc@example.com","private_key":"","private_key_id":"kid"}`))
	if err == nil || !strings.Contains(err.Error(), "missing private_key") {
		t.Fatalf("expected missing private_key error, got %v", err)
	}
	if strings.Contains(err.Error(), "svc@example.com") || strings.Contains(err.Error(), "private_key\":\"") {
		t.Fatalf("error should not include credential json: %v", err)
	}
}

func TestClient_GetToken_GoogleServiceAccountJWTBearer(t *testing.T) {
	t.Parallel()

	var tokenCalls atomic.Int32
	var publicKey *rsa.PublicKey
	scope := "https://www.googleapis.com/auth/cloud-platform"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/token" {
			http.NotFound(w, r)
			return
		}
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:jwt-bearer" {
			t.Fatalf("grant_type=%q", got)
		}
		assertion := r.Form.Get("assertion")
		claims := verifyServiceAccountAssertion(t, assertion, publicKey)
		if got := claims["iss"]; got != "svc@example.iam.gserviceaccount.com" {
			t.Fatalf("iss=%v", got)
		}
		if got := claims["scope"]; got != scope {
			t.Fatalf("scope=%v", got)
		}
		if got := claims["aud"]; got != requestHostURL(r)+"/token" {
			t.Fatalf("aud=%v want %s", got, requestHostURL(r)+"/token")
		}
		if exp, ok := claims["exp"].(float64); !ok || exp <= float64(time.Now().Unix()) {
			t.Fatalf("exp=%v", claims["exp"])
		}
		n := tokenCalls.Add(1)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "sa-token-" + strconvI32(n),
			"token_type":   "Bearer",
			"expires_in":   3600,
		})
	}))
	t.Cleanup(srv.Close)

	tokenURL := srv.URL + "/token"
	raw, key := testServiceAccountJSON(t, tokenURL, "proj-1")
	publicKey = &key.PublicKey

	c := New(srv.Client(), false, "")
	in := AcquireInput{
		CacheKey:                     "google-sa",
		Mode:                         modeGoogleServiceAccountFile,
		Method:                       http.MethodPost,
		ServiceAccountCredentialJSON: raw,
		ServiceAccountScope:          scope,
		TokenPath:                    "$.access_token",
		ExpiresInPath:                "$.expires_in",
		TokenTypePath:                "$.token_type",
		Timeout:                      3 * time.Second,
		RefreshSkew:                  time.Second,
		FallbackTTL:                  30 * time.Minute,
	}

	tok1, err := c.GetToken(context.Background(), in)
	if err != nil {
		t.Fatalf("first get token err=%v", err)
	}
	tok2, err := c.GetToken(context.Background(), in)
	if err != nil {
		t.Fatalf("second get token err=%v", err)
	}
	if tok1.AccessToken != "sa-token-1" || tok2.AccessToken != tok1.AccessToken {
		t.Fatalf("unexpected cached tokens: %#v %#v", tok1, tok2)
	}
	if got := tokenCalls.Load(); got != 1 {
		t.Fatalf("token endpoint calls=%d want=1", got)
	}
}

func strconvI32(v int32) string {
	return strconv.FormatInt(int64(v), 10)
}

func testServiceAccountJSON(t *testing.T, tokenURI string, projectID string) (string, *rsa.PrivateKey) {
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
		"project_id":     projectID,
		"private_key_id": "kid-1",
		"private_key":    string(pemBytes),
		"client_email":   "svc@example.iam.gserviceaccount.com",
		"token_uri":      tokenURI,
	})
	if err != nil {
		t.Fatalf("Marshal credential: %v", err)
	}
	return string(raw), key
}

func verifyServiceAccountAssertion(t *testing.T, assertion string, publicKey *rsa.PublicKey) map[string]any {
	t.Helper()
	parts := strings.Split(assertion, ".")
	if len(parts) != 3 {
		t.Fatalf("jwt parts=%d", len(parts))
	}
	headerRaw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		t.Fatalf("decode header: %v", err)
	}
	var header map[string]any
	if err := json.Unmarshal(headerRaw, &header); err != nil {
		t.Fatalf("unmarshal header: %v", err)
	}
	if header["alg"] != "RS256" || header["kid"] != "kid-1" {
		t.Fatalf("unexpected jwt header: %#v", header)
	}
	claimsRaw, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatalf("decode claims: %v", err)
	}
	var claims map[string]any
	if err := json.Unmarshal(claimsRaw, &claims); err != nil {
		t.Fatalf("unmarshal claims: %v", err)
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("decode signature: %v", err)
	}
	sum := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	if err := rsa.VerifyPKCS1v15(publicKey, crypto.SHA256, sum[:], sig); err != nil {
		t.Fatalf("VerifyPKCS1v15: %v", err)
	}
	return claims
}

func requestHostURL(r *http.Request) string {
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}
