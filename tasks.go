package stash

import (
	"context"
	"encoding/json"
	"fmt"
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
