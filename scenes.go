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
//	query := `query { sceneWall(q: "beach") { ` + stash.SceneFields + ` } }`
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
  galleries { id title }
  captions { language_code caption_type }
  groups { group { id name } scene_index }
  play_count
  play_duration
  last_played_at
  resume_time`

// FindScene returns one scene by ID. found is false when no scene has that ID,
// which is not an error.
func (c *Client) FindScene(ctx context.Context, id string) (scene *Scene, found bool, err error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `query($id: ID!) { findScene(id: $id) {` + SceneFields + ` } }`,
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

// SceneFilterCriteria renders a [SceneFilter] as the criterion map Stash
// speaks, resolving performer, studio and tag names to ids on the way.
//
// Exported because it is what a saved filter stores: the same filter that
// selects scenes here can be handed to [Client.SaveSceneFilter] and appear in
// the UI, rather than being described twice in two notations.
func (c *Client) SceneFilterCriteria(ctx context.Context, filter SceneFilter) (map[string]any, error) {
	sceneFilter := map[string]any{}

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
			return nil, fmt.Errorf("stash: resolving performer %q: %w", filter.PerformerName, err)
		}
		if !found {
			return nil, fmt.Errorf("%w: %q", ErrPerformerNotFound, filter.PerformerName)
		}
		sceneFilter["performers"] = map[string]any{
			"value":    []string{id},
			"modifier": "INCLUDES_ALL",
		}
	}

	if filter.StudioName != "" {
		id, found, err := c.FindStudio(ctx, filter.StudioName)
		if err != nil {
			return nil, fmt.Errorf("stash: resolving studio %q: %w", filter.StudioName, err)
		}
		if !found {
			return nil, fmt.Errorf("%w: %q", ErrStudioNotFound, filter.StudioName)
		}
		// depth 0: this studio only, not its children.
		sceneFilter["studios"] = map[string]any{
			"value":    []string{id},
			"modifier": "INCLUDES_ALL",
			"depth":    0,
		}
	}

	// Stash takes one tags criterion, so "has these" and "has none of
	// these" cannot both be sent — the second would overwrite the first.
	if len(filter.TagNames) > 0 && len(filter.ExcludeTagNames) > 0 {
		return nil, errTwoTagFilters
	}
	for _, tags := range []struct {
		names    []string
		modifier string
	}{
		{filter.TagNames, "INCLUDES_ALL"},
		{filter.ExcludeTagNames, "EXCLUDES"},
	} {
		if len(tags.names) == 0 {
			continue
		}
		ids := make([]string, 0, len(tags.names))
		for _, name := range tags.names {
			id, found, err := c.FindTag(ctx, name)
			if err != nil {
				return nil, fmt.Errorf("stash: resolving tag %q: %w", name, err)
			}
			if !found {
				return nil, fmt.Errorf("%w: %q", ErrTagNotFound, name)
			}
			ids = append(ids, id)
		}
		sceneFilter["tags"] = map[string]any{
			"value":    ids,
			"modifier": tags.modifier,
			"depth":    0,
		}
	}

	if filter.HasDate != nil {
		modifier := "IS_NULL"
		if *filter.HasDate {
			modifier = "NOT_NULL"
		}
		// value is required even where the modifier ignores it: Stash's
		// DateCriterionInput declares it non-null, and omitting it fails
		// the query rather than defaulting.
		sceneFilter["date"] = map[string]any{"modifier": modifier, "value": ""}
	}

	// Stash takes one date criterion, so the two bounds become a range
	// rather than two filters — asking for both separately would silently
	// keep only the last.
	switch {
	case filter.DateAfter != "" && filter.DateBefore != "":
		sceneFilter["date"] = map[string]any{
			"value":    filter.DateAfter,
			"value2":   filter.DateBefore,
			"modifier": "BETWEEN",
		}
	case filter.DateAfter != "":
		sceneFilter["date"] = map[string]any{
			"value":    filter.DateAfter,
			"modifier": "GREATER_THAN",
		}
	case filter.DateBefore != "":
		sceneFilter["date"] = map[string]any{
			"value":    filter.DateBefore,
			"modifier": "LESS_THAN",
		}
	}

	if filter.PathContains != "" {
		sceneFilter["path"] = map[string]any{
			"value":    filter.PathContains,
			"modifier": "INCLUDES",
		}
	}

	if filter.MultiFile != nil {
		modifier := "EQUALS"
		if *filter.MultiFile {
			modifier = "GREATER_THAN"
		}
		sceneFilter["file_count"] = map[string]any{"value": 1, "modifier": modifier}
	}

	return sceneFilter, nil
}

// FindScenes returns one page of scenes plus the total count matching the
// filter. Pages are 1-based and sorted by path, so paging is stable.
//
// A filter naming a performer or studio that does not exist returns
// [ErrPerformerNotFound] or [ErrStudioNotFound] rather than an empty page —
// otherwise a typo is indistinguishable from a genuine zero-result query.
func (c *Client) FindScenes(ctx context.Context, filter SceneFilter, page, perPage int) ([]Scene, int, error) {
	sceneFilter, err := c.SceneFilterCriteria(ctx, filter)
	if err != nil {
		return nil, 0, err
	}
	findFilter := map[string]any{
		"page":      page,
		"per_page":  perPage,
		"sort":      "path",
		"direction": "ASC",
	}

	vars := map[string]any{"filter": findFilter}
	if len(sceneFilter) > 0 {
		vars["scene_filter"] = sceneFilter
	}

	data, err := c.do(ctx, graphqlRequest{Query: findScenesQuery(SceneFields), Variables: vars})
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

// SetSceneStashIDs replaces a scene's stash-box ids, and can clear them.
//
// [SceneUpdate] cannot. Its fields are omitted when empty so that an unset
// one leaves the stored value alone, which is what makes partial updates
// safe — and it means an empty StashIDs slice is indistinguishable from "do
// not touch the stash ids". Removing the last one therefore needs a call that
// always sends the field.
//
// Passing nil clears them.
func (c *Client) SetSceneStashIDs(ctx context.Context, sceneID string, ids []StashID) error {
	if sceneID == "" {
		return fmt.Errorf("stash: setting stash ids: no scene id")
	}
	list := make([]map[string]string, 0, len(ids))
	for _, id := range ids {
		list = append(list, map[string]string{"endpoint": id.Endpoint, "stash_id": id.ID})
	}
	_, err := c.do(ctx, graphqlRequest{
		Query: `mutation($input: SceneUpdateInput!) { sceneUpdate(input: $input) { id } }`,
		Variables: map[string]any{"input": map[string]any{
			"id":        sceneID,
			"stash_ids": list,
		}},
	})
	if err != nil {
		return fmt.Errorf("stash: setting stash ids on scene %s: %w", sceneID, err)
	}
	return nil
}

// ClearSceneFields empties the named fields, which [SceneUpdate] cannot: it
// omits what is unset so that a partial update is safe, and that makes "" and
// "leave it alone" the same request.
//
// Names are the SceneUpdateInput field names — "title", "details", "date",
// "code". A name Stash does not know fails the whole mutation rather than
// being ignored.
func (c *Client) ClearSceneFields(ctx context.Context, id string, fields ...string) error {
	if id == "" {
		return fmt.Errorf("stash: clearing scene fields: no id")
	}
	if len(fields) == 0 {
		return nil
	}
	in := map[string]any{"id": id}
	for _, f := range fields {
		if !isFieldName(f) {
			return fmt.Errorf("stash: clearing scene fields: %q is not a field name", f)
		}
		switch f {
		case "urls", "tag_ids", "performer_ids", "gallery_ids", "stash_ids":
			in[f] = []any{}
		default:
			in[f] = ""
		}
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: SceneUpdateInput!) { sceneUpdate(input: $input) { id } }`,
		Variables: map[string]any{"input": in},
	})
	if err != nil {
		return fmt.Errorf("stash: clearing fields on scene %s: %w", id, err)
	}
	return nil
}

