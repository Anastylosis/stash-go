package stash

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// backupInput decodes the BackupDatabaseInput a call put on the wire.
func backupInput(t *testing.T, req graphqlRequest) map[string]any {
	t.Helper()
	b, err := json.Marshal(req.Variables["input"])
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decoding input: %v", err)
	}
	return out
}

// backupServer stubs both halves: the mutation, and the download route the
// URL it returns points at.
func backupServer(t *testing.T, payload string) (*httptest.Server, *Client, *capture) {
	t.Helper()
	cap := &capture{}
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req graphqlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		cap.reqs = append(cap.reqs, req)
		body := `{"data":{"backupDatabase":"C:\\Users\\me\\.stash\\local.sqlite.85.20260101_000000"}}`
		if in := backupInput(t, req); in["download"] == true {
			body = `{"data":{"backupDatabase":"` + base + `/downloads/abc123/local.sqlite.85.20260101_000000"}}`
		}
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/downloads/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, payload)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return srv, NewClient(srv.URL), cap
}

func TestBackupDatabaseReturnsServerPath(t *testing.T) {
	_, c, cap := backupServer(t, "")
	path, err := c.BackupDatabase(context.Background(), BackupOptions{})
	if err != nil {
		t.Fatalf("BackupDatabase: %v", err)
	}
	// The server's own notation, kept as-is: this is a Windows path and
	// normalising it would make it name a file that does not exist.
	if want := `C:\Users\me\.stash\local.sqlite.85.20260101_000000`; path != want {
		t.Errorf("path = %q, want %q", path, want)
	}
	if got := backupInput(t, cap.reqs[0])["download"]; got != false {
		t.Errorf("download = %v, want false", got)
	}
}

func TestDownloadBackupStreamsToWriter(t *testing.T) {
	const payload = "SQLite format 3\x00...the database..."
	_, c, cap := backupServer(t, payload)

	var buf bytes.Buffer
	name, n, err := c.DownloadBackup(context.Background(), BackupOptions{}, &buf)
	if err != nil {
		t.Fatalf("DownloadBackup: %v", err)
	}
	if buf.String() != payload {
		t.Errorf("body = %q, want %q", buf.String(), payload)
	}
	if n != int64(len(payload)) {
		t.Errorf("written = %d, want %d", n, len(payload))
	}
	// The name is what the caller has to save the file under; losing it
	// costs the schema version and timestamp the server encoded there.
	if name != "local.sqlite.85.20260101_000000" {
		t.Errorf("name = %q", name)
	}
	if got := backupInput(t, cap.reqs[0])["download"]; got != true {
		t.Errorf("download = %v, want true", got)
	}
}

func TestBackupPassesIncludeBlobs(t *testing.T) {
	for _, want := range []bool{true, false} {
		_, c, cap := backupServer(t, "x")
		if _, err := c.BackupDatabase(context.Background(), BackupOptions{IncludeBlobs: want}); err != nil {
			t.Fatalf("BackupDatabase: %v", err)
		}
		if got := backupInput(t, cap.reqs[0])["includeBlobs"]; got != want {
			t.Errorf("includeBlobs = %v, want %v", got, want)
		}
	}
}

