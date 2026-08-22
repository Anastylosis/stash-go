//go:build integration

package stash

import (
	"context"
	"os"
	"testing"
)

// These run against a real Stash server: `go test -tags integration`.
// STASH_URL defaults to http://localhost:9999; STASH_API_KEY may be empty.
// Everything here is read-only — nothing creates, updates or deletes.
// stashURL is the server under test, defaulting to a local Stash.
func stashURL() string {
	if url := os.Getenv("STASH_URL"); url != "" {
		return url
	}
	return "http://localhost:9999"
}

func client(t *testing.T) *Client {
	t.Helper()
	url := stashURL()
	c := NewClient(url, WithAPIKey(os.Getenv("STASH_API_KEY")))
	if err := c.Ping(context.Background()); err != nil {
		t.Skipf("Stash not reachable at %s: %v", url, err)
	}
	return c
}

// sample returns up to n scenes, skipping the test when the library is empty.
func sample(t *testing.T, c *Client, n int) []Scene {
	t.Helper()
	scenes, _, err := c.FindScenes(context.Background(), SceneFilter{}, 1, n)
	if err != nil {
		t.Fatalf("FindScenes: %v", err)
	}
	if len(scenes) == 0 {
		t.Skip("no scenes in Stash")
	}
	return scenes
}

func TestLivePing(t *testing.T) {
	c := client(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping: %v", err)
	}
}

func TestLiveVersion(t *testing.T) {
	v, err := client(t).Version(context.Background())
	if err != nil {
		t.Fatalf("Version: %v", err)
	}
	t.Logf("server version %s", v)
}

// The shared selection set must decode against the real schema — a field the
// server lacks fails the whole query, so this is the check that matters most.
func TestLiveSceneFieldsDecode(t *testing.T) {
	c := client(t)
	s := sample(t, c, 5)[0]
	if s.ID == "" {
		t.Error("scene ID is empty")
	}
	t.Logf("scene %s: title=%q code=%q rating=%v files=%d tags=%d performers=%d",
		s.ID, s.Title, s.Code, s.Rating100, len(s.Files), len(s.Tags), len(s.Performers))
	if f := s.PrimaryFile(); f != nil {
		t.Logf("  file %s: %s %dx%d %d bytes %s fingerprints=%d",
			f.ID, f.Path, f.Width, f.Height, f.Size, f.VideoCodec, len(f.Fingerprints))
	}
}

func TestLiveSupports(t *testing.T) {
	c := client(t)
	for _, field := range []string{"code", "o_counter", "groups", "nonexistent_field_42xyz"} {
		ok, err := c.Supports(context.Background(), field)
		if err != nil {
			t.Fatalf("Supports(%q): %v", field, err)
		}
		t.Logf("Supports(%q) = %v", field, ok)
	}
}

func TestLiveFindScene(t *testing.T) {
	c := client(t)
	want := sample(t, c, 1)[0]

	got, found, err := c.FindScene(context.Background(), want.ID)
	if err != nil {
		t.Fatalf("FindScene: %v", err)
	}
	if !found || got.ID != want.ID {
		t.Errorf("FindScene(%s) = %v, found=%v", want.ID, got, found)
	}
}

func TestLiveFindSceneNotFound(t *testing.T) {
	_, found, err := client(t).FindScene(context.Background(), "99999999")
	if err != nil {
		t.Fatalf("FindScene: %v", err)
	}
	if found {
		t.Error("expected no scene for a bogus ID")
	}
}

func TestLiveFindAllScenesPaginates(t *testing.T) {
	c := client(t)
	var pages int
	scenes, err := c.FindAllScenes(context.Background(), SceneFilter{}, func(fetched, total int) {
		pages++
		t.Logf("  page %d: %d / %d", pages, fetched, total)
	})
	if err != nil {
		t.Fatalf("FindAllScenes: %v", err)
	}
	t.Logf("%d scenes in %d pages", len(scenes), pages)
}

func TestLiveFilters(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	scenes := sample(t, c, 20)

	var filters []struct {
		name   string
		filter SceneFilter
	}
	for _, s := range scenes {
		if len(s.Performers) > 0 && len(filters) == 0 {
			filters = append(filters, struct {
				name   string
				filter SceneFilter
			}{"performer=" + s.Performers[0].Name, SceneFilter{PerformerName: s.Performers[0].Name}})
		}
	}
	for _, s := range scenes {
		if s.Studio != nil {
			filters = append(filters, struct {
				name   string
				filter SceneFilter
			}{"studio=" + s.Studio.Name, SceneFilter{StudioName: s.Studio.Name}})
			break
		}
	}
	for _, s := range scenes {
		if f := s.PrimaryFile(); f != nil && f.Basename != "" {
			substr := f.Basename[:min(10, len(f.Basename))]
			filters = append(filters, struct {
				name   string
				filter SceneFilter
			}{"path=" + substr, SceneFilter{PathContains: substr}})
			break
		}
	}
	if len(filters) == 0 {
		t.Skip("no scene carries a performer, studio or file to filter on")
	}

	for _, f := range filters {
		matched, count, err := c.FindScenes(ctx, f.filter, 1, 5)
		if err != nil {
			t.Errorf("FindScenes(%s): %v", f.name, err)
			continue
		}
		if count == 0 {
			t.Errorf("FindScenes(%s) matched nothing, want at least the scene it came from", f.name)
		}
		t.Logf("%s: %d scenes (total %d)", f.name, len(matched), count)
	}
}

