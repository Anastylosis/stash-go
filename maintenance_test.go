package stash

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMigrateSendsTheBackupPath(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"migrate":true}}`))
	defer srv.Close()

	err := NewClient(srv.URL).Migrate(context.Background(), `C:\Users\me\.stash\pre-migration.sqlite`)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if got := sentInput(t, capt.reqs[0])["backupPath"]; got != `C:\Users\me\.stash\pre-migration.sqlite` {
		t.Errorf("backupPath = %v", got)
	}
}

// Migrating without a backup is a legitimate thing to ask for — the field is
// required, so it goes on the wire empty rather than being omitted.
func TestMigrateWithoutABackupStillSendsTheField(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"migrate":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).Migrate(context.Background(), ""); err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	in := sentInput(t, capt.reqs[0])
	if v, present := in["backupPath"]; !present || v != "" {
		t.Errorf("backupPath = %v (present=%v), want a present empty string", v, present)
	}
}

func TestMigrateBlobsPassesDeleteOld(t *testing.T) {
	for _, want := range []bool{true, false} {
		capt := &capture{}
		srv := httptest.NewServer(capt.handler(`{"data":{"migrateBlobs":"4321"}}`))

		id, err := NewClient(srv.URL).MigrateBlobs(context.Background(), want)
		if err != nil {
			t.Fatalf("MigrateBlobs: %v", err)
		}
		if id != "4321" {
			t.Errorf("job id = %q", id)
		}
		if got := sentInput(t, capt.reqs[0])["deleteOld"]; got != want {
			t.Errorf("deleteOld = %v, want %v", got, want)
		}
		srv.Close()
	}
}

func TestMigrateSceneScreenshotsPassesBothFlags(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"migrateSceneScreenshots":"9"}}`))
	defer srv.Close()

	_, err := NewClient(srv.URL).MigrateSceneScreenshots(context.Background(),
		ScreenshotMigration{DeleteFiles: true, OverwriteExisting: false})
	if err != nil {
		t.Fatalf("MigrateSceneScreenshots: %v", err)
	}
	in := sentInput(t, capt.reqs[0])
	if in["deleteFiles"] != true || in["overwriteExisting"] != false {
		t.Errorf("input = %v", in)
	}
}

// The two mutations that take no input at all still answer with a job id.
func TestInputlessJobsReturnTheirID(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		run  func(*Client) (string, error)
	}{
		{"MigrateHashNaming", `{"data":{"migrateHashNaming":"11"}}`,
			func(c *Client) (string, error) { return c.MigrateHashNaming(context.Background()) }},
		{"DownloadFFMpeg", `{"data":{"downloadFFMpeg":"12"}}`,
			func(c *Client) (string, error) { return c.DownloadFFMpeg(context.Background()) }},
		{"OptimiseDatabase", `{"data":{"optimiseDatabase":"13"}}`,
			func(c *Client) (string, error) { return c.OptimiseDatabase(context.Background()) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			capt := &capture{}
			srv := httptest.NewServer(capt.handler(tc.body))
			defer srv.Close()

			id, err := tc.run(NewClient(srv.URL))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if id == "" {
				t.Errorf("%s returned no job id", tc.name)
			}
			if capt.reqs[0].Variables != nil {
				t.Errorf("%s sent variables: %v", tc.name, capt.reqs[0].Variables)
			}
		})
	}
}

// anonymiseServer stubs the mutation and the download route the URL it
// returns points at.
func anonymiseServer(t *testing.T, payload string) (*Client, *capture) {
	t.Helper()
	capt := &capture{}
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req graphqlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		capt.reqs = append(capt.reqs, req)
		body := `{"data":{"anonymiseDatabase":"/root/.stash/anonymous.sqlite"}}`
		if in := sentInput(t, req); in["download"] == true {
			body = `{"data":{"anonymiseDatabase":"` + base + `/downloads/xyz/anonymous.sqlite"}}`
		}
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/downloads/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, payload)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return NewClient(srv.URL), capt
}

func TestAnonymiseDatabaseReturnsServerPath(t *testing.T) {
	c, capt := anonymiseServer(t, "")
	path, err := c.AnonymiseDatabase(context.Background())
	if err != nil {
		t.Fatalf("AnonymiseDatabase: %v", err)
	}
	if path != "/root/.stash/anonymous.sqlite" {
		t.Errorf("path = %q", path)
	}
	if got := sentInput(t, capt.reqs[0])["download"]; got != false {
		t.Errorf("download = %v, want false", got)
	}
}

func TestDownloadAnonymisedDatabaseStreamsToWriter(t *testing.T) {
	const payload = "SQLite format 3\x00...no names in here..."
	c, capt := anonymiseServer(t, payload)

	var buf bytes.Buffer
	name, n, err := c.DownloadAnonymisedDatabase(context.Background(), &buf)
	if err != nil {
		t.Fatalf("DownloadAnonymisedDatabase: %v", err)
	}
	if buf.String() != payload || n != int64(len(payload)) {
		t.Errorf("wrote %d bytes: %q", n, buf.String())
	}
	if name != "anonymous.sqlite" {
		t.Errorf("name = %q", name)
	}
	if got := sentInput(t, capt.reqs[0])["download"]; got != true {
		t.Errorf("download = %v, want true", got)
	}
}

// The mutation is nullable, so a server that wrote nothing is not an error at
// the GraphQL level. Handing back "" would make it the caller's problem.
func TestAnonymiseDatabaseReportsAnEmptyAnswer(t *testing.T) {
	for _, body := range []string{`{"data":{"anonymiseDatabase":null}}`, `{"data":{"anonymiseDatabase":""}}`} {
		_, c := server(t, reply(body))
		_, err := c.AnonymiseDatabase(context.Background())
		if err == nil {
			t.Fatalf("AnonymiseDatabase(%s) = nil error", body)
		}
		if !strings.Contains(err.Error(), "no anonymised database") {
			t.Errorf("error = %q", err)
		}
	}
}
