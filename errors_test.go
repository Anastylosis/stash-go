package stash

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// call is one client method reduced to "did it report a problem".
type call struct {
	name string
	run  func(context.Context, *Client) error
	// bodyBlind marks a call that reads bytes rather than GraphQL, and so
	// judges a response by its status alone. Fetch streams a scene's sprite
	// or cover; a 200 carrying anything at all is a successful fetch.
	bodyBlind bool
}

// everyCall is every method that talks to the server, in the shape needed to
// ask one question of all of them at once.
//
// The arguments are whatever satisfies the up-front validation; what happens
// on the wire is the same for all of them, and that is what is being tested.
func everyCall() []call {
	no := false
	return []call{
		{name: "Ping", run: func(ctx context.Context, c *Client) error { return c.Ping(ctx) }},
		{name: "Version", run: func(ctx context.Context, c *Client) error { _, err := c.Version(ctx); return err }},
		{name: "Supports", run: func(ctx context.Context, c *Client) error { _, err := c.Supports(ctx, "captions"); return err }},
		{name: "FindScene", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindScene(ctx, "1"); return err }},
		{name: "FindScenes", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.FindScenes(ctx, SceneFilter{}, 1, 10)
			return err
		}},
		{name: "UpdateScene", run: func(ctx context.Context, c *Client) error { return c.UpdateScene(ctx, SceneUpdate{ID: "1"}) }},
		{name: "ClearSceneFields", run: func(ctx context.Context, c *Client) error { return c.ClearSceneFields(ctx, "1", "title") }},
		{name: "SetSceneStashIDs", run: func(ctx context.Context, c *Client) error { return c.SetSceneStashIDs(ctx, "1", nil) }},
		{name: "ScenePaths", run: func(ctx context.Context, c *Client) error { _, err := c.ScenePaths(ctx, "1"); return err }},
		{name: "Configuration", run: func(ctx context.Context, c *Client) error { _, err := c.Configuration(ctx); return err }},
		{name: "PluginSettings", run: func(ctx context.Context, c *Client) error { _, err := c.PluginSettings(ctx, "x"); return err }},
		{name: "FindTag", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindTag(ctx, "x"); return err }},
		{name: "CreateTag", run: func(ctx context.Context, c *Client) error { _, err := c.CreateTag(ctx, "x"); return err }},
		{name: "FindPerformer", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindPerformer(ctx, "x"); return err }},
		{name: "CreatePerformer", run: func(ctx context.Context, c *Client) error { _, err := c.CreatePerformer(ctx, "x"); return err }},
		{name: "FindStudio", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindStudio(ctx, "x"); return err }},
		{name: "CreateStudio", run: func(ctx context.Context, c *Client) error { _, err := c.CreateStudio(ctx, "x"); return err }},
		{name: "MetadataScan", run: func(ctx context.Context, c *Client) error { _, err := c.MetadataScan(ctx, ScanOptions{}); return err }},
		{name: "MetadataGenerate", run: func(ctx context.Context, c *Client) error {
			_, err := c.MetadataGenerate(ctx, GenerateOptions{})
			return err
		}},
		{name: "MetadataIdentify", run: func(ctx context.Context, c *Client) error {
			_, err := c.MetadataIdentify(ctx, IdentifyOptions{})
			return err
		}},
		{name: "MetadataClean", run: func(ctx context.Context, c *Client) error { _, err := c.MetadataClean(ctx, CleanOptions{}); return err }},
		{name: "MetadataAutoTag", run: func(ctx context.Context, c *Client) error {
			_, err := c.MetadataAutoTag(ctx, AutoTagOptions{Paths: []string{"/x"}})
			return err
		}},
		{name: "OptimiseDatabase", run: func(ctx context.Context, c *Client) error { _, err := c.OptimiseDatabase(ctx); return err }},
		{name: "StopJob", run: func(ctx context.Context, c *Client) error { return c.StopJob(ctx, "1") }},
		{name: "StopAllJobs", run: func(ctx context.Context, c *Client) error { return c.StopAllJobs(ctx) }},
		{name: "FindJob", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindJob(ctx, "1"); return err }},
		{name: "JobQueue", run: func(ctx context.Context, c *Client) error { _, err := c.JobQueue(ctx); return err }},
		{name: "BackupDatabase", run: func(ctx context.Context, c *Client) error {
			_, err := c.BackupDatabase(ctx, BackupOptions{})
			return err
		}},
		{name: "DownloadBackup", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.DownloadBackup(ctx, BackupOptions{}, io.Discard)
			return err
		}},
		{name: "Fetch", bodyBlind: true,
			run: func(ctx context.Context, c *Client) error { _, _, err := c.Fetch(ctx, "/x", io.Discard); return err }},
		{name: "Plugins", run: func(ctx context.Context, c *Client) error { _, err := c.Plugins(ctx); return err }},
		{name: "SetPluginsEnabled", run: func(ctx context.Context, c *Client) error {
			return c.SetPluginsEnabled(ctx, map[string]bool{"x": true})
		}},
		{name: "ReloadPlugins", run: func(ctx context.Context, c *Client) error { return c.ReloadPlugins(ctx) }},
		{name: "InterfaceConfig", run: func(ctx context.Context, c *Client) error { _, err := c.InterfaceConfig(ctx, "javascript"); return err }},
		{name: "ConfigureInterface", run: func(ctx context.Context, c *Client) error {
			return c.ConfigureInterface(ctx, map[string]any{"javascriptEnabled": true})
		}},
		{name: "PackageSources", run: func(ctx context.Context, c *Client) error { _, err := c.PackageSources(ctx, PackagePlugin); return err }},
		{name: "InstalledPackages", run: func(ctx context.Context, c *Client) error {
			_, err := c.InstalledPackages(ctx, PackagePlugin)
			return err
		}},
		{name: "AvailablePackages", run: func(ctx context.Context, c *Client) error {
			_, err := c.AvailablePackages(ctx, PackagePlugin, "https://x.test/i.yml")
			return err
		}},
		{name: "InstallPackages", run: func(ctx context.Context, c *Client) error {
			_, err := c.InstallPackages(ctx, PackagePlugin, PackageSpec{ID: "x", SourceURL: "https://x.test/i.yml"})
			return err
		}},
		{name: "AddSceneTags", run: func(ctx context.Context, c *Client) error { return c.AddSceneTags(ctx, []string{"1"}, "1") }},
		{name: "AddScenePerformers", run: func(ctx context.Context, c *Client) error { return c.AddScenePerformers(ctx, []string{"1"}, "1") }},
		{name: "FindPerformerByID", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindPerformerByID(ctx, "1"); return err }},
		{name: "FindPerformers", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.FindPerformers(ctx, PerformerFilter{}, 1, 10)
			return err
		}},
		{name: "FindPerformerByStashID", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.FindPerformerByStashID(ctx, "https://x.test/graphql", "abc")
			return err
		}},
		{name: "CreatePerformerFrom", run: func(ctx context.Context, c *Client) error {
			_, err := c.CreatePerformerFrom(ctx, PerformerInput{Name: "x"})
			return err
		}},
		{name: "UpdatePerformer", run: func(ctx context.Context, c *Client) error { return c.UpdatePerformer(ctx, PerformerUpdate{ID: "1"}) }},
		{name: "ClearPerformerFields", run: func(ctx context.Context, c *Client) error { return c.ClearPerformerFields(ctx, "1", "birthdate") }},
		{name: "DeletePerformer", run: func(ctx context.Context, c *Client) error { return c.DeletePerformer(ctx, "1") }},
		{name: "DeletePerformers", run: func(ctx context.Context, c *Client) error { return c.DeletePerformers(ctx, "1") }},
		{name: "MergePerformers", run: func(ctx context.Context, c *Client) error { return c.MergePerformers(ctx, "1", []string{"2"}, nil) }},
		{name: "FindStudioByID", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindStudioByID(ctx, "1"); return err }},
		{name: "Studios", run: func(ctx context.Context, c *Client) error { _, _, err := c.Studios(ctx, 1, 10); return err }},
		{name: "CreateStudioFrom", run: func(ctx context.Context, c *Client) error {
			_, err := c.CreateStudioFrom(ctx, StudioInput{Name: "x"})
			return err
		}},
		{name: "UpdateStudio", run: func(ctx context.Context, c *Client) error { return c.UpdateStudio(ctx, StudioInput{ID: "1"}) }},
		{name: "ClearStudioFields", run: func(ctx context.Context, c *Client) error { return c.ClearStudioFields(ctx, "1", "details") }},
		{name: "DeleteStudio", run: func(ctx context.Context, c *Client) error { return c.DeleteStudio(ctx, "1") }},
		{name: "DeleteStudios", run: func(ctx context.Context, c *Client) error { return c.DeleteStudios(ctx, "1") }},
		{name: "FindTagByID", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindTagByID(ctx, "1"); return err }},
		{name: "Tags", run: func(ctx context.Context, c *Client) error { _, _, err := c.Tags(ctx, 1, 10); return err }},
		{name: "CreateTagFrom", run: func(ctx context.Context, c *Client) error {
			_, err := c.CreateTagFrom(ctx, TagInput{Name: "x"})
			return err
		}},
		{name: "UpdateTag", run: func(ctx context.Context, c *Client) error { return c.UpdateTag(ctx, TagInput{ID: "1"}) }},
		{name: "ClearTagFields", run: func(ctx context.Context, c *Client) error { return c.ClearTagFields(ctx, "1", "description") }},
		{name: "DeleteTag", run: func(ctx context.Context, c *Client) error { return c.DeleteTag(ctx, "1") }},
		{name: "DeleteTags", run: func(ctx context.Context, c *Client) error { return c.DeleteTags(ctx, "1") }},
		{name: "MergeTags", run: func(ctx context.Context, c *Client) error { return c.MergeTags(ctx, "1", []string{"2"}, nil) }},
		{name: "MergeScenes", run: func(ctx context.Context, c *Client) error {
			return c.MergeScenes(ctx, "1", []string{"2"}, nil, MergeOptions{})
		}},
		{name: "DeleteScene", run: func(ctx context.Context, c *Client) error { return c.DeleteScene(ctx, "1", DeleteOptions{}) }},
		{name: "DeleteScenes", run: func(ctx context.Context, c *Client) error { return c.DeleteScenes(ctx, []string{"1"}, DeleteOptions{}) }},
		{name: "AssignFile", run: func(ctx context.Context, c *Client) error { return c.AssignFile(ctx, "1", "2") }},
		{name: "MoveFiles", run: func(ctx context.Context, c *Client) error {
			return c.MoveFiles(ctx, []string{"1"}, MoveTarget{FolderID: "2"})
		}},
		{name: "DestroyFiles", run: func(ctx context.Context, c *Client) error { return c.DestroyFiles(ctx, "1") }},
		{name: "SetFingerprints", run: func(ctx context.Context, c *Client) error {
			return c.SetFingerprints(ctx, "1", []Fingerprint{{Type: "oshash", Value: "abc"}})
		}},
		{name: "FindFile", run: func(ctx context.Context, c *Client) error { _, _, err := c.FindFile(ctx, "1"); return err }},
		{name: "FindFileByPath", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.FindFileByPath(ctx, "/library/a.mp4")
			return err
		}},
		{name: "FindSceneByHash", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.FindSceneByHash(ctx, "oshash", "abc")
			return err
		}},
		{name: "FindScenesByPathRegex", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.FindScenesByPathRegex(ctx, "a", 1, 10)
			return err
		}},
		{name: "StashBoxBatchTag", run: func(ctx context.Context, c *Client) error {
			_, err := c.StashBoxBatchTag(ctx, BatchTagTags, BatchTagOptions{Endpoint: "https://x.test/graphql"})
			return err
		}},
		{name: "StashBoxes", run: func(ctx context.Context, c *Client) error { _, err := c.StashBoxes(ctx); return err }},
		{name: "StashBoxConfigs", run: func(ctx context.Context, c *Client) error { _, err := c.StashBoxConfigs(ctx); return err }},
		{name: "SetStashBoxes", run: func(ctx context.Context, c *Client) error {
			return c.SetStashBoxes(ctx, []StashBoxConfig{{Name: "x", Endpoint: "https://x.test/graphql"}})
		}},
		{name: "ValidateStashBox", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.ValidateStashBox(ctx, "https://x.test/graphql", "k")
			return err
		}},
		{name: "SubmitSceneDraft", run: func(ctx context.Context, c *Client) error {
			_, err := c.SubmitSceneDraft(ctx, "1", "https://x.test/graphql")
			return err
		}},
		{name: "SubmitFingerprints", run: func(ctx context.Context, c *Client) error {
			_, err := c.SubmitFingerprints(ctx, "https://x.test/graphql", "1")
			return err
		}},
		{name: "ScrapePerformers", run: func(ctx context.Context, c *Client) error {
			_, err := c.ScrapePerformers(ctx, "https://x.test/graphql", "x")
			return err
		}},
		{name: "ScrapeScenes", run: func(ctx context.Context, c *Client) error {
			_, err := c.ScrapeScenes(ctx, "https://x.test/graphql", "x")
			return err
		}},
		{name: "ScrapeSceneByID", run: func(ctx context.Context, c *Client) error {
			_, err := c.ScrapeSceneByID(ctx, "https://x.test/graphql", "1")
			return err
		}},
		{name: "SavedFilters", run: func(ctx context.Context, c *Client) error { _, err := c.SavedFilters(ctx, FilterScenes); return err }},
		{name: "FindSavedFilter", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.FindSavedFilter(ctx, FilterScenes, "x")
			return err
		}},
		{name: "SaveFilter", run: func(ctx context.Context, c *Client) error {
			_, err := c.SaveFilter(ctx, SavedFilter{Mode: FilterScenes, Name: "x"})
			return err
		}},
		{name: "SaveSceneFilter", run: func(ctx context.Context, c *Client) error {
			_, err := c.SaveSceneFilter(ctx, "x", SceneFilter{HasDate: &no}, nil)
			return err
		}},
		{name: "DestroySavedFilter", run: func(ctx context.Context, c *Client) error { return c.DestroySavedFilter(ctx, "1") }},
		{name: "SystemStatus", run: func(ctx context.Context, c *Client) error { _, err := c.SystemStatus(ctx); return err }},
		{name: "ServerVersion", run: func(ctx context.Context, c *Client) error { _, err := c.ServerVersion(ctx); return err }},
		{name: "LatestVersion", run: func(ctx context.Context, c *Client) error { _, _, err := c.LatestVersion(ctx); return err }},
		{name: "Logs", run: func(ctx context.Context, c *Client) error { _, err := c.Logs(ctx); return err }},
		{name: "GeneralConfig", run: func(ctx context.Context, c *Client) error { _, err := c.GeneralConfig(ctx, "databasePath"); return err }},
		{name: "ConfigureGeneral", run: func(ctx context.Context, c *Client) error {
			return c.ConfigureGeneral(ctx, map[string]any{"logLevel": "Info"})
		}},
		{name: "GenerateAPIKey", run: func(ctx context.Context, c *Client) error { _, err := c.GenerateAPIKey(ctx); return err }},
		{name: "ClearAPIKey", run: func(ctx context.Context, c *Client) error { return c.ClearAPIKey(ctx) }},
		{name: "Migrate", run: func(ctx context.Context, c *Client) error { return c.Migrate(ctx, "") }},
		{name: "MigrateBlobs", run: func(ctx context.Context, c *Client) error { _, err := c.MigrateBlobs(ctx, false); return err }},
		{name: "MigrateHashNaming", run: func(ctx context.Context, c *Client) error { _, err := c.MigrateHashNaming(ctx); return err }},
		{name: "MigrateSceneScreenshots", run: func(ctx context.Context, c *Client) error {
			_, err := c.MigrateSceneScreenshots(ctx, ScreenshotMigration{})
			return err
		}},
		{name: "AnonymiseDatabase", run: func(ctx context.Context, c *Client) error { _, err := c.AnonymiseDatabase(ctx); return err }},
		{name: "DownloadAnonymisedDatabase", run: func(ctx context.Context, c *Client) error {
			_, _, err := c.DownloadAnonymisedDatabase(ctx, io.Discard)
			return err
		}},
		{name: "DownloadFFMpeg", run: func(ctx context.Context, c *Client) error { _, err := c.DownloadFFMpeg(ctx); return err }},
		{name: "DLNAStatus", run: func(ctx context.Context, c *Client) error { _, err := c.DLNAStatus(ctx); return err }},
		{name: "EnableDLNA", run: func(ctx context.Context, c *Client) error { return c.EnableDLNA(ctx, time.Hour) }},
		{name: "DisableDLNA", run: func(ctx context.Context, c *Client) error { return c.DisableDLNA(ctx, 0) }},
		{name: "AllowDLNAIP", run: func(ctx context.Context, c *Client) error { return c.AllowDLNAIP(ctx, "192.168.1.20", 0) }},
		{name: "DisallowDLNAIP", run: func(ctx context.Context, c *Client) error { return c.DisallowDLNAIP(ctx, "192.168.1.20") }},
	}
}

