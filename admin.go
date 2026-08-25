package stash

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// SystemState is what the server says about its own readiness. Stash's own
// vocabulary, not a normalisation of it.
type SystemState string

const (
	// SystemOK means the server is set up, migrated and serving.
	SystemOK SystemState = "OK"
	// SystemSetup means Stash has no configuration file yet and is showing
	// its setup wizard. Nothing in the library API answers usefully.
	SystemSetup SystemState = "SETUP"
	// SystemNeedsMigration means the database on disk is older than the
	// binary reading it. Stash refuses to touch a library in that state
	// until [Client.Migrate] runs.
	SystemNeedsMigration SystemState = "NEEDS_MIGRATION"
)

// SystemStatus is the server's account of itself: what state it is in, which
// schema version its database is at, and where it keeps both.
//
// The fields are the ones every supported server has. Stash has added others
// since (the operating system, the working and home directories, the resolved
// ffmpeg and ffprobe paths), and naming one here would fail the whole query
// against a server that lacks it — reach those through [Client.Execute].
type SystemStatus struct {
	Status SystemState `json:"status"`
	// DatabaseSchema is the version of the database on disk, and is nil on
	// a server that has none yet — which is what SETUP means.
	DatabaseSchema *int `json:"databaseSchema"`
	// AppSchema is the version the running binary expects. It being ahead
	// of DatabaseSchema is exactly the NEEDS_MIGRATION condition.
	AppSchema    int    `json:"appSchema"`
	DatabasePath string `json:"databasePath"`
	ConfigPath   string `json:"configPath"`
}

// Ready reports whether the server is in a state where the rest of this
// package will work. A server mid-setup or awaiting migration answers
// queries, but answers them with errors.
func (s SystemStatus) Ready() bool { return s.Status == SystemOK }

// SystemStatus asks the server what state it is in.
//
// Worth a call before a long unattended run: [Client.Ping] succeeds against a
// server that is showing its setup wizard or refusing to open an unmigrated
// database, because answering "SETUP" is itself a successful answer. This is
// how the two are told apart.
func (c *Client) SystemStatus(ctx context.Context) (SystemStatus, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ systemStatus { status databaseSchema appSchema databasePath configPath } }`,
	})
	if err != nil {
		return SystemStatus{}, fmt.Errorf("stash: reading system status: %w", err)
	}
	var result struct {
		SystemStatus SystemStatus `json:"systemStatus"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return SystemStatus{}, fmt.Errorf("stash: decoding system status: %w", err)
	}
	return result.SystemStatus, nil
}

// ServerVersion is the running build.
type ServerVersion struct {
	// Version is the release tag ("v0.31.1"), and is empty on a binary
	// built from source outside a release — where Hash is what identifies
	// it.
	Version   string `json:"version"`
	Hash      string `json:"hash"`
	BuildTime string `json:"build_time"`
}

// ServerVersion returns the build the server is running, hash and build time
// included. [Client.Version] is the same call when only the version string is
// wanted.
func (c *Client) ServerVersion(ctx context.Context) (ServerVersion, error) {
	data, err := c.do(ctx, graphqlRequest{Query: `{ version { version hash build_time } }`})
	if err != nil {
		return ServerVersion{}, fmt.Errorf("stash: reading version: %w", err)
	}
	var result struct {
		Version ServerVersion `json:"version"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return ServerVersion{}, fmt.Errorf("stash: decoding version: %w", err)
	}
	return result.Version, nil
}

// LatestVersion returns the newest release Stash knows of, as a short commit
// hash and the URL to it.
//
// The *server* fetches this from GitHub when the call is made. It therefore
// fails when the server has no route to the internet — which is not the same
// thing as this program having none — and it is slower than a local query.
// Nothing here compares it to [Client.ServerVersion]: the two are a tag and a
// hash, and only the server knows whether it is behind.
func (c *Client) LatestVersion(ctx context.Context) (shortHash, url string, err error) {
	data, err := c.do(ctx, graphqlRequest{Query: `{ latestversion { shorthash url } }`})
	if err != nil {
		return "", "", fmt.Errorf("stash: reading latest version: %w", err)
	}
	var result struct {
		Latest struct {
			ShortHash string `json:"shorthash"`
			URL       string `json:"url"`
		} `json:"latestversion"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", "", fmt.Errorf("stash: decoding latest version: %w", err)
	}
	return result.Latest.ShortHash, result.Latest.URL, nil
}

