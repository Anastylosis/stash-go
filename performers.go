package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// PerformerInput is a performer to create, with everything Stash will accept
// about one.
//
// Every field but Name is optional and omitted when empty, so a caller that
// knows only a name sends the same request [Client.CreatePerformer] does.
type PerformerInput struct {
	Name           string
	Disambiguation string
	Gender         string
	Birthdate      string
	DeathDate      string
	Country        string
	Ethnicity      string
	EyeColor       string
	HairColor      string
	HeightCM       int
	Weight         int
	Measurements   string
	FakeTits       string
	CareerLength   string
	Tattoos        string
	Piercings      string
	Aliases        []string
	URLs           []string
	Details        string
	// Image is either a URL or a data: URI. Given a URL, Stash fetches it
	// itself — which means the fetch happens from the server, and a URL only
	// this machine can reach will not work.
	Image string
	// StashIDs ties the performer to its entry in a stash-box. Worth setting
	// whenever it is known: it is the only stable identity a performer has,
	// and it is what stops the same person being created twice under two
	// spellings.
	StashIDs []StashID
}

func (p PerformerInput) fields() map[string]any {
	in := map[string]any{"name": p.Name}
	str := map[string]string{
		"disambiguation": p.Disambiguation,
		"gender":         p.Gender,
		"birthdate":      p.Birthdate,
		"death_date":     p.DeathDate,
		"country":        p.Country,
		"ethnicity":      p.Ethnicity,
		"eye_color":      p.EyeColor,
		"hair_color":     p.HairColor,
		"measurements":   p.Measurements,
		"fake_tits":      p.FakeTits,
		"career_length":  p.CareerLength,
		"tattoos":        p.Tattoos,
		"piercings":      p.Piercings,
		"details":        p.Details,
		"image":          p.Image,
	}
	for k, v := range str {
		if v != "" {
			in[k] = v
		}
	}
	if p.HeightCM > 0 {
		in["height_cm"] = p.HeightCM
	}
	if p.Weight > 0 {
		in["weight"] = p.Weight
	}
	if len(p.Aliases) > 0 {
		in["alias_list"] = p.Aliases
	}
	if len(p.URLs) > 0 {
		in["urls"] = p.URLs
	}
	if len(p.StashIDs) > 0 {
		ids := make([]map[string]string, len(p.StashIDs))
		for i, s := range p.StashIDs {
			ids[i] = map[string]string{"endpoint": s.Endpoint, "stash_id": s.ID}
		}
		in["stash_ids"] = ids
	}
	return in
}

// CreatePerformerFrom creates a performer with full details and returns its
// ID. [Client.CreatePerformer] is the same call for a name alone.
func (c *Client) CreatePerformerFrom(ctx context.Context, in PerformerInput) (string, error) {
	if in.Name == "" {
		return "", fmt.Errorf("stash: creating performer: no name")
	}
	data, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: PerformerCreateInput!) { performerCreate(input: $input) { id } }`,
		Variables: map[string]any{"input": in.fields()},
	})
	if err != nil {
		return "", fmt.Errorf("stash: creating performer %q: %w", in.Name, err)
	}
	var result struct {
		PerformerCreate struct {
			ID string `json:"id"`
		} `json:"performerCreate"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding created performer: %w", err)
	}
	return result.PerformerCreate.ID, nil
}