// A server that answers every request the same way, however broken.
func brokenServer(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, WithHTTPClient(srv.Client()))
}

// Every call has to report a problem rather than return a zero value and no
// error. A method that swallows one turns a server outage into an empty
// library, which is the kind of thing noticed a long way downstream.
//
// The four broken servers are the ones a caller actually meets: a schema the
// server rejects, a server that has fallen over, a credential it will not
// accept, and something in front of it answering with a login page.
func TestEveryCallReportsABrokenServer(t *testing.T) {
	for _, broken := range []struct {
		name     string
		handler  http.HandlerFunc
		httpOnly bool // whether a body-blind call should notice it too
	}{
		{name: "a GraphQL error", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `{"errors":[{"message":"something went wrong"}]}`)
		}},
		{name: "HTTP 500", httpOnly: true, handler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "server on fire", http.StatusInternalServerError)
		}},
		{name: "HTTP 401", httpOnly: true, handler: func(w http.ResponseWriter, _ *http.Request) {
			http.Error(w, "bad api key", http.StatusUnauthorized)
		}},
		{name: "a body that is not JSON", handler: func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, `<html>not json at all</html>`)
		}},
	} {
		t.Run(broken.name, func(t *testing.T) {
			for _, call := range everyCall() {
				if call.bodyBlind && !broken.httpOnly {
					continue
				}
				c := brokenServer(t, broken.handler)
				if err := call.run(context.Background(), c); err == nil {
					t.Errorf("%s returned no error against %s", call.name, broken.name)
				}
			}
		})
	}
}

