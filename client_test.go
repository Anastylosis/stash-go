package stash

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// server returns a Stash stub that replies with the given body and records the
// last request it saw.
func server(t *testing.T, handler func(w http.ResponseWriter, r *http.Request)) (*httptest.Server, *Client) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(handler))
	t.Cleanup(srv.Close)
	return srv, NewClient(srv.URL)
}

// reply answers every request with a fixed GraphQL body.
func reply(body string) func(http.ResponseWriter, *http.Request) {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
}

func TestNewClientAppendsGraphQLPath(t *testing.T) {
	for _, base := range []string{"http://x:9999", "http://x:9999/"} {
		if got := NewClient(base).endpoint; got != "http://x:9999/graphql" {
			t.Errorf("NewClient(%q).endpoint = %q", base, got)
		}
	}
}

func TestAPIKeyHeaderSentOnlyWhenSet(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("ApiKey")
		_, _ = io.WriteString(w, `{"data":{}}`)
	}))
	defer srv.Close()

	if err := NewClient(srv.URL).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if got != "" {
		t.Errorf("ApiKey header = %q, want empty when no key configured", got)
	}

	if err := NewClient(srv.URL, WithAPIKey("secret")).Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if got != "secret" {
		t.Errorf("ApiKey header = %q, want %q", got, "secret")
	}
}

// A GraphQL error must surface as *APIError so callers can inspect the
// messages rather than string-matching an error.
func TestGraphQLErrorsBecomeAPIError(t *testing.T) {
	_, c := server(t, reply(`{"errors":[{"message":"Cannot query field \"captions\""},{"message":"second"}]}`))

	err := c.Ping(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("Ping error = %v (%T), want *APIError", err, err)
	}
	if len(apiErr.Errors) != 2 {
		t.Fatalf("Errors = %#v, want 2", apiErr.Errors)
	}
	if got := apiErr.Messages(); got[1] != "second" {
		t.Errorf("Messages() = %#v", got)
	}
	if !strings.Contains(apiErr.Error(), "Cannot query field") {
		t.Errorf("Error() = %q, missing the server message", apiErr.Error())
	}
}

// Some GraphQL middlewares echo the request back on an auth failure. The key
// must never reach an error string, because those get logged.
func TestAPIKeyRedactedFromErrors(t *testing.T) {
	_, _ = server(t, nil)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"bad key: hunter2"}]}`)
	}))
	defer srv.Close()

	err := NewClient(srv.URL, WithAPIKey("hunter2")).Ping(context.Background())
	if err == nil {
		t.Fatal("want an error")
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("error leaks the API key: %q", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("error = %q, want the key replaced with [redacted]", err)
	}
}

func TestHTTPErrorCarriesStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	err := NewClient(srv.URL).Ping(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %v (%T), want *HTTPError", err, err)
	}
	if httpErr.StatusCode != http.StatusUnauthorized {
		t.Errorf("StatusCode = %d, want 401", httpErr.StatusCode)
	}
}

func TestMaxResponseBytesRejectsOversizedBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"x":"`+strings.Repeat("a", 4096)+`"}}`)
	}))
	defer srv.Close()

	err := NewClient(srv.URL, WithMaxResponseBytes(512)).Ping(context.Background())
	if err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("error = %v, want an 'exceeds' size error", err)
	}
}

func TestWithHTTPClientIsUsed(t *testing.T) {
	used := false
	rt := roundTripperFunc(func(r *http.Request) (*http.Response, error) {
		used = true
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader(`{"data":{}}`)),
			Header:     make(http.Header),
		}, nil
	})
	c := NewClient("http://example.invalid", WithHTTPClient(&http.Client{Transport: rt}))
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
	if !used {
		t.Error("the supplied http.Client was not used")
	}
}

// A nil client must not wipe out the default, or the zero value of a caller's
// config struct would leave the client unusable.
func TestWithHTTPClientIgnoresNil(t *testing.T) {
	c := NewClient("http://x", WithHTTPClient(nil))
	if c.http == nil {
		t.Fatal("WithHTTPClient(nil) cleared the default client")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestSupportsIntrospectsOnceAndCaches(t *testing.T) {
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = io.WriteString(w, `{"data":{"__type":{"fields":[{"name":"id"},{"name":"captions"}]}}}`)
	}))
	defer srv.Close()
	c := NewClient(srv.URL)

	ok, err := c.Supports(context.Background(), "captions")
	if err != nil || !ok {
		t.Fatalf("Supports(captions) = %v, %v; want true, nil", ok, err)
	}
	if ok, _ := c.Supports(context.Background(), "nope"); ok {
		t.Error("Supports(nope) = true, want false")
	}
	if calls != 1 {
		t.Errorf("introspected %d times, want 1 (result should be cached)", calls)
	}
}

func TestExecuteReturnsRawData(t *testing.T) {
	_, c := server(t, reply(`{"data":{"anything":{"n":7}}}`))
	data, err := c.Execute(context.Background(), `{ anything { n } }`, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	var got struct {
		Anything struct {
			N int `json:"n"`
		} `json:"anything"`
	}
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("decoding: %v", err)
	}
	if got.Anything.N != 7 {
		t.Errorf("n = %d, want 7", got.Anything.N)
	}
}

func TestVersion(t *testing.T) {
	_, c := server(t, reply(`{"data":{"version":{"version":"v0.28.1"}}}`))
	v, err := c.Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	if v != "v0.28.1" {
		t.Errorf("Version = %q, want v0.28.1", v)
	}
}

// GraphQL errors carry `path` and `extensions`, and that is where a server
// says something machine-readable. Flattening to a message string throws away
// the only structured error the API offers.
func TestAPIErrorKeepsPathAndExtensions(t *testing.T) {
	_, c := server(t, reply(`{"errors":[{
	  "message":"Cannot query field \"captions\"",
	  "path":["findScene","captions"],
	  "extensions":{"code":"GRAPHQL_VALIDATION_FAILED"}}]}`))

	err := c.Ping(context.Background())
	var apiErr *APIError
	if !errors.As(err, &apiErr) {
		t.Fatalf("error = %v (%T), want *APIError", err, err)
	}
	item := apiErr.Errors[0]
	if len(item.Path) != 2 || item.Path[1] != "captions" {
		t.Errorf("Path = %#v, want the failing field", item.Path)
	}
	if item.Extensions["code"] != "GRAPHQL_VALIDATION_FAILED" {
		t.Errorf("Extensions = %#v", item.Extensions)
	}
}

// A bare status code sends the reader to the server logs for something that
// was already in the response.
func TestHTTPErrorCarriesTheResponseBody(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = io.WriteString(w, "invalid api key")
	}))
	defer srv.Close()

	err := NewClient(srv.URL).Ping(context.Background())
	var httpErr *HTTPError
	if !errors.As(err, &httpErr) {
		t.Fatalf("error = %v (%T), want *HTTPError", err, err)
	}
	if httpErr.Body != "invalid api key" {
		t.Errorf("Body = %q, want the server's message", httpErr.Body)
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Errorf("Error() = %q, should include the body", err)
	}
}

// The key must not leak through an error body either.
func TestHTTPErrorBodyIsRedacted(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "rejected key hunter2")
	}))
	defer srv.Close()

	err := NewClient(srv.URL, WithAPIKey("hunter2")).Ping(context.Background())
	if strings.Contains(err.Error(), "hunter2") {
		t.Fatalf("HTTP error body leaks the API key: %q", err)
	}
}
