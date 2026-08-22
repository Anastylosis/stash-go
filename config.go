package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// Configuration returns the server's whole configuration tree as decoded
// JSON. Stash's ConfigResult is a large, version-dependent shape whose
// sections gain and lose fields between releases, so this deliberately does
// not model it: a typed struct would fail the entire query the first time a
// field it names is dropped, which is the failure mode this library exists
// to spare callers.
//
// Prefer [Client.PluginSettings] when that is what you want — it asks for
// one section rather than the whole tree.
func (c *Client) Configuration(ctx context.Context) (map[string]any, error) {
	data, err := c.do(ctx, graphqlRequest{Query: `{ configuration { general { databasePath } plugins } }`})
	if err != nil {
		return nil, fmt.Errorf("stash: reading configuration: %w", err)
	}
	var result struct {
		Configuration map[string]any `json:"configuration"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding configuration: %w", err)
	}
	return result.Configuration, nil
}

// PluginSettings returns the stored settings for one plugin, keyed by the
// setting name its YAML declares. The plugin id is what Stash derives from
// the config filename — `moansubs.yml` gives "moansubs".
//
// An unconfigured plugin, or one Stash has never heard of, returns an empty
// map rather than an error: a plugin whose settings have all been left at
// their defaults is indistinguishable from one that is not installed, and
// both mean "nothing has been set".
//
// Values are whatever JSON Stash stored, so a caller asserting a type must
// tolerate what the UI actually writes. In particular a boolean setting the
// user has never touched can come back as nil rather than false, and a
// task's declared default arrives as a string.
func (c *Client) PluginSettings(ctx context.Context, pluginID string) (map[string]any, error) {
	data, err := c.do(ctx, graphqlRequest{Query: `{ configuration { plugins } }`})
	if err != nil {
		return nil, fmt.Errorf("stash: reading plugin settings: %w", err)
	}
	var result struct {
		Configuration struct {
			Plugins map[string]json.RawMessage `json:"plugins"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding plugin settings: %w", err)
	}
	raw, ok := result.Configuration.Plugins[pluginID]
	if !ok {
		return map[string]any{}, nil
	}
	settings := map[string]any{}
	if err := json.Unmarshal(raw, &settings); err != nil {
		return nil, fmt.Errorf("stash: decoding settings for plugin %s: %w", pluginID, err)
	}
	return settings, nil
}
