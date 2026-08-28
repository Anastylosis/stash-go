package stash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sqliteFile builds a synthetic database: a header claiming pages pages of
// pageSize bytes, padded out to the length that claim implies. counterOK
// false leaves the change counter disagreeing with version-valid-for, which
// is the state SQLite itself treats as "page count unknown".
func sqliteFile(pageSize uint16, pages uint32, counterOK bool) []byte {
	size := int64(pageSize) * int64(pages)
	if pageSize == 1 {
		size = 65536 * int64(pages)
	}
	b := make([]byte, size)
	copy(b, SQLiteMagic)
	binary.BigEndian.PutUint16(b[16:18], pageSize)
	binary.BigEndian.PutUint32(b[24:28], 7)
	binary.BigEndian.PutUint32(b[28:32], pages)
	if counterOK {
		binary.BigEndian.PutUint32(b[92:96], 7)
	}
	return b
}

func TestVerifySQLiteAcceptsWholeDatabase(t *testing.T) {
	db := sqliteFile(1024, 4, true)
	if err := VerifySQLite(bytes.NewReader(db), int64(len(db))); err != nil {
		t.Fatalf("VerifySQLite: %v", err)
	}
}

func TestVerifySQLiteHandlesLargestPageSize(t *testing.T) {
	db := sqliteFile(1, 2, true)
	if err := VerifySQLite(bytes.NewReader(db), int64(len(db))); err != nil {
		t.Fatalf("VerifySQLite: %v", err)
	}
}

func TestVerifySQLiteRejectsTruncatedDatabase(t *testing.T) {
	db := sqliteFile(1024, 4, true)
	cut := db[:len(db)-512]
	err := VerifySQLite(bytes.NewReader(cut), int64(len(cut)))
	if !errors.Is(err, ErrTruncatedBackup) {
		t.Fatalf("err = %v, want ErrTruncatedBackup", err)
	}
	if !strings.Contains(err.Error(), "4096") || !strings.Contains(err.Error(), "3584") {
		t.Errorf("err = %v, want it to name both lengths", err)
	}
}

// A login page answering the download URL is the common way a backup is
// not a database at all, and it is named as such rather than as a short
// read even when it is shorter than the header.
func TestVerifySQLiteRejectsHTML(t *testing.T) {
	body := "<!DOCTYPE html><html><head><title>Login</title></head><body></body></html>"
	err := VerifySQLite(strings.NewReader(body), int64(len(body)))
	if !errors.Is(err, ErrNotSQLite) {
		t.Fatalf("err = %v, want ErrNotSQLite", err)
	}
	if !strings.Contains(err.Error(), "<!DOCTYPE") {
		t.Errorf("err = %v, want it to show the start of the file", err)
	}
}

func TestVerifySQLiteRejectsShortHeader(t *testing.T) {
	short := []byte(SQLiteMagic + "abc")
	err := VerifySQLite(bytes.NewReader(short), int64(len(short)))
	if !errors.Is(err, ErrNotSQLite) {
		t.Fatalf("err = %v, want ErrNotSQLite", err)
	}
}

// When the change counter and version-valid-for disagree, SQLite does not
// trust the header's page count, and neither does the check: the file is a
// database, its length just cannot be confirmed from the header.
func TestVerifySQLiteSkipsPageCountWhenCounterStale(t *testing.T) {
	db := sqliteFile(1024, 4, false)
	cut := db[:len(db)-512]
	if err := VerifySQLite(bytes.NewReader(cut), int64(len(cut))); err != nil {
		t.Fatalf("VerifySQLite: %v, want nil for a non-authoritative page count", err)
	}
}

