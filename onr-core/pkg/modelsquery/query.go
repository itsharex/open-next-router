package modelsquery

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslconfig"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/dslmeta"
	"github.com/r9s-ai/open-next-router/onr-core/pkg/httpclient"
)

type Params struct {
	Provider string
	File     dslconfig.ProviderFile
	Meta     *dslmeta.Meta
	BaseURL  string
	APIKey   string

	// HTTPClient allows next-router to inject a fake client for tests or to
	// override the default http.Client behavior.
	HTTPClient httpclient.HTTPDoer
	DebugOut   io.Writer
}

type Result struct {
	Provider string
	IDs      []string
}

// Query requires a non-nil ctx and p.Meta.
// It returns a non-nil Result on success.
func Query(ctx context.Context, p Params) (*Result, error) {
	provider := strings.ToLower(strings.TrimSpace(p.Provider))
	if provider == "" {
		return nil, errors.New("provider is empty")
	}

	meta := cloneMetaForQuery(p.Meta)
	meta.API = strings.TrimSpace(meta.API)
	if meta.API == "" {
		meta.API = "chat.completions"
	}
	if strings.TrimSpace(p.APIKey) != "" {
		meta.APIKey = strings.TrimSpace(p.APIKey)
	}

	cfg, ok := p.File.Models.Select(meta)
	if !ok {
		return nil, fmt.Errorf("provider %q has no models config", provider)
	}

	baseURL := strings.TrimSpace(p.BaseURL)
	if baseURL == "" {
		baseURL = resolveBaseURLFromExpr(p.File.Routing.BaseURLExpr)
	}
	if baseURL == "" {
		return nil, errors.New("base url is empty")
	}
	meta.BaseURL = baseURL

	headers := make(http.Header)
	p.File.Headers.Apply(meta, nil, headers)
	applyHeaderOps(headers, cfg.Headers, meta)

	client := p.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	method := strings.ToUpper(strings.TrimSpace(cfg.Method))
	if method == "" {
		method = http.MethodGet
	}
	reqURL, err := buildModelsRequestURL(baseURL, dslconfig.EvalStringExpr(cfg.Path, meta))
	if err != nil {
		return nil, err
	}
	body, err := getResponseBody(ctx, client, method, reqURL, headers, p.DebugOut)
	if err != nil {
		return nil, err
	}
	ids, err := dslconfig.ExtractModelIDs(cfg, body)
	if err != nil {
		return nil, err
	}
	return &Result{
		Provider: provider,
		IDs:      ids,
	}, nil
}

// cloneMetaForQuery requires a non-nil source meta.
func cloneMetaForQuery(src *dslmeta.Meta) *dslmeta.Meta {
	return &dslmeta.Meta{
		API:                 src.API,
		IsStream:            src.IsStream,
		BaseURL:             src.BaseURL,
		APIKey:              src.APIKey,
		OAuthAccessToken:    src.OAuthAccessToken,
		OAuthCacheKey:       src.OAuthCacheKey,
		CredentialFile:      src.CredentialFile,
		CredentialJSON:      src.CredentialJSON,
		CredentialProjectID: src.CredentialProjectID,
		ChannelLocation:     src.ChannelLocation,
		OriginModelName:     src.OriginModelName,
		DSLModelMapped:      src.DSLModelMapped,
		RequestURLPath:      src.RequestURLPath,
		StartTime:           src.StartTime,
	}
}

func getResponseBody(ctx context.Context, client httpclient.HTTPDoer, method, reqURL string, headers http.Header, debugOut io.Writer) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	mergeHeaders(req.Header, headers)
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if debugOut != nil {
		_, _ = fmt.Fprintf(debugOut, "debug upstream_response method=%s url=%s status=%d body=%s\n", method, reqURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request %s %s failed: status=%d body=%s", method, reqURL, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func mergeHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

func buildModelsRequestURL(baseURL, path string) (string, error) {
	p := strings.TrimSpace(path)
	if p == "" {
		return "", errors.New("models path is empty")
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p, nil
	}
	b := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if b == "" {
		return "", errors.New("models baseURL is empty")
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u, err := url.Parse(b + p)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

func applyHeaderOps(h http.Header, ops []dslconfig.HeaderOp, meta *dslmeta.Meta) {
	for _, op := range ops {
		name := strings.TrimSpace(dslconfig.EvalStringExpr(op.NameExpr, meta))
		if name == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(op.Op)) {
		case "header_set":
			h.Set(name, dslconfig.EvalStringExpr(op.ValueExpr, meta))
		case "header_del":
			h.Del(name)
		}
	}
}

func resolveBaseURLFromExpr(expr string) string {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return ""
	}
	if strings.HasPrefix(raw, "\"") && strings.HasSuffix(raw, "\"") {
		v, err := strconv.Unquote(raw)
		if err == nil {
			return strings.TrimSpace(v)
		}
	}
	return raw
}
