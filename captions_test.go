package stash

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

const introspectionWithCaptions = `{"data":{"__type":{"fields":[{"name":"id"},{"name":"captions"}]}}}`
const introspectionWithoutCaptions = `{"data":{"__type":{"fields":[{"name":"id"}]}}}`

// The default must be exactly the behaviour that shipped before captions
// existed: no probe, no captions field, one request.
func TestSceneQueriesDoNotProbeByDefault(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findScene":{"id":"1"}}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if _, _, err := c.FindScene(context.Background(), "1"); err != nil {
		t.Fatalf("FindScene: %v", err)
	}
	if len(cap.reqs) != 1 {
		t.Fatalf("made %d requests, want 1 — no introspection unless captions were asked for", len(cap.reqs))
	}
	if strings.Contains(cap.reqs[0].Query, "captions") {
		t.Error("the default selection set asked for captions")
	}
}

func TestWithCaptionsAddsTheFieldWhenSupported(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		introspectionWithCaptions,
		`{"data":{"findScene":{"id":"1","captions":[{"language_code":"pt","caption_type":"srt"}]}}}`,
	))
	defer srv.Close()
	c := NewClient(srv.URL, WithCaptions())

	scene, found, err := c.FindScene(context.Background(), "1")
	if err != nil || !found {
		t.Fatalf("FindScene: %v found=%v", err, found)
	}
	if len(cap.reqs) != 2 || !strings.Contains(cap.reqs[0].Query, "__type") {
		t.Fatalf("expected an introspection then the query, got %d requests", len(cap.reqs))
	}
	if !strings.Contains(cap.reqs[1].Query, "captions") {
		t.Error("the scene query did not ask for captions")
	}
	if len(scene.Captions) != 1 || scene.Captions[0].LanguageCode != "pt" {
		t.Errorf("captions = %+v, want one pt caption", scene.Captions)
	}
}

// A server too old to have the field must still serve scenes: the whole
// reason this is probed rather than simply asked for is that requesting an
// absent field fails the entire query.
func TestWithCaptionsDegradesOnOldServer(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		introspectionWithoutCaptions,
		`{"data":{"findScene":{"id":"1"}}}`,
	))
	defer srv.Close()
	c := NewClient(srv.URL, WithCaptions())

	scene, found, err := c.FindScene(context.Background(), "1")
	if err != nil || !found {
		t.Fatalf("FindScene: %v found=%v", err, found)
	}
	if strings.Contains(cap.reqs[1].Query, "captions") {
		t.Error("asked a server that lacks captions for captions; that fails the whole query")
	}
	if scene.Captions != nil {
		t.Errorf("captions = %v, want nil", scene.Captions)
	}
}

// A server that refuses introspection says nothing about whether it has
// captions. Failing every scene query over that would be the worse trade.
func TestWithCaptionsDegradesWhenIntrospectionFails(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		`{"errors":[{"message":"introspection disabled"}]}`,
		`{"data":{"findScene":{"id":"1"}}}`,
	))
	defer srv.Close()
	c := NewClient(srv.URL, WithCaptions())

	if _, found, err := c.FindScene(context.Background(), "1"); err != nil || !found {
		t.Fatalf("FindScene: %v found=%v", err, found)
	}
	if strings.Contains(cap.reqs[1].Query, "captions") {
		t.Error("asked for captions on a server whose schema could not be read")
	}
}

// The probe is cached: a task walking a library must not introspect once
// per scene.
func TestWithCaptionsProbesOnlyOnce(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		introspectionWithCaptions,
		`{"data":{"findScene":{"id":"1"}}}`,
	))
	defer srv.Close()
	c := NewClient(srv.URL, WithCaptions())

	for range 3 {
		if _, _, err := c.FindScene(context.Background(), "1"); err != nil {
			t.Fatalf("FindScene: %v", err)
		}
	}
	introspections := 0
	for _, r := range cap.reqs {
		if strings.Contains(r.Query, "__type") {
			introspections++
		}
	}
	if introspections != 1 {
		t.Errorf("introspected %d times across 3 scene fetches, want 1", introspections)
	}
}

// FindScenes goes through the same selection, so a library walk gets
// captions too rather than only the single-scene path.
func TestWithCaptionsAppliesToFindScenes(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(
		introspectionWithCaptions,
		`{"data":{"findScenes":{"count":1,"scenes":[{"id":"1","captions":[{"language_code":"en","caption_type":"srt"}]}]}}}`,
	))
	defer srv.Close()
	c := NewClient(srv.URL, WithCaptions())

	scenes, _, err := c.FindScenes(context.Background(), SceneFilter{}, 1, 10)
	if err != nil {
		t.Fatalf("FindScenes: %v", err)
	}
	if len(scenes) != 1 || len(scenes[0].Captions) != 1 || scenes[0].Captions[0].LanguageCode != "en" {
		t.Errorf("scenes = %+v, want one scene with an en caption", scenes)
	}
}