// FindDuplicateScenes returns groups of scenes Stash considers the same
// content, matched on the perceptual hash of the video rather than on any
// metadata. Each group holds two or more scenes; a library with no duplicates
// returns none.
//
// distance is the hamming distance allowed between two phashes. 0 demands
// identical hashes, which finds the same encode stored twice. 4 is the useful
// setting and catches re-encodes and resolution changes. Past 8 the matches
// stop being trustworthy.
//
// durationDiff bounds how far apart two scenes' runtimes may be, in seconds,
// and is the strong filter of the two: phash collides across unrelated videos
// often enough to matter, but a collision that also agrees on length to
// within a second rarely does. Pass 0 to demand equal durations, or a
// negative value to leave duration out of it.
//
// The whole result arrives in one response, and every scene in it carries the
// full selection set — on a large library that is megabytes. There is no
// paged form of this query: Stash computes the grouping in one pass and has
// nowhere to hold it between calls.
func (c *Client) FindDuplicateScenes(ctx context.Context, distance int, durationDiff float64) ([][]Scene, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($distance: Int, $duration_diff: Float) {
			findDuplicateScenes(distance: $distance, duration_diff: $duration_diff) {` +
			SceneFields + ` } }`,
		Variables: map[string]any{"distance": distance, "duration_diff": durationDiff},
	})
	if err != nil {
		return nil, fmt.Errorf("stash: finding duplicate scenes: %w", err)
	}
	var result struct {
		FindDuplicateScenes [][]Scene `json:"findDuplicateScenes"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding duplicate scenes: %w", err)
	}
	return result.FindDuplicateScenes, nil
}
