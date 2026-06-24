package dslquery

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

// GetResponseBody executes a no-body query request and requires a 200 OK response.
func GetResponseBody(ctx context.Context, client httpclient.HTTPDoer, method, reqURL string, headers http.Header, debugOut io.Writer) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, reqURL, http.NoBody)
	if err != nil {
		return nil, err
	}
	MergeHeaders(req.Header, headers)
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

// MergeHeaders appends all source header values into dst.
func MergeHeaders(dst, src http.Header) {
	for k, vals := range src {
		for _, v := range vals {
			dst.Add(k, v)
		}
	}
}

// CloneHeaders returns a shallow key copy with copied value slices.
func CloneHeaders(h http.Header) http.Header {
	out := make(http.Header, len(h))
	for k, vals := range h {
		cp := make([]string, len(vals))
		copy(cp, vals)
		out[k] = cp
	}
	return out
}

// BuildRequestURL resolves a query path against a base URL.
func BuildRequestURL(baseURL, defaultPath, configuredPath, resourceName string) (string, error) {
	path := strings.TrimSpace(configuredPath)
	if path == "" {
		path = strings.TrimSpace(defaultPath)
	}
	resourceName = strings.TrimSpace(resourceName)
	if resourceName == "" {
		resourceName = "query"
	}
	if path == "" {
		return "", errors.New(resourceName + " path is empty")
	}
	if strings.HasPrefix(path, "http://") || strings.HasPrefix(path, "https://") {
		return path, nil
	}
	b := strings.TrimSuffix(strings.TrimSpace(baseURL), "/")
	if b == "" {
		return "", errors.New(resourceName + " baseURL is empty")
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	u, err := url.Parse(b + path)
	if err != nil {
		return "", err
	}
	return u.String(), nil
}

// ApplyHeaderOps applies DSL header_set/header_del operations to h.
func ApplyHeaderOps(h http.Header, ops []dslconfig.HeaderOp, meta *dslmeta.Meta) {
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

// ResolveBaseURLFromExpr extracts a static base URL from a DSL string expression.
func ResolveBaseURLFromExpr(expr string) string {
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
