package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// SceneUpdate.TagIDs replaces a scene's tags. Adding one has to go through
// the bulk update's ADD mode, or every other tag on the scene is lost.
func TestAddSceneTagsUsesAddMode(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"bulkSceneUpdate":[{"id":"1"}]}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).AddSceneTags(context.Background(), []string{"7"}, "1", "2"); err != nil {
		t.Fatalf("AddSceneTags: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	var in struct {
		IDs    []string `json:"ids"`
		TagIDs struct {
			IDs  []string `json:"ids"`
			Mode string   `json:"mode"`
		} `json:"tag_ids"`
	}
	if err := json.Unmarshal(b, &in); err != nil {
		t.Fatalf("decoding input: %v", err)
	}
	if in.TagIDs.Mode != "ADD" {
		t.Errorf("mode = %q, want ADD", in.TagIDs.Mode)
	}
	if len(in.IDs) != 2 || len(in.TagIDs.IDs) != 1 {
		t.Errorf("input = %+v", in)
	}
}

func TestRemoveSceneTagsUsesRemoveMode(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"bulkSceneUpdate":[{"id":"1"}]}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).RemoveSceneTags(context.Background(), []string{"7"}, "1"); err != nil {
		t.Fatalf("RemoveSceneTags: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	var in struct {
		TagIDs struct {
			Mode string `json:"mode"`
		} `json:"tag_ids"`
	}
	_ = json.Unmarshal(b, &in)
	if in.TagIDs.Mode != "REMOVE" {
		t.Errorf("mode = %q, want REMOVE", in.TagIDs.Mode)
	}
}

// An empty list either side is a request that changes nothing.
func TestSceneTagsWithNothingToDoMakesNoRequest(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if err := c.AddSceneTags(context.Background(), nil, "1"); err != nil {
		t.Fatalf("AddSceneTags: %v", err)
	}
	if err := c.AddSceneTags(context.Background(), []string{"7"}); err != nil {
		t.Fatalf("AddSceneTags: %v", err)
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests, want none", len(cap.reqs))
	}
}
