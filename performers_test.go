package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func inputOf(t *testing.T, req graphqlRequest) map[string]any {
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

func TestCreatePerformerFromSendsEverythingGiven(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerCreate":{"id":"9"}}}`))
	defer srv.Close()

	id, err := NewClient(srv.URL).CreatePerformerFrom(context.Background(), PerformerInput{
		Name:     "Example Performer",
		Gender:   "FEMALE",
		Country:  "US",
		HeightCM: 167,
		Aliases:  []string{"Alias One", "Alias Two"},
		Image:    "https://example.test/images/1",
		StashIDs: []StashID{{Endpoint: "https://example.test/graphql", ID: "abc-123"}},
	})
	if err != nil {
		t.Fatalf("CreatePerformerFrom: %v", err)
	}
	if id != "9" {
		t.Errorf("id = %q, want 9", id)
	}

	in := inputOf(t, cap.reqs[0])
	if in["name"] != "Example Performer" || in["height_cm"] != float64(167) {
		t.Errorf("input = %+v", in)
	}
	ids, _ := in["stash_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("stash_ids = %v", in["stash_ids"])
	}
	if got := ids[0].(map[string]any)["stash_id"]; got != "abc-123" {
		t.Errorf("stash_id = %v", got)
	}
}

// Sending an empty field is not the same as not sending it: Stash stores the
// empty string, and a performer ends up with a birthdate of "".
func TestCreatePerformerFromOmitsWhatItWasNotGiven(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerCreate":{"id":"1"}}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).CreatePerformerFrom(context.Background(),
		PerformerInput{Name: "Only A Name"}); err != nil {
		t.Fatalf("CreatePerformerFrom: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	if len(in) != 1 {
		t.Errorf("sent %d fields, want only name: %+v", len(in), in)
	}
	// Zero is a real height nobody has, and sending it would be a lie.
	if _, ok := in["height_cm"]; ok {
		t.Error("height_cm was sent as zero")
	}
}

func TestCreatePerformerFromRefusesAnEmptyName(t *testing.T) {
	_, c := server(t, reply(`{"data":{"performerCreate":{"id":"1"}}}`))
	if _, err := c.CreatePerformerFrom(context.Background(), PerformerInput{}); err == nil {
		t.Error("CreatePerformerFrom with no name: want an error")
	}
}

func TestFindPerformerByStashID(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findPerformers":{"performers":[{"id":"42"}]}}}`))
	defer srv.Close()

	id, found, err := NewClient(srv.URL).FindPerformerByStashID(context.Background(),
		"https://example.test/graphql", "abc-123")
	if err != nil {
		t.Fatalf("FindPerformerByStashID: %v", err)
	}
	if !found || id != "42" {
		t.Errorf("got (%q, %v)", id, found)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["f"])
	if !strings.Contains(string(b), "abc-123") || !strings.Contains(string(b), "EQUALS") {
		t.Errorf("filter = %s", b)
	}
}

func TestFindPerformerByStashIDNotFound(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findPerformers":{"performers":[]}}}`))
	id, found, err := c.FindPerformerByStashID(context.Background(), "https://example.test/graphql", "nope")
	if err != nil {
		t.Fatalf("FindPerformerByStashID: %v", err)
	}
	if found || id != "" {
		t.Errorf("got (%q, %v), want empty", id, found)
	}
}

func TestFindPerformerByStashIDRefusesAnEmptyID(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if _, _, err := c.FindPerformerByStashID(context.Background(), "https://example.test/graphql", ""); err == nil {
		t.Error("want an error for an empty stash id")
	}
}

func TestFindPerformerByIDDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findPerformer":{
		"id":"1","name":"Example Performer","gender":"FEMALE","country":"US",
		"height_cm":167,"birthdate":"1990-01-01","alias_list":["Alias A","Alias B"],
		"urls":["https://example.test/1"],"scene_count":42,
		"tags":[{"id":"3","name":"a tag"}],
		"stash_ids":[{"endpoint":"https://example.test/graphql","stash_id":"abc-123"}]}}}`))

	p, found, err := c.FindPerformerByID(context.Background(), "1")
	if err != nil || !found {
		t.Fatalf("FindPerformerByID: %v, found=%v", err, found)
	}
	if p.HeightCM != 167 || p.SceneCount != 42 || len(p.Aliases) != 2 {
		t.Errorf("performer = %+v", p)
	}
	if len(p.StashIDs) != 1 || p.StashIDs[0].ID != "abc-123" {
		t.Errorf("stash ids = %+v", p.StashIDs)
	}
}

func TestFindPerformerByIDNotFound(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findPerformer":null}}`))
	p, found, err := c.FindPerformerByID(context.Background(), "404")
	if err != nil || found || p != nil {
		t.Errorf("got (%v, %v, %v)", p, found, err)
	}
}