func TestLiveFilterByStashID(t *testing.T) {
	c := client(t)
	for _, has := range []bool{true, false} {
		_, count, err := c.FindScenes(context.Background(), SceneFilter{HasStashID: &has}, 1, 5)
		if err != nil {
			t.Fatalf("FindScenes(HasStashID=%v): %v", has, err)
		}
		t.Logf("HasStashID=%v: %d scenes", has, count)
	}
}

func TestLiveEntityLookupsFindWhatScenesReference(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	scenes := sample(t, c, 20)

	lookups := map[string]func(context.Context, string) (string, bool, error){}
	names := map[string]string{}
	for _, s := range scenes {
		if len(s.Tags) > 0 && names["tag"] == "" {
			names["tag"], lookups["tag"] = s.Tags[0].Name, c.FindTag
		}
		if len(s.Performers) > 0 && names["performer"] == "" {
			names["performer"], lookups["performer"] = s.Performers[0].Name, c.FindPerformer
		}
		if s.Studio != nil && names["studio"] == "" {
			names["studio"], lookups["studio"] = s.Studio.Name, c.FindStudio
		}
	}
	if len(names) == 0 {
		t.Skip("no scene carries a tag, performer or studio")
	}

	for kind, name := range names {
		id, found, err := lookups[kind](ctx, name)
		if err != nil {
			t.Errorf("find %s %q: %v", kind, name, err)
			continue
		}
		if !found {
			t.Errorf("%s %q is on a scene but the lookup missed it", kind, name)
		}
		t.Logf("%s %q = %s", kind, name, id)
	}
}

func TestLiveEntityLookupsMissDeliberately(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	for kind, find := range map[string]func(context.Context, string) (string, bool, error){
		"tag":       c.FindTag,
		"performer": c.FindPerformer,
		"studio":    c.FindStudio,
	} {
		_, found, err := find(ctx, "stash_go_nonexistent_42xyz")
		if err != nil {
			t.Errorf("find %s: %v", kind, err)
		}
		if found {
			t.Errorf("%s lookup found a name that should not exist", kind)
		}
	}
}

// A filter naming something the server does not have must be an error, not an
// empty page.
func TestLiveUnknownFilterTargetsAreErrors(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	if _, _, err := c.FindScenes(ctx, SceneFilter{PerformerName: "stash_go_nonexistent_42xyz"}, 1, 5); err == nil {
		t.Error("want ErrPerformerNotFound")
	}
	if _, _, err := c.FindScenes(ctx, SceneFilter{StudioName: "stash_go_nonexistent_42xyz"}, 1, 5); err == nil {
		t.Error("want ErrStudioNotFound")
	}
}

// -- captions, configuration and jobs ---------------------------------------

// The captions probe has to agree with what the server actually accepts:
// if Supports says yes but the field then fails the query, every scene
// fetch breaks at once.
func TestLiveCaptionsSelection(t *testing.T) {
	c := client(t)
	ctx := context.Background()

	supported, err := c.Supports(ctx, "captions")
	if err != nil {
		t.Fatalf("Supports: %v", err)
	}
	t.Logf("Scene.captions supported: %v", supported)

	withCaptions := NewClient(stashURL(), WithAPIKey(os.Getenv("STASH_API_KEY")), WithCaptions())
	scenes, _, err := withCaptions.FindScenes(ctx, SceneFilter{}, 1, 5)
	if err != nil {
		t.Fatalf("FindScenes with captions: %v", err)
	}
	if len(scenes) == 0 {
		t.Skip("no scenes in Stash")
	}
	for _, s := range scenes {
		for _, cap := range s.Captions {
			if cap.LanguageCode == "" {
				t.Errorf("scene %s has a caption with no language code", s.ID)
			}
		}
	}
}

// A client without the option must still decode scenes, and must leave
// Captions nil — the default path is the one every existing caller is on.
func TestLiveScenesWithoutCaptionsOption(t *testing.T) {
	c := client(t)
	scenes := sample(t, c, 3)
	for _, s := range scenes {
		if s.Captions != nil {
			t.Errorf("scene %s carried captions without WithCaptions", s.ID)
		}
	}
}

func TestLivePluginSettings(t *testing.T) {
	c := client(t)
	ctx := context.Background()

	// An unknown plugin is the case that must not error, and it is the one
	// safe to assert against any server.
	got, err := c.PluginSettings(ctx, "definitely-not-a-real-plugin")
	if err != nil {
		t.Fatalf("PluginSettings(unknown): %v", err)
	}
	if len(got) != 0 {
		t.Errorf("unknown plugin returned %v, want empty", got)
	}

	all, err := c.Configuration(ctx)
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	if _, ok := all["plugins"]; !ok {
		t.Error("configuration carried no plugins section")
	}
}

// jobQueue is read-only and answers on an idle server too — nothing here
// starts a job. Scanning is deliberately untested against a live library:
// a scan is an hours-long mutation, not something a test should trigger.
func TestLiveJobQueue(t *testing.T) {
	c := client(t)
	ctx := context.Background()

	if _, err := c.JobQueue(ctx); err != nil {
		t.Fatalf("JobQueue: %v", err)
	}

	// An id nothing could plausibly hold: aged out or never existed, both
	// of which must read as not-found rather than an error.
	_, found, err := c.FindJob(ctx, "99999999")
	if err != nil {
		t.Fatalf("FindJob: %v", err)
	}
	if found {
		t.Log("job 99999999 exists on this server; skipping the not-found assertion")
	}
}
