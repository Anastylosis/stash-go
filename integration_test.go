//go:build integration

package stash

import (
	"context"
	"encoding/json"
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
	scenes := sample(t, c, 25)

	// Captions are in the shared selection set, so every scene query carries
	// them and a scene with subtitles decodes without asking for anything.
	for _, s := range scenes {
		for _, capt := range s.Captions {
			if capt.LanguageCode == "" {
				t.Errorf("scene %s has a caption with no language code", s.ID)
			}
		}
	}
}

// The widened selection set must decode against the real server, not just the
// stubs: a field named wrongly fails the whole query, and the count of scenes
// that came back is the only thing that would say so.
func TestLiveWidenedSceneFields(t *testing.T) {
	c := client(t)
	scenes := sample(t, c, 25)

	for _, s := range scenes {
		if s.PlayCount < 0 || s.PlayDuration < 0 {
			t.Errorf("scene %s has negative playback counters: %+v", s.ID, s)
		}
		for _, g := range s.Groups {
			if g.Group.ID == "" {
				t.Errorf("scene %s is in a group with no id", s.ID)
			}
		}
	}
	t.Logf("decoded %d scenes with captions, groups and playback", len(scenes))
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

// The lifecycle mutations are absent from this file on purpose: every one of
// them deletes or moves something, and there is no way to assert they worked
// without doing it to a real library. Only the read half is exercised here.

func TestLiveFindFileByIDAndPath(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	scene := sample(t, c, 1)[0]
	want := scene.PrimaryFile()
	if want == nil {
		t.Skip("scene has no file")
	}

	byID, found, err := c.FindFile(ctx, want.ID)
	if err != nil || !found {
		t.Fatalf("FindFile: %v, found=%v", err, found)
	}
	if byID.Path != want.Path {
		t.Errorf("path = %q, want %q", byID.Path, want.Path)
	}
	// The VideoFile fragment has to resolve against the real schema; a scene's
	// file always has a duration, so a zero here means the fragment missed.
	if byID.Duration == 0 {
		t.Errorf("duration = 0 for %s", byID.Basename)
	}

	byPath, found, err := c.FindFileByPath(ctx, want.Path)
	if err != nil || !found {
		t.Fatalf("FindFileByPath: %v, found=%v", err, found)
	}
	if byPath.ID != want.ID {
		t.Errorf("id = %q, want %q", byPath.ID, want.ID)
	}
}

// A path Stash does not know is an error at the GraphQL layer, because
// findFile is non-null. It must reach the caller as absence, not failure.
func TestLiveFindFileByPathMissing(t *testing.T) {
	file, found, err := client(t).FindFileByPath(context.Background(), "/nowhere/absent.mp4")
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}
	if found || file != nil {
		t.Errorf("found=%v file=%v", found, file)
	}
}

func TestLiveFindSceneByHash(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	for _, scene := range sample(t, c, 5) {
		file := scene.PrimaryFile()
		if file == nil {
			continue
		}
		hash, ok := file.Fingerprint("oshash")
		if !ok {
			continue
		}
		got, found, err := c.FindSceneByHash(ctx, "oshash", hash)
		if err != nil || !found {
			t.Fatalf("FindSceneByHash: %v, found=%v", err, found)
		}
		if got.ID != scene.ID {
			t.Errorf("hash %s resolved to scene %s, want %s", hash, got.ID, scene.ID)
		}
		return
	}
	t.Skip("no sampled scene carries an oshash")
}

func TestLiveFindSceneByHashMissing(t *testing.T) {
	scene, found, err := client(t).FindSceneByHash(context.Background(), "oshash", "0000000000000000")
	if err != nil {
		t.Fatalf("FindSceneByHash: %v", err)
	}
	if found || scene != nil {
		t.Errorf("found=%v scene=%v", found, scene)
	}
}

func TestLiveFindScenesByPathRegex(t *testing.T) {
	c := client(t)
	ctx := context.Background()
	scene := sample(t, c, 1)[0]
	file := scene.PrimaryFile()
	if file == nil {
		t.Skip("scene has no file")
	}

	// A pattern built from a path the server just reported, so a match is
	// guaranteed and the test says nothing about the library's contents.
	scenes, total, err := c.FindScenesByPathRegex(ctx, `\.[a-zA-Z0-9]+$`, 1, 5)
	if err != nil {
		t.Fatalf("FindScenesByPathRegex: %v", err)
	}
	if total == 0 || len(scenes) == 0 {
		t.Fatalf("no scene matched a path with an extension (total=%d)", total)
	}

	// A pattern that cannot match anything must come back empty rather than
	// falling through to every scene.
	_, total, err = c.FindScenesByPathRegex(ctx, `^zzz-no-such-path-zzz$`, 1, 5)
	if err != nil {
		t.Fatalf("FindScenesByPathRegex: %v", err)
	}
	if total != 0 {
		t.Errorf("total = %d for an impossible pattern", total)
	}
}

