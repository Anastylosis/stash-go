package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

// scanInput decodes the ScanMetadataInput a MetadataScan call put on the
// wire, which is the part worth pinning: every generate flag is a job that
// can run for hours across a library.
func scanInput(t *testing.T, req graphqlRequest) map[string]any {
	t.Helper()
	raw, ok := req.Variables["input"]
	if !ok {
		t.Fatal("request carried no input variable")
	}
	b, err := json.Marshal(raw)
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decoding input: %v", err)
	}
	return out
}

func TestMetadataScanReturnsJobID(t *testing.T) {
	_, c := server(t, reply(`{"data":{"metadataScan":"42"}}`))
	id, err := c.MetadataScan(context.Background(), ScanOptions{Paths: []string{"/v"}})
	if err != nil {
		t.Fatalf("MetadataScan: %v", err)
	}
	if id != "42" {
		t.Errorf("job id = %q, want 42", id)
	}
}

// The whole point of defaulting every generate flag to off: a library call
// that quietly started generating covers, previews and sprites would be a
// very expensive surprise on a large library.
func TestMetadataScanGeneratesNothingByDefault(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"metadataScan":"1"}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if _, err := c.MetadataScan(context.Background(), ScanOptions{Paths: []string{"/v"}}); err != nil {
		t.Fatalf("MetadataScan: %v", err)
	}
	input := scanInput(t, cap.reqs[0])
	for _, flag := range []string{
		"scanGeneratePhashes", "scanGenerateCovers", "scanGeneratePreviews",
		"scanGenerateSprites", "scanGenerateThumbnails",
	} {
		if v, ok := input[flag]; !ok || v != false {
			t.Errorf("%s = %v (present=%v), want false", flag, v, ok)
		}
	}
	if input["rescan"] != false {
		t.Errorf("rescan = %v, want false", input["rescan"])
	}
}

func TestMetadataScanSendsRequestedFlags(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"metadataScan":"1"}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if _, err := c.MetadataScan(context.Background(), ScanOptions{
		Paths: []string{"/a", "/b"}, Rescan: true, GeneratePhashes: true,
	}); err != nil {
		t.Fatalf("MetadataScan: %v", err)
	}
	input := scanInput(t, cap.reqs[0])
	if input["rescan"] != true || input["scanGeneratePhashes"] != true {
		t.Errorf("rescan=%v phashes=%v, want both true", input["rescan"], input["scanGeneratePhashes"])
	}
	paths, _ := input["paths"].([]any)
	if len(paths) != 2 || paths[0] != "/a" || paths[1] != "/b" {
		t.Errorf("paths = %v, want [/a /b]", input["paths"])
	}
}

// `paths: []` and no paths at all are different requests — Stash reads the
// second as "scan every library path", which on a big library is hours of
// work nobody asked for. An empty slice must therefore be omitted, not sent.
func TestMetadataScanOmitsEmptyPaths(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"metadataScan":"1"}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if _, err := c.MetadataScan(context.Background(), ScanOptions{}); err != nil {
		t.Fatalf("MetadataScan: %v", err)
	}
	if _, present := scanInput(t, cap.reqs[0])["paths"]; present {
		t.Error("an empty Paths was sent as `paths`; it must be omitted entirely")
	}
}

func TestFindJobDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findJob":{"id":"7","status":"RUNNING",
	  "description":"Scanning...","progress":0.25,"addTime":"2026-08-22T10:00:00Z"}}}`))
	job, found, err := c.FindJob(context.Background(), "7")
	if err != nil || !found {
		t.Fatalf("FindJob: %v, found=%v", err, found)
	}
	if job.Status != JobRunning {
		t.Errorf("status = %q, want RUNNING", job.Status)
	}
	if job.Progress == nil || *job.Progress != 0.25 {
		t.Errorf("progress = %v, want 0.25", job.Progress)
	}
}

// Stash drops finished jobs from the queue after a while, so a poll that
// starts late sees exactly what a poll for a job that never existed sees.
// Neither is an error.
func TestFindJobMissingIsNotAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findJob":null}}`))
	job, found, err := c.FindJob(context.Background(), "404")
	if err != nil {
		t.Fatalf("FindJob: %v", err)
	}
	if found || job != nil {
		t.Errorf("got (%v, %v), want (nil, false)", job, found)
	}
}

// Progress is a pointer because Stash reports a negative value while it has
// not worked out a total. Collapsing that to 0 would render "unknown" as
// "just started", which is a progress bar that lies.
func TestFindJobProgressUnknownIsDistinguishable(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findJob":{"id":"7","status":"RUNNING","progress":-1}}}`))
	job, _, err := c.FindJob(context.Background(), "7")
	if err != nil {
		t.Fatalf("FindJob: %v", err)
	}
	if job.Progress == nil || *job.Progress >= 0 {
		t.Errorf("progress = %v, want the negative Stash reported", job.Progress)
	}
}

func TestJobQueueDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"jobQueue":[
	  {"id":"1","status":"RUNNING","description":"a"},
	  {"id":"2","status":"READY","description":"b"}]}}`))
	jobs, err := c.JobQueue(context.Background())
	if err != nil {
		t.Fatalf("JobQueue: %v", err)
	}
	if len(jobs) != 2 || jobs[0].ID != "1" || jobs[1].Status != JobReady {
		t.Errorf("jobs = %+v", jobs)
	}
}

func TestJobQueueEmpty(t *testing.T) {
	_, c := server(t, reply(`{"data":{"jobQueue":null}}`))
	jobs, err := c.JobQueue(context.Background())
	if err != nil {
		t.Fatalf("JobQueue: %v", err)
	}
	if len(jobs) != 0 {
		t.Errorf("jobs = %v, want none", jobs)
	}
}

// Done exists because the terminal set is easy to enumerate incompletely by
// hand, and treating CANCELLED as still-running turns a poll into a hang.
func TestJobStatusDone(t *testing.T) {
	for _, s := range []JobStatus{JobFinished, JobCancelled, JobFailed} {
		if !s.Done() {
			t.Errorf("%s.Done() = false, want true", s)
		}
	}
	for _, s := range []JobStatus{JobReady, JobRunning, JobStopping} {
		if s.Done() {
			t.Errorf("%s.Done() = true, want false", s)
		}
	}
	if JobStatus("SOMETHING_NEW").Done() {
		t.Error("an unknown status reported Done; a poll must keep waiting rather than stop early")
	}
}

func TestMetadataScanSurfacesServerError(t *testing.T) {
	_, c := server(t, reply(`{"errors":[{"message":"no such path"}]}`))
	if _, err := c.MetadataScan(context.Background(), ScanOptions{Paths: []string{"/nope"}}); err == nil {
		t.Error("MetadataScan = nil error, want the server's rejection")
	} else if !strings.Contains(err.Error(), "no such path") {
		t.Errorf("error = %v, want it to carry the server's message", err)
	}
}