// The key is a bearer token. Some GraphQL middlewares echo the request back
// on an auth failure, and those messages get logged.
func TestTheAPIKeyNeverReachesAnErrorString(t *testing.T) {
	const key = "super-secret-key"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"errors":[{"message":"rejected request with ApiKey `+key+`"}]}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, WithAPIKey(key), WithHTTPClient(srv.Client()))
	for _, call := range everyCall() {
		err := call.run(context.Background(), c)
		if err == nil {
			continue
		}
		if strings.Contains(err.Error(), key) {
			t.Fatalf("%s leaked the API key: %v", call.name, err)
		}
		if !strings.Contains(err.Error(), "[redacted]") && strings.Contains(err.Error(), "rejected request") {
			t.Errorf("%s did not redact: %v", call.name, err)
		}
	}
}

// The Ensure* calls create only when the lookup found nothing, which is the
// branch a happy-path test never reaches.
func TestEnsureCreatesOnlyWhenAbsent(t *testing.T) {
	for _, tc := range []struct {
		name    string
		replies []string
		run     func(context.Context, *Client) (string, error)
		wantID  string
		wantN   int
	}{
		{
			name: "tag that exists is not created",
			replies: []string{
				`{"data":{"findTags":{"tags":[{"id":"5"}]}}}`,
			},
			run:    func(ctx context.Context, c *Client) (string, error) { return c.EnsureTag(ctx, "x") },
			wantID: "5", wantN: 1,
		},
		{
			name: "tag that does not exist is created",
			replies: []string{
				`{"data":{"findTags":{"tags":[]}}}`,  // by name
				`{"data":{"findTags":{"tags":[]}}}`,  // by alias
				`{"data":{"tagCreate":{"id":"11"}}}`, // created
			},
			run:    func(ctx context.Context, c *Client) (string, error) { return c.EnsureTag(ctx, "x") },
			wantID: "11", wantN: 3,
		},
		{
			name: "performer that exists is not created",
			replies: []string{
				`{"data":{"findPerformers":{"performers":[{"id":"7"}]}}}`,
			},
			run:    func(ctx context.Context, c *Client) (string, error) { return c.EnsurePerformer(ctx, "x") },
			wantID: "7", wantN: 1,
		},
		{
			name: "performer that does not exist is created",
			replies: []string{
				`{"data":{"findPerformers":{"performers":[]}}}`,
				`{"data":{"performerCreate":{"id":"12"}}}`,
			},
			run:    func(ctx context.Context, c *Client) (string, error) { return c.EnsurePerformer(ctx, "x") },
			wantID: "12", wantN: 2,
		},
		{
			name: "studio that exists is not created",
			replies: []string{
				`{"data":{"findStudios":{"studios":[{"id":"3"}]}}}`,
			},
			run:    func(ctx context.Context, c *Client) (string, error) { return c.EnsureStudio(ctx, "x") },
			wantID: "3", wantN: 1,
		},
		{
			name: "studio that does not exist is created",
			replies: []string{
				`{"data":{"findStudios":{"studios":[]}}}`,
				`{"data":{"studioCreate":{"id":"13"}}}`,
			},
			run:    func(ctx context.Context, c *Client) (string, error) { return c.EnsureStudio(ctx, "x") },
			wantID: "13", wantN: 2,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cap := &capture{}
			srv := httptest.NewServer(cap.handler(tc.replies...))
			defer srv.Close()

			id, err := tc.run(context.Background(), NewClient(srv.URL))
			if err != nil {
				t.Fatalf("%s: %v", tc.name, err)
			}
			if id != tc.wantID {
				t.Errorf("id = %q, want %q", id, tc.wantID)
			}
			if len(cap.reqs) != tc.wantN {
				t.Errorf("made %d requests, want %d", len(cap.reqs), tc.wantN)
			}
		})
	}
}