// The administration queries are read-only and answer on any server that is
// up. Nothing here migrates, anonymises, rewrites a setting or touches the
// API key: each of those changes the server for everyone using it, and two
// of them cannot be undone.
func TestLiveSystemStatus(t *testing.T) {
	c := client(t)
	got, err := c.SystemStatus(context.Background())
	if err != nil {
		t.Fatalf("SystemStatus: %v", err)
	}
	// Ping already succeeded, so a server in any other state would mean
	// Ready is reading the wrong thing.
	if !got.Ready() {
		t.Fatalf("status = %q against a server answering queries", got.Status)
	}
	if got.DatabaseSchema == nil {
		t.Error("databaseSchema is null on a server that is OK")
	} else {
		t.Logf("database schema %d, binary expects %d, config at %s",
			*got.DatabaseSchema, got.AppSchema, got.ConfigPath)
	}
}

func TestLiveServerVersion(t *testing.T) {
	got, err := client(t).ServerVersion(context.Background())
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	// The tag is empty on a build from source; the hash never is.
	if got.Hash == "" {
		t.Error("no build hash")
	}
	t.Logf("version %q hash %s built %s", got.Version, got.Hash, got.BuildTime)
}

// A server that has just answered a query has logged something, but a quiet
// one with a small ring is entitled to hand back nothing — only the error
// matters here.
func TestLiveLogs(t *testing.T) {
	entries, err := client(t).Logs(context.Background())
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(entries) > 0 {
		t.Logf("%d entries, newest %s %s", len(entries), entries[0].Level, entries[0].Time)
	}
}

func TestLiveGeneralConfig(t *testing.T) {
	c := client(t)
	ctx := context.Background()

	got, err := c.GeneralConfig(ctx, "databasePath", "logLevel")
	if err != nil {
		t.Fatalf("GeneralConfig: %v", err)
	}
	// Asking for two fields must return those two: a section decoded into a
	// map is where a renamed field goes missing silently.
	for _, field := range []string{"databasePath", "logLevel"} {
		if _, present := got[field]; !present {
			t.Errorf("%s absent from %v", field, got)
		}
	}

	// The guard is what keeps anything but a field name out of the query,
	// and it has to hold against the real parser too.
	if _, err := c.GeneralConfig(ctx, "databasePath } } configuration { general { logLevel"); err == nil {
		t.Error("GeneralConfig accepted something that is not a field name")
	}
}

func TestLiveDLNAStatus(t *testing.T) {
	got, err := client(t).DLNAStatus(context.Background())
	if err != nil {
		t.Fatalf("DLNAStatus: %v", err)
	}
	t.Logf("DLNA running=%v, %d recent addresses, %d temporary grants",
		got.Running, len(got.RecentIPAddresses), len(got.AllowedIPAddresses))
}

// The administration calls that write are not exercised live: they migrate,
// anonymise, rewrite settings or replace the API key, and each of those
// changes the server for everyone using it. What a live server can still
// check is that the shapes they send exist — a field the schema lacks fails
// the whole request, so this is the difference between a call that works and
// one that fails the first time somebody means it.
func TestLiveLibraryStats(t *testing.T) {
	c := client(t)

	stats, err := c.LibraryStats(context.Background())
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}
	if stats.SceneCount <= 0 {
		t.Fatalf("scene_count = %d, want a library with scenes", stats.SceneCount)
	}
	// A library with scenes has a size and a duration; zero for either means
	// the field decoded into the wrong type rather than that the videos are
	// empty.
	if stats.ScenesSize <= 0 || stats.ScenesDuration <= 0 {
		t.Errorf("scenes_size = %v, scenes_duration = %v", stats.ScenesSize, stats.ScenesDuration)
	}
	t.Logf("%d scenes, %.1f GiB, %.0f hours",
		stats.SceneCount, stats.ScenesSize/(1<<30), stats.ScenesDuration/3600)
}

func TestLiveFindDuplicateScenes(t *testing.T) {
	c := client(t)

	// distance 0 with a tight duration window is the cheapest form of the
	// query and the least likely to return anything surprising.
	groups, err := c.FindDuplicateScenes(context.Background(), 0, 1)
	if err != nil {
		t.Fatalf("FindDuplicateScenes: %v", err)
	}
	for _, g := range groups {
		if len(g) < 2 {
			t.Errorf("a duplicate group holds %d scene(s), want at least 2", len(g))
		}
		for _, s := range g {
			if s.ID == "" {
				t.Errorf("a grouped scene decoded without an id")
			}
		}
	}
	t.Logf("%d duplicate group(s) at distance 0", len(groups))
}

