package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// SceneFields is the selection set [Scene] decodes from, exported so a caller
// writing its own query with [Client.Execute] fills the same type completely
// rather than maintaining a parallel field list that drifts.
//
//	query := `query { findDuplicateScenes { ` + stash.SceneFields + ` } }`
//
// Every field here must exist on the oldest supported server — see
// docs/design.md.
const SceneFields = `
  id
  title
  code
  date
  details
  director
  urls
  rating100
  organized
  o_counter
  files {
    id
    basename
    path
    size
    mod_time
    format
    width
    height
    duration
    video_codec
    audio_codec
    frame_rate
    bit_rate
    fingerprints { type value }
  }
  tags { id name }
  performers { id name }
  studio { id name }
  stash_ids { endpoint stash_id }
  galleries { id title }`

// sceneCaptionFields is the optional captions selection. Deliberately not
// part of [SceneFields]: that constant is contracted to name only fields the
// oldest supported server has, and asking for one it lacks fails the whole
// query rather than that field.
const sceneCaptionFields = `
  captions { language_code caption_type }`

// sceneSelection is [SceneFields] plus any optional field the caller asked
// for and this server turns out to have.
//
// Nothing is probed unless an option asked for it: a client that never
// wants captions issues exactly the requests it always did, and pays no
// introspection round trip to find that out.
//
// When captions were requested, an introspection failure is treated as
// "unsupported" rather than returned. A server that will not answer __type
// says nothing about whether it has captions, and failing every scene query
// over a probe would be the worse trade. [Client.Supports] caches its
// answer for the client's lifetime, so a transient failure costs captions
// for that client rather than for one call — build a new client to
// re-probe.
func (c *Client) sceneSelection(ctx context.Context) string {
	if !c.wantCaptions {
		return SceneFields
	}
	if ok, err := c.Supports(ctx, "captions"); err == nil && ok {
		return SceneFields + sceneCaptionFields
	}
	return SceneFields
}

// FindScene returns one scene by ID. found is false when no scene has that ID,
// which is not an error.
func (c *Client) FindScene(ctx context.Context, id string) (scene *Scene, found bool, err error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `query($id: ID!) { findScene(id: $id) {` + c.sceneSelection(ctx) + ` } }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return nil, false, fmt.Errorf("stash: finding scene %s: %w", id, err)
	}
	var result struct {
		FindScene *Scene `json:"findScene"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("stash: decoding scene %s: %w", id, err)
	}
	if result.FindScene == nil {
		return nil, false, nil
	}
	return result.FindScene, true, nil
}

// findScenesQuery is built per call rather than being a constant: the
// selection set depends on what the server supports.
func findScenesQuery(selection string) string {
	return `
query FindScenes($filter: FindFilterType, $scene_filter: SceneFilterType) {
  findScenes(filter: $filter, scene_filter: $scene_filter) {
    count
    scenes {` + selection + `
    }
  }
}`
}

// FindScenes returns one page of scenes plus the total count matching the
// filter. Pages are 1-based and sorted by path, so paging is stable.
//
// A filter naming a performer or studio that does not exist returns
// [ErrPerformerNotFound] or [ErrStudioNotFound] rather than an empty page —
// otherwise a typo is indistinguishable from a genuine zero-result query.
func (c *Client) FindScenes(ctx context.Context, filter SceneFilter, page, perPage int) ([]Scene, int, error) {
	sceneFilter := map[string]any{}
	findFilter := map[string]any{
		"page":      page,
		"per_page":  perPage,
		"sort":      "path",
		"direction": "ASC",
	}

	if filter.Organized != nil {
		sceneFilter["organized"] = *filter.Organized
	}

	if filter.HasStashID != nil {
		modifier := "IS_NULL"
		if *filter.HasStashID {
			modifier = "NOT_NULL"
		}
		sceneFilter["stash_id_endpoint"] = map[string]any{"modifier": modifier}
	}

	if filter.PerformerName != "" {
		id, found, err := c.FindPerformer(ctx, filter.PerformerName)
		if err != nil {
			return nil, 0, fmt.Errorf("stash: resolving performer %q: %w", filter.PerformerName, err)
		}
		if !found {
			return nil, 0, fmt.Errorf("%w: %q", ErrPerformerNotFound, filter.PerformerName)
		}
		sceneFilter["performers"] = map[string]any{
			"value":    []string{id},
			"modifier": "INCLUDES_ALL",
		}
	}

	if filter.StudioName != "" {
		id, found, err := c.FindStudio(ctx, filter.StudioName)
		if err != nil {
			return nil, 0, fmt.Errorf("stash: resolving studio %q: %w", filter.StudioName, err)
		}
		if !found {
			return nil, 0, fmt.Errorf("%w: %q", ErrStudioNotFound, filter.StudioName)
		}
		// depth 0: this studio only, not its children.
		sceneFilter["studios"] = map[string]any{
			"value":    []string{id},
			"modifier": "INCLUDES_ALL",
			"depth":    0,
		}
	}

	if filter.PathContains != "" {
		sceneFilter["path"] = map[string]any{
			"value":    filter.PathContains,
			"modifier": "INCLUDES",
		}
	}

	vars := map[string]any{"filter": findFilter}
	if len(sceneFilter) > 0 {
		vars["scene_filter"] = sceneFilter
	}

	data, err := c.do(ctx, graphqlRequest{Query: findScenesQuery(c.sceneSelection(ctx)), Variables: vars})
	if err != nil {
		return nil, 0, fmt.Errorf("stash: finding scenes (page %d): %w", page, err)
	}

	var result struct {
		FindScenes struct {
			Count  int     `json:"count"`
			Scenes []Scene `json:"scenes"`
		} `json:"findScenes"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, fmt.Errorf("stash: decoding scenes: %w", err)
	}
	return result.FindScenes.Scenes, result.FindScenes.Count, nil
}

// Progress reports pagination advancing. total is the count reported by the
// first page.
type Progress func(fetched, total int)

// FindAllScenes pages through every scene matching the filter.
//
// On a large library this is a long operation — a 61k-scene instance takes
// several minutes and hundreds of requests. Pass a non-nil progress to report
// it, and a cancellable context: cancellation returns what was collected so
// far alongside ctx.Err().
func (c *Client) FindAllScenes(ctx context.Context, filter SceneFilter, progress Progress) ([]Scene, error) {
	const perPage = 100
	var all []Scene
	var total int
	for page := 1; ; page++ {
		scenes, count, err := c.FindScenes(ctx, filter, page, perPage)
		if err != nil {
			return nil, fmt.Errorf("stash: paginating scenes: %w", err)
		}
		if page == 1 {
			total = count
		}
		all = append(all, scenes...)
		if progress != nil {
			progress(len(all), total)
		}
		if len(scenes) < perPage {
			return all, nil
		}
		if err := ctx.Err(); err != nil {
			return all, err
		}
	}
}

// UpdateScene writes the fields set on update, leaving the rest untouched.
func (c *Client) UpdateScene(ctx context.Context, update SceneUpdate) error {
	if update.ID == "" {
		return fmt.Errorf("stash: UpdateScene: ID is required")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: SceneUpdateInput!) { sceneUpdate(input: $input) { id } }`,
		Variables: map[string]any{"input": update},
	})
	if err != nil {
		return fmt.Errorf("stash: updating scene %s: %w", update.ID, err)
	}
	return nil
}