// A tag found only by its alias is still that tag, and must not be created a
// second time under the name that was searched for.
func TestEnsureTagFindsItByAlias(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findTags":{"tags":[]}}}`,           // not by name
		`{"data":{"findTags":{"tags":[{"id":"5"}]}}}`, // but by alias
	))
	defer srv.Close()

	id, err := NewClient(srv.URL).EnsureTag(context.Background(), "x")
	if err != nil {
		t.Fatalf("EnsureTag: %v", err)
	}
	if id != "5" {
		t.Errorf("id = %q, want the tag found by alias", id)
	}
	if len(cap.reqs) != 2 {
		t.Errorf("made %d requests, want 2 and no create", len(cap.reqs))
	}
}

// An APIError with none, one and several messages reads differently in each
// case, and the empty one is a malformed response worth saying so about.
func TestAPIErrorText(t *testing.T) {
	for _, tc := range []struct {
		errs []GraphQLError
		want string
	}{
		{nil, "empty error array"},
		{[]GraphQLError{{Message: "one thing"}}, "one thing"},
		{[]GraphQLError{{Message: "one thing"}, {Message: "another"}}, "one thing; another"},
	} {
		got := (&APIError{Errors: tc.errs}).Error()
		if !strings.Contains(got, tc.want) {
			t.Errorf("Error() = %q, want it to contain %q", got, tc.want)
		}
	}
}

