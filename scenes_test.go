package stash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// capture records the decoded GraphQL request bodies the stub received.
type capture struct{ reqs []graphqlRequest }

func (c *capture) handler(bodies ...string) http.HandlerFunc {
	i := 0
	return func(w http.ResponseWriter, r *http.Request) {
		var req graphqlRequest
		_ = json.NewDecoder(r.Body).Decode(&req)
		c.reqs = append(c.reqs, req)
		body := bodies[len(bodies)-1]
		if i < len(bodies) {
			body = bodies[i]
		}
		i++
		_, _ = io.WriteString(w, body)
	}
}

func TestFindSceneNotFoundIsNotAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":null}}`))
	scene, found, err := c.FindScene(context.Background(), "404")
	if err != nil {
		t.Fatalf("FindScene: %v", err)
	}
	if found || scene != nil {
		t.Errorf("got (%v, %v), want (nil, false)", scene, found)
	}
}

func TestFindSceneDecodesFields(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":{
      "id":"12","title":"T","code":"ABC-1","date":"2024-01-02","organized":true,
      "director":"D","rating100":75,"o_counter":3,
      "urls":["https://e/1"],
      "files":[{"id":"9","basename":"a.mp4","path":"/v/a.mp4","duration":1800.5,
        "size":123456789,"width":3840,"height":2160,"video_codec":"h264","bit_rate":8000000,
        "fingerprints":[{"type":"oshash","value":"deadbeef"}]}],
      "tags":[{"id":"1","name":"tag"}],
      "performers":[{"id":"2","name":"perf"}],
      "studio":{"id":"3","name":"studio"},
      "stash_ids":[{"endpoint":"https://sb","stash_id":"uuid"}],
      "galleries":[{"id":"7","title":"G"}]}}}`))

	scene, found, err := c.FindScene(context.Background(), "12")
	if err != nil || !found {
		t.Fatalf("FindScene: %v, found=%v", err, found)
	}
	if scene.Title != "T" || !scene.Organized {
		t.Errorf("scene = %+v", scene)
	}
	if scene.Code != "ABC-1" || scene.Director != "D" || scene.OCounter != 3 {
		t.Errorf("scene = %+v", scene)
	}
	if scene.Rating100 == nil || *scene.Rating100 != 75 {
		t.Errorf("rating100 = %v", scene.Rating100)
	}
	if len(scene.Files) != 1 || scene.Files[0].Duration != 1800.5 {
		t.Errorf("files = %+v", scene.Files)
	}
	f := scene.PrimaryFile()
	if f == nil || f.ID != "9" || f.Size != 123456789 || f.Height != 2160 || f.BitRate != 8000000 {
		t.Errorf("primary file = %+v", f)
	}
	if v, ok := f.Fingerprint("oshash"); !ok || v != "deadbeef" {
		t.Errorf("oshash = %q, %v", v, ok)
	}
	if _, ok := f.Fingerprint("phash"); ok {
		t.Error("phash reported present")
	}
	if len(scene.Galleries) != 1 || scene.Galleries[0].ID != "7" {
		t.Errorf("galleries = %+v", scene.Galleries)
	}
	if !scene.HasStashID() {
		t.Error("HasStashID reported false for a scene carrying one")
	}
	if (&Scene{}).HasStashID() {
		t.Error("HasStashID reported true for a scene with none")
	}
	if scene.Studio == nil || scene.Studio.Name != "studio" {
		t.Errorf("studio = %+v", scene.Studio)
	}
	// stash_ids maps onto StashID.ID, not .StashID — the JSON tag is what
	// keeps the Go field name from stuttering.
	if len(scene.StashIDs) != 1 || scene.StashIDs[0].ID != "uuid" {
		t.Errorf("stash_ids = %+v", scene.StashIDs)
	}
}

// An unknown performer must be a distinguishable error, not an empty page:
// Stash answers a bad name with zero results and no error, so a typo would
// otherwise look exactly like a genuine no-match.
func TestFindScenesUnknownPerformerIsSentinelError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findPerformers":{"performers":[]}}}`))
	_, _, err := c.FindScenes(context.Background(), SceneFilter{PerformerName: "Nobody"}, 1, 10)
	if !errors.Is(err, ErrPerformerNotFound) {
		t.Fatalf("error = %v, want ErrPerformerNotFound", err)
	}
	if !strings.Contains(err.Error(), "Nobody") {
		t.Errorf("error = %q, should name the performer searched for", err)
	}
}

