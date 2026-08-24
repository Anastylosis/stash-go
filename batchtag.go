package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// BatchTagTarget is what a stash-box batch tag job works on.
type BatchTagTarget string

const (
	// BatchTagPerformers matches performers against the box.
	BatchTagPerformers BatchTagTarget = "stashBoxBatchPerformerTag"
	// BatchTagStudios matches studios.
	BatchTagStudios BatchTagTarget = "stashBoxBatchStudioTag"
	// BatchTagTags matches tags.
	BatchTagTags BatchTagTarget = "stashBoxBatchTagTag"
)

// BatchTagOptions selects what a batch tag job covers and how much of it it is
// allowed to change.
//
// Either IDs or Names, not both: ids name entities the library already has,
// where names ask the box to find something the library only knows by name.
type BatchTagOptions struct {
	// Endpoint is the stash-box GraphQL URL to match against. Required —
	// Stash will not guess even when only one box is configured.
	Endpoint string

	// IDs restricts the job to these entities. Empty means every one of that
	// kind, which on a large library is a long job.
	IDs []string
	// Names asks the box about these names instead of about entities.
	Names []string

	// Refresh re-queries entities that already carry a stash id for this
	// endpoint. Without it Stash only visits the ones with none, which is
	// what makes a repeat run cheap.
	Refresh bool

	// ExcludeFields names the fields the job must not write, so a library
	// that trusts its own data more than the box's can keep it. The names are
	// the box's, not Stash's: "name", "aliases", "description", "image".
	ExcludeFields []string

	// CreateParent creates a missing parent studio rather than leaving the
	// studio unparented. Only meaningful for [BatchTagStudios].
	CreateParent bool
}

// StashBoxBatchTag starts Stash's own batch tagger and returns the id of the
// job doing it. It does not wait; follow it with [Client.FindJob].
//
// This is the server-side version of matching a library against a stash-box:
// Stash queries the box for each entity, and writes back the stash id plus
// whatever fields are not excluded. Doing the same thing client-side means one
// round trip per entity and no access to Stash's own matching, which is why
// this exists even though [Client.ScrapePerformers] can reach the same data.
//
// What it writes is not reviewable in advance. ExcludeFields is the only
// control over that, so on a library whose own metadata is better than the
// box's, exclude everything except the stash id.
func (c *Client) StashBoxBatchTag(ctx context.Context, target BatchTagTarget, opts BatchTagOptions) (jobID string, err error) {
	switch target {
	case BatchTagPerformers, BatchTagStudios, BatchTagTags:
	default:
		return "", fmt.Errorf("stash: batch tagging: %q is not a target", target)
	}
	if opts.Endpoint == "" {
		return "", fmt.Errorf("stash: batch tagging: no stash-box endpoint")
	}
	if len(opts.IDs) > 0 && len(opts.Names) > 0 {
		return "", fmt.Errorf("stash: batch tagging: ids and names are alternatives, not a pair")
	}

	input := map[string]any{
		"stash_box_endpoint": opts.Endpoint,
		"refresh":            opts.Refresh,
	}
	if len(opts.IDs) > 0 {
		input["ids"] = opts.IDs
	}
	if len(opts.Names) > 0 {
		input["names"] = opts.Names
	}
	if len(opts.ExcludeFields) > 0 {
		input["exclude_fields"] = opts.ExcludeFields
	}
	if opts.CreateParent {
		input["createParent"] = true
	}

	data, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: StashBoxBatchTagInput!) { ` + string(target) + `(input: $input) }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return "", fmt.Errorf("stash: batch tagging with %s: %w", target, err)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding job id: %w", err)
	}
	return result[string(target)], nil
}
