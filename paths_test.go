package stash

import (
	"context"
	"testing"
)

func TestScenePathsDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":{"paths":{
		"screenshot":"http://x/scene/1/screenshot","preview":"http://x/scene/1/preview",
		"webp":"http://x/scene/1/webp","stream":"http://x/scene/1/stream",
		"sprite":"http://x/scene/abc_sprite.jpg","vtt":"http://x/scene/abc_thumbs.vtt"}}}}`))

	paths, err := c.ScenePaths(context.Background(), "1")
	if err != nil {
		t.Fatalf("ScenePaths: %v", err)
	}
	if paths.Sprite != "http://x/scene/abc_sprite.jpg" || paths.VTT != "http://x/scene/abc_thumbs.vtt" {
		t.Errorf("paths = %+v", paths)
	}
	if paths.Stream != "http://x/scene/1/stream" {
		t.Errorf("stream = %q", paths.Stream)
	}
}

// Stash generates these lazily, so an empty field is "not generated yet"
// rather than an error.
func TestScenePathsOfAnUngeneratedScene(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":{"paths":{"screenshot":"http://x/s","sprite":""}}}}`))
	paths, err := c.ScenePaths(context.Background(), "1")
	if err != nil {
		t.Fatalf("ScenePaths: %v", err)
	}
	if paths.Sprite != "" || paths.Screenshot == "" {
		t.Errorf("paths = %+v", paths)
	}
}

func TestScenePathsOfAMissingScene(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScene":null}}`))
	if _, err := c.ScenePaths(context.Background(), "404"); err == nil {
		t.Error("ScenePaths: want an error for a scene that does not exist")
	}
}
