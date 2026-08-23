package stash

import (
	"context"
	"net/url"
	"strconv"
)

// A saved filter's criteria are not written the way a query's are, and the
// difference is silent: Stash accepts the query notation, stores it, and the
// filter then does nothing in the UI.
//
//	query:  "date": {"modifier": "NOT_NULL", "value": ""}
//	saved:  "date": {"modifier": "NOT_NULL", "value": {"value": ""}}
//
//	query:  "tags": {"modifier": "EXCLUDES", "value": ["4"], "depth": 0}
//	saved:  "tags": {"modifier": "EXCLUDES", "value": {"depth": 0,
//	                 "items": [{"id": 4, "label": "HD%20Available"}]}}
//
// Booleans are stringly typed there too — "organized" is the string "false".
// This file writes the second notation, which is the one the UI reads back.

// savedSceneCriteria renders a [SceneFilter] the way a saved filter stores
// it, resolving names to ids as [Client.SceneFilterCriteria] does.
func (c *Client) savedSceneCriteria(ctx context.Context, filter SceneFilter) (map[string]any, error) {
	out := map[string]any{}

	if filter.Organized != nil {
		out["organized"] = map[string]any{
			"modifier": "EQUALS",
			"value":    strconv.FormatBool(*filter.Organized),
		}
	}

	if filter.HasStashID != nil {
		modifier := "IS_NULL"
		if *filter.HasStashID {
			modifier = "NOT_NULL"
		}
		out["stash_id_endpoint"] = map[string]any{
			"modifier": modifier,
			"value":    map[string]any{"endpoint": "", "stashID": ""},
		}
	}

	if filter.HasDate != nil {
		modifier := "IS_NULL"
		if *filter.HasDate {
			modifier = "NOT_NULL"
		}
		out["date"] = map[string]any{"modifier": modifier, "value": map[string]any{"value": ""}}
	}
	switch {
	case filter.DateAfter != "" && filter.DateBefore != "":
		out["date"] = map[string]any{"modifier": "BETWEEN",
			"value": map[string]any{"value": filter.DateAfter, "value2": filter.DateBefore}}
	case filter.DateAfter != "":
		out["date"] = map[string]any{"modifier": "GREATER_THAN",
			"value": map[string]any{"value": filter.DateAfter}}
	case filter.DateBefore != "":
		out["date"] = map[string]any{"modifier": "LESS_THAN",
			"value": map[string]any{"value": filter.DateBefore}}
	}

	if filter.PathContains != "" {
		out["path"] = map[string]any{"modifier": "INCLUDES", "value": filter.PathContains}
	}

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
		items, err := c.labelled(ctx, tags.names, c.FindTag, ErrTagNotFound)
		if err != nil {
			return nil, err
		}
		out["tags"] = map[string]any{
			"modifier": tags.modifier,
			"value":    map[string]any{"depth": 0, "items": items},
		}
	}

	if filter.PerformerName != "" {
		items, err := c.labelled(ctx, []string{filter.PerformerName}, c.FindPerformer, ErrPerformerNotFound)
		if err != nil {
			return nil, err
		}
		out["performers"] = map[string]any{
			"modifier": "INCLUDES_ALL",
			"value":    map[string]any{"items": items},
		}
	}

	if filter.StudioName != "" {
		items, err := c.labelled(ctx, []string{filter.StudioName}, c.FindStudio, ErrStudioNotFound)
		if err != nil {
			return nil, err
		}
		out["studios"] = map[string]any{
			"modifier": "INCLUDES_ALL",
			"value":    map[string]any{"depth": 0, "items": items},
		}
	}
	return out, nil
}

type finder func(ctx context.Context, name string) (string, bool, error)

// labelled turns names into the {id, label} pairs a saved filter lists.
//
// The label is what the UI prints; the id is what it filters on. Stash writes
// the label percent-encoded, so this does too — a filter whose label reads
// "HD%20Available" is Stash's own doing, not a bug here.
func (c *Client) labelled(ctx context.Context, names []string, find finder, missing error) ([]map[string]any, error) {
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		id, found, err := find(ctx, name)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, wrapMissing(missing, name)
		}
		item := map[string]any{"label": url.PathEscape(name)}
		// Stash writes these as numbers where they are numbers.
		if n, err := strconv.Atoi(id); err == nil {
			item["id"] = n
		} else {
			item["id"] = id
		}
		items = append(items, item)
	}
	return items, nil
}
