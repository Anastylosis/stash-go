package stash

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchStreamsAndReportsType(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = io.WriteString(w, "\xff\xd8\xff sprite bytes")
	}))
	defer srv.Close()

	var buf bytes.Buffer
	ct, n, err := NewClient(srv.URL).Fetch(context.Background(), "/scene/abc_sprite.jpg", &buf)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if ct != "image/jpeg" {
		t.Errorf("content type = %q", ct)
	}
	if n != int64(buf.Len()) || buf.Len() == 0 {
		t.Errorf("wrote %d bytes, buffer holds %d", n, buf.Len())
	}
}

// Scene paths come back as absolute URLs the server built from the request it
// received, which behind a proxy can name a host this process cannot reach.
func TestFetchReRootsAbsoluteURLs(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.String()
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL).Fetch(context.Background(),
		"https://stash.example.com/scene/abc_sprite.jpg?t=1", io.Discard)
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if path != "/scene/abc_sprite.jpg?t=1" {
		t.Errorf("fetched %q, want the path and query preserved", path)
	}
}

func TestFetchAuthenticates(t *testing.T) {
	var key string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("ApiKey")
		_, _ = io.WriteString(w, "ok")
	}))
	defer srv.Close()

	if _, _, err := NewClient(srv.URL, WithAPIKey("secret")).Fetch(context.Background(), "/scene/1/screenshot", io.Discard); err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if key != "secret" {
		t.Errorf("ApiKey = %q, want secret", key)
	}
}

// Sprites are generated lazily, so "this scene has none" is an ordinary
// answer a caller skips past rather than a failure.
func TestFetchReportsMissingAsErrNotFound(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.NotFound(w, nil)
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL).Fetch(context.Background(), "/scene/abc_sprite.jpg", io.Discard)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestFetchReportsOtherStatuses(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusUnauthorized)
	}))
	defer srv.Close()

	_, _, err := NewClient(srv.URL).Fetch(context.Background(), "/scene/1/screenshot", io.Discard)
	var herr *HTTPError
	if !errors.As(err, &herr) || herr.StatusCode != http.StatusUnauthorized {
		t.Fatalf("err = %v, want a 401 HTTPError", err)
	}
	if errors.Is(err, ErrNotFound) {
		t.Error("a 401 was reported as ErrNotFound")
	}
}
