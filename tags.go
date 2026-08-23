package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// TagFields is the selection the tag queries use.
const TagFields = `
	id name sort_name description aliases favorite image_path scene_count
	stash_ids { endpoint stash_id }
	parents { id name }
	children { id name }`

// TagInput is a tag to create or update. Every field but Name is optional and
// omitted when empty, so an unset one leaves the stored value alone.
type TagInput struct {
	// ID is set on an update and empty on a create.
	ID   string
	Name string

	SortName    string
	Description string
	Aliases     []string
	// ParentIDs and ChildIDs replace the tag's place in the hierarchy rather
	// than adding to it. Read the tag first and send the union if adding is
	// what you meant.
	ParentIDs []string
	ChildIDs  []string
	StashIDs  []StashID
	Image     string
	Favorite  *bool
}

func (t TagInput) fields() map[string]any {
	in := map[string]any{}
	if t.ID != "" {
		in["id"] = t.ID
	}
	for key, v := range map[string]string{
		"name": t.Name, "sort_name": t.SortName,
		"description": t.Description, "image": t.Image,
	} {
		if v != "" {
			in[key] = v
		}
	}
	for key, v := range map[string][]string{
		"aliases": t.Aliases, "parent_ids": t.ParentIDs, "child_ids": t.ChildIDs,
	} {
		if len(v) > 0 {
			in[key] = v
		}
	}
	if len(t.StashIDs) > 0 {
		ids := make([]map[string]string, len(t.StashIDs))
		for i, id := range t.StashIDs {
			ids[i] = map[string]string{"endpoint": id.Endpoint, "stash_id": id.ID}
		}
		in["stash_ids"] = ids
	}
	if t.Favorite != nil {
		in["favorite"] = *t.Favorite
	}
	return in
}

// FindTagByID returns one tag with everything Stash stores about it.
func (c *Client) FindTagByID(ctx context.Context, id string) (tag *Tag, found bool, err error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `query($id: ID!) { findTag(id: $id) {` + TagFields + `} }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return nil, false, fmt.Errorf("stash: finding tag %s: %w", id, err)
	}
	var result struct {
		FindTag *Tag `json:"findTag"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("stash: decoding tag %s: %w", id, err)
	}
	if result.FindTag == nil {
		return nil, false, nil
	}
	return result.FindTag, true, nil
}

// Tags returns one page of tags plus the total count, sorted by name.
func (c *Client) Tags(ctx context.Context, page, perPage int) ([]Tag, int, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($filter: FindFilterType) { findTags(filter: $filter) {
			count tags {` + TagFields + `} } }`,
		Variables: map[string]any{"filter": map[string]any{
			"page": page, "per_page": perPage, "sort": "name", "direction": "ASC",
		}},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("stash: finding tags: %w", err)
	}
	var result struct {
		FindTags struct {
			Count int   `json:"count"`
			Tags  []Tag `json:"tags"`
		} `json:"findTags"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, fmt.Errorf("stash: decoding tags: %w", err)
	}
	return result.FindTags.Tags, result.FindTags.Count, nil
}

// CreateTagFrom creates a tag with full details and returns its ID.
// [Client.CreateTag] is the same call for a name alone.
func (c *Client) CreateTagFrom(ctx context.Context, in TagInput) (string, error) {
	if in.Name == "" {
		return "", fmt.Errorf("stash: creating tag: no name")
	}
	in.ID = ""
	return c.tagMutation(ctx, "tagCreate", "TagCreateInput", in)
}

// UpdateTag writes the fields that are set and leaves the rest alone.
func (c *Client) UpdateTag(ctx context.Context, in TagInput) error {
	if in.ID == "" {
		return fmt.Errorf("stash: updating tag: no id")
	}
	_, err := c.tagMutation(ctx, "tagUpdate", "TagUpdateInput", in)
	return err
}

func (c *Client) tagMutation(ctx context.Context, mutation, inputType string, in TagInput) (string, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ` + inputType + `!) { ` + mutation + `(input: $input) { id } }`,
		Variables: map[string]any{"input": in.fields()},
	})
	if err != nil {
		return "", fmt.Errorf("stash: %s %q: %w", mutation, in.Name, err)
	}
	var result map[string]struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding %s: %w", mutation, err)
	}
	return result[mutation].ID, nil
}

// ClearTagFields empties the named fields, which [TagInput] cannot.
func (c *Client) ClearTagFields(ctx context.Context, id string, fields ...string) error {
	return c.clearFields(ctx, "tagUpdate", "TagUpdateInput", id, fields,
		map[string]bool{"aliases": true, "parent_ids": true, "child_ids": true, "stash_ids": true})
}

// DeleteTag removes a tag. Scenes carrying it simply lose it.
func (c *Client) DeleteTag(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("stash: deleting tag: no id")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($id: ID!) { tagDestroy(input: {id: $id}) }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return fmt.Errorf("stash: deleting tag %s: %w", id, err)
	}
	return nil
}

// DeleteTags removes several tags in one request. All or nothing: one id that
// does not exist fails the call with nothing deleted.
func (c *Client) DeleteTags(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($ids: [ID!]!) { tagsDestroy(ids: $ids) }`,
		Variables: map[string]any{"ids": ids},
	})
	if err != nil {
		return fmt.Errorf("stash: deleting %d tags: %w", len(ids), err)
	}
	return nil
}

// MergeTags folds the source tags into the destination and deletes them,
// moving everything they were on across.
//
// values, when set, is applied to the destination as part of the merge — the
// place to keep a source's better name or description, since the sources are
// gone afterwards. This is not reversible.
func (c *Client) MergeTags(ctx context.Context, destinationID string, sourceIDs []string, values *TagInput) error {
	if destinationID == "" {
		return fmt.Errorf("stash: merging tags: no destination")
	}
	if len(sourceIDs) == 0 {
		return fmt.Errorf("stash: merging tags: no sources")
	}
	for _, id := range sourceIDs {
		if id == destinationID {
			// Stash would fold the destination into itself and delete it.
			return fmt.Errorf("stash: merging tags: %s is both source and destination", id)
		}
	}
	input := map[string]any{"source": sourceIDs, "destination": destinationID}
	if values != nil {
		v := *values
		v.ID = destinationID
		input["values"] = v.fields()
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: TagsMergeInput!) { tagsMerge(input: $input) { id } }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return fmt.Errorf("stash: merging %d tags into %s: %w", len(sourceIDs), destinationID, err)
	}
	return nil
}