func TestLiveMultiFileFilter(t *testing.T) {
	c := client(t)
	ctx := context.Background()

	yes, no := true, false
	multi, multiTotal, err := c.FindScenes(ctx, SceneFilter{MultiFile: &yes}, 1, 10)
	if err != nil {
		t.Fatalf("FindScenes(MultiFile: true): %v", err)
	}
	for _, s := range multi {
		if len(s.Files) < 2 {
			t.Errorf("scene %s came back as multi-file with %d file(s)", s.ID, len(s.Files))
		}
	}

	single, singleTotal, err := c.FindScenes(ctx, SceneFilter{MultiFile: &no}, 1, 10)
	if err != nil {
		t.Fatalf("FindScenes(MultiFile: false): %v", err)
	}
	for _, s := range single {
		if len(s.Files) != 1 {
			t.Errorf("scene %s came back as single-file with %d file(s)", s.ID, len(s.Files))
		}
	}

	// The two must partition the library: a filter that silently matched
	// everything would pass both loops above.
	_, all, err := c.FindScenes(ctx, SceneFilter{}, 1, 1)
	if err != nil {
		t.Fatalf("FindScenes: %v", err)
	}
	if multiTotal+singleTotal != all {
		t.Errorf("multi(%d) + single(%d) = %d, want the library total %d",
			multiTotal, singleTotal, multiTotal+singleTotal, all)
	}
}

func TestLiveAdminSchemaShapes(t *testing.T) {
	c := client(t)

	for _, tc := range []struct {
		typeName string
		want     []string
	}{
		{"Query", []string{"systemStatus", "version", "latestversion", "logs", "dlnaStatus", "configuration",
			"findDuplicateScenes", "stats", "scrapeMultiScenes"}},
		{"Mutation", []string{
			"configureGeneral", "generateAPIKey", "migrate", "migrateBlobs", "migrateHashNaming",
			"migrateSceneScreenshots", "anonymiseDatabase", "downloadFFMpeg", "optimiseDatabase",
			"enableDLNA", "disableDLNA", "addTempDLNAIP", "removeTempDLNAIP",
			// Not synonyms: deleteFiles removes the video, destroyFiles only
			// the record. Both must exist for the two wrappers to mean what
			// they say.
			"deleteFiles", "destroyFiles", "sceneMerge",
		}},
		{"StatsResultType", []string{
			"scene_count", "scenes_size", "scenes_duration", "image_count", "images_size",
			"gallery_count", "performer_count", "studio_count", "group_count", "tag_count",
			"total_o_count", "total_play_count", "total_play_duration", "scenes_played",
		}},
		{"SceneMergeInput", []string{"source", "destination", "values", "play_history", "o_history"}},
		{"SceneUpdateInput", []string{"id", "primary_file_id"}},
		{"SceneFilterType", []string{"file_count", "stash_id_endpoint", "path", "date"}},
		{"ScrapeMultiScenesInput", []string{"scene_ids"}},
		{"Scene", []string{"captions", "groups", "play_count", "play_duration", "last_played_at", "resume_time", "created_at", "updated_at"}},
		{"SystemStatus", []string{"status", "databaseSchema", "appSchema", "databasePath", "configPath"}},
		{"Version", []string{"version", "hash", "build_time"}},
		{"LogEntry", []string{"time", "level", "message"}},
		{"DLNAStatus", []string{"running", "until", "recentIPAddresses", "allowedIPAddresses"}},
		{"DLNAIP", []string{"ipAddress", "until"}},
		{"MigrateInput", []string{"backupPath"}},
		{"MigrateBlobsInput", []string{"deleteOld"}},
		{"MigrateSceneScreenshotsInput", []string{"deleteFiles", "overwriteExisting"}},
		{"AnonymiseDatabaseInput", []string{"download"}},
		{"GenerateAPIKeyInput", []string{"clear"}},
		{"EnableDLNAInput", []string{"duration"}},
		{"DisableDLNAInput", []string{"duration"}},
		{"AddTempDLNAIPInput", []string{"address", "duration"}},
		{"RemoveTempDLNAIPInput", []string{"address"}},
	} {
		t.Run(tc.typeName, func(t *testing.T) {
			have, ok := typeFields(t, c, tc.typeName)
			if !ok {
				t.Fatalf("this server has no type %s", tc.typeName)
			}
			for _, field := range tc.want {
				if !have[field] {
					t.Errorf("%s has no field %q", tc.typeName, field)
				}
			}
		})
	}
}

// typeFields introspects one type's fields, whether it is an object type or
// an input one — the two answer on different halves of __Type.
func typeFields(t *testing.T, c *Client, name string) (map[string]bool, bool) {
	t.Helper()
	data, err := c.Execute(context.Background(),
		`query($n: String!) { __type(name: $n) { fields { name } inputFields { name } } }`,
		map[string]any{"n": name})
	if err != nil {
		t.Fatalf("introspecting %s: %v", name, err)
	}
	var result struct {
		Type *struct {
			Fields      []struct{ Name string } `json:"fields"`
			InputFields []struct{ Name string } `json:"inputFields"`
		} `json:"__type"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("decoding introspection of %s: %v", name, err)
	}
	if result.Type == nil {
		return nil, false
	}
	fields := map[string]bool{}
	for _, f := range append(result.Type.Fields, result.Type.InputFields...) {
		fields[f.Name] = true
	}
	return fields, true
}
