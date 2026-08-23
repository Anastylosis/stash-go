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

func generateInput(t *testing.T, req graphqlRequest) map[string]any {
	t.Helper()
	b, err := json.Marshal(req.Variables["input"])
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decoding input: %v", err)
	}
	return out
}

// The same reason MetadataScan generates nothing by default: a generate
// across a library is hours of work and gigabytes of output.
func TestMetadataGenerateProducesNothingByDefault(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"metadataGenerate":"12"}}`))
	defer srv.Close()

	id, err := NewClient(srv.URL).MetadataGenerate(context.Background(), GenerateOptions{})
	if err != nil {
		t.Fatalf("MetadataGenerate: %v", err)
	}
	if id != "12" {
		t.Errorf("job id = %q", id)
	}
	in := generateInput(t, cap.reqs[0])
	for _, flag := range []string{"covers", "sprites", "phashes", "previews", "transcodes", "overwrite"} {
		if v, ok := in[flag]; !ok || v != false {
			t.Errorf("%s = %v (present=%v), want false", flag, v, ok)
		}
	}
}

// An empty list is not the same request as no list — Stash reads the second
// as "the whole library".
func TestMetadataGenerateOmitsEmptyScopes(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"metadataGenerate":"1"}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).MetadataGenerate(context.Background(),
		GenerateOptions{Sprites: true}); err != nil {
		t.Fatalf("MetadataGenerate: %v", err)
	}
	in := generateInput(t, cap.reqs[0])
	if _, ok := in["sceneIDs"]; ok {
		t.Error("sceneIDs was sent empty")
	}
	if _, ok := in["paths"]; ok {
		t.Error("paths was sent empty")
	}
	if in["sprites"] != true {
		t.Errorf("sprites = %v", in["sprites"])
	}
}

func TestMetadataGenerateScopedToScenes(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"metadataGenerate":"1"}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).MetadataGenerate(context.Background(),
		GenerateOptions{Phashes: true, SceneIDs: []string{"1", "2"}}); err != nil {
		t.Fatalf("MetadataGenerate: %v", err)
	}
	ids, _ := generateInput(t, cap.reqs[0])["sceneIDs"].([]any)
	if len(ids) != 2 {
		t.Errorf("sceneIDs = %v", ids)
	}
}

// An endpoint is a stash-box; anything else is a scraper id, and sending one
// as the other silently identifies against nothing.
func TestMetadataIdentifySortsSourcesByShape(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"metadataIdentify":"3"}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).MetadataIdentify(context.Background(),
		IdentifyOptions{Sources: []string{"https://example.test/graphql", "builtin_scraper"}}); err != nil {
		t.Fatalf("MetadataIdentify: %v", err)
	}
	b, _ := json.Marshal(generateInput(t, cap.reqs[0])["sources"])
	if !strings.Contains(string(b), "stash_box_endpoint") {
		t.Errorf("sources = %s, want the endpoint recognised", b)
	}
	if !strings.Contains(string(b), "scraper_id") {
		t.Errorf("sources = %s, want the scraper recognised", b)
	}
}

// Clean deletes the records of files it cannot find, and an unmounted drive
// looks exactly like a library whose files were all deleted.
func TestMetadataCleanCarriesDryRun(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"metadataClean":"4"}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).MetadataClean(context.Background(),
		CleanOptions{DryRun: true}); err != nil {
		t.Fatalf("MetadataClean: %v", err)
	}
	if got := generateInput(t, cap.reqs[0])["dryRun"]; got != true {
		t.Errorf("dryRun = %v", got)
	}
}

// Auto-tag with nothing to match against is a job that does nothing, and
// starting one is not what the caller meant.
func TestMetadataAutoTagRefusesAnEmptyRequest(t *testing.T) {
	_, c := server(t, reply(`{"data":{"metadataAutoTag":"1"}}`))
	if _, err := c.MetadataAutoTag(context.Background(), AutoTagOptions{}); err == nil {
		t.Error("want an error when there is nothing to match against")
	}
}

func TestStopJob(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"stopJob":true}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if err := c.StopJob(context.Background(), "7"); err != nil {
		t.Fatalf("StopJob: %v", err)
	}
	if got := cap.reqs[0].Variables["id"]; got != "7" {
		t.Errorf("id = %v", got)
	}
	if err := c.StopJob(context.Background(), ""); err == nil {
		t.Error("want an error without a job id")
	}
}

func TestOptimiseDatabaseReturnsAJobID(t *testing.T) {
	_, c := server(t, reply(`{"data":{"optimiseDatabase":"9"}}`))
	id, err := c.OptimiseDatabase(context.Background())
	if err != nil || id != "9" {
		t.Errorf("got (%q, %v)", id, err)
	}
}

func TestStopAllJobs(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"stopAllJobs":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).StopAllJobs(context.Background()); err != nil {
		t.Fatalf("StopAllJobs: %v", err)
	}
	if !strings.Contains(cap.reqs[0].Query, "stopAllJobs") {
		t.Errorf("query = %s", cap.reqs[0].Query)
	}
}

func TestStopAllJobsReportsFailure(t *testing.T) {
	_, c := server(t, reply(`{"errors":[{"message":"nope"}]}`))
	if err := c.StopAllJobs(context.Background()); err == nil {
		t.Error("want the server's error")
	}
}
