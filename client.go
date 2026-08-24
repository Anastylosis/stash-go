// Package stash is a Go client for the GraphQL API of a running Stash server
// (https://stashapp.cc).
//
// It has no dependencies outside the standard library, and it does not impose
// an HTTP client: pass your own with [WithHTTPClient] to get whatever retry,
// timeout and transport behaviour your program already uses.
//
//	c := stash.NewClient("http://localhost:9999", stash.WithAPIKey(key))
//	scenes, _, err := c.FindScenes(ctx, stash.SceneFilter{}, 1, 100)
//
// The server's schema varies by version. Ask before relying on a field that
// older releases lack — see [Client.Supports].
package stash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DefaultMaxResponseBytes caps how much of a response is read. A full page of
// scenes with metadata is large, but a runaway server should not be able to
// exhaust the caller's memory.
const DefaultMaxResponseBytes = 50 << 20 // 50 MiB

// Sentinel errors. Stash answers "no such performer" with an empty result set
// rather than an error, so a typo in a filter is otherwise indistinguishable
// from a filter that legitimately matched nothing.
var (
	// ErrPerformerNotFound means no performer has the requested name. Stash
	// matches names exactly, so this is usually a typo or stray whitespace.
	ErrPerformerNotFound = errors.New("stash: no such performer")
	// ErrStudioNotFound means no studio has the requested name.
	ErrStudioNotFound = errors.New("stash: no such studio")
	// ErrTagNotFound means no tag has the requested name.
	ErrTagNotFound = errors.New("stash: no such tag")

	// errTwoTagFilters is the one filter combination Stash cannot express:
	// it takes a single tags criterion, so asking for both directions would
	// silently keep only one.
	errTwoTagFilters = errors.New("stash: TagNames and ExcludeTagNames cannot both be set")
)

// GraphQLError is one entry from a GraphQL `errors` array.
//
// Path and Extensions are kept because they are where a server says something
// useful: Path names the field that failed, and Stash puts its own error codes
// in Extensions. Flattening these to a string loses the machine-readable part
// of the only structured error the API offers.
type GraphQLError struct {
	Message    string         `json:"message"`
	Path       []any          `json:"path,omitempty"`
	Extensions map[string]any `json:"extensions,omitempty"`
}

// APIError is one or more errors returned in a GraphQL response body. The
// request itself succeeded at the HTTP level.
//
// Inspect Errors to distinguish a schema mismatch ("Cannot query field …")
// from an authentication failure, rather than matching on error text.
type APIError struct {
	Errors []GraphQLError
}

// Messages returns just the message strings, for the common case where the
// structure is not needed.
func (e *APIError) Messages() []string {
	out := make([]string, len(e.Errors))
	for i, item := range e.Errors {
		out[i] = item.Message
	}
	return out
}

func (e *APIError) Error() string {
	switch len(e.Errors) {
	case 0:
		// A response with an empty errors array is malformed; say so rather
		// than returning a bare prefix that reads like a truncated message.
		return "stash api: empty error array"
	case 1:
		return "stash api: " + e.Errors[0].Message
	default:
		return "stash api: " + strings.Join(e.Messages(), "; ")
	}
}

// HTTPError is a non-2xx response from the server.
//
// Body carries what the server actually said, truncated. Stash returns useful
// text on an auth failure or a bad endpoint, and a bare status code sends the
// reader to the server logs for something that was already in the response.
type HTTPError struct {
	StatusCode int
	Status     string
	Body       string
}

func (e *HTTPError) Error() string {
	if e.Body == "" {
		return "stash: unexpected status " + e.Status
	}
	return "stash: unexpected status " + e.Status + ": " + e.Body
}

// maxErrorBody caps how much of a failing response is quoted back. Enough to
// carry a real message, short enough to stay readable in a log line.
const maxErrorBody = 2048

// Client talks to one Stash server. It is safe for concurrent use.
type Client struct {
	baseURL  string
	endpoint string
	apiKey   string
	cookie   *http.Cookie
	http     *http.Client
	maxBytes int64

	capsOnce sync.Once
	caps     map[string]bool
	capsErr  error
}

// Option configures a Client.
type Option func(*Client)

// WithAPIKey authenticates as the given API key. Omit it for a Stash instance
// with authentication disabled.
func WithAPIKey(key string) Option {
	return func(c *Client) { c.apiKey = key }
}

// WithCookie authenticates with a session cookie.
//
// Stash hands its plugin processes a session cookie in server_connection, and
// a plugin has no API key unless the operator configured one. An API key takes
// precedence when both are set: session cookies expire mid-run, which on a
// long task fails partway through rather than at startup.
func WithCookie(cookie *http.Cookie) Option {
	return func(c *Client) { c.cookie = cookie }
}

// WithHTTPClient supplies the HTTP client used for every request, so retry,
// backoff, proxying and timeouts stay under the caller's control.
//
// The default is a plain client with a 30s timeout and no retry.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithMaxResponseBytes overrides [DefaultMaxResponseBytes].
func WithMaxResponseBytes(n int64) Option {
	return func(c *Client) {
		if n > 0 {
			c.maxBytes = n
		}
	}
}

// WithCaptions once asked the scene queries to include [Scene.Captions],
// which they now always do.
//
// It survives as a no-op so that callers written against the option still
// compile. There is nothing to opt into: the supported server has the field,
// so it is in [SceneFields] with everything else.
//
// Deprecated: captions are always selected. Remove the option.
func WithCaptions() Option {
	return func(*Client) {}
}

