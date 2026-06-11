package httpclienttest

import (
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/r9s-ai/open-next-router/onr-core/pkg/httpclient"
)

// FakeDoer implements httpclient.HTTPDoer so callers can run tests without
// making outbound HTTP requests.
type FakeDoer struct {
	t         testing.TB
	responses []Response
	requests  []*http.Request
}

// Response describes a fake HTTP response that will be materialized when Do is
// called.
type Response struct {
	StatusCode int
	Body       string
	Header     http.Header
}

// NewFakeDoer returns a FakeDoer seeded with the responses that should be
// returned for each Do call.
// NewFakeDoer returns a non-nil fake doer.
func NewFakeDoer(t testing.TB, responses ...Response) *FakeDoer {
	return &FakeDoer{
		t:         t,
		responses: append([]Response(nil), responses...),
	}
}

// Do records the request and returns the next queued response.
func (f *FakeDoer) Do(req *http.Request) (*http.Response, error) {
	f.requests = append(f.requests, req)
	if len(f.responses) == 0 {
		f.t.Fatalf("fake http client has no responses left for request %s %s", req.Method, req.URL.String())
	}
	resp := f.responses[0]
	f.responses = f.responses[1:]
	return &http.Response{
		StatusCode: resp.StatusCode,
		Body:       io.NopCloser(strings.NewReader(resp.Body)),
		Header:     resp.Header.Clone(),
	}, nil
}

// Requests returns the HTTP requests captured so far.
func (f *FakeDoer) Requests() []*http.Request {
	return append([]*http.Request(nil), f.requests...)
}

// NewStringResponse builds a fake response with the provided status code and
// body string.
func NewStringResponse(status int, body string) Response {
	return Response{
		StatusCode: status,
		Body:       body,
		Header:     make(http.Header),
	}
}

var _ httpclient.HTTPDoer = (*FakeDoer)(nil)