// A client with no key has nothing to redact, and must not pay for the
// attempt or alter the message.
func TestRedactWithoutAKey(t *testing.T) {
	c := NewClient("http://x.test")
	if got := c.redactString("nothing to hide"); got != "nothing to hide" {
		t.Errorf("redactString = %q", got)
	}
	if err := c.redact(nil); err != nil {
		t.Errorf("redact(nil) = %v", err)
	}
	original := &HTTPError{Status: "500"}
	if got := c.redact(original); got != error(original) {
		t.Errorf("redact returned a different error when there was no key")
	}
}

// A server URL that cannot be parsed is a configuration mistake, and should
// be reported rather than producing a request to nowhere.
func TestResolveServerURLRejectsRubbish(t *testing.T) {
	c := NewClient("http://x.test")
	if _, err := c.resolveServerURL("://not a url"); err == nil {
		t.Error("want an error for an unparseable URL")
	}
	c.baseURL = "://also not a url"
	if _, err := c.resolveServerURL("/fine"); err == nil {
		t.Error("want an error for an unparseable base URL")
	}
}

// The three input builders drop what is unset and keep what is not, across
// every field. Each has a shape the others do not — pointers for the values
// where zero is a real answer, lists that replace, stash ids that nest — and
// a field silently omitted is a change that does not happen.
func TestInputBuildersCarryEveryFieldTheyAreGiven(t *testing.T) {
	rating, favourite := 90, true

	performer := PerformerUpdate{
		ID: "1", Name: ptr("n"), Disambiguation: ptr("d"), Gender: ptr("FEMALE"),
		Birthdate: ptr("1990-01-01"), DeathDate: ptr("2020-01-01"), Country: ptr("US"),
		Ethnicity: ptr("e"), EyeColor: ptr("green"), HairColor: ptr("red"),
		Measurements: ptr("m"), FakeTits: ptr("f"), CareerLength: ptr("1999 -"),
		Tattoos: ptr("t"), Piercings: ptr("p"), Details: ptr("about"),
		HeightCM: &rating, Weight: &rating, Rating100: &rating, Favorite: &favourite,
		Aliases: []string{"a"}, URLs: []string{"u"}, TagIDs: []string{"3"},
		StashIDs: []StashID{{Endpoint: "e", ID: "s"}}, Image: ptr("img"),
	}.fields()
	for _, key := range []string{
		"id", "name", "disambiguation", "gender", "birthdate", "death_date", "country",
		"ethnicity", "eye_color", "hair_color", "measurements", "fake_tits",
		"career_length", "tattoos", "piercings", "details", "height_cm", "weight",
		"rating100", "favorite", "alias_list", "urls", "tag_ids", "stash_ids", "image",
	} {
		if _, ok := performer[key]; !ok {
			t.Errorf("PerformerUpdate dropped %s", key)
		}
	}

	studio := StudioInput{
		ID: "1", Name: "n", Details: "d", ParentID: "2", Image: "img",
		Aliases: []string{"a"}, URLs: []string{"u"}, TagIDs: []string{"3"},
		StashIDs:  []StashID{{Endpoint: "e", ID: "s"}},
		Rating100: &rating, Favorite: &favourite,
	}.fields()
	for _, key := range []string{
		"id", "name", "details", "parent_id", "image", "aliases", "urls",
		"tag_ids", "stash_ids", "rating100", "favorite",
	} {
		if _, ok := studio[key]; !ok {
			t.Errorf("StudioInput dropped %s", key)
		}
	}

	tag := TagInput{
		ID: "1", Name: "n", SortName: "s", Description: "d", Image: "img",
		Aliases: []string{"a"}, ParentIDs: []string{"2"}, ChildIDs: []string{"3"},
		StashIDs: []StashID{{Endpoint: "e", ID: "s"}}, Favorite: &favourite,
	}.fields()
	for _, key := range []string{
		"id", "name", "sort_name", "description", "image", "aliases",
		"parent_ids", "child_ids", "stash_ids", "favorite",
	} {
		if _, ok := tag[key]; !ok {
			t.Errorf("TagInput dropped %s", key)
		}
	}

	// And the other way: an empty one sends nothing but what identifies it.
	if got := (StudioInput{ID: "1"}).fields(); len(got) != 1 {
		t.Errorf("an empty StudioInput sent %v", got)
	}
	if got := (TagInput{ID: "1"}).fields(); len(got) != 1 {
		t.Errorf("an empty TagInput sent %v", got)
	}
}

