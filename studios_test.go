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