// FindPerformerByStashID returns the ID of the performer carrying this
// stash-box id.
//
// This is the identity check worth making before creating anything.
// [Client.FindPerformer] matches on a name, and a name is neither unique nor
// stable — two performers share one, one performer changes theirs, and a
// scraper writes it with different punctuation. A stash-box id is the same
// string forever.
func (c *Client) FindPerformerByStashID(ctx context.Context, endpoint, stashID string) (id string, found bool, err error) {
	if stashID == "" {
		return "", false, fmt.Errorf("stash: finding performer: no stash id")
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($f: PerformerFilterType!) {
			findPerformers(performer_filter: $f, filter: {per_page: 2}) { performers { id } } }`,
		Variables: map[string]any{"f": map[string]any{
			"stash_id_endpoint": map[string]any{
				"endpoint": endpoint,
				"stash_id": stashID,
				"modifier": "EQUALS",
			},
		}},
	})
	if err != nil {
		return "", false, fmt.Errorf("stash: finding performer by stash id: %w", err)
	}
	return firstID(data, "findPerformers", "performers")
}

// PerformerFields is the selection the performer queries use. Exported for
// the same reason [SceneFields] is: a caller writing its own query through
// [Client.Execute] can drop it in and decode the result into [Performer].
const PerformerFields = `
	id name disambiguation gender birthdate death_date country ethnicity
	eye_color hair_color height_cm weight measurements fake_tits
	career_start career_end tattoos piercings alias_list urls details
	favorite rating100 image_path scene_count
	tags { id name }
	stash_ids { endpoint stash_id }`

// FindPerformerByID returns one performer with everything Stash stores about
// them. found is false when there is no such performer, which is not an
// error.
func (c *Client) FindPerformerByID(ctx context.Context, id string) (performer *Performer, found bool, err error) {
	data, err := c.do(ctx, graphqlRequest{
		Query:     `query($id: ID!) { findPerformer(id: $id) {` + PerformerFields + `} }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return nil, false, fmt.Errorf("stash: finding performer %s: %w", id, err)
	}
	var result struct {
		FindPerformer *Performer `json:"findPerformer"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("stash: decoding performer %s: %w", id, err)
	}
	if result.FindPerformer == nil {
		return nil, false, nil
	}
	return result.FindPerformer, true, nil
}

// PerformerFilter selects performers. An unset field does not filter.
type PerformerFilter struct {
	// NameContains matches anywhere in the name, case-insensitively.
	NameContains string
	// Gender is Stash's own vocabulary — "FEMALE", "MALE",
	// "TRANSGENDER_FEMALE" and the rest.
	Gender string
	// Favorite selects favourites (true) or everything else (false).
	Favorite *bool
	// HasStashID selects performers that do (true) or do not (false) carry
	// stash-box metadata.
	HasStashID *bool
	// HasScenes selects performers with at least one scene (true) or none
	// (false). Performers with none are usually leftovers.
	HasScenes *bool
}

func (f PerformerFilter) criteria() map[string]any {
	out := map[string]any{}
	if f.NameContains != "" {
		out["name"] = map[string]any{"value": f.NameContains, "modifier": "INCLUDES"}
	}
	if f.Gender != "" {
		out["gender"] = map[string]any{"value": f.Gender, "modifier": "EQUALS"}
	}
	if f.Favorite != nil {
		out["filter_favorites"] = *f.Favorite
	}
	if f.HasStashID != nil {
		modifier := "IS_NULL"
		if *f.HasStashID {
			modifier = "NOT_NULL"
		}
		out["stash_id_endpoint"] = map[string]any{"modifier": modifier}
	}
	if f.HasScenes != nil {
		// GREATER_THAN 0 rather than NOT_NULL: the count is always present,
		// and zero is the thing being asked about.
		modifier := "EQUALS"
		if *f.HasScenes {
			modifier = "GREATER_THAN"
		}
		out["scene_count"] = map[string]any{"value": 0, "modifier": modifier}
	}
	return out
}

// FindPerformers returns one page of performers plus the total count. Pages
// are 1-based and sorted by name, so paging is stable.
func (c *Client) FindPerformers(ctx context.Context, filter PerformerFilter, page, perPage int) ([]Performer, int, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($filter: FindFilterType, $performer_filter: PerformerFilterType) {
			findPerformers(filter: $filter, performer_filter: $performer_filter) {
				count performers {` + PerformerFields + `} } }`,
		Variables: map[string]any{
			"filter": map[string]any{
				"page": page, "per_page": perPage, "sort": "name", "direction": "ASC",
			},
			"performer_filter": filter.criteria(),
		},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("stash: finding performers: %w", err)
	}
	var result struct {
		FindPerformers struct {
			Count      int         `json:"count"`
			Performers []Performer `json:"performers"`
		} `json:"findPerformers"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, fmt.Errorf("stash: decoding performers: %w", err)
	}
	return result.FindPerformers.Performers, result.FindPerformers.Count, nil
}

// PerformerUpdate is the payload for [Client.UpdatePerformer].
//
// Only the fields you set are sent, so an unset one leaves the stored value
// alone — the same shape as [SceneUpdate], and with the same limitation: it
// cannot clear a field, because empty and absent look identical on the wire.
// [Client.ClearPerformerFields] is the way to empty one.
type PerformerUpdate struct {
	ID string

	Name           *string
	Disambiguation *string
	Gender         *string
	Birthdate      *string
	DeathDate      *string
	Country        *string
	Ethnicity      *string
	EyeColor       *string
	HairColor      *string
	HeightCM       *int
	Weight         *int
	Measurements   *string
	FakeTits       *string
	CareerLength   *string
	Tattoos        *string
	Piercings      *string
	Details        *string
	Favorite       *bool
	Rating100      *int

	// Aliases, URLs, TagIDs and StashIDs replace what is stored rather than
	// adding to it. Read the performer first and send the union if adding is
	// what you meant.
	Aliases  []string
	URLs     []string
	TagIDs   []string
	StashIDs []StashID

	// Image is a URL or a data: URI. Given a URL, the server fetches it.
	Image *string
}

func (p PerformerUpdate) fields() map[string]any {
	in := map[string]any{"id": p.ID}
	for key, v := range map[string]*string{
		"name": p.Name, "disambiguation": p.Disambiguation, "gender": p.Gender,
		"birthdate": p.Birthdate, "death_date": p.DeathDate, "country": p.Country,
		"ethnicity": p.Ethnicity, "eye_color": p.EyeColor, "hair_color": p.HairColor,
		"measurements": p.Measurements, "fake_tits": p.FakeTits,
		"career_length": p.CareerLength, "tattoos": p.Tattoos,
		"piercings": p.Piercings, "details": p.Details, "image": p.Image,
	} {
		if v != nil {
			in[key] = *v
		}
	}
	if p.HeightCM != nil {
		in["height_cm"] = *p.HeightCM
	}
	if p.Weight != nil {
		in["weight"] = *p.Weight
	}
	if p.Rating100 != nil {
		in["rating100"] = *p.Rating100
	}
	if p.Favorite != nil {
		in["favorite"] = *p.Favorite
	}
	if p.Aliases != nil {
		in["alias_list"] = p.Aliases
	}
	if p.URLs != nil {
		in["urls"] = p.URLs
	}
	if p.TagIDs != nil {
		in["tag_ids"] = p.TagIDs
	}
	if p.StashIDs != nil {
		ids := make([]map[string]string, len(p.StashIDs))
		for i, s := range p.StashIDs {
			ids[i] = map[string]string{"endpoint": s.Endpoint, "stash_id": s.ID}
		}
		in["stash_ids"] = ids
	}
	return in
}

// UpdatePerformer writes the fields that are set and leaves the rest alone.
func (c *Client) UpdatePerformer(ctx context.Context, update PerformerUpdate) error {
	if update.ID == "" {
		return fmt.Errorf("stash: updating performer: no id")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: PerformerUpdateInput!) { performerUpdate(input: $input) { id } }`,
		Variables: map[string]any{"input": update.fields()},
	})
	if err != nil {
		return fmt.Errorf("stash: updating performer %s: %w", update.ID, err)
	}
	return nil
}