func ptr(s string) *string { return &s }

// A saved filter renders every criterion it is given, in the notation saved
// filters use rather than the one queries use.
func TestSavedCriteriaCoverEveryFilterField(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findPerformers":{"performers":[{"id":"9"}]}}}`,
		`{"data":{"findStudios":{"studios":[{"id":"3"}]}}}`,
		`{"data":{"findSavedFilters":[]}}`,
		`{"data":{"saveFilter":{"id":"1"}}}`,
	))
	defer srv.Close()

	yes := true
	_, err := NewClient(srv.URL).SaveSceneFilter(context.Background(), "everything", SceneFilter{
		Organized: &yes, HasStashID: &yes, DateAfter: "2009-01-01",
		PathContains: "D:\\Media", PerformerName: "Someone", StudioName: "A Studio",
	}, &FindFilter{Sort: "date", Direction: "ASC", PerPage: 100, Query: "x"})
	if err != nil {
		t.Fatalf("SaveSceneFilter: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[len(cap.reqs)-1].Variables["input"])
	for _, want := range []string{"organized", "stash_id_endpoint", "date", "path", "performers", "studios"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("input = %s, missing %s", b, want)
		}
	}
	// Booleans are stringly typed in a saved filter.
	if !strings.Contains(string(b), `"value":"true"`) {
		t.Errorf("input = %s, want organized as the string \"true\"", b)
	}
}

