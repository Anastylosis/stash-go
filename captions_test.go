package stash

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// Captions are part of the shared selection set: the supported server has the
// field, so there is nothing to opt into and no probe to pay for.
func TestSceneQueriesAlwaysAskForCaptions(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"findScene":{"id":"1"}}}`))
	defer srv.Close()

	if _, _, err := NewClient(srv.URL).FindScene(context.Background(), "1"); err != nil {
		t.Fatalf("FindScene: %v", err)
	}
	if len(capt.reqs) != 1 {
		t.Fatalf("made %d requests, want 1 — nothing is probed", len(capt.reqs))
	}
	if !strings.Contains(capt.reqs[0].Query, "captions") {
		t.Error("the selection set did not ask for captions")
	}
}

func TestCaptionsDecode(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":{"id":"1","captions":[
		{"language_code":"en","caption_type":"srt"},
		{"language_code":"pl","caption_type":"vtt"}
	]}}}`))

	scene, found, err := c.FindScene(context.Background(), "1")
	if err != nil || !found {
		t.Fatalf("FindScene: %v found=%v", err, found)
	}
	if len(scene.Captions) != 2 {
		t.Fatalf("len(captions) = %d, want 2", len(scene.Captions))
	}
	if scene.Captions[0].LanguageCode != "en" || scene.Captions[0].CaptionType != "srt" {
		t.Errorf("captions[0] = %+v", scene.Captions[0])
	}
	if scene.Captions[1].LanguageCode != "pl" {
		t.Errorf("captions[1] = %+v", scene.Captions[1])
	}
}

// A scene with no subtitles answers null, which must read as "none" rather
// than tripping the decode.
func TestNoCaptionsIsNotAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":{"id":"1","captions":null}}}`))

	scene, found, err := c.FindScene(context.Background(), "1")
	if err != nil || !found {
		t.Fatalf("FindScene: %v found=%v", err, found)
	}
	if len(scene.Captions) != 0 {
		t.Errorf("captions = %+v, want none", scene.Captions)
	}
}

// WithCaptions is kept so callers written against it still compile.
func TestWithCaptionsIsANoOp(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"findScene":{"id":"1"}}}`))
	defer srv.Close()

	c := NewClient(srv.URL, WithCaptions())
	if _, _, err := c.FindScene(context.Background(), "1"); err != nil {
		t.Fatalf("FindScene: %v", err)
	}
	if len(capt.reqs) != 1 {
		t.Errorf("made %d requests with the option set, want 1 — it must not probe", len(capt.reqs))
	}
}

func TestSceneGroupsAndPlaybackDecode(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":{"id":"1",
		"groups":[{"group":{"id":"7","name":"A Series"},"scene_index":3}],
		"play_count":12,"play_duration":3456.7,"resume_time":120.5,
		"last_played_at":"2026-08-01T10:00:00Z"}}}`))

	scene, _, err := c.FindScene(context.Background(), "1")
	if err != nil {
		t.Fatalf("FindScene: %v", err)
	}
	if len(scene.Groups) != 1 || scene.Groups[0].Group.Name != "A Series" {
		t.Fatalf("groups = %+v", scene.Groups)
	}
	if scene.Groups[0].SceneIndex == nil || *scene.Groups[0].SceneIndex != 3 {
		t.Errorf("scene_index = %v, want 3", scene.Groups[0].SceneIndex)
	}
	if scene.PlayCount != 12 || scene.PlayDuration != 3456.7 || scene.ResumeTime != 120.5 {
		t.Errorf("playback = %+v", scene)
	}
	if scene.LastPlayedAt == nil || *scene.LastPlayedAt != "2026-08-01T10:00:00Z" {
		t.Errorf("last_played_at = %v", scene.LastPlayedAt)
	}
}

// An unordered group leaves scene_index null; nil must survive as "no index"
// rather than decoding to a positional zero.
func TestSceneGroupWithoutIndex(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":{"id":"1",
		"groups":[{"group":{"id":"7","name":"Unordered"},"scene_index":null}]}}}`))

	scene, _, err := c.FindScene(context.Background(), "1")
	if err != nil {
		t.Fatalf("FindScene: %v", err)
	}
	if scene.Groups[0].SceneIndex != nil {
		t.Errorf("scene_index = %v, want nil", *scene.Groups[0].SceneIndex)
	}
}
