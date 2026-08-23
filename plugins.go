package stash

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// Plugin is one plugin the server has loaded.
type Plugin struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	URL         string `json:"url"`
	Version     string `json:"version"`
	Enabled     bool   `json:"enabled"`
}

// Plugins returns every plugin the server has loaded, enabled or not.
//
// This is not the same list as [Client.InstalledPackages]: a plugin installed
// from a source appears in both, one dropped into the plugins directory by
// hand appears only here, and one whose files are present but unparseable
// appears in neither.
func (c *Client) Plugins(ctx context.Context) ([]Plugin, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ plugins { id name description url version enabled } }`,
	})
	if err != nil {
		return nil, fmt.Errorf("stash: listing plugins: %w", err)
	}
	var result struct {
		Plugins []Plugin `json:"plugins"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding plugins: %w", err)
	}
	return result.Plugins, nil
}

// SetPluginsEnabled enables and disables plugins by id. Plugins not named are
// left as they are.
func (c *Client) SetPluginsEnabled(ctx context.Context, enabled map[string]bool) error {
	if len(enabled) == 0 {
		return nil
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($m: BoolMap!) { setPluginsEnabled(enabledMap: $m) }`,
		Variables: map[string]any{"m": enabled},
	})
	if err != nil {
		return fmt.Errorf("stash: enabling plugins: %w", err)
	}
	return nil
}

// ReloadPlugins makes the server re-read its plugin directory. Needed after
// files change on disk; an install through the package manager does it
// itself.
func (c *Client) ReloadPlugins(ctx context.Context) error {
	if _, err := c.do(ctx, graphqlRequest{Query: `mutation { reloadPlugins }`}); err != nil {
		return fmt.Errorf("stash: reloading plugins: %w", err)
	}
	return nil
}

// InterfaceConfig returns the named fields of the server's interface
// configuration.
//
// The caller names the fields because the section is large and changes
// between releases, and one field the schema lacks fails the whole query. A
// caller asking for what it is about to write cannot be broken by a field it
// does not use.
func (c *Client) InterfaceConfig(ctx context.Context, fields ...string) (map[string]any, error) {
	if len(fields) == 0 {
		return nil, fmt.Errorf("stash: reading interface configuration: no fields requested")
	}
	for _, f := range fields {
		if !isFieldName(f) {
			return nil, fmt.Errorf("stash: reading interface configuration: %q is not a field name", f)
		}
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ configuration { interface { ` + strings.Join(fields, " ") + ` } } }`,
	})
	if err != nil {
		return nil, fmt.Errorf("stash: reading interface configuration: %w", err)
	}
	var result struct {
		Configuration struct {
			Interface map[string]any `json:"interface"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding interface configuration: %w", err)
	}
	return result.Configuration.Interface, nil
}

// ConfigureInterface writes the given interface settings and leaves the rest
// alone, the same way [Client.UpdateScene] does — Stash applies only the keys
// present in the input.
//
// Keys are the ConfigInterfaceInput field names ("javascript",
// "javascriptEnabled", "css", "cssEnabled", …). They are not modelled here
// for the reason [Client.Configuration] gives: the section gains and loses
// fields between releases, and a struct naming them all would fail the whole
// mutation the first time one went away.
//
// Custom JavaScript in particular runs in every browser that opens this
// Stash. Read it before writing it, and preserve what is there.
func (c *Client) ConfigureInterface(ctx context.Context, settings map[string]any) error {
	if len(settings) == 0 {
		return nil
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ConfigInterfaceInput!) { configureInterface(input: $input) { __typename } }`,
		Variables: map[string]any{"input": settings},
	})
	if err != nil {
		return fmt.Errorf("stash: configuring interface: %w", err)
	}
	return nil
}

// isFieldName reports whether s is a plain GraphQL field name, which is what
// keeps [Client.InterfaceConfig] from splicing anything else into a query.
func isFieldName(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r == '_':
		case i > 0 && r >= '0' && r <= '9':
		default:
			return false
		}
	}
	return true
}
