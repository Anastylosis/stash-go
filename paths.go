package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// ScenePaths are the URLs of a scene's generated media — the things GraphQL
// will not return as data. Fetch one with [Client.Fetch].
//
// An empty field means Stash has not generated that piece for this scene.
// Sprite and VTT go together: the sheet is a grid of frames and the WebVTT is
// what says which frame is which moment.
type ScenePaths struct {
	// Screenshot is the scene's cover, at the video's own resolution.
	Screenshot string `json:"screenshot"`
	// Preview is a short video, Webp a short animation.
	Preview string `json:"preview"`
	Webp    string `json:"webp"`
	// Stream serves the video itself, and honours range requests — which is
	// what lets a frame be pulled from the middle of a file without
	// downloading it.
	Stream string `json:"stream"`
	Sprite string `json:"sprite"`
	VTT    string `json:"vtt"`
}

// ScenePaths returns the media URLs for one scene.
//
// This is a call of its own rather than a field on [Scene] because
// [SceneFields] is shared by every scene query, and a field that turns out to
// be missing on an older server costs all of them their whole response. A
// caller that wants paths asks for paths.
func (c *Client) ScenePaths(ctx context.Context, id string) (ScenePaths, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($id: ID!) { findScene(id: $id) {
			paths { screenshot preview webp stream sprite vtt } } }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return ScenePaths{}, fmt.Errorf("stash: reading paths for scene %s: %w", id, err)
	}
	var result struct {
		FindScene *struct {
			Paths ScenePaths `json:"paths"`
		} `json:"findScene"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return ScenePaths{}, fmt.Errorf("stash: decoding paths for scene %s: %w", id, err)
	}
	if result.FindScene == nil {
		return ScenePaths{}, fmt.Errorf("stash: no scene %s", id)
	}
	return result.FindScene.Paths, nil
}