// The same shape as SceneUpdate: only what is set goes on the wire, so an
// unset field leaves the stored value alone.
func TestUpdatePerformerSendsOnlyWhatIsSet(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerUpdate":{"id":"1"}}}`))
	defer srv.Close()

	details := "Only this."
	if err := NewClient(srv.URL).UpdatePerformer(context.Background(),
		PerformerUpdate{ID: "1", Details: &details}); err != nil {
		t.Fatalf("UpdatePerformer: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	if len(in) != 2 || in["id"] != "1" || in["details"] != "Only this." {
		t.Errorf("input = %+v, want only id and details", in)
	}
}

// A zero is a real value for these, not an absence, so the pointer is what
// separates "set it to zero" from "leave it".
func TestUpdatePerformerSendsExplicitZeroes(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerUpdate":{"id":"1"}}}`))
	defer srv.Close()

	zero, no := 0, false
	err := NewClient(srv.URL).UpdatePerformer(context.Background(),
		PerformerUpdate{ID: "1", Rating100: &zero, Favorite: &no})
	if err != nil {
		t.Fatalf("UpdatePerformer: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	if in["rating100"] != float64(0) {
		t.Errorf("rating100 = %#v, want 0 sent", in["rating100"])
	}
	if in["favorite"] != false {
		t.Errorf("favorite = %#v, want false sent", in["favorite"])
	}
}

func TestUpdatePerformerNeedsAnID(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if err := c.UpdatePerformer(context.Background(), PerformerUpdate{}); err == nil {
		t.Error("want an error without an id")
	}
}

// PerformerUpdate omits what is unset, so it cannot empty a field at all.
func TestClearPerformerFieldsSendsTheRightEmptyValue(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerUpdate":{"id":"1"}}}`))
	defer srv.Close()

	err := NewClient(srv.URL).ClearPerformerFields(context.Background(), "1", "birthdate", "alias_list")
	if err != nil {
		t.Fatalf("ClearPerformerFields: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	if in["birthdate"] != "" {
		t.Errorf("birthdate = %#v, want an empty string", in["birthdate"])
	}
	// A list wants a list; sending "" for one is a type error.
	list, ok := in["alias_list"].([]any)
	if !ok || len(list) != 0 {
		t.Errorf("alias_list = %#v, want an empty list", in["alias_list"])
	}
}

// The names are spliced into nothing, but they do reach Stash, and a name it
// does not know fails the whole mutation rather than being ignored.
func TestClearPerformerFieldsRefusesAnythingButAFieldName(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	for _, bad := range []string{"birthdate details", "a-b", "1st", "a}"} {
		if err := c.ClearPerformerFields(context.Background(), "1", bad); err == nil {
			t.Errorf("ClearPerformerFields(%q): want an error", bad)
		}
	}
	if err := c.ClearPerformerFields(context.Background(), "1"); err != nil {
		t.Errorf("clearing nothing should be a no-op, got %v", err)
	}
}

func TestDeletePerformers(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performersDestroy":true}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if err := c.DeletePerformers(context.Background(), "1", "2"); err != nil {
		t.Fatalf("DeletePerformers: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["ids"])
	if string(b) != `["1","2"]` {
		t.Errorf("ids = %s", b)
	}
	// Nothing to delete is not a request worth making.
	if err := c.DeletePerformers(context.Background()); err != nil {
		t.Errorf("DeletePerformers with no ids: %v", err)
	}
	if len(cap.reqs) != 1 {
		t.Errorf("sent %d requests, want 1", len(cap.reqs))
	}
}

func TestMergePerformersSendsSourcesAndDestination(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerMerge":{"id":"1"}}}`))
	defer srv.Close()

	name := "The better name"
	err := NewClient(srv.URL).MergePerformers(context.Background(), "1", []string{"2", "3"},
		&PerformerUpdate{Name: &name})
	if err != nil {
		t.Fatalf("MergePerformers: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	if in["destination"] != "1" {
		t.Errorf("destination = %v", in["destination"])
	}
	sources, _ := in["source"].([]any)
	if len(sources) != 2 {
		t.Errorf("source = %v", in["source"])
	}
	// values carries the destination's id, not a source's.
	values, ok := in["values"].(map[string]any)
	if !ok || values["id"] != "1" || values["name"] != "The better name" {
		t.Errorf("values = %v", in["values"])
	}
}

// Stash would delete the destination as one of its own sources.
func TestMergePerformersRefusesToMergeIntoItself(t *testing.T) {
	_, c := server(t, reply(`{"data":{"performerMerge":{"id":"1"}}}`))
	if err := c.MergePerformers(context.Background(), "1", []string{"2", "1"}, nil); err == nil {
		t.Error("want an error when a source is the destination")
	}
	if err := c.MergePerformers(context.Background(), "1", nil, nil); err == nil {
		t.Error("want an error with no sources")
	}
	if err := c.MergePerformers(context.Background(), "", []string{"2"}, nil); err == nil {
		t.Error("want an error with no destination")
	}
}

func TestPerformerFilterCriteria(t *testing.T) {
	yes := true
	got := PerformerFilter{NameContains: "example", Gender: "FEMALE", Favorite: &yes, HasScenes: &yes}.criteria()
	if got["name"].(map[string]any)["modifier"] != "INCLUDES" {
		t.Errorf("name = %v", got["name"])
	}
	if got["filter_favorites"] != true {
		t.Errorf("filter_favorites = %v", got["filter_favorites"])
	}
	// Zero is the thing being asked about, so the count is compared, not
	// tested for null.
	if got["scene_count"].(map[string]any)["modifier"] != "GREATER_THAN" {
		t.Errorf("scene_count = %v", got["scene_count"])
	}
	if len(PerformerFilter{}.criteria()) != 0 {
		t.Error("an empty filter should filter nothing")
	}
}

// Stash declares these String even though they hold years. Decoding them as
// numbers failed every performer query, and only against a real server —
// nothing in a stub had said otherwise.
func TestPerformerCareerYearsDecodeAsStrings(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findPerformer":{
		"id":"1","name":"Example","career_start":"1999","career_end":"2010"}}}`))
	p, found, err := c.FindPerformerByID(context.Background(), "1")
	if err != nil || !found {
		t.Fatalf("FindPerformerByID: %v, found=%v", err, found)
	}
	if p.CareerStart != "1999" || p.CareerEnd != "2010" {
		t.Errorf("career = %q..%q", p.CareerStart, p.CareerEnd)
	}
}

func TestFindPerformersDecodesAndCounts(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findPerformers":{"count":42,"performers":[
		{"id":"1","name":"Example","scene_count":7}]}}}`))
	defer srv.Close()

	yes := true
	got, count, err := NewClient(srv.URL).FindPerformers(context.Background(),
		PerformerFilter{Gender: "FEMALE", HasStashID: &yes}, 1, 10)
	if err != nil {
		t.Fatalf("FindPerformers: %v", err)
	}
	if count != 42 || len(got) != 1 || got[0].SceneCount != 7 {
		t.Errorf("got %d of %d: %+v", len(got), count, got)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["performer_filter"])
	if !strings.Contains(string(b), "FEMALE") || !strings.Contains(string(b), "NOT_NULL") {
		t.Errorf("filter = %s", b)
	}
}

func TestDeletePerformer(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerDestroy":true}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if err := c.DeletePerformer(context.Background(), "9"); err != nil {
		t.Fatalf("DeletePerformer: %v", err)
	}
	if got := cap.reqs[0].Variables["id"]; got != "9" {
		t.Errorf("id = %v", got)
	}
	if err := c.DeletePerformer(context.Background(), ""); err == nil {
		t.Error("want an error without an id")
	}
}

// All or nothing, so the error has to reach the caller rather than being
// mistaken for a partial success.
func TestDeletePerformersReportsAMissingID(t *testing.T) {
	_, c := server(t, reply(`{"errors":[{"message":"id 999 does not exist in performers"}]}`))
	err := c.DeletePerformers(context.Background(), "1", "999")
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Errorf("err = %v", err)
	}
	if err := c.DeletePerformers(context.Background(), "1", ""); err == nil {
		t.Error("want an error for an empty id in the list")
	}
}
