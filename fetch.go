package stash

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// ErrNotFound is what [Client.Fetch] returns for a 404, so a caller can tell
// it from the other failures without inspecting a status code. Stash
// generates sprites, previews and covers lazily, which makes a missing one an
// ordinary state of a scene rather than a reason to stop.
var ErrNotFound = errors.New("stash: not found")

// Fetch streams one of the server's plain HTTP resources to w and returns its
// content type and length.
//
// Scenes carry URLs to things GraphQL will not hand over as data — the sprite
// sheet, its WebVTT, the cover, the stream — and those routes want the same
// credential as /graphql. This applies it, and re-roots the URL the way
// [Client.DownloadBackup] does, so a URL the server built from a proxied
// request still resolves to the address this client was given.
//
// url may be absolute or a path. As with the backup download, neither
// [WithMaxResponseBytes] nor a short HTTP client timeout suits a stream; bound
// it with ctx.
func (c *Client) Fetch(ctx context.Context, url string, w io.Writer) (contentType string, n int64, err error) {
	loc, err := c.resolveServerURL(url)
	if err != nil {
		return "", 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, loc.String(), nil)
	if err != nil {
		return "", 0, fmt.Errorf("stash: building request for %s: %w", loc, err)
	}
	c.authorize(req)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("stash: fetching %s: %w", loc.Path, c.explainTimeout(ctx, c.redact(err)))
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode == http.StatusNotFound {
		return "", 0, fmt.Errorf("stash: fetching %s: %w", loc.Path, ErrNotFound)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return "", 0, &HTTPError{
			StatusCode: resp.StatusCode,
			Status:     resp.Status,
			Body:       c.redactString(strings.TrimSpace(string(b))),
		}
	}

	contentType = resp.Header.Get("Content-Type")
	n, err = io.Copy(w, resp.Body)
	if err != nil {
		return contentType, n, fmt.Errorf("stash: fetching %s: %w", loc.Path, c.explainTimeout(ctx, c.redact(err)))
	}
	return contentType, n, nil
}
