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
      "stash_ids":[{"endpoint":"https://sb","stash_id":"uuid"}]}}}`))

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
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findScenes":{"count":0,"scenes":[]}}}`))
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

	vars := cap.reqs[0].Variables
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
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findScenes":{"count":0,"scenes":[]}}}`))
	defer srv.Close()

	if _, _, err := NewClient(srv.URL).FindScenes(context.Background(), SceneFilter{}, 1, 10); err != nil {
		t.Fatalf("FindScenes: %v", err)
	}
	if _, ok := cap.reqs[0].Variables["scene_filter"]; ok {
		t.Error("scene_filter should be omitted entirely when no filter is set")
	}
}

func TestFindAllScenesPaginatesUntilShortPage(t *testing.T) {
	full := `{"data":{"findScenes":{"count":250,"scenes":[` +
		strings.TrimSuffix(strings.Repeat(`{"id":"x"},`, 100), ",") + `]}}}`
	short := `{"data":{"findScenes":{"count":250,"scenes":[{"id":"last"}]}}}`

	cap := &capture{}
	srv := httptest.NewServer(cap.handler(full, full, short))
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
	if len(cap.reqs) != 3 {
		t.Errorf("made %d requests, want 3 (stop on the short page)", len(cap.reqs))
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
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"sceneUpdate":{"id":"1"}}}`))
	defer srv.Close()

	title := "New"
	err := NewClient(srv.URL).UpdateScene(context.Background(), SceneUpdate{ID: "1", Title: &title})
	if err != nil {
		t.Fatalf("UpdateScene: %v", err)
	}

	input := cap.reqs[0].Variables["input"].(map[string]any)
	if input["title"] != "New" {
		t.Errorf("title = %v", input["title"])
	}
	for _, absent := range []string{"code", "details", "director", "date", "rating100", "urls", "tag_ids", "performer_ids", "studio_id", "organized", "stash_ids", "cover_image"} {
		if _, ok := input[absent]; ok {
			t.Errorf("unset field %q was sent; a partial update would blank it", absent)
		}
	}
}

func TestUpdateSceneSendsSetFields(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"sceneUpdate":{"id":"1"}}}`))
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

	input := cap.reqs[0].Variables["input"].(map[string]any)
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