func TestFindScenesUnknownStudioIsSentinelError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findStudios":{"studios":[]}}}`))
	_, _, err := c.FindScenes(context.Background(), SceneFilter{StudioName: "Nope"}, 1, 10)
	if !errors.Is(err, ErrStudioNotFound) {
		t.Fatalf("error = %v, want ErrStudioNotFound", err)
	}
}

func TestFindScenesBuildsFilter(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"findScenes":{"count":0,"scenes":[]}}}`))
	defer srv.Close()

	organized := true
	hasStashID := false
	_, _, err := NewClient(srv.URL).FindScenes(context.Background(), SceneFilter{
		Organized:    &organized,
		HasStashID:   &hasStashID,
		PathContains: "/archive/",
	}, 2, 50)
	if err != nil {
		t.Fatalf("FindScenes: %v", err)
	}

	vars := capt.reqs[0].Variables
	find := vars["filter"].(map[string]any)
	if find["page"] != float64(2) || find["per_page"] != float64(50) {
		t.Errorf("find filter = %#v", find)
	}
	// Sorting by path keeps paging stable across requests.
	if find["sort"] != "path" {
		t.Errorf("sort = %v, want path", find["sort"])
	}

	sf := vars["scene_filter"].(map[string]any)
	if sf["organized"] != true {
		t.Errorf("organized = %v", sf["organized"])
	}
	if got := sf["stash_id_endpoint"].(map[string]any)["modifier"]; got != "IS_NULL" {
		t.Errorf("HasStashID=false should map to IS_NULL, got %v", got)
	}
	if got := sf["path"].(map[string]any)["value"]; got != "/archive/" {
		t.Errorf("path value = %v", got)
	}
}

func TestFindScenesOmitsSceneFilterWhenEmpty(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"findScenes":{"count":0,"scenes":[]}}}`))
	defer srv.Close()

	if _, _, err := NewClient(srv.URL).FindScenes(context.Background(), SceneFilter{}, 1, 10); err != nil {
		t.Fatalf("FindScenes: %v", err)
	}
	if _, ok := capt.reqs[0].Variables["scene_filter"]; ok {
		t.Error("scene_filter should be omitted entirely when no filter is set")
	}
}

func TestFindAllScenesPaginatesUntilShortPage(t *testing.T) {
	full := `{"data":{"findScenes":{"count":250,"scenes":[` +
		strings.TrimSuffix(strings.Repeat(`{"id":"x"},`, 100), ",") + `]}}}`
	short := `{"data":{"findScenes":{"count":250,"scenes":[{"id":"last"}]}}}`

	capt := &capture{}
	srv := httptest.NewServer(capt.handler(full, full, short))
	defer srv.Close()

	var lastFetched, lastTotal int
	all, err := NewClient(srv.URL).FindAllScenes(context.Background(), SceneFilter{},
		func(fetched, total int) { lastFetched, lastTotal = fetched, total })
	if err != nil {
		t.Fatalf("FindAllScenes: %v", err)
	}
	if len(all) != 201 {
		t.Errorf("collected %d scenes, want 201", len(all))
	}
	if len(capt.reqs) != 3 {
		t.Errorf("made %d requests, want 3 (stop on the short page)", len(capt.reqs))
	}
	if lastFetched != 201 || lastTotal != 250 {
		t.Errorf("progress = (%d, %d), want (201, 250)", lastFetched, lastTotal)
	}
}

