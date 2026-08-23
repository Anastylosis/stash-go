package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// Studio a scene belongs to, and as the studio queries return one.
//
// A studio reached through a scene carries only ID and Name, for the reason
// [Performer] does: the shared scene selection asks for no more, because a
// page of scenes should not drag a full record along for each one.
type Studio struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	URLs         []string  `json:"urls"`
	Details      string    `json:"details"`
	Aliases      []string  `json:"aliases"`
	Rating100    *int      `json:"rating100"`
	Favorite     bool      `json:"favorite"`
	ImagePath    string    `json:"image_path"`
	SceneCount   int       `json:"scene_count"`
	StashIDs     []StashID `json:"stash_ids"`
	ParentStudio *Studio   `json:"parent_studio"`
}

// StudioFields is the selection the studio queries use. Exported for the same
// reason [SceneFields] is.
//
// child_studios is deliberately absent: a studio with many children would
// carry them all on every query, and a caller that wants the tree can ask for
// it. parent_studio is one level deep for the same reason.
const StudioFields = `
	id name urls details aliases rating100 favorite image_path scene_count
	stash_ids { endpoint stash_id }
	parent_studio { id name }`

// StudioInput is a studio to create or update. Every field but Name is
// optional and omitted when empty, so an unset one leaves the stored value
// alone — the same shape as [SceneUpdate], with the same limitation:
// [Client.ClearStudioFields] is what empties one.
type StudioInput struct {
	// ID is set on an update and empty on a create.
	ID   string
	Name string

	Details  string
	ParentID string
	Aliases  []string
	URLs     []string
	TagIDs   []string
	StashIDs []StashID
	// Image is a URL or a data: URI. Given a URL the server fetches it.
	Image     string
	Rating100 *int
	Favorite  *bool
}

func (s StudioInput) fields() map[string]any {
	in := map[string]any{}
	if s.ID != "" {
		in["id"] = s.ID
	}
	if s.Name != "" {
		in["name"] = s.Name
	}
	for key, v := range map[string]string{
		"details": s.Details, "parent_id": s.ParentID, "image": s.Image,
	} {
		if v != "" {
			in[key] = v
		}
	}
	if len(s.Aliases) > 0 {
		in["aliases"] = s.Aliases
	}
	if len(s.URLs) > 0 {
		in["urls"] = s.URLs
	}
	if len(s.TagIDs) > 0 {
		in["tag_ids"] = s.TagIDs
	}
	if len(s.StashIDs) > 0 {
		ids := make([]map[string]string, len(s.StashIDs))
		for i, id := range s.StashIDs {
			ids[i] = map[string]string{"endpoint": id.Endpoint, "stash_id": id.ID}
		}
		in["stash_ids"] = ids
	}
	if s.Rating100 != nil {
		in["rating100"] = *s.Rating100
	}
	if s.Favorite != nil {
		in["favorite"] = *s.Favorite
	}
	return in
}

// FindStudioByID returns one studio with everything Stash stores about it.
func (c *Client) FindStudioByID(ctx context.Context, id string) (studio *Studio, found bool, err error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `query($id: ID!) { findStudio(id: $id) {` + StudioFields + `} }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return nil, false, fmt.Errorf("stash: finding studio %s: %w", id, err)
	}
	var result struct {
		FindStudio *Studio `json:"findStudio"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("stash: decoding studio %s: %w", id, err)
	}
	if result.FindStudio == nil {
		return nil, false, nil
	}
	return result.FindStudio, true, nil
}

// Studios returns one page of studios plus the total count, sorted by name.
func (c *Client) Studios(ctx context.Context, page, perPage int) ([]Studio, int, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($filter: FindFilterType) { findStudios(filter: $filter) {
			count studios {` + StudioFields + `} } }`,
		Variables: map[string]any{"filter": map[string]any{
			"page": page, "per_page": perPage, "sort": "name", "direction": "ASC",
		}},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("stash: finding studios: %w", err)
	}
	var result struct {
		FindStudios struct {
			Count   int      `json:"count"`
			Studios []Studio `json:"studios"`
		} `json:"findStudios"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, fmt.Errorf("stash: decoding studios: %w", err)
	}
	return result.FindStudios.Studios, result.FindStudios.Count, nil
}

// CreateStudioFrom creates a studio with full details and returns its ID.
// [Client.CreateStudio] is the same call for a name alone.
func (c *Client) CreateStudioFrom(ctx context.Context, in StudioInput) (string, error) {
	if in.Name == "" {
		return "", fmt.Errorf("stash: creating studio: no name")
	}
	in.ID = ""
	return c.studioMutation(ctx, "studioCreate", "StudioCreateInput", in)
}

// UpdateStudio writes the fields that are set and leaves the rest alone.
func (c *Client) UpdateStudio(ctx context.Context, in StudioInput) error {
	if in.ID == "" {
		return fmt.Errorf("stash: updating studio: no id")
	}
	_, err := c.studioMutation(ctx, "studioUpdate", "StudioUpdateInput", in)
	return err
}

func (c *Client) studioMutation(ctx context.Context, mutation, inputType string, in StudioInput) (string, error) {
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

// ClearStudioFields empties the named fields, which [StudioInput] cannot.
func (c *Client) ClearStudioFields(ctx context.Context, id string, fields ...string) error {
	return c.clearFields(ctx, "studioUpdate", "StudioUpdateInput", id, fields,
		map[string]bool{"aliases": true, "urls": true, "tag_ids": true, "stash_ids": true})
}

// DeleteStudio removes a studio. Its scenes are not touched; they lose the
// studio.
func (c *Client) DeleteStudio(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("stash: deleting studio: no id")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($id: ID!) { studioDestroy(input: {id: $id}) }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return fmt.Errorf("stash: deleting studio %s: %w", id, err)
	}
	return nil
}

// DeleteStudios removes several studios in one request. All or nothing: one
// id that does not exist fails the call with nothing deleted.
func (c *Client) DeleteStudios(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($ids: [ID!]!) { studiosDestroy(ids: $ids) }`,
		Variables: map[string]any{"ids": ids},
	})
	if err != nil {
		return fmt.Errorf("stash: deleting %d studios: %w", len(ids), err)
	}
	return nil
}

// clearFields is the shared body of the ClearXFields calls: the field is
// always sent, with the empty value its type wants.
func (c *Client) clearFields(ctx context.Context, mutation, inputType, id string, fields []string, lists map[string]bool) error {
	if id == "" {
		return fmt.Errorf("stash: %s: no id", mutation)
	}
	if len(fields) == 0 {
		return nil
	}
	in := map[string]any{"id": id}
	for _, f := range fields {
		if !isFieldName(f) {
			return fmt.Errorf("stash: %s: %q is not a field name", mutation, f)
		}
		if lists[f] {
			in[f] = []any{}
		} else {
			in[f] = ""
		}
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ` + inputType + `!) { ` + mutation + `(input: $input) { id } }`,
		Variables: map[string]any{"input": in},
	})
	if err != nil {
		return fmt.Errorf("stash: clearing fields on %s: %w", id, err)
	}
	return nil
}