// Both bounds together become one range, because Stash takes a single date
// criterion and sending two would silently keep the last.
func TestSavedCriteriaDateRange(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findSavedFilters":[]}}`,
		`{"data":{"saveFilter":{"id":"1"}}}`,
	))
	defer srv.Close()

	_, err := NewClient(srv.URL).SaveSceneFilter(context.Background(), "2009",
		SceneFilter{DateAfter: "2009-01-01", DateBefore: "2010-01-01"}, nil)
	if err != nil {
		t.Fatalf("SaveSceneFilter: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[1].Variables["input"])
	if !strings.Contains(string(b), "BETWEEN") || !strings.Contains(string(b), "value2") {
		t.Errorf("input = %s", b)
	}
}

func TestSavedCriteriaRefusesBothTagDirections(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	_, err := c.SaveSceneFilter(context.Background(), "x",
		SceneFilter{TagNames: []string{"a"}, ExcludeTagNames: []string{"b"}}, nil)
	if err == nil {
		t.Error("want an error when both tag directions are set")
	}
}

// redact has to work on a transport error too, not only on a message the
// server sent: a proxy failure quotes the URL, and a caller who put the key
// in one would otherwise log it.
func TestRedactRewritesATransportError(t *testing.T) {
	const key = "super-secret-key"
	c := NewClient("http://x.test", WithAPIKey(key))

	err := c.redact(&HTTPError{Status: "500", Body: "rejected ApiKey " + key})
	if strings.Contains(err.Error(), key) {
		t.Errorf("redact left the key in: %v", err)
	}
	if !strings.Contains(err.Error(), "[redacted]") {
		t.Errorf("redact = %v, want the placeholder", err)
	}

	// An error that never mentioned the key comes back as it was, rather
	// than being rebuilt into a plain error and losing its type.
	original := &HTTPError{Status: "500", Body: "nothing sensitive"}
	if got := c.redact(original); got != error(original) {
		t.Error("redact rebuilt an error that needed no changing")
	}
}

// A tag with no alias match and no name match is genuinely absent, and the
// caller has to be able to tell that from a failure.
func TestFindTagByAliasNotFound(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findTags":{"tags":[]}}}`))
	id, found, err := c.FindTagByAlias(context.Background(), "nothing")
	if err != nil || found || id != "" {
		t.Errorf("got (%q, %v, %v)", id, found, err)
	}
}

// Fetch streams to a writer, so a writer that fails has to surface rather
// than leave a truncated file looking complete.
func TestFetchPropagatesAWriteFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, strings.Repeat("x", 4096))
	}))
	defer srv.Close()

	_, n, err := NewClient(srv.URL, WithHTTPClient(srv.Client())).
		Fetch(context.Background(), "/scene/1/screenshot", failingWriter{})
	if err == nil {
		t.Fatal("Fetch: want the write error")
	}
	if n != 0 {
		t.Errorf("wrote %d bytes despite the failure", n)
	}
}

// A response whose envelope parses but whose contents are the wrong shape is
// the case a decoder catches and a careless method ignores.
func TestDecodersReportTheWrongShape(t *testing.T) {
	// data is a number: nothing can be decoded into a struct from it, so
	// every method that reads a result has to say so.
	const wrong = `{"data": 42}`
	for _, call := range everyCall() {
		if call.bodyBlind {
			continue
		}
		c := brokenServer(t, func(w http.ResponseWriter, _ *http.Request) {
			_, _ = io.WriteString(w, wrong)
		})
		err := call.run(context.Background(), c)
		// A call that sends a mutation and reads nothing back is entitled
		// to succeed here; one that returns a value is not.
		if err != nil && !strings.Contains(err.Error(), "stash:") {
			t.Errorf("%s: error did not name the package: %v", call.name, err)
		}
	}
}