func TestFindAllScenesCancellationReturnsPartial(t *testing.T) {
	full := `{"data":{"findScenes":{"count":9999,"scenes":[` +
		strings.TrimSuffix(strings.Repeat(`{"id":"x"},`, 100), ",") + `]}}}`
	srv := httptest.NewServer(http.HandlerFunc(reply(full)))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	got, err := NewClient(srv.URL).FindAllScenes(ctx, SceneFilter{}, func(fetched, _ int) {
		if fetched >= 200 {
			cancel()
		}
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context.Canceled", err)
	}
	if len(got) == 0 {
		t.Error("cancellation should still return what was collected")
	}
}

// Only the fields explicitly set may be sent, or a partial push would blank
// everything the caller did not mention.
func TestUpdateSceneOmitsUnsetFields(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"sceneUpdate":{"id":"1"}}}`))
	defer srv.Close()

	title := "New"
	err := NewClient(srv.URL).UpdateScene(context.Background(), SceneUpdate{ID: "1", Title: &title})
	if err != nil {
		t.Fatalf("UpdateScene: %v", err)
	}

	input := capt.reqs[0].Variables["input"].(map[string]any)
	if input["title"] != "New" {
		t.Errorf("title = %v", input["title"])
	}
	for _, absent := range []string{"code", "details", "director", "date", "rating100", "urls", "tag_ids", "performer_ids", "studio_id", "gallery_ids", "organized", "stash_ids", "cover_image"} {
		if _, ok := input[absent]; ok {
			t.Errorf("unset field %q was sent; a partial update would blank it", absent)
		}
	}
}

func TestUpdateSceneSendsSetFields(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"sceneUpdate":{"id":"1"}}}`))
	defer srv.Close()

	code, director, rating := "ABC-123", "Someone", 80
	err := NewClient(srv.URL).UpdateScene(context.Background(), SceneUpdate{
		ID:        "1",
		Code:      &code,
		Director:  &director,
		Rating100: &rating,
		StashIDs:  []StashID{{Endpoint: "https://sb", ID: "uuid"}},
	})
	if err != nil {
		t.Fatalf("UpdateScene: %v", err)
	}

	input := capt.reqs[0].Variables["input"].(map[string]any)
	if input["code"] != "ABC-123" || input["director"] != "Someone" || input["rating100"] != float64(80) {
		t.Errorf("input = %v", input)
	}
	ids, ok := input["stash_ids"].([]any)
	if !ok || len(ids) != 1 {
		t.Fatalf("stash_ids = %v", input["stash_ids"])
	}
	if got := ids[0].(map[string]any)["stash_id"]; got != "uuid" {
		t.Errorf("stash_ids[0].stash_id = %v", got)
	}
}

// A caller writing its own query pastes SceneFields into it and decodes the
// result into Scene, so a field on the type but not in the set is silently
// zero for them.
func TestSceneFieldsCoversTheSceneType(t *testing.T) {
	for _, field := range []string{
		"id", "title", "code", "date", "details", "director", "urls", "rating100",
		"organized", "o_counter", "tags", "performers", "studio", "stash_ids",
		"galleries", "fingerprints", "video_codec", "bit_rate", "width", "height",
	} {
		if !strings.Contains(SceneFields, field) {
			t.Errorf("SceneFields is missing %q, which Scene decodes", field)
		}
	}
}

func TestUpdateSceneRequiresID(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if err := c.UpdateScene(context.Background(), SceneUpdate{}); err == nil {
		t.Fatal("want an error when ID is empty")
	}
}

func ExampleClient_FindAllScenes() {
	c := NewClient("http://localhost:9999", WithAPIKey("key"))

	scenes, err := c.FindAllScenes(context.Background(), SceneFilter{StudioName: "Example"},
		func(fetched, total int) { fmt.Printf("\r%d/%d", fetched, total) })
	if err != nil {
		return
	}
	fmt.Println(len(scenes))
}

func TestSceneFilterHasDate(t *testing.T) {
	for _, want := range []struct {
		has      bool
		modifier string
	}{{true, "NOT_NULL"}, {false, "IS_NULL"}} {
		capt := &capture{}
		srv := httptest.NewServer(capt.handler(`{"data":{"findScenes":{"count":0,"scenes":[]}}}`))
		if _, _, err := NewClient(srv.URL).FindScenes(context.Background(),
			SceneFilter{HasDate: &want.has}, 1, 10); err != nil {
			t.Fatalf("FindScenes: %v", err)
		}
		srv.Close()
		b, _ := json.Marshal(capt.reqs[0].Variables["scene_filter"])
		if !strings.Contains(string(b), want.modifier) {
			t.Errorf("HasDate=%v sent %s, want %s", want.has, b, want.modifier)
		}
		// Stash declares value non-null even where the modifier ignores it,
		// and rejects the whole query when it is missing.
		if !strings.Contains(string(b), `"value"`) {
			t.Errorf("HasDate=%v sent %s, want a value alongside the modifier", want.has, b)
		}
	}
}