// LogEntry is one line from the server's log.
type LogEntry struct {
	Time string `json:"time"`
	// Level is Stash's own word for it: "Trace", "Debug", "Info",
	// "Progress", "Warning" or "Error".
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Logs returns the server's recent log entries, newest first.
//
// This is not the log file. Stash keeps a bounded in-memory ring of the last
// few hundred entries and serves that, so a server restarted since the event
// has nothing to say about it, and a busy one has already dropped it. For
// anything that must not be missed, read the file the server's logFile
// setting names — [Client.GeneralConfig] reports where that is.
//
// There is no way to follow the log from here: Stash streams new entries over
// a GraphQL subscription, which is a websocket this package does not open.
func (c *Client) Logs(ctx context.Context) ([]LogEntry, error) {
	data, err := c.do(ctx, graphqlRequest{Query: `{ logs { time level message } }`})
	if err != nil {
		return nil, fmt.Errorf("stash: reading logs: %w", err)
	}
	var result struct {
		Logs []LogEntry `json:"logs"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding logs: %w", err)
	}
	return result.Logs, nil
}

// GeneralConfig returns the named fields of the server's general
// configuration — the section holding library paths, the database and blob
// locations, ffmpeg settings, the log file and the rest of Settings > System.
//
// The caller names the fields for the reason [Client.InterfaceConfig] gives:
// the section is large, it changes between releases, and one field the schema
// lacks fails the whole query rather than just that field.
//
//	cfg, err := c.GeneralConfig(ctx, "databasePath", "blobsPath", "logFile")
//
// [Client.StashBoxConfigs] is the way to read stashBoxes: it is the one field
// in here that carries credentials, and it has a type of its own.
func (c *Client) GeneralConfig(ctx context.Context, fields ...string) (map[string]any, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("stash: reading general configuration: no fields requested")
	}
	for _, f := range fields {
		if !isFieldName(f) {
			return nil, fmt.Errorf("stash: reading general configuration: %q is not a field name", f)
		}
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ configuration { general { ` + strings.Join(fields, " ") + ` } } }`,
	})
	if err != nil {
		return nil, fmt.Errorf("stash: reading general configuration: %w", err)
	}
	var result struct {
		Configuration struct {
			General map[string]any `json:"general"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding general configuration: %w", err)
	}
	return result.Configuration.General, nil
}

// ConfigureGeneral writes the given general settings and leaves the rest
// alone: Stash applies only the keys present in the input.
//
// Keys are the ConfigGeneralInput field names ("logLevel", "maxSessionAge",
// "previewSegments", …), unmodelled here for the reason
// [Client.Configuration] gives.
//
// Two things this cannot do gently. A list-valued field is *replaced* rather
// than extended — sending one entry of "stashes" makes it the only library
// path Stash has, and [Client.SetStashBoxes] exists because that same trap
// costs API keys. And several of these are how the server reaches its own
// data: point databasePath or generatedPath somewhere new and Stash starts
// afresh there rather than moving anything. Read a field before writing it.
func (c *Client) ConfigureGeneral(ctx context.Context, settings map[string]any) error {
	if len(settings) == 0 {
		return nil
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ConfigGeneralInput!) { configureGeneral(input: $input) { __typename } }`,
		Variables: map[string]any{"input": settings},
	})
	if err != nil {
		return fmt.Errorf("stash: configuring general settings: %w", err)
	}
	return nil
}

