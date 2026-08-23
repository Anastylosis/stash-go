package stash

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ScanOptions selects what [Client.MetadataScan] scans and what it
// generates while it does.
//
// Every generate flag defaults to off. That is not Stash's own default —
// the UI remembers whatever was ticked last — but a library call that
// quietly started generating covers, previews and sprites across a library
// would be a very expensive surprise. Ask for what you want.
type ScanOptions struct {
	// Paths restricts the scan to these library paths. Empty scans every
	// configured library path, which on a large library is an hours-long
	// job — pass the directory you actually changed.
	Paths []string
	// Rescan re-reads files Stash has already indexed rather than only
	// picking up new ones.
	Rescan bool
	// GeneratePhashes computes perceptual hashes for newly scanned files.
	// Worth setting for anything that matches on content rather than
	// filename: without a phash, only byte-identical files can be matched.
	GeneratePhashes    bool
	GenerateCovers     bool
	GeneratePreviews   bool
	GenerateSprites    bool
	GenerateThumbnails bool
}

// MetadataScan starts a library scan and returns the id of the job doing
// it. It does not wait: scanning is a long-running background task, and the
// id is how you follow it with [Client.FindJob].
//
// This is the only way to make Stash notice a file that appeared on disk.
// Captions in particular are read-only in GraphQL, so writing a sidecar
// next to a video and calling this is the entire mechanism for attaching a
// subtitle.
func (c *Client) MetadataScan(ctx context.Context, opts ScanOptions) (jobID string, err error) {
	input := map[string]any{
		"rescan":                 opts.Rescan,
		"scanGeneratePhashes":    opts.GeneratePhashes,
		"scanGenerateCovers":     opts.GenerateCovers,
		"scanGeneratePreviews":   opts.GeneratePreviews,
		"scanGenerateSprites":    opts.GenerateSprites,
		"scanGenerateThumbnails": opts.GenerateThumbnails,
	}
	// Omitted rather than sent empty: `paths: []` is not the same request
	// as no paths at all, and Stash reads the second as "everything".
	if len(opts.Paths) > 0 {
		input["paths"] = opts.Paths
	}

	data, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ScanMetadataInput!) { metadataScan(input: $input) }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return "", fmt.Errorf("stash: starting metadata scan: %w", err)
	}
	var result struct {
		MetadataScan string `json:"metadataScan"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding scan job id: %w", err)
	}
	return result.MetadataScan, nil
}

// JobStatus is where a job has got to. Stash's own vocabulary, not a
// normalisation of it.
type JobStatus string

const (
	JobReady     JobStatus = "READY"
	JobRunning   JobStatus = "RUNNING"
	JobFinished  JobStatus = "FINISHED"
	JobStopping  JobStatus = "STOPPING"
	JobCancelled JobStatus = "CANCELLED"
	JobFailed    JobStatus = "FAILED"
)

// Done reports whether the job has stopped, however it stopped. Useful as
// a poll condition: the three terminal states are easy to enumerate
// incompletely by hand, and treating CANCELLED as still-running is a hang.
func (s JobStatus) Done() bool {
	switch s {
	case JobFinished, JobCancelled, JobFailed:
		return true
	}
	return false
}

// Job is one entry in Stash's task queue.
type Job struct {
	ID          string    `json:"id"`
	Status      JobStatus `json:"status"`
	Description string    `json:"description"`
	// Progress is 0..1 while running, and negative when Stash has not
	// worked out a total yet — not 0, which would render as "just started"
	// rather than "unknown".
	Progress  *float64 `json:"progress"`
	Error     *string  `json:"error"`
	AddTime   string   `json:"addTime"`
	StartTime *string  `json:"startTime"`
	EndTime   *string  `json:"endTime"`
}

const jobFields = `id status description progress error addTime startTime endTime`

// FindJob returns one job by id. found is false when the job has aged out
// of the queue, which is not an error — Stash drops finished jobs after a
// while, so a poll that starts too late sees the same thing as a poll for a
// job that never existed.
func (c *Client) FindJob(ctx context.Context, id string) (job *Job, found bool, err error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `query($input: FindJobInput!) { findJob(input: $input) {` + jobFields + `} }`,
		Variables: map[string]any{"input": map[string]any{"id": id}},
	})
	if err != nil {
		return nil, false, fmt.Errorf("stash: finding job %s: %w", id, err)
	}
	var result struct {
		FindJob *Job `json:"findJob"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("stash: decoding job %s: %w", id, err)
	}
	if result.FindJob == nil {
		return nil, false, nil
	}
	return result.FindJob, true, nil
}