// verifiedBackupServer answers the four description queries and the backup
// mutation, and serves payload from the download route.
func verifiedBackupServer(t *testing.T, payload []byte) *Client {
	t.Helper()
	mux := http.NewServeMux()
	var base string
	mux.HandleFunc("/graphql", func(w http.ResponseWriter, r *http.Request) {
		var req graphqlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		var body string
		switch {
		case strings.Contains(req.Query, "backupDatabase"):
			body = `{"data":{"backupDatabase":"` + base + `/downloads/abc123/local.sqlite.85.20260101_000000"}}`
		case strings.Contains(req.Query, "systemStatus { os }"):
			body = `{"data":{"systemStatus":{"os":"windows"}}}`
		case strings.Contains(req.Query, "systemStatus"):
			body = `{"data":{"systemStatus":{"status":"OK","databaseSchema":85,"appSchema":85,"databasePath":"C:\\Users\\me\\.stash\\local.sqlite","configPath":"C:\\Users\\me\\.stash\\config.yml"}}}`
		case strings.Contains(req.Query, "version"):
			body = `{"data":{"version":{"version":"v0.31.1","hash":"abc","build_time":"2026-01-01"}}}`
		case strings.Contains(req.Query, "stats"):
			body = `{"data":{"stats":{"scene_count":61000}}}`
		default:
			t.Errorf("unexpected query %q", req.Query)
		}
		_, _ = io.WriteString(w, body)
	})
	mux.HandleFunc("/downloads/", func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(payload)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	base = srv.URL
	return NewClient(srv.URL)
}

func TestDownloadVerifiedBackupWritesFileAndManifest(t *testing.T) {
	db := sqliteFile(1024, 4, true)
	c := verifiedBackupServer(t, db)
	dir := t.TempDir()

	m, err := c.DownloadVerifiedBackup(context.Background(), BackupOptions{}, dir)
	if err != nil {
		t.Fatalf("DownloadVerifiedBackup: %v", err)
	}

	sum := sha256.Sum256(db)
	want := BackupManifest{
		File:   "local.sqlite.85.20260101_000000",
		Bytes:  int64(len(db)),
		SHA256: hex.EncodeToString(sum[:]),
		Server: BackupServer{
			Version:      "v0.31.1",
			Schema:       85,
			OS:           "windows",
			DatabasePath: `C:\Users\me\.stash\local.sqlite`,
			SceneCount:   61000,
		},
	}
	if m != want {
		t.Errorf("manifest = %+v\nwant %+v", m, want)
	}

	got, err := os.ReadFile(filepath.Join(dir, m.File))
	if err != nil {
		t.Fatalf("reading backup: %v", err)
	}
	if !bytes.Equal(got, db) {
		t.Error("backup on disk differs from what the server sent")
	}

	raw, err := os.ReadFile(filepath.Join(dir, m.File+ManifestSuffix))
	if err != nil {
		t.Fatalf("reading manifest: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(raw, &onDisk); err != nil {
		t.Fatalf("decoding manifest: %v", err)
	}
	for _, key := range []string{"file", "bytes", "sha256", "server"} {
		if _, ok := onDisk[key]; !ok {
			t.Errorf("manifest lacks %q", key)
		}
	}
	server, _ := onDisk["server"].(map[string]any)
	for _, key := range []string{"version", "schema", "os", "database_path", "scene_count"} {
		if _, ok := server[key]; !ok {
			t.Errorf("manifest server lacks %q", key)
		}
	}

	if _, err := os.Stat(filepath.Join(dir, m.File+".part")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf(".part still present after success: %v", err)
	}
}

// A download that is not a database must leave nothing behind under a name
// that looks like a backup.
func TestDownloadVerifiedBackupRemovesBadDownload(t *testing.T) {
	c := verifiedBackupServer(t, []byte("<html><body>Please log in</body></html>"))
	dir := t.TempDir()

	_, err := c.DownloadVerifiedBackup(context.Background(), BackupOptions{}, dir)
	if !errors.Is(err, ErrNotSQLite) {
		t.Fatalf("err = %v, want ErrNotSQLite", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 0 {
		t.Errorf("directory not empty after failure: %v", entries)
	}
}

func TestDownloadVerifiedBackupRemovesTruncatedDownload(t *testing.T) {
	db := sqliteFile(1024, 4, true)
	c := verifiedBackupServer(t, db[:len(db)-100])
	dir := t.TempDir()

	_, err := c.DownloadVerifiedBackup(context.Background(), BackupOptions{}, dir)
	if !errors.Is(err, ErrTruncatedBackup) {
		t.Fatalf("err = %v, want ErrTruncatedBackup", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("directory not empty after failure: %v", entries)
	}
}

func TestDownloadVerifiedBackupRefusesUnreadyServer(t *testing.T) {
	_, c := server(t, reply(`{"data":{"systemStatus":{"status":"NEEDS_MIGRATION","appSchema":86}}}`))
	_, err := c.DownloadVerifiedBackup(context.Background(), BackupOptions{}, t.TempDir())
	if err == nil || !strings.Contains(err.Error(), "NEEDS_MIGRATION") {
		t.Fatalf("err = %v, want a not-ready error naming the status", err)
	}
}
