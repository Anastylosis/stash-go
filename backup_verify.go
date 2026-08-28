package stash

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

// SQLiteMagic opens every SQLite 3 database file.
const SQLiteMagic = "SQLite format 3\x00"

// sqliteHeaderSize is the length of the SQLite file header, which is all
// [VerifySQLite] reads.
const sqliteHeaderSize = 100

var (
	// ErrNotSQLite means the file does not begin with [SQLiteMagic]:
	// whatever was downloaded, it is not a database. A login page or a
	// proxy's error page answering the download URL looks like this.
	ErrNotSQLite = errors.New("stash: not a SQLite database")
	// ErrTruncatedBackup means the file is a SQLite database whose header
	// describes more bytes than the file has. A transfer that dropped
	// mid-stream looks like this, and the prefix it leaves opens fine.
	ErrTruncatedBackup = errors.New("stash: truncated backup")
)

// VerifySQLite reports whether r, of exactly size bytes, is a whole SQLite
// database: it has to begin with [SQLiteMagic], and the page count in its
// header has to agree with size.
//
// The page-count comparison is skipped, rather than failed, when the header's
// count is not authoritative. SQLite only trusts it while the change counter
// at offset 24 matches the version-valid-for number at offset 92, and a
// database last written by a very old library leaves it zero. The magic is
// still required in that case; the length simply cannot be confirmed from
// the header alone.
func VerifySQLite(r io.ReaderAt, size int64) error {
	n := int64(sqliteHeaderSize)
	if size < n {
		n = size
	}
	header := make([]byte, n)
	if _, err := r.ReadAt(header, 0); err != nil && !errors.Is(err, io.EOF) {
		return fmt.Errorf("stash: reading SQLite header: %w", err)
	}

	if len(header) >= len(SQLiteMagic) && string(header[:len(SQLiteMagic)]) != SQLiteMagic {
		return fmt.Errorf("%w: file begins %q", ErrNotSQLite, printable(header[:len(SQLiteMagic)]))
	}
	if len(header) < sqliteHeaderSize {
		return fmt.Errorf("%w: got %d bytes, need %d", ErrNotSQLite, len(header), sqliteHeaderSize)
	}

	pageSize := int64(binary.BigEndian.Uint16(header[16:18]))
	if pageSize == 1 {
		pageSize = 65536
	}
	changeCounter := binary.BigEndian.Uint32(header[24:28])
	pageCount := int64(binary.BigEndian.Uint32(header[28:32]))
	validFor := binary.BigEndian.Uint32(header[92:96])

	if pageCount == 0 || changeCounter != validFor {
		return nil
	}
	if want := pageSize * pageCount; want != size {
		return fmt.Errorf("%w: header describes %d bytes (%d pages of %d), file is %d",
			ErrTruncatedBackup, want, pageCount, pageSize, size)
	}
	return nil
}

// printable renders the start of a non-database file for an error message:
// enough to recognise an HTML page, without pasting raw bytes into a log.
func printable(b []byte) string {
	out := make([]rune, 0, len(b))
	for _, c := range b {
		if c < 0x20 || c > 0x7e {
			c = '.'
		}
		out = append(out, rune(c))
	}
	return string(out)
}

// BackupManifest records what [Client.DownloadVerifiedBackup] wrote and
// which server it came from. It is written beside the backup as JSON.
type BackupManifest struct {
	// File is the server's name for the backup, and the filename it was
	// saved under.
	File   string       `json:"file"`
	Bytes  int64        `json:"bytes"`
	SHA256 string       `json:"sha256"`
	Server BackupServer `json:"server"`
}

// BackupServer is what a manifest records about the server a backup came
// from. The database path in particular: a file called local.sqlite says
// nothing about which machine or which Stash it belongs to.
type BackupServer struct {
	Version      string `json:"version"`
	Schema       int    `json:"schema"`
	OS           string `json:"os"`
	DatabasePath string `json:"database_path"`
	SceneCount   int    `json:"scene_count"`
}

// ManifestSuffix is appended to a backup's filename to name its manifest.
const ManifestSuffix = ".manifest.json"