// JobQueue returns every job Stash currently knows about, queued or
// running. Empty when the server is idle.
func (c *Client) JobQueue(ctx context.Context) ([]Job, error) {
	data, err := c.do(ctx, graphqlRequest{Query: `{ jobQueue {` + jobFields + `} }`})
	if err != nil {
		return nil, fmt.Errorf("stash: reading job queue: %w", err)
	}
	var result struct {
		JobQueue []Job `json:"jobQueue"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding job queue: %w", err)
	}
	return result.JobQueue, nil
}

// GenerateOptions selects what [Client.MetadataGenerate] produces.
//
// Every flag defaults to off, for the reason [ScanOptions] gives: generating
// across a library is hours of work and gigabytes of output, and a library
// call that quietly started doing it would be an expensive surprise. Ask for
// what you want.
type GenerateOptions struct {
	// Covers, Sprites and Phashes are the three that other work depends on.
	// A scene with no sprite cannot be read for a title card; a scene with
	// no phash cannot be matched against a stash-box.
	Covers  bool
	Sprites bool
	Phashes bool

	Previews      bool
	ImagePreviews bool
	Markers       bool
	Transcodes    bool
	// ForceTranscodes re-encodes even where a transcode already exists.
	ForceTranscodes           bool
	InteractiveHeatmapsSpeeds bool
	ImagePhashes              bool
	ImageThumbnails           bool
	ClipPreviews              bool

	// SceneIDs restricts the job to these scenes; Paths to these library
	// paths. Both empty means the whole library.
	SceneIDs []string
	Paths    []string

	// Overwrite regenerates what is already there. Without it Stash skips
	// anything it has, which is what makes a second run cheap.
	Overwrite bool
}

// MetadataGenerate starts a generate job and returns the id of the job doing
// it. It does not wait; follow it with [Client.FindJob].
//
// This is how a scene gets the sprite, cover or perceptual hash that other
// work needs and a plain scan does not produce.
func (c *Client) MetadataGenerate(ctx context.Context, opts GenerateOptions) (jobID string, err error) {
	input := map[string]any{
		"covers":                    opts.Covers,
		"sprites":                   opts.Sprites,
		"phashes":                   opts.Phashes,
		"previews":                  opts.Previews,
		"imagePreviews":             opts.ImagePreviews,
		"markers":                   opts.Markers,
		"transcodes":                opts.Transcodes,
		"forceTranscodes":           opts.ForceTranscodes,
		"interactiveHeatmapsSpeeds": opts.InteractiveHeatmapsSpeeds,
		"imagePhashes":              opts.ImagePhashes,
		"imageThumbnails":           opts.ImageThumbnails,
		"clipPreviews":              opts.ClipPreviews,
		"overwrite":                 opts.Overwrite,
	}
	// Omitted rather than sent empty, the same way MetadataScan handles
	// paths: an empty list is not the same request as no list, and Stash
	// reads the second as "everything".
	if len(opts.SceneIDs) > 0 {
		input["sceneIDs"] = opts.SceneIDs
	}
	if len(opts.Paths) > 0 {
		input["paths"] = opts.Paths
	}
	return c.startJob(ctx, "metadataGenerate", "GenerateMetadataInput", input)
}

// IdentifyOptions configures [Client.MetadataIdentify], Stash's own matching
// of scenes against a stash-box.
type IdentifyOptions struct {
	// Sources are the stash-box endpoints or scraper ids to try, in order.
	// Empty uses whatever the server has configured as its defaults.
	Sources []string
	// SceneIDs restricts the job to these scenes; Paths to these library
	// paths. Both empty means everything.
	SceneIDs []string
	Paths    []string
}

// MetadataIdentify starts an identify job and returns its id.
//
// Identify is a *writing* task: it matches scenes against a stash-box and
// applies what it finds, according to the field rules configured on the
// server. Those rules decide whether it overwrites what is already there, and
// this call cannot see them — check them before starting one on a library
// whose metadata you care about.
func (c *Client) MetadataIdentify(ctx context.Context, opts IdentifyOptions) (jobID string, err error) {
	sources := make([]map[string]any, 0, len(opts.Sources))
	for _, s := range opts.Sources {
		source := map[string]any{}
		// An endpoint is a stash-box; anything else is a scraper id.
		if strings.Contains(s, "://") {
			source["stash_box_endpoint"] = s
		} else {
			source["scraper_id"] = s
		}
		sources = append(sources, map[string]any{"source": source})
	}
	input := map[string]any{}
	if len(sources) > 0 {
		input["sources"] = sources
	}
	if len(opts.SceneIDs) > 0 {
		input["sceneIDs"] = opts.SceneIDs
	}
	if len(opts.Paths) > 0 {
		input["paths"] = opts.Paths
	}
	return c.startJob(ctx, "metadataIdentify", "IdentifyMetadataInput", input)
}

// CleanOptions configures [Client.MetadataClean].
type CleanOptions struct {
	// Paths restricts the clean to these library paths.
	Paths []string
	// DryRun makes Stash report what it would remove without removing it.
	// Worth using first: clean deletes database records for files it cannot
	// find, and a library on a disconnected drive looks exactly like a
	// library whose files were deleted.
	DryRun bool
	// IgnoreZipFileContents skips files inside zips.
	IgnoreZipFileContents bool
}

// MetadataClean starts a clean job and returns its id.
//
// Clean removes the records of files that are no longer on disk. That is
// destructive and depends on the disk being readable at the time: an
// unmounted drive presents as a library whose files have all been deleted.
// Use DryRun first.
func (c *Client) MetadataClean(ctx context.Context, opts CleanOptions) (jobID string, err error) {
	input := map[string]any{
		"dryRun":                opts.DryRun,
		"ignoreZipFileContents": opts.IgnoreZipFileContents,
	}
	if len(opts.Paths) > 0 {
		input["paths"] = opts.Paths
	}
	return c.startJob(ctx, "metadataClean", "CleanMetadataInput", input)
}

// AutoTagOptions configures [Client.MetadataAutoTag].
//
// Each list names what to match against, and "*" means all of that kind —
// which is what the UI's button sends. Empty lists match nothing, so a call
// with none set is a job that does nothing.
type AutoTagOptions struct {
	Paths      []string
	Performers []string
	Studios    []string
	Tags       []string
}

// MetadataAutoTag starts an auto-tag job and returns its id.
//
// Auto-tag attaches performers, studios and tags to scenes whose *path*
// contains their name. That is a guess about filenames, and it writes: on a
// library whose files are named after their content it is useful, and on one
// where a performer is called "Angel" it is not.
func (c *Client) MetadataAutoTag(ctx context.Context, opts AutoTagOptions) (jobID string, err error) {
	input := map[string]any{}
	for key, v := range map[string][]string{
		"paths": opts.Paths, "performers": opts.Performers,
		"studios": opts.Studios, "tags": opts.Tags,
	} {
		if len(v) > 0 {
			input[key] = v
		}
	}
	if len(input) == 0 {
		return "", fmt.Errorf("stash: auto-tag: nothing to match against")
	}
	return c.startJob(ctx, "metadataAutoTag", "AutoTagMetadataInput", input)
}

// StopJob asks Stash to stop one running job. It returns without waiting: the
// job moves to STOPPING and reaches a terminal state in its own time, which
// [Client.FindJob] reports.
func (c *Client) StopJob(ctx context.Context, jobID string) error {
	if jobID == "" {
		return fmt.Errorf("stash: stopping job: no id")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($id: ID!) { stopJob(job_id: $id) }`,
		Variables: map[string]any{"id": jobID},
	})
	if err != nil {
		return fmt.Errorf("stash: stopping job %s: %w", jobID, err)
	}
	return nil
}

// StopAllJobs asks Stash to stop everything queued and running.
func (c *Client) StopAllJobs(ctx context.Context) error {
	if _, err := c.do(ctx, graphqlRequest{Query: `mutation { stopAllJobs }`}); err != nil {
		return fmt.Errorf("stash: stopping all jobs: %w", err)
	}
	return nil
}

// OptimiseDatabase starts a database optimisation job and returns its id.
func (c *Client) OptimiseDatabase(ctx context.Context) (jobID string, err error) {
	data, err := c.do(ctx, graphqlRequest{Query: `mutation { optimiseDatabase }`})
	if err != nil {
		return "", fmt.Errorf("stash: optimising the database: %w", err)
	}
	var result struct {
		OptimiseDatabase string `json:"optimiseDatabase"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding job id: %w", err)
	}
	return result.OptimiseDatabase, nil
}

// startJob runs one of the metadata mutations and returns the job id they all
// answer with.
func (c *Client) startJob(ctx context.Context, mutation, inputType string, input map[string]any) (string, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ` + inputType + `!) { ` + mutation + `(input: $input) }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return "", fmt.Errorf("stash: starting %s: %w", mutation, err)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding %s job id: %w", mutation, err)
	}
	return result[mutation], nil
}
