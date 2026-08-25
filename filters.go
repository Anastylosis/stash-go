package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// FilterMode is which list a saved filter belongs to.
type FilterMode string

// FilterMode values, one per list a saved filter can belong to.
const (
	FilterScenes       FilterMode = "SCENES"
	FilterPerformers   FilterMode = "PERFORMERS"
	FilterStudios      FilterMode = "STUDIOS"
	FilterGalleries    FilterMode = "GALLERIES"
	FilterSceneMarkers FilterMode = "SCENE_MARKERS"
	FilterGroups       FilterMode = "GROUPS"
	FilterTags         FilterMode = "TAGS"
	FilterImages       FilterMode = "IMAGES"
)

// FindFilter is the sorting and paging half of a filter — what the UI puts
// above the list rather than in the sidebar.
type FindFilter struct {
	// Query is the free-text box.
	Query string `json:"q,omitempty"`
	// Sort is a field name ("date", "path", "title"), and Direction is
	// "ASC" or "DESC".
	Sort      string `json:"sort,omitempty"`
	Direction string `json:"direction,omitempty"`
	PerPage   int    `json:"per_page,omitempty"`
}

// SavedFilter is one of the filters that appear in Stash's sidebar.
type SavedFilter struct {
	ID   string     `json:"id"`
	Mode FilterMode `json:"mode"`
	Name string     `json:"name"`
	// FindFilter is the sort and page size.
	FindFilter *FindFilter `json:"find_filter"`
	// ObjectFilter is the criteria, in Stash's own notation. Build one for
	// scenes with [Client.SceneFilterCriteria] rather than by hand.
	ObjectFilter map[string]any `json:"object_filter"`
	// UIOptions is what the UI remembers about how to display the list —
	// card size, zoom, which columns. Carried so an update does not discard
	// it.
	UIOptions map[string]any `json:"ui_options"`
}

const savedFilterFields = `id mode name find_filter { q sort direction per_page } object_filter ui_options`

// SavedFilters returns the saved filters for one list, in the order Stash
// holds them.
func (c *Client) SavedFilters(ctx context.Context, mode FilterMode) ([]SavedFilter, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `query($mode: FilterMode) { findSavedFilters(mode: $mode) {` + savedFilterFields + `} }`,
		Variables: map[string]any{"mode": string(mode)},
	})
	if err != nil {
		return nil, fmt.Errorf("stash: reading saved filters: %w", err)
	}
	var result struct {
		FindSavedFilters []SavedFilter `json:"findSavedFilters"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding saved filters: %w", err)
	}
	return result.FindSavedFilters, nil
}

// FindSavedFilter returns the saved filter with this name in this list.
//
// Stash does not require names to be unique, so this returns the first match
// and a caller creating filters should check before creating a second one of
// the same name — which is what [Client.SaveSceneFilter] does.
func (c *Client) FindSavedFilter(ctx context.Context, mode FilterMode, name string) (filter *SavedFilter, found bool, err error) {
	all, err := c.SavedFilters(ctx, mode)
	if err != nil {
		return nil, false, err
	}
	for i, f := range all {
		if f.Name == name {
			return &all[i], true, nil
		}
	}
	return nil, false, nil
}

// SaveFilter creates a saved filter, or updates the one whose ID is set, and
// returns its id.
func (c *Client) SaveFilter(ctx context.Context, filter SavedFilter) (string, error) {
	if filter.Name == "" {
		return "", fmt.Errorf("stash: saving filter: no name")
	}
	if filter.Mode == "" {
		return "", fmt.Errorf("stash: saving filter %q: no mode", filter.Name)
	}
	input := map[string]any{
		"mode": string(filter.Mode),
		"name": filter.Name,
	}
	if filter.ID != "" {
		input["id"] = filter.ID
	}
	if filter.FindFilter != nil {
		input["find_filter"] = filter.FindFilter
	}
	// Sent even when empty: a saved filter with no criteria is "everything",
	// which is a filter someone may legitimately want.
	input["object_filter"] = orEmpty(filter.ObjectFilter)
	if filter.UIOptions != nil {
		input["ui_options"] = filter.UIOptions
	}

	data, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: SaveFilterInput!) { saveFilter(input: $input) { id } }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return "", fmt.Errorf("stash: saving filter %q: %w", filter.Name, err)
	}
	var result struct {
		SaveFilter struct {
			ID string `json:"id"`
		} `json:"saveFilter"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding saved filter: %w", err)
	}
	return result.SaveFilter.ID, nil
}

// SaveSceneFilter saves a scene filter under a name, replacing one of that
// name if it exists.
//
// The criteria come from a [SceneFilter], so a filter that selects scenes in
// a program and a filter the user clicks in the sidebar are the same thing
// written once — including the translation into the notation saved filters
// use, which is not the one queries use. find may be nil, in which case
// Stash's own defaults apply.
func (c *Client) SaveSceneFilter(ctx context.Context, name string, filter SceneFilter, find *FindFilter) (string, error) {
	criteria, err := c.savedSceneCriteria(ctx, filter)
	if err != nil {
		return "", err
	}
	saved := SavedFilter{Mode: FilterScenes, Name: name, FindFilter: find, ObjectFilter: criteria}

	// Updating in place rather than adding a second filter of the same name:
	// Stash allows the duplicate, and a program run twice should not leave
	// two identical entries in someone's sidebar.
	if existing, found, err := c.FindSavedFilter(ctx, FilterScenes, name); err != nil {
		return "", err
	} else if found {
		saved.ID = existing.ID
		saved.UIOptions = existing.UIOptions
	}
	return c.SaveFilter(ctx, saved)
}

// DestroySavedFilter deletes a saved filter by id.
func (c *Client) DestroySavedFilter(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("stash: destroying saved filter: no id")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($id: ID!) { destroySavedFilter(input: {id: $id}) }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return fmt.Errorf("stash: destroying saved filter %s: %w", id, err)
	}
	return nil
}

func orEmpty(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}
