package stash

import (
	"context"
	"fmt"
	"strings"
)

// AddSceneTags adds tags to scenes, leaving the tags they already have alone.
//
// This is not what [SceneUpdate].TagIDs does. That field *replaces* a scene's
// tags, so adding one through it means reading the current list, appending,
// and writing it back — which drops any tag added in between, and drops all
// of them if the read is skipped. Stash's bulk update takes an ADD mode
// instead, and applies it to every scene named in one request.
func (c *Client) AddSceneTags(ctx context.Context, tagIDs []string, sceneIDs ...string) error {
	return c.bulkSceneTags(ctx, "ADD", tagIDs, sceneIDs)
}

// RemoveSceneTags removes tags from scenes, leaving their other tags alone.
// Removing a tag a scene does not have is not an error.
func (c *Client) RemoveSceneTags(ctx context.Context, tagIDs []string, sceneIDs ...string) error {
	return c.bulkSceneTags(ctx, "REMOVE", tagIDs, sceneIDs)
}

// AddScenePerformers adds performers to scenes, leaving the ones they
// already have alone — the same reason [Client.AddSceneTags] exists.
func (c *Client) AddScenePerformers(ctx context.Context, performerIDs []string, sceneIDs ...string) error {
	return c.bulkSceneIDs(ctx, "performer_ids", "ADD", performerIDs, sceneIDs)
}

// RemoveScenePerformers removes performers from scenes, leaving their others
// alone.
func (c *Client) RemoveScenePerformers(ctx context.Context, performerIDs []string, sceneIDs ...string) error {
	return c.bulkSceneIDs(ctx, "performer_ids", "REMOVE", performerIDs, sceneIDs)
}

func (c *Client) bulkSceneTags(ctx context.Context, mode string, tagIDs, sceneIDs []string) error {
	return c.bulkSceneIDs(ctx, "tag_ids", mode, tagIDs, sceneIDs)
}

func (c *Client) bulkSceneIDs(ctx context.Context, field, mode string, ids, sceneIDs []string) error {
	if len(ids) == 0 || len(sceneIDs) == 0 {
		// Sending either empty is a request that changes nothing, and Stash
		// need not hear about it.
		return nil
	}
	_, err := c.do(ctx, graphqlRequest{
		Query: `mutation($input: BulkSceneUpdateInput!) { bulkSceneUpdate(input: $input) { id } }`,
		Variables: map[string]any{"input": map[string]any{
			"ids": sceneIDs,
			field: map[string]any{"ids": ids, "mode": mode},
		}},
	})
	if err != nil {
		verb := map[string]string{"ADD": "adding", "REMOVE": "removing"}[mode]
		return fmt.Errorf("stash: %s scene %s: %w", verb, strings.TrimSuffix(field, "_ids"), err)
	}
	return nil
}