// NewClient returns a client for the Stash server at baseURL, which is the
// server root ("http://localhost:9999") — "/graphql" is appended.
func NewClient(baseURL string, opts ...Option) *Client {
	root := strings.TrimSuffix(baseURL, "/")
	c := &Client{
		baseURL:  root,
		endpoint: root + "/graphql",
		http:     &http.Client{Timeout: 30 * time.Second},
		maxBytes: DefaultMaxResponseBytes,
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

type graphqlRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables,omitempty"`
}

type graphqlResponse struct {
	Data   json.RawMessage `json:"data"`
	Errors []GraphQLError  `json:"errors"`
}

// Execute runs a raw GraphQL query or mutation and returns the `data` object.
//
// Exported so callers can reach parts of the schema this package does not
// wrap, without hand-rolling the transport, auth and error handling again.
func (c *Client) Execute(ctx context.Context, query string, variables map[string]any) (json.RawMessage, error) {
	return c.do(ctx, graphqlRequest{Query: query, Variables: variables})
}

func (c *Client) do(ctx context.Context, gql graphqlRequest) (json.RawMessage, error) {
	body, err := json.Marshal(gql)
	if err != nil {
		return nil, fmt.Errorf("stash: marshalling request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("stash: building request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("stash: %w", c.redact(err))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		// Read a bounded amount rather than discarding it: Stash puts a real
		// message here on auth failures and bad endpoints. Reading also lets
		// the connection be reused.
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return nil, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       c.redactString(strings.TrimSpace(string(b))),
		}
	}

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("stash: reading response: %w", err)
	}
	if int64(len(respBody)) > c.maxBytes {
		return nil, fmt.Errorf("stash: response exceeds %d bytes", c.maxBytes)
	}

	var gqlResp graphqlResponse
	if err := json.Unmarshal(respBody, &gqlResp); err != nil {
		return nil, fmt.Errorf("stash: parsing response: %w", err)
	}
	if len(gqlResp.Errors) > 0 {
		items := make([]GraphQLError, len(gqlResp.Errors))
		for i, e := range gqlResp.Errors {
			// Some GraphQL middlewares echo the request back on auth failure.
			// Never let the key reach a log line.
			e.Message = c.redactString(e.Message)
			items[i] = e
		}
		return nil, &APIError{Errors: items}
	}
	return gqlResp.Data, nil
}

// authorize applies whichever credential the client was given. Stash accepts
// the same two on its plain HTTP routes as on /graphql, so anything fetching
// a download URL authenticates through here rather than rebuilding it.
func (c *Client) authorize(req *http.Request) {
	switch {
	case c.apiKey != "":
		req.Header.Set("ApiKey", c.apiKey)
	case c.cookie != nil:
		req.AddCookie(c.cookie)
	}
}

func (c *Client) redactString(s string) string {
	if c.apiKey == "" {
		return s
	}
	return strings.ReplaceAll(s, c.apiKey, "[redacted]")
}

func (c *Client) redact(err error) error {
	if c.apiKey == "" || err == nil {
		return err
	}
	if msg := c.redactString(err.Error()); msg != err.Error() {
		return errors.New(msg)
	}
	return err
}

// Ping checks that the server is reachable and answering GraphQL.
func (c *Client) Ping(ctx context.Context) error {
	if _, err := c.do(ctx, graphqlRequest{Query: `{ systemStatus { status } }`}); err != nil {
		return fmt.Errorf("stash: ping: %w", err)
	}
	return nil
}

// Version reports the server's version string.
func (c *Client) Version(ctx context.Context) (string, error) {
	data, err := c.do(ctx, graphqlRequest{Query: `{ version { version } }`})
	if err != nil {
		return "", fmt.Errorf("stash: version: %w", err)
	}
	var result struct {
		Version struct {
			Version string `json:"version"`
		} `json:"version"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding version: %w", err)
	}
	return result.Version.Version, nil
}

// Supports reports whether the server's schema has a named field on Scene.
//
// This exists because GraphQL fails the *whole* query when it is asked for a
// field the schema lacks — one unknown field costs the entire response, not
// just that field. Probing once is cheaper than discovering it mid-import
// against an older server.
//
//	if ok, _ := c.Supports(ctx, "captions"); ok { ... }
//
// The schema is fetched once per client and cached.
func (c *Client) Supports(ctx context.Context, field string) (bool, error) {
	c.capsOnce.Do(func() { c.caps, c.capsErr = c.sceneFields(ctx) })
	if c.capsErr != nil {
		return false, c.capsErr
	}
	return c.caps[field], nil
}

func (c *Client) sceneFields(ctx context.Context) (map[string]bool, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ __type(name: "Scene") { fields { name } } }`,
	})
	if err != nil {
		return nil, fmt.Errorf("stash: introspecting Scene: %w", err)
	}
	var result struct {
		Type struct {
			Fields []struct {
				Name string `json:"name"`
			} `json:"fields"`
		} `json:"__type"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding introspection: %w", err)
	}
	fields := make(map[string]bool, len(result.Type.Fields))
	for _, f := range result.Type.Fields {
		fields[f.Name] = true
	}
	return fields, nil
}
