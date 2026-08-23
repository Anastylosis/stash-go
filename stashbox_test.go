package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestStashBoxConfigsCarryTheKey(t *testing.T) {
	_, c := server(t, reply(`{"data":{"configuration":{"general":{"stashBoxes":[
		{"name":"Stash","endpoint":"https://example.test/graphql","api_key":"secret","max_requests_per_minute":0}]}}}}`))

	got, err := c.StashBoxConfigs(context.Background())
	if err != nil {
		t.Fatalf("StashBoxConfigs: %v", err)
	}
	// This one does carry the key, unlike StashBoxes, because rewriting the
	// list means sending back the keys of the entries being kept.
	if len(got) != 1 || got[0].APIKey != "secret" {
		t.Errorf("configs = %+v", got)
	}
}

// It replaces; it does not add. Sending one box removes every other, along
// with its key, and nothing asks first.
func TestSetStashBoxesSendsTheWholeList(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"configureGeneral":{"__typename":"ConfigResult"}}}`))
	defer srv.Close()

	err := NewClient(srv.URL).SetStashBoxes(context.Background(), []StashBoxConfig{
		{Name: "Stash", Endpoint: "https://example.test/graphql", APIKey: "a"},
		{Name: "Local", Endpoint: "http://localhost:9997/graphql", APIKey: "b"},
	})
	if err != nil {
		t.Fatalf("SetStashBoxes: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	var in struct {
		StashBoxes []map[string]any `json:"stashBoxes"`
	}
	_ = json.Unmarshal(b, &in)
	if len(in.StashBoxes) != 2 {
		t.Fatalf("sent %d boxes, want both", len(in.StashBoxes))
	}
	if in.StashBoxes[0]["api_key"] != "a" {
		t.Errorf("the kept entry lost its key: %v", in.StashBoxes[0])
	}
}

// Removing them all is a legitimate thing to want, so it is not refused.
func TestSetStashBoxesAcceptsAnEmptyList(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"configureGeneral":{"__typename":"ConfigResult"}}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).SetStashBoxes(context.Background(), nil); err != nil {
		t.Fatalf("SetStashBoxes: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	if !strings.Contains(string(b), `"stashBoxes":[]`) {
		t.Errorf("input = %s, want an explicit empty list", b)
	}
}

func TestSetStashBoxesRefusesOneWithNoEndpoint(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	err := c.SetStashBoxes(context.Background(), []StashBoxConfig{{Name: "nowhere"}})
	if err == nil {
		t.Error("want an error for a box with no endpoint")
	}
}

// The server makes the request, which is the point when the server and this
// program are on different machines.
func TestValidateStashBox(t *testing.T) {
	_, c := server(t, reply(`{"data":{"validateStashBoxCredentials":{"valid":true,"status":"Successfully authenticated as root"}}}`))
	valid, status, err := c.ValidateStashBox(context.Background(), "https://example.test/graphql", "key")
	if err != nil {
		t.Fatalf("ValidateStashBox: %v", err)
	}
	if !valid || !strings.Contains(status, "authenticated") {
		t.Errorf("got (%v, %q)", valid, status)
	}
}

func TestSubmitSceneDraftReturnsTheDraftID(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"submitStashBoxSceneDraft":"draft-1"}}`))
	defer srv.Close()

	id, err := NewClient(srv.URL).SubmitSceneDraft(context.Background(), "36", "https://example.test/graphql")
	if err != nil {
		t.Fatalf("SubmitSceneDraft: %v", err)
	}
	if id != "draft-1" {
		t.Errorf("draft id = %q", id)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	if !strings.Contains(string(b), `"id":"36"`) || !strings.Contains(string(b), "stash_box_endpoint") {
		t.Errorf("input = %s", b)
	}
}

func TestSubmitPerformerDraft(t *testing.T) {
	_, c := server(t, reply(`{"data":{"submitStashBoxPerformerDraft":"draft-2"}}`))
	id, err := c.SubmitPerformerDraft(context.Background(), "1", "https://example.test/graphql")
	if err != nil || id != "draft-2" {
		t.Errorf("got (%q, %v)", id, err)
	}
}

// The mutation is a nullable ID, so a submission the box declined is not an
// error at the GraphQL level — and "" with no error would be a caller's bug
// waiting to happen.
func TestSubmitDraftNullIsAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"submitStashBoxSceneDraft":null}}`))
	if _, err := c.SubmitSceneDraft(context.Background(), "36", "https://example.test/graphql"); err == nil {
		t.Error("want an error when the box returns no draft id")
	}
}

func TestSubmitDraftNeedsBothArguments(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if _, err := c.SubmitSceneDraft(context.Background(), "", "https://example.test/graphql"); err == nil {
		t.Error("want an error without an id")
	}
	if _, err := c.SubmitSceneDraft(context.Background(), "36", ""); err == nil {
		t.Error("want an error without an endpoint")
	}
}

func TestSubmitFingerprints(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"submitStashBoxFingerprints":true}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	ok, err := c.SubmitFingerprints(context.Background(), "https://example.test/graphql", "1", "2")
	if err != nil || !ok {
		t.Fatalf("got (%v, %v)", ok, err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	var in struct {
		SceneIDs []string `json:"scene_ids"`
	}
	_ = json.Unmarshal(b, &in)
	if !reflect.DeepEqual(in.SceneIDs, []string{"1", "2"}) {
		t.Errorf("scene_ids = %v", in.SceneIDs)
	}

	// No scenes is not a request worth making.
	if ok, err := c.SubmitFingerprints(context.Background(), "https://example.test/graphql"); err != nil || ok {
		t.Errorf("got (%v, %v) for no scenes", ok, err)
	}
	if len(cap.reqs) != 1 {
		t.Errorf("sent %d requests, want 1", len(cap.reqs))
	}
	if _, err := c.SubmitFingerprints(context.Background(), "", "1"); err == nil {
		t.Error("want an error without an endpoint")
	}
}
