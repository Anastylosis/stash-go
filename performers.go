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