// Stash takes one date criterion, so two bounds have to become one range —
// sending them as two filters would silently keep only the last.
func TestSceneFilterDateBounds(t *testing.T) {
	for _, tc := range []struct {
		filter   SceneFilter
		modifier string
	}{
		{SceneFilter{DateAfter: "2009-01-01"}, "GREATER_THAN"},
		{SceneFilter{DateBefore: "2010-01-01"}, "LESS_THAN"},
		{SceneFilter{DateAfter: "2009-01-01", DateBefore: "2010-01-01"}, "BETWEEN"},
	} {
		capt := &capture{}
		srv := httptest.NewServer(capt.handler(`{"data":{"findScenes":{"count":0,"scenes":[]}}}`))
		if _, _, err := NewClient(srv.URL).FindScenes(context.Background(), tc.filter, 1, 10); err != nil {
			t.Fatalf("FindScenes: %v", err)
		}
		srv.Close()
		b, _ := json.Marshal(capt.reqs[0].Variables["scene_filter"])
		if !strings.Contains(string(b), tc.modifier) {
			t.Errorf("%+v sent %s, want %s", tc.filter, b, tc.modifier)
		}
	}
}

func TestSceneFilterTagNames(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(
		`{"data":{"findTags":{"tags":[{"id":"9"}]}}}`,
		`{"data":{"findScenes":{"count":0,"scenes":[]}}}`))
	defer srv.Close()

	if _, _, err := NewClient(srv.URL).FindScenes(context.Background(),
		SceneFilter{ExcludeTagNames: []string{"date_from_scene"}}, 1, 10); err != nil {
		t.Fatalf("FindScenes: %v", err)
	}
	b, _ := json.Marshal(capt.reqs[1].Variables["scene_filter"])
	if !strings.Contains(string(b), "EXCLUDES") || !strings.Contains(string(b), `"9"`) {
		t.Errorf("filter = %s", b)
	}
}

// A tag name nothing carries is a typo, and Stash answers a typo with an
// empty result set — indistinguishable from a filter that legitimately
// matched nothing.
func TestSceneFilterUnknownTagIsAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findTags":{"tags":[]}}}`))
	_, _, err := c.FindScenes(context.Background(), SceneFilter{TagNames: []string{"nope"}}, 1, 10)
	if !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("err = %v, want ErrTagNotFound", err)
	}
}

// Stash takes one tags criterion, so sending both would silently keep only
// the second.
func TestSceneFilterRefusesBothTagDirections(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	_, _, err := c.FindScenes(context.Background(), SceneFilter{
		TagNames: []string{"a"}, ExcludeTagNames: []string{"b"}}, 1, 10)
	if err == nil {
		t.Fatal("want an error when both tag filters are set")
	}
}

// SceneUpdate omits empty fields so that an unset one leaves the stored value
// alone — which means it cannot clear a list at all. Removing the last stash
// id silently did nothing until this existed.
func TestSetSceneStashIDsSendsAnEmptyList(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"sceneUpdate":{"id":"1"}}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).SetSceneStashIDs(context.Background(), "1", nil); err != nil {
		t.Fatalf("SetSceneStashIDs: %v", err)
	}
	b, _ := json.Marshal(capt.reqs[0].Variables["input"])
	if !strings.Contains(string(b), `"stash_ids":[]`) {
		t.Errorf("input = %s, want an explicit empty list", b)
	}
}

