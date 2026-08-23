package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// Tags, performers and studios all follow the same three-step shape:
// find by exact name, create, or ensure (find-then-create). Ensure is what
// callers pushing scraped metadata almost always want.

// FindTag returns the ID of the tag with this exact name.
func (c *Client) FindTag(ctx context.Context, name string) (id string, found bool, err error) {
	return c.findNamed(ctx, "findTags", "tag_filter", "tags", name)
}

// FindTagByAlias returns the ID of a tag carrying this alias. Stash treats
// aliases as first-class, so a tag can be present under a name the caller does
// not know — checking aliases before creating avoids duplicates.
func (c *Client) FindTagByAlias(ctx context.Context, alias string) (id string, found bool, err error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($v: String!) {
  findTags(tag_filter: { aliases: { value: $v, modifier: EQUALS } }) { tags { id name } }
}`,
		Variables: map[string]any{"v": alias},
	})
	if err != nil {
		return "", false, fmt.Errorf("stash: finding tag by alias %q: %w", alias, err)
	}
	return firstID(data, "findTags", "tags")
}

// CreateTag creates a tag and returns its ID.
func (c *Client) CreateTag(ctx context.Context, name string) (string, error) {
	return c.create(ctx, "tagCreate", "TagCreateInput", name)
}

// EnsureTag returns the ID of a tag with this name, creating it if neither the
// name nor an alias matches.
func (c *Client) EnsureTag(ctx context.Context, name string) (string, error) {
	if id, found, err := c.FindTag(ctx, name); err != nil || found {
		return id, err
	}
	if id, found, err := c.FindTagByAlias(ctx, name); err != nil || found {
		return id, err
	}
	return c.CreateTag(ctx, name)
}

// FindPerformer returns the ID of the performer with this exact name.
func (c *Client) FindPerformer(ctx context.Context, name string) (id string, found bool, err error) {
	return c.findNamed(ctx, "findPerformers", "performer_filter", "performers", name)
}

// CreatePerformer creates a performer and returns its ID.
func (c *Client) CreatePerformer(ctx context.Context, name string) (string, error) {
	return c.create(ctx, "performerCreate", "PerformerCreateInput", name)
}

// EnsurePerformer returns the ID of a performer with this name, creating it if
// absent.
func (c *Client) EnsurePerformer(ctx context.Context, name string) (string, error) {
	if id, found, err := c.FindPerformer(ctx, name); err != nil || found {
		return id, err
	}
	return c.CreatePerformer(ctx, name)
}

// FindStudio returns the ID of the studio with this exact name.
func (c *Client) FindStudio(ctx context.Context, name string) (id string, found bool, err error) {
	return c.findNamed(ctx, "findStudios", "studio_filter", "studios", name)
}

// CreateStudio creates a studio and returns its ID.
func (c *Client) CreateStudio(ctx context.Context, name string) (string, error) {
	return c.create(ctx, "studioCreate", "StudioCreateInput", name)
}

// EnsureStudio returns the ID of a studio with this name, creating it if
// absent.
func (c *Client) EnsureStudio(ctx context.Context, name string) (string, error) {
	if id, found, err := c.FindStudio(ctx, name); err != nil || found {
		return id, err
	}
	return c.CreateStudio(ctx, name)
}

func (c *Client) findNamed(ctx context.Context, query, filterArg, listField, name string) (string, bool, error) {
	q := fmt.Sprintf(
		`query($v: String!) { %s(%s: { name: { value: $v, modifier: EQUALS } }) { %s { id name } } }`,
		query, filterArg, listField,
	)
	data, err := c.do(ctx, graphqlRequest{Query: q, Variables: map[string]any{"v": name}})
	if err != nil {
		return "", false, fmt.Errorf("stash: %s %q: %w", query, name, err)
	}
	return firstID(data, query, listField)
}

func (c *Client) create(ctx context.Context, mutation, inputType, name string) (string, error) {
	q := fmt.Sprintf(`mutation($input: %s!) { %s(input: $input) { id } }`, inputType, mutation)
	data, err := c.do(ctx, graphqlRequest{
		Query:     q,
		Variables: map[string]any{"input": map[string]any{"name": name}},
	})
	if err != nil {
		return "", fmt.Errorf("stash: %s %q: %w", mutation, name, err)
	}
	var result map[string]struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding %s response: %w", mutation, err)
	}
	created, ok := result[mutation]
	if !ok || created.ID == "" {
		return "", fmt.Errorf("stash: %s %q returned no id", mutation, name)
	}
	return created.ID, nil
}

// firstID pulls data.<query>.<listField>[0].id out of a find response.
func firstID(data json.RawMessage, query, listField string) (string, bool, error) {
	var outer map[string]map[string][]struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(data, &outer); err != nil {
		return "", false, fmt.Errorf("stash: decoding %s response: %w", query, err)
	}
	items := outer[query][listField]
	if len(items) == 0 {
		return "", false, nil
	}
	return items[0].ID, true, nil
}

// wrapMissing attaches the name to a not-found sentinel, so the error says
// which lookup failed rather than only that one did.
func wrapMissing(sentinel error, name string) error {
	return fmt.Errorf("%w: %q", sentinel, name)
}
