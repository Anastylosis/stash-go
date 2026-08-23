package stash

import (
	"context"
	"encoding/json"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
)

func savedInput(t *testing.T, req graphqlRequest) map[string]any {
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

func criteria(t *testing.T, req graphqlRequest, key string) map[string]any {
	t.Helper()
	in := savedInput(t, req)
	obj, ok := in["object_filter"].(map[string]any)
	if !ok {
		t.Fatalf("object_filter = %v", in["object_filter"])
	}
	c, ok := obj[key].(map[string]any)
	if !ok {
		t.Fatalf("no %q criterion in %v", key, obj)
	}
	return c
}

func TestSavedFiltersDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findSavedFilters":[
		{"id":"8","mode":"SCENES","name":"Made before 2010",
		 "find_filter":{"q":"","sort":"date","direction":"ASC","per_page":100},
		 "object_filter":{"date":{"modifier":"LESS_THAN","value":{"value":"2010-01-01"}}},
		 "ui_options":{"zoom_index":1}}]}}`))

	got, err := c.SavedFilters(context.Background(), FilterScenes)
	if err != nil {
		t.Fatalf("SavedFilters: %v", err)
	}
	if len(got) != 1 || got[0].Name != "Made before 2010" {
		t.Fatalf("filters = %+v", got)
	}
	if got[0].FindFilter == nil || got[0].FindFilter.Sort != "date" {
		t.Errorf("find_filter = %+v", got[0].FindFilter)
	}
	if got[0].ObjectFilter["date"] == nil {
		t.Errorf("object_filter = %v", got[0].ObjectFilter)
	}
}

// The whole reason this file exists: a saved filter does not write its
// criteria the way a query does, and Stash accepts the query notation, stores
// it, and then the filter does nothing in the UI.
func TestSaveSceneFilterWrapsTheDateValue(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findSavedFilters":[]}}`,
		`{"data":{"saveFilter":{"id":"8"}}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).SaveSceneFilter(context.Background(), "Made before 2010",
		SceneFilter{DateBefore: "2010-01-01"}, nil); err != nil {
		t.Fatalf("SaveSceneFilter: %v", err)
	}
	date := criteria(t, cap.reqs[1], "date")
	if date["modifier"] != "LESS_THAN" {
		t.Errorf("modifier = %v", date["modifier"])
	}
	inner, ok := date["value"].(map[string]any)
	if !ok {
		t.Fatalf("value = %v, want it wrapped in an object", date["value"])
	}
	if inner["value"] != "2010-01-01" {
		t.Errorf("value.value = %v", inner["value"])
	}
}

func TestSaveSceneFilterDateRange(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findSavedFilters":[]}}`,
		`{"data":{"saveFilter":{"id":"1"}}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).SaveSceneFilter(context.Background(), "2009",
		SceneFilter{DateAfter: "2009-01-01", DateBefore: "2010-01-01"}, nil); err != nil {
		t.Fatalf("SaveSceneFilter: %v", err)
	}
	date := criteria(t, cap.reqs[1], "date")
	if date["modifier"] != "BETWEEN" {
		t.Errorf("modifier = %v, want BETWEEN", date["modifier"])
	}
	inner := date["value"].(map[string]any)
	if inner["value"] != "2009-01-01" || inner["value2"] != "2010-01-01" {
		t.Errorf("value = %v", inner)
	}
}

// Tags are listed as labelled items, not as a list of ids.
func TestSaveSceneFilterWritesTagsAsItems(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findTags":{"tags":[{"id":"4"}]}}}`,
		`{"data":{"findSavedFilters":[]}}`,
		`{"data":{"saveFilter":{"id":"1"}}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).SaveSceneFilter(context.Background(), "untagged",
		SceneFilter{ExcludeTagNames: []string{"HD Available"}}, nil); err != nil {
		t.Fatalf("SaveSceneFilter: %v", err)
	}
	tags := criteria(t, cap.reqs[2], "tags")
	if tags["modifier"] != "EXCLUDES" {
		t.Errorf("modifier = %v", tags["modifier"])
	}
	value := tags["value"].(map[string]any)
	items, ok := value["items"].([]any)
	if !ok || len(items) != 1 {
		t.Fatalf("items = %v", value["items"])
	}
	item := items[0].(map[string]any)
	if item["id"] != float64(4) {
		t.Errorf("id = %v, want the number 4", item["id"])
	}
	// Stash percent-encodes the label it prints, so this does too.
	if item["label"] != "HD%20Available" {
		t.Errorf("label = %v", item["label"])
	}
	if value["depth"] != float64(0) {
		t.Errorf("depth = %v", value["depth"])
	}
}

// Booleans are stringly typed in a saved filter.
func TestSaveSceneFilterWritesOrganizedAsAString(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findSavedFilters":[]}}`,
		`{"data":{"saveFilter":{"id":"1"}}}`))
	defer srv.Close()

	no := false
	if _, err := NewClient(srv.URL).SaveSceneFilter(context.Background(), "unorganised",
		SceneFilter{Organized: &no}, nil); err != nil {
		t.Fatalf("SaveSceneFilter: %v", err)
	}
	if got := criteria(t, cap.reqs[1], "organized")["value"]; got != "false" {
		t.Errorf("value = %#v, want the string \"false\"", got)
	}
}