// ClearPerformerFields empties the named fields, which [PerformerUpdate]
// cannot: it omits what is unset so that a partial update is safe, and that
// makes "" indistinguishable from "leave it".
//
// Names are the PerformerUpdateInput field names — "birthdate", "details",
// "alias_list", "measurements". A name Stash does not know fails the whole
// mutation rather than being ignored.
func (c *Client) ClearPerformerFields(ctx context.Context, id string, fields ...string) error {
	if id == "" {
		return fmt.Errorf("stash: clearing performer fields: no id")
	}
	if len(fields) == 0 {
		return nil
	}
	in := map[string]any{"id": id}
	for _, f := range fields {
		if !isFieldName(f) {
			return fmt.Errorf("stash: clearing performer fields: %q is not a field name", f)
		}
		// The list-valued fields want an empty list; everything else an
		// empty string. Sending "" for a list is a type error, and sending
		// [] for a string is too.
		switch f {
		case "alias_list", "urls", "tag_ids", "stash_ids":
			in[f] = []any{}
		default:
			in[f] = ""
		}
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: PerformerUpdateInput!) { performerUpdate(input: $input) { id } }`,
		Variables: map[string]any{"input": in},
	})
	if err != nil {
		return fmt.Errorf("stash: clearing fields on performer %s: %w", id, err)
	}
	return nil
}

// DeletePerformer removes a performer.
//
// The performer's scenes are not touched; they simply lose the credit. There
// is no undo, and no confirmation — Stash deletes on being asked.
func (c *Client) DeletePerformer(ctx context.Context, id string) error {
	if id == "" {
		return fmt.Errorf("stash: deleting performer: no id")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($id: ID!) { performerDestroy(input: {id: $id}) }`,
		Variables: map[string]any{"id": id},
	})
	if err != nil {
		return fmt.Errorf("stash: deleting performer %s: %w", id, err)
	}
	return nil
}

// DeletePerformers removes several performers in one request.
//
// All or nothing: Stash checks every id first, and one that does not exist
// fails the whole call with nothing deleted. That matters after a merge,
// which has already removed its sources — passing them again deletes none of
// the rest.
func (c *Client) DeletePerformers(ctx context.Context, ids ...string) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("stash: deleting performers: an id is empty")
		}
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($ids: [ID!]!) { performersDestroy(ids: $ids) }`,
		Variables: map[string]any{"ids": ids},
	})
	if err != nil {
		return fmt.Errorf("stash: deleting %d performers: %w", len(ids), err)
	}
	return nil
}

// MergePerformers folds the source performers into the destination and
// deletes them, moving their scenes across.
//
// values, when set, is applied to the destination as part of the merge — the
// place to keep a source's better name or birthdate, since the destination's
// own fields otherwise win and the sources are gone afterwards.
//
// This is not reversible.
func (c *Client) MergePerformers(ctx context.Context, destinationID string, sourceIDs []string, values *PerformerUpdate) error {
	if destinationID == "" {
		return fmt.Errorf("stash: merging performers: no destination")
	}
	if len(sourceIDs) == 0 {
		return fmt.Errorf("stash: merging performers: no sources")
	}
	for _, id := range sourceIDs {
		if id == destinationID {
			// Stash would delete the destination as one of its own sources.
			return fmt.Errorf("stash: merging performers: %s is both source and destination", id)
		}
	}
	input := map[string]any{"source": sourceIDs, "destination": destinationID}
	if values != nil {
		v := *values
		v.ID = destinationID
		input["values"] = v.fields()
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: PerformerMergeInput!) { performerMerge(input: $input) { id } }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return fmt.Errorf("stash: merging %d performers into %s: %w", len(sourceIDs), destinationID, err)
	}
	return nil
}
