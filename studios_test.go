package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindStudioByIDDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findStudio":{
		"id":"3","name":"Example Studio","details":"A studio.","scene_count":42,
		"urls":["https://example.test"],"aliases":["Example"],
		"stash_ids":[{"endpoint":"https://example.test/graphql","stash_id":"abc"}],
		"parent_studio":{"id":"1","name":"Parent"}}}}`))

	s, found, err := c.FindStudioByID(context.Background(), "3")
	if err != nil || !found {
		t.Fatalf("FindStudioByID: %v, found=%v", err, found)
	}
	if s.SceneCount != 42 || s.ParentStudio == nil || s.ParentStudio.Name != "Parent" {
		t.Errorf("studio = %+v", s)
	}
	if len(s.StashIDs) != 1 {
		t.Errorf("stash ids = %+v", s.StashIDs)
	}
}

func TestFindStudioByIDNotFound(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findStudio":null}}`))
	s, found, err := c.FindStudioByID(context.Background(), "404")
	if err != nil || found || s != nil {
		t.Errorf("got (%v, %v, %v)", s, found, err)
	}
}

// The same partial-update shape as scenes and performers: only what is set
// goes on the wire.
func TestUpdateStudioSendsOnlyWhatIsSet(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"studioUpdate":{"id":"3"}}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).UpdateStudio(context.Background(),
		StudioInput{ID: "3", Details: "Only this."}); err != nil {
		t.Fatalf("UpdateStudio: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	var in map[string]any
	_ = json.Unmarshal(b, &in)
	if len(in) != 2 || in["id"] != "3" || in["details"] != "Only this." {
		t.Errorf("input = %v", in)
	}
}

func TestUpdateStudioNeedsAnID(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if err := c.UpdateStudio(context.Background(), StudioInput{Name: "x"}); err == nil {
		t.Error("want an error without an id")
	}
	if _, err := c.CreateStudioFrom(context.Background(), StudioInput{}); err == nil {
		t.Error("want an error creating without a name")
	}
}

// A create must not carry an id, even when the caller reused a struct that
// had one.
func TestCreateStudioDropsAnyID(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"studioCreate":{"id":"9"}}}`))
	defer srv.Close()

	id, err := NewClient(srv.URL).CreateStudioFrom(context.Background(),
		StudioInput{ID: "leftover", Name: "New Studio"})
	if err != nil {
		t.Fatalf("CreateStudioFrom: %v", err)
	}
	if id != "9" {
		t.Errorf("id = %q", id)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	if strings.Contains(string(b), "leftover") {
		t.Errorf("input = %s, want no id on a create", b)
	}
}

func TestClearStudioFieldsPicksTheRightEmptyValue(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"studioUpdate":{"id":"3"}}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).ClearStudioFields(context.Background(), "3", "details", "aliases"); err != nil {
		t.Fatalf("ClearStudioFields: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	if !strings.Contains(string(b), `"details":""`) || !strings.Contains(string(b), `"aliases":[]`) {
		t.Errorf("input = %s", b)
	}
}

func TestStudiosPagesAndCounts(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findStudios":{"count":9,"studios":[
		{"id":"3","name":"Example Studio","scene_count":42}]}}}`))
	defer srv.Close()

	got, count, err := NewClient(srv.URL).Studios(context.Background(), 2, 25)
	if err != nil {
		t.Fatalf("Studios: %v", err)
	}
	if count != 9 || len(got) != 1 {
		t.Errorf("got %d of %d", len(got), count)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["filter"])
	if !strings.Contains(string(b), `"page":2`) || !strings.Contains(string(b), `"per_page":25`) {
		t.Errorf("filter = %s", b)
	}
}

func TestDeleteStudio(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"studioDestroy":true}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if err := c.DeleteStudio(context.Background(), "3"); err != nil {
		t.Fatalf("DeleteStudio: %v", err)
	}
	if got := cap.reqs[0].Variables["id"]; got != "3" {
		t.Errorf("id = %v", got)
	}
	if err := c.DeleteStudio(context.Background(), ""); err == nil {
		t.Error("want an error without an id")
	}
}

func TestDeleteStudios(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"studiosDestroy":true}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if err := c.DeleteStudios(context.Background(), "3", "4"); err != nil {
		t.Fatalf("DeleteStudios: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["ids"])
	if string(b) != `["3","4"]` {
		t.Errorf("ids = %s", b)
	}
	// Nothing to delete is not a request worth making.
	if err := c.DeleteStudios(context.Background()); err != nil {
		t.Fatalf("DeleteStudios with no ids: %v", err)
	}
	if len(cap.reqs) != 1 {
		t.Errorf("sent %d requests, want 1", len(cap.reqs))
	}
}

func TestClearStudioFieldsRejectsSomethingThatIsNotAField(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if err := c.ClearStudioFields(context.Background(), "3", "details aliases"); err == nil {
		t.Error("want an error for something that is not a field name")
	}
	if err := c.ClearStudioFields(context.Background(), "", "details"); err == nil {
		t.Error("want an error without an id")
	}
	if err := c.ClearStudioFields(context.Background(), "3"); err != nil {
		t.Errorf("clearing nothing should be a no-op, got %v", err)
	}
}