// Stash builds the download URL from the request it received. Behind a proxy
// that can name a host this process cannot reach, so only the path is taken
// from it.
func TestDownloadBackupReRootsURLOnClientAddress(t *testing.T) {
	var hit string
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", reply(`{"data":{"backupDatabase":"https://stash.example.com:443/downloads/abc/db.sqlite?x=1"}}`))
	mux.HandleFunc("/downloads/", func(w http.ResponseWriter, r *http.Request) {
		hit = r.URL.String()
		_, _ = io.WriteString(w, "db")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL)
	if _, _, err := c.DownloadBackup(context.Background(), BackupOptions{}, io.Discard); err != nil {
		t.Fatalf("DownloadBackup: %v", err)
	}
	if hit != "/downloads/abc/db.sqlite?x=1" {
		t.Errorf("fetched %q, want the path and query preserved", hit)
	}
}

func TestDownloadBackupAuthenticates(t *testing.T) {
	var key string
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"data":{"backupDatabase":"/downloads/abc/db.sqlite"}}`)
	})
	mux.HandleFunc("/downloads/", func(w http.ResponseWriter, r *http.Request) {
		key = r.Header.Get("ApiKey")
		_, _ = io.WriteString(w, "db")
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, WithAPIKey("secret"))
	if _, _, err := c.DownloadBackup(context.Background(), BackupOptions{}, io.Discard); err != nil {
		t.Fatalf("DownloadBackup: %v", err)
	}
	if key != "secret" {
		t.Errorf("download sent ApiKey %q, want secret", key)
	}
}

// A nullable String means a server that backed up nothing is not a GraphQL
// error. Returning "" and no error would push that onto every caller.
func TestBackupNullIsAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"backupDatabase":null}}`))
	if _, err := c.BackupDatabase(context.Background(), BackupOptions{}); err == nil {
		t.Fatal("BackupDatabase: want an error for a null result")
	}
}

func TestDownloadBackupReportsHTTPFailure(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", reply(`{"data":{"backupDatabase":"/downloads/abc/db.sqlite"}}`))
	mux.HandleFunc("/downloads/", func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "backup expired", http.StatusNotFound)
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	_, _, err := NewClient(srv.URL).DownloadBackup(context.Background(), BackupOptions{}, io.Discard)
	var herr *HTTPError
	if !errors.As(err, &herr) {
		t.Fatalf("err = %v, want *HTTPError", err)
	}
	if herr.StatusCode != http.StatusNotFound || !strings.Contains(herr.Body, "backup expired") {
		t.Errorf("HTTPError = %+v", herr)
	}
}

// A short write must not be swallowed: a full disk that returned nil here
// would leave a truncated file looking like a complete backup.
func TestDownloadBackupPropagatesWriteFailure(t *testing.T) {
	_, c, _ := backupServer(t, strings.Repeat("x", 4096))
	_, n, err := c.DownloadBackup(context.Background(), BackupOptions{}, failingWriter{})
	if err == nil {
		t.Fatal("DownloadBackup: want the write error")
	}
	if n != 0 {
		t.Errorf("written = %d, want 0", n)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("disk full") }

// The HTTP client's timeout covers the whole transfer, and on a database
// that takes minutes to stream it fires as a bare "Client.Timeout exceeded"
// naming nothing that suggests the fix.
func TestDownloadBackupExplainsClientTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", reply(`{"data":{"backupDatabase":"/downloads/abc/db.sqlite"}}`))
	mux.HandleFunc("/downloads/", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, WithHTTPClient(&http.Client{Timeout: 50 * time.Millisecond}))
	_, _, err := c.DownloadBackup(context.Background(), BackupOptions{}, io.Discard)
	if err == nil {
		t.Fatal("DownloadBackup: want a timeout error")
	}
	if !strings.Contains(err.Error(), "WithHTTPClient") {
		t.Errorf("err = %v, want it to name the client timeout as the cause", err)
	}
}

// A context deadline reports Timeout() true as well, and pointing at the
// HTTP client there sends the reader to fix the wrong thing.
func TestDownloadBackupDoesNotBlameClientForContextDeadline(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/graphql", reply(`{"data":{"backupDatabase":"/downloads/abc/db.sqlite"}}`))
	mux.HandleFunc("/downloads/", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(2 * time.Second):
		case <-r.Context().Done():
		}
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	c := NewClient(srv.URL, WithHTTPClient(&http.Client{Timeout: time.Minute}))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	_, _, err := c.DownloadBackup(ctx, BackupOptions{}, io.Discard)
	if err == nil {
		t.Fatal("DownloadBackup: want a deadline error")
	}
	if strings.Contains(err.Error(), "WithHTTPClient") {
		t.Errorf("err = %v, want no HTTP-client hint for a context deadline", err)
	}
}