// GenerateAPIKey replaces the server's API key and returns the new one.
//
// There is exactly one key per Stash, so this invalidates the old one — and
// that includes the key this client is authenticating with, which stops
// working the moment the mutation returns. The new key does not apply itself:
// build a fresh client with it.
//
//	key, err := c.GenerateAPIKey(ctx)
//	c = stash.NewClient(url, stash.WithAPIKey(key))
//
// The returned key is a credential in a variable rather than in an error
// string, so nothing redacts it for you — [WithAPIKey]'s scrubbing
// covers the key a client was built with, not one it has just been handed.
//
// A server with authentication disabled — no username and password
// configured — refuses this: Stash will not hand out a key that would then be
// the only thing standing in front of the library, or not stand there at all.
func (c *Client) GenerateAPIKey(ctx context.Context) (string, error) {
	key, err := c.apiKeyMutation(ctx, false)
	if err != nil {
		return "", fmt.Errorf("stash: generating an API key: %w", err)
	}
	return key, nil
}

// ClearAPIKey removes the server's API key without issuing a new one. Clients
// authenticating with it — this one included — stop working; a session login
// still does.
func (c *Client) ClearAPIKey(ctx context.Context) error {
	if _, err := c.apiKeyMutation(ctx, true); err != nil {
		return fmt.Errorf("stash: clearing the API key: %w", err)
	}
	return nil
}

func (c *Client) apiKeyMutation(ctx context.Context, revoke bool) (string, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: GenerateAPIKeyInput!) { generateAPIKey(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{"clear": revoke}},
	})
	if err != nil {
		return "", err
	}
	var result struct {
		Key string `json:"generateAPIKey"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("decoding the API key: %w", err)
	}
	return result.Key, nil
}

// LibraryStats is what the server counts about the library as a whole. Every
// field is the server's own tally, computed from the database rather than by
// walking the scenes.
//
// The counts cover what Stash has indexed, which is not the same as what is
// on disk: a file the last scan did not reach is not here.
type LibraryStats struct {
	SceneCount int `json:"scene_count"`
	// ScenesSize is the total size of every scene's files, in bytes.
	ScenesSize float64 `json:"scenes_size"`
	// ScenesDuration is the total runtime of every scene, in seconds.
	ScenesDuration float64 `json:"scenes_duration"`
	ImageCount     int     `json:"image_count"`
	ImagesSize     float64 `json:"images_size"`
	GalleryCount   int     `json:"gallery_count"`
	PerformerCount int     `json:"performer_count"`
	StudioCount    int     `json:"studio_count"`
	GroupCount     int     `json:"group_count"`
	TagCount       int     `json:"tag_count"`

	// TotalPlayDuration is how long scenes have been watched for in total,
	// in seconds, and ScenesPlayed how many have been watched at all.
	TotalOCount       int     `json:"total_o_count"`
	TotalPlayCount    int     `json:"total_play_count"`
	TotalPlayDuration float64 `json:"total_play_duration"`
	ScenesPlayed      int     `json:"scenes_played"`
}

// LibraryStats reports the server's own counts for the library.
//
// This is the cheap way to size a library before doing anything with it: one
// query, answered from the database, where counting the same thing through
// [Client.FindScenes] would page through every scene.
func (c *Client) LibraryStats(ctx context.Context) (LibraryStats, error) {
	data, err := c.do(ctx, graphqlRequest{Query: `{ stats {
		scene_count scenes_size scenes_duration
		image_count images_size gallery_count
		performer_count studio_count group_count tag_count
		total_o_count total_play_count total_play_duration scenes_played
	} }`})
	if err != nil {
		return LibraryStats{}, fmt.Errorf("stash: library stats: %w", err)
	}
	var result struct {
		Stats LibraryStats `json:"stats"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return LibraryStats{}, fmt.Errorf("stash: decoding library stats: %w", err)
	}
	return result.Stats, nil
}
