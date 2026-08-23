package stash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

// BackupOptions configures a database backup.
type BackupOptions struct {
	// IncludeBlobs asks for blob data — covers, and the rest of what Stash
	// stores as blobs — to be included.
	//
	// It changes nothing on a server that keeps blobs in the database, which
	// is what an empty blobsPath in the configuration means: there they are
	// part of the file whatever this is set to. It matters on a server
	// storing blobs on the filesystem, where a backup without them is not a
	// backup of everything.
	IncludeBlobs bool
}

// BackupDatabase asks the server to write a backup of its database and
// returns the path it wrote.
//
// That path is on the *server's* filesystem, in the server's own notation: a
// Windows-hosted Stash answers with something like
// `C:\Users\you\.stash\local.sqlite.85.20260101_000000`, which the calling
// machine has no way to open. Where it lands is the server's
// backupDirectoryPath setting, or the database's own directory when that is
// unset.
//
// A backup that stays on the machine being backed up is a limited kind of
// insurance. [Client.DownloadBackup] fetches one instead.
func (c *Client) BackupDatabase(ctx context.Context, opts BackupOptions) (serverPath string, err error) {
	path, err := c.backup(ctx, opts, false)
	if err != nil {
		return "", fmt.Errorf("stash: backing up database: %w", err)
	}
	return path, nil
}

// DownloadBackup backs up the server's database and streams it to w,
// returning the server's name for the backup and the number of bytes
// written.
//
// The server writes the backup to a temporary file and serves that over
// HTTP. It does not delete the temporary file once the download finishes —
// it clears that directory on restart — so a program backing up on a
// schedule leaves copies on the server's temp volume.
//
// Nothing here is bounded by [WithMaxResponseBytes]. That cap protects a
// caller decoding a GraphQL response into memory; this is a stream to a
// writer the caller chose, of a database that is hundreds of megabytes on a
// real library. For the same reason the HTTP client's timeout matters more
// than usual: it covers the whole transfer, not just the response headers,
// and the default client's is 30 seconds. Pass one with no timeout (see
// [WithHTTPClient]) and bound the transfer with ctx instead.
//
// A short write to w aborts the download and is returned as-is, so a full
// disk does not leave a truncated file looking like a complete one.
func (c *Client) DownloadBackup(ctx context.Context, opts BackupOptions, w io.Writer) (name string, written int64, err error) {
	raw, err := c.backup(ctx, opts, true)
	if err != nil {
		return "", 0, fmt.Errorf("stash: backing up database: %w", err)
	}

	loc, err := c.resolveServerURL(raw)
	if err != nil {
		return "", 0, err
	}
	name = loc.Path[strings.LastIndex(loc.Path, "/")+1:]

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc.String(), nil)
	if err != nil {
		return name, 0, fmt.Errorf("stash: building backup download request: %w", err)
	}
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return name, 0, fmt.Errorf("stash: downloading backup: %w", c.explainTimeout(ctx, c.redact(err)))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return name, 0, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       c.redactString(strings.TrimSpace(string(b))),
		}
	}

	written, err = io.Copy(w, resp.Body)
	if err != nil {
		return name, written, fmt.Errorf("stash: downloading backup: %w", c.explainTimeout(ctx, c.redact(err)))
	}
	return name, written, nil
}

func (c *Client) backup(ctx context.Context, opts BackupOptions, download bool) (string, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `mutation($input: BackupDatabaseInput!) { backupDatabase(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{
			"download":     download,
			"includeBlobs": opts.IncludeBlobs,
		}},
	})
	if err != nil {
		return "", err
	}
	var result struct {
		BackupDatabase *string `json:"backupDatabase"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decoding backup result: %w", err)
	}
	if result.BackupDatabase == nil || *result.BackupDatabase == "" {
		// The mutation is typed as a nullable String, so a server that
		// backed up nothing is not an error at the GraphQL level. Saying so
		// beats handing back "" for the caller to notice.
		return "", errors.New("server reported no backup")
	}
	return *result.BackupDatabase, nil
}

// resolveServerURL re-roots a URL the server produced on the address this
// client was built with.
//
// Stash builds such URLs from the request it received, so they usually point
// back where the request came from. Usually is not always: behind a proxy, or
// with an external URL configured, one can name a host that answers for
// browsers and not for this process. The path identifies the resource; the
// route to the server is something the caller already knew.
func (c *Client) resolveServerURL(raw string) (*url.URL, error) {
	ref, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("stash: parsing server URL %q: %w", raw, err)
	}
	base, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, fmt.Errorf("stash: parsing server URL %q: %w", c.baseURL, err)
	}
	return base.ResolveReference(&url.URL{Path: ref.Path, RawQuery: ref.RawQuery}), nil
}

// explainTimeout names the cause when the HTTP client's own timeout, rather
// than the caller's context, is what killed a transfer. The two have opposite
// fixes and are otherwise hard to tell apart: net/http reports its own
// timeout as a wrapped context.DeadlineExceeded too, so what separates them
// is whether the caller's context is the one that expired.
func (c *Client) explainTimeout(ctx context.Context, err error) error {
	if ctx.Err() != nil {
		return err
	}
	var uerr *url.Error
	if errors.As(err, &uerr) && uerr.Timeout() && c.http.Timeout > 0 {
		return fmt.Errorf("%w (the HTTP client's %s timeout covers the whole transfer; pass one without a timeout via WithHTTPClient for backups)", err, c.http.Timeout)
	}
	return err
}