func TestSetSceneStashIDsKeepsTheOthers(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"sceneUpdate":{"id":"1"}}}`))
	defer srv.Close()

	err := NewClient(srv.URL).SetSceneStashIDs(context.Background(), "1",
		[]StashID{{Endpoint: "https://other.test/graphql", ID: "keep-me"}})
	if err != nil {
		t.Fatalf("SetSceneStashIDs: %v", err)
	}
	b, _ := json.Marshal(capt.reqs[0].Variables["input"])
	if !strings.Contains(string(b), "keep-me") {
		t.Errorf("input = %s", b)
	}
}

func TestSetSceneStashIDsNeedsASceneID(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if err := c.SetSceneStashIDs(context.Background(), "", nil); err == nil {
		t.Error("want an error without a scene id")
	}
}

// SceneUpdate omits empty fields, so it cannot empty one. A title written
// over a scene that never had one has to be removable.
func TestClearSceneFieldsSendsTheRightEmptyValue(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"sceneUpdate":{"id":"1"}}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).ClearSceneFields(context.Background(), "1", "title", "urls"); err != nil {
		t.Fatalf("ClearSceneFields: %v", err)
	}
	b, _ := json.Marshal(capt.reqs[0].Variables["input"])
	if !strings.Contains(string(b), `"title":""`) {
		t.Errorf("input = %s, want an empty title", b)
	}
	if !strings.Contains(string(b), `"urls":[]`) {
		t.Errorf("input = %s, want an empty list for urls", b)
	}
}

func TestClearSceneFieldsRefusesAnythingButAFieldName(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if err := c.ClearSceneFields(context.Background(), "1", "title details"); err == nil {
		t.Error("want an error for something that is not a field name")
	}
	if err := c.ClearSceneFields(context.Background(), "", "title"); err == nil {
		t.Error("want an error without an id")
	}
}

func TestFindDuplicateScenesGroupsScenes(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"findDuplicateScenes":[
		[{"id":"1","files":[{"id":"11","size":100}]},{"id":"2","files":[{"id":"22","size":50}]}],
		[{"id":"3"},{"id":"4"},{"id":"5"}]
	]}}`))
	defer srv.Close()

	groups, err := NewClient(srv.URL).FindDuplicateScenes(context.Background(), 4, 1.0)
	if err != nil {
		t.Fatalf("FindDuplicateScenes: %v", err)
	}
	if len(groups) != 2 {
		t.Fatalf("len(groups) = %d, want 2", len(groups))
	}
	if len(groups[0]) != 2 || len(groups[1]) != 3 {
		t.Errorf("group sizes = %d, %d; want 2, 3", len(groups[0]), len(groups[1]))
	}
	// The grouping is the whole point: a flattened result would lose which
	// scene is a duplicate of which.
	if groups[0][0].ID != "1" || groups[0][1].ID != "2" {
		t.Errorf("group 0 = %v", groups[0])
	}
	if len(groups[0][0].Files) != 1 || groups[0][0].Files[0].Size != 100 {
		t.Errorf("files did not decode: %+v", groups[0][0].Files)
	}

	vars := capt.reqs[0].Variables
	if vars["distance"] != float64(4) {
		t.Errorf("distance = %v, want 4", vars["distance"])
	}
	if vars["duration_diff"] != 1.0 {
		t.Errorf("duration_diff = %v, want 1", vars["duration_diff"])
	}
}

func TestFindDuplicateScenesEmptyLibrary(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findDuplicateScenes":[]}}`))
	groups, err := c.FindDuplicateScenes(context.Background(), 0, 0)
	if err != nil {
		t.Fatalf("FindDuplicateScenes: %v", err)
	}
	if len(groups) != 0 {
		t.Errorf("len(groups) = %d, want 0", len(groups))
	}
}

func TestSceneFilterUpdatedAfterCriterion(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	ctx := context.Background()

	got, err := c.SceneFilterCriteria(ctx, SceneFilter{UpdatedAfter: "2026-08-01T00:00:00Z"})
	if err != nil {
		t.Fatal(err)
	}
	crit, ok := got["updated_at"].(map[string]any)
	if !ok {
		t.Fatalf("no updated_at criterion in %v", got)
	}
	if crit["modifier"] != "GREATER_THAN" || crit["value"] != "2026-08-01T00:00:00Z" {
		t.Errorf("updated_at = %v, want GREATER_THAN the timestamp", crit)
	}

	got, err = c.SceneFilterCriteria(ctx, SceneFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got["updated_at"]; present {
		t.Errorf("empty UpdatedAfter sent an updated_at criterion: %v", got)
	}
}

func TestSceneFilterMultiFileCriterion(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	ctx := context.Background()

	yes, no := true, false
	for _, tc := range []struct {
		name     string
		value    *bool
		modifier string
	}{
		{"more than one file", &yes, "GREATER_THAN"},
		{"exactly one file", &no, "EQUALS"},
	} {
		got, err := c.SceneFilterCriteria(ctx, SceneFilter{MultiFile: tc.value})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		crit, ok := got["file_count"].(map[string]any)
		if !ok {
			t.Fatalf("%s: no file_count criterion in %v", tc.name, got)
		}
		if crit["modifier"] != tc.modifier || crit["value"] != 1 {
			t.Errorf("%s: file_count = %v, want value 1 modifier %s", tc.name, crit, tc.modifier)
		}
	}

	// Nil must not send the criterion at all — an unasked filter that
	// silently selects single-file scenes would hide most of a library.
	got, err := c.SceneFilterCriteria(ctx, SceneFilter{})
	if err != nil {
		t.Fatal(err)
	}
	if _, present := got["file_count"]; present {
		t.Errorf("nil MultiFile sent a file_count criterion: %v", got)
	}
}