func TestSaveSceneFilterHasDate(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findSavedFilters":[]}}`,
		`{"data":{"saveFilter":{"id":"1"}}}`))
	defer srv.Close()

	no := false
	if _, err := NewClient(srv.URL).SaveSceneFilter(context.Background(), "undated",
		SceneFilter{HasDate: &no}, nil); err != nil {
		t.Fatalf("SaveSceneFilter: %v", err)
	}
	if got := criteria(t, cap.reqs[1], "date")["modifier"]; got != "IS_NULL" {
		t.Errorf("modifier = %v", got)
	}
}

// A program run twice should not leave two identical entries in someone's
// sidebar, and Stash allows the duplicate.
func TestSaveSceneFilterUpdatesOneOfTheSameName(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"data":{"findSavedFilters":[{"id":"8","mode":"SCENES","name":"Made before 2010","ui_options":{"zoom_index":2}}]}}`,
		`{"data":{"saveFilter":{"id":"8"}}}`))
	defer srv.Close()

	id, err := NewClient(srv.URL).SaveSceneFilter(context.Background(), "Made before 2010",
		SceneFilter{DateBefore: "2010-01-01"}, nil)
	if err != nil {
		t.Fatalf("SaveSceneFilter: %v", err)
	}
	if id != "8" {
		t.Errorf("id = %q, want the existing 8", id)
	}
	in := savedInput(t, cap.reqs[1])
	if in["id"] != "8" {
		t.Errorf("input id = %v, want 8", in["id"])
	}
	// What the UI remembers about the list is not this program's to discard.
	if in["ui_options"] == nil {
		t.Error("ui_options were dropped on update")
	}
}

func TestSaveFilterNeedsANameAndMode(t *testing.T) {
	_, c := server(t, reply(`{"data":{"saveFilter":{"id":"1"}}}`))
	if _, err := c.SaveFilter(context.Background(), SavedFilter{Mode: FilterScenes}); err == nil {
		t.Error("want an error without a name")
	}
	if _, err := c.SaveFilter(context.Background(), SavedFilter{Name: "x"}); err == nil {
		t.Error("want an error without a mode")
	}
}

func TestSaveSceneFilterReportsAnUnknownTag(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findTags":{"tags":[]}}}`))
	_, err := c.SaveSceneFilter(context.Background(), "x", SceneFilter{TagNames: []string{"nope"}}, nil)
	if !errors.Is(err, ErrTagNotFound) {
		t.Fatalf("err = %v, want ErrTagNotFound", err)
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v, want it to name the tag", err)
	}
}

func TestFindSavedFilterNotFound(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findSavedFilters":[]}}`))
	_, found, err := c.FindSavedFilter(context.Background(), FilterScenes, "nothing")
	if err != nil || found {
		t.Errorf("got (%v, %v)", found, err)
	}
}

func TestDestroySavedFilterNeedsAnID(t *testing.T) {
	_, c := server(t, reply(`{"data":{"destroySavedFilter":true}}`))
	if err := c.DestroySavedFilter(context.Background(), ""); err == nil {
		t.Error("want an error without an id")
	}
}
