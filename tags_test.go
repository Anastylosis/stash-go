package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindTagByIDDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findTag":{
		"id":"5","name":"date_from_scene","description":"read off the video",
		"aliases":["from the card"],"scene_count":2070,
		"parents":[{"id":"1","name":"dates"}],"children":[]}}}`))

	tag, found, err := c.FindTagByID(context.Background(), "5")
	if err != nil || !found {
		t.Fatalf("FindTagByID: %v, found=%v", err, found)
	}
	if tag.SceneCount != 2070 || len(tag.Parents) != 1 || tag.Parents[0].Name != "dates" {
		t.Errorf("tag = %+v", tag)
	}
}

func TestUpdateTagSendsOnlyWhatIsSet(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"tagUpdate":{"id":"5"}}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).UpdateTag(context.Background(),
		TagInput{ID: "5", Description: "Only this."}); err != nil {
		t.Fatalf("UpdateTag: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	var in map[string]any
	_ = json.Unmarshal(b, &in)
	if len(in) != 2 || in["description"] != "Only this." {
		t.Errorf("input = %v", in)
	}
}

func TestMergeTagsSendsSourcesAndDestination(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"tagsMerge":{"id":"5"}}}`))
	defer srv.Close()

	name := "the better name"
	err := NewClient(srv.URL).MergeTags(context.Background(), "5", []string{"6", "7"}, &TagInput{Name: name})
	if err != nil {
		t.Fatalf("MergeTags: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	var in map[string]any
	_ = json.Unmarshal(b, &in)
	if in["destination"] != "5" {
		t.Errorf("destination = %v", in["destination"])
	}
	if sources, _ := in["source"].([]any); len(sources) != 2 {
		t.Errorf("source = %v", in["source"])
	}
	// values carries the destination's id, not a source's.
	values, ok := in["values"].(map[string]any)
	if !ok || values["id"] != "5" || values["name"] != name {
		t.Errorf("values = %v", in["values"])
	}
}

// Stash would fold the destination into itself and delete it.
func TestMergeTagsRefusesToMergeIntoItself(t *testing.T) {
	_, c := server(t, reply(`{"data":{"tagsMerge":{"id":"5"}}}`))
	if err := c.MergeTags(context.Background(), "5", []string{"6", "5"}, nil); err == nil {
		t.Error("want an error when a source is the destination")
	}
	if err := c.MergeTags(context.Background(), "5", nil, nil); err == nil {
		t.Error("want an error with no sources")
	}
	if err := c.MergeTags(context.Background(), "", []string{"6"}, nil); err == nil {
		t.Error("want an error with no destination")
	}
}

func TestClearTagFieldsPicksTheRightEmptyValue(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"tagUpdate":{"id":"5"}}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).ClearTagFields(context.Background(), "5", "description", "aliases"); err != nil {
		t.Fatalf("ClearTagFields: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	if !strings.Contains(string(b), `"description":""`) || !strings.Contains(string(b), `"aliases":[]`) {
		t.Errorf("input = %s", b)
	}
}

func TestDeleteTagsWithNothingToDo(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{}}`))
	defer srv.Close()
	if err := NewClient(srv.URL).DeleteTags(context.Background()); err != nil {
		t.Fatalf("DeleteTags: %v", err)
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests, want none", len(cap.reqs))
	}
}