// DownloadVerifiedBackup backs up the server's database into dir, checks
// that what arrived is a whole SQLite database, and writes a manifest
// beside it.
//
// The download lands in `<name>.part`, where name is the server's name for
// the backup, and is renamed once [VerifySQLite] accepts it; the manifest
// goes to `<name>.manifest.json`. Any failure removes the partial file and
// returns the error, so the directory never holds a half-backup under a
// usable name. [ErrNotSQLite] and [ErrTruncatedBackup] are the two ways the
// transfer itself can be bad.
//
// The server is described before the backup is taken, and one that is not
// [SystemStatus.Ready] is refused: a database mid-migration is the one
// moment the file on disk is least worth having. Everything
// [Client.DownloadBackup] says about timeouts applies here too.
func (c *Client) DownloadVerifiedBackup(ctx context.Context, opts BackupOptions, dir string) (BackupManifest, error) {
	server, err := c.describeForBackup(ctx)
	if err != nil {
		return BackupManifest{}, err
	}

	raw, err := c.backup(ctx, opts, true)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("stash: backing up database: %w", err)
	}
	loc, err := c.resolveServerURL(raw)
	if err != nil {
		return BackupManifest{}, err
	}
	name := filepath.Base(loc.Path)
	if name == "." || name == "/" || name == "" {
		return BackupManifest{}, fmt.Errorf("stash: backing up database: server returned no file name in %q", raw)
	}

	partPath := filepath.Join(dir, name+".part")
	part, err := os.OpenFile(partPath, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return BackupManifest{}, fmt.Errorf("stash: creating %s: %w", partPath, err)
	}
	defer func() {
		_ = part.Close()
		_ = os.Remove(partPath)
	}()

	sum := sha256.New()
	_, written, err := c.downloadServerFile(ctx, raw, io.MultiWriter(part, sum), "backup")
	if err != nil {
		return BackupManifest{}, err
	}
	if err := part.Sync(); err != nil {
		return BackupManifest{}, fmt.Errorf("stash: flushing %s: %w", partPath, err)
	}
	if err := VerifySQLite(part, written); err != nil {
		return BackupManifest{}, err
	}
	if err := part.Close(); err != nil {
		return BackupManifest{}, fmt.Errorf("stash: closing %s: %w", partPath, err)
	}

	dest := filepath.Join(dir, name)
	if err := os.Rename(partPath, dest); err != nil {
		return BackupManifest{}, fmt.Errorf("stash: moving backup into place: %w", err)
	}

	m := BackupManifest{
		File:   name,
		Bytes:  written,
		SHA256: hex.EncodeToString(sum.Sum(nil)),
		Server: server,
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		_ = os.Remove(dest)
		return BackupManifest{}, fmt.Errorf("stash: encoding manifest: %w", err)
	}
	if err := os.WriteFile(dest+ManifestSuffix, append(b, '\n'), 0o644); err != nil {
		_ = os.Remove(dest)
		return BackupManifest{}, fmt.Errorf("stash: writing manifest: %w", err)
	}
	return m, nil
}

// describeForBackup gathers what the manifest records about the server, and
// refuses one that is not ready.
func (c *Client) describeForBackup(ctx context.Context) (BackupServer, error) {
	status, err := c.SystemStatus(ctx)
	if err != nil {
		return BackupServer{}, err
	}
	if !status.Ready() {
		return BackupServer{}, fmt.Errorf("stash: backing up database: server is not ready: status %s", status.Status)
	}
	version, err := c.ServerVersion(ctx)
	if err != nil {
		return BackupServer{}, err
	}
	stats, err := c.LibraryStats(ctx)
	if err != nil {
		return BackupServer{}, err
	}
	data, err := c.Execute(ctx, `{ systemStatus { os } }`, nil)
	if err != nil {
		return BackupServer{}, fmt.Errorf("stash: reading server OS: %w", err)
	}
	var osResult struct {
		SystemStatus struct {
			OS string `json:"os"`
		} `json:"systemStatus"`
	}
	if err := json.Unmarshal(data, &osResult); err != nil {
		return BackupServer{}, fmt.Errorf("stash: decoding server OS: %w", err)
	}

	server := BackupServer{
		Version:      version.Version,
		OS:           osResult.SystemStatus.OS,
		DatabasePath: status.DatabasePath,
		SceneCount:   stats.SceneCount,
	}
	if status.DatabaseSchema != nil {
		server.Schema = *status.DatabaseSchema
	}
	return server, nil
}
