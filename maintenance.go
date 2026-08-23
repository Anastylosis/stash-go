package stash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

// Migrate runs the database schema migration a server in
// [SystemNeedsMigration] is waiting for, writing a copy of the old database
// to backupPath first. An empty backupPath skips that copy.
//
// Three things make this unlike the rest of the package. It is *irreversible*
// — a migrated database cannot be opened by the older Stash that wrote it,
// which is what the backup is for. It runs synchronously rather than as a
// job, so it holds the request open for as long as the migration takes; on a
// large library that is minutes, and the default HTTP client gives up after
// thirty seconds while the server carries on regardless (pass one without a
// timeout via [WithHTTPClient] and bound it with ctx). And a server in this
// state answers almost nothing else: [Client.SystemStatus] and this are what
// work.
//
// backupPath is a path on the *server's* filesystem, in the server's own
// notation.
func (c *Client) Migrate(ctx context.Context, backupPath string) error {
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: MigrateInput!) { migrate(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{"backupPath": backupPath}},
	})
	if err != nil {
		return fmt.Errorf("stash: migrating the database: %w", c.explainTimeout(ctx, err))
	}
	return nil
}

// MigrateBlobs moves blob data — covers, images, and the rest — between the
// database and the filesystem, following whatever the blobsPath setting now
// says. It returns the id of the job doing it; follow it with
// [Client.FindJob].
//
// deleteOld removes each blob from where it came from once it has been
// written to where it is going. With it false the data exists in both places
// afterwards, which is the safe way round and the reason it is not the
// default: a migration that turns out to have gone wrong is then still
// undoable by putting the setting back.
func (c *Client) MigrateBlobs(ctx context.Context, deleteOld bool) (jobID string, err error) {
	id, err := c.startJob(ctx, "migrateBlobs", "MigrateBlobsInput", map[string]any{"deleteOld": deleteOld})
	if err != nil {
		return "", err
	}
	return id, nil
}

// MigrateHashNaming renames generated files — sprites, previews, covers — from
// the MD5 naming an old Stash used to the oshash naming it uses now, and
// returns the id of the job doing it.
//
// Only a library that predates the change has anything to rename; on
// everything else the job runs and finds nothing. It is not reversible, and
// while it runs the generated files it has not reached yet are the ones the
// UI cannot find.
func (c *Client) MigrateHashNaming(ctx context.Context) (jobID string, err error) {
	id, err := c.simpleJob(ctx, "migrateHashNaming")
	if err != nil {
		return "", fmt.Errorf("stash: migrating hash naming: %w", err)
	}
	return id, nil
}

// ScreenshotMigration configures [Client.MigrateSceneScreenshots].
type ScreenshotMigration struct {
	// DeleteFiles removes each screenshot file once it has been read into
	// the database as a blob.
	DeleteFiles bool
	// OverwriteExisting replaces a cover the scene already has. Off, a
	// scene with a cover keeps it and the file on disk is ignored.
	OverwriteExisting bool
}

// MigrateSceneScreenshots reads the loose screenshot files an older Stash
// wrote next to its generated content and stores them as scene covers,
// returning the id of the job doing it.
//
// This is the one-off that follows an upgrade past the release where covers
// stopped being files. A library that never had those files has nothing to
// migrate.
func (c *Client) MigrateSceneScreenshots(ctx context.Context, opts ScreenshotMigration) (jobID string, err error) {
	id, err := c.startJob(ctx, "migrateSceneScreenshots", "MigrateSceneScreenshotsInput", map[string]any{
		"deleteFiles":       opts.DeleteFiles,
		"overwriteExisting": opts.OverwriteExisting,
	})
	if err != nil {
		return "", err
	}
	return id, nil
}

// AnonymiseDatabase writes a copy of the database with every name, path, URL
// and free-text field stripped, and returns the path it wrote.
//
// This is what a bug report attaches: it keeps the shape of a library — the
// counts, the relationships, the schema — and none of what it is a library
// of. The path is on the *server's* filesystem;
// [Client.DownloadAnonymisedDatabase] fetches the copy instead, which is
// usually the point.
//
// It is a copy. Nothing about the live database changes.
func (c *Client) AnonymiseDatabase(ctx context.Context) (serverPath string, err error) {
	path, err := c.anonymise(ctx, false)
	if err != nil {
		return "", fmt.Errorf("stash: anonymising the database: %w", err)
	}
	return path, nil
}

// DownloadAnonymisedDatabase anonymises the database and streams the copy to
// w, returning the server's name for it and the number of bytes written.
//
// The caveats are [Client.DownloadBackup]'s, for the same reasons: the
// transfer is not bounded by [WithMaxResponseBytes], the HTTP client's
// timeout covers the whole of it, and the server leaves its temporary copy
// behind until it restarts.
func (c *Client) DownloadAnonymisedDatabase(ctx context.Context, w io.Writer) (name string, written int64, err error) {
	raw, err := c.anonymise(ctx, true)
	if err != nil {
		return "", 0, fmt.Errorf("stash: anonymising the database: %w", err)
	}
	return c.downloadServerFile(ctx, raw, w, "anonymised database")
}

func (c *Client) anonymise(ctx context.Context, download bool) (string, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: AnonymiseDatabaseInput!) { anonymiseDatabase(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{"download": download}},
	})
	if err != nil {
		return "", err
	}
	var result struct {
		AnonymiseDatabase *string `json:"anonymiseDatabase"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decoding anonymise result: %w", err)
	}
	if result.AnonymiseDatabase == nil || *result.AnonymiseDatabase == "" {
		// Nullable on the wire, so a server that wrote nothing is not an
		// error at the GraphQL level. Say so rather than handing back "".
		return "", errors.New("server reported no anonymised database")
	}
	return *result.AnonymiseDatabase, nil
}

// DownloadFFMpeg has the server fetch ffmpeg and ffprobe for its own platform
// and put them beside its configuration, returning the id of the job doing
// it.
//
// The download is the *server's*, from the internet, and it is what a Stash
// with no system ffmpeg needs before it can generate anything. A server that
// already found ffmpeg on its PATH does not need this, and running it anyway
// gives Stash its own copy to prefer.
func (c *Client) DownloadFFMpeg(ctx context.Context) (jobID string, err error) {
	id, err := c.simpleJob(ctx, "downloadFFMpeg")
	if err != nil {
		return "", fmt.Errorf("stash: downloading ffmpeg: %w", err)
	}
	return id, nil
}

// simpleJob runs a mutation that takes no input and answers with a job id.
func (c *Client) simpleJob(ctx context.Context, mutation string) (string, error) {
	data, err := c.do(ctx, graphqlRequest{Query: `mutation { ` + mutation + ` }`})
	if err != nil {
		return "", err
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decoding job id: %w", err)
	}
	return result[mutation], nil
}
