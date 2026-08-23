package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// StashBoxConfig is a stash-box as it is configured, credential included.
//
// Separate from [StashBox] on purpose: that type is what a caller reads and
// deliberately has no API key, because it is the server's credential for a
// third party. This one exists because configuring a stash-box means sending
// the key, and rewriting the list means sending back the keys of the entries
// being kept.
type StashBoxConfig struct {
	Name     string `json:"name"`
	Endpoint string `json:"endpoint"`
	APIKey   string `json:"api_key"`
	// MaxRequestsPerMinute throttles the server's calls to this box. Zero
	// means the server's default rather than "no requests".
	MaxRequestsPerMinute int `json:"max_requests_per_minute"`
}

// StashBoxConfigs returns the configured stash-boxes with their API keys.
//
// Prefer [Client.StashBoxes] for anything that only needs to know which boxes
// exist. This one hands back credentials, and is here for the one job that
// needs them: rewriting the list without destroying the entries it keeps.
func (c *Client) StashBoxConfigs(ctx context.Context) ([]StashBoxConfig, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ configuration { general { stashBoxes { name endpoint api_key max_requests_per_minute } } } }`,
	})
	if err != nil {
		return nil, fmt.Errorf("stash: reading stash-box configuration: %w", err)
	}
	var result struct {
		Configuration struct {
			General struct {
				StashBoxes []StashBoxConfig `json:"stashBoxes"`
			} `json:"general"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding stash-box configuration: %w", err)
	}
	return result.Configuration.General.StashBoxes, nil
}

// SetStashBoxes replaces the configured stash-boxes with exactly this list.
//
// It replaces; it does not add. Passing one box removes every other, along
// with its API key, and Stash asks nothing before doing so. Read the current
// list with [Client.StashBoxConfigs], append to it, and send the result.
//
// Passing an empty list removes them all, which is a legitimate thing to want
// and so is not refused.
func (c *Client) SetStashBoxes(ctx context.Context, boxes []StashBoxConfig) error {
	list := make([]map[string]any, 0, len(boxes))
	for _, b := range boxes {
		if b.Endpoint == "" {
			return fmt.Errorf("stash: configuring stash-boxes: %q has no endpoint", b.Name)
		}
		list = append(list, map[string]any{
			"name":                    b.Name,
			"endpoint":                b.Endpoint,
			"api_key":                 b.APIKey,
			"max_requests_per_minute": b.MaxRequestsPerMinute,
		})
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ConfigGeneralInput!) { configureGeneral(input: $input) { __typename } }`,
		Variables: map[string]any{"input": map[string]any{"stashBoxes": list}},
	})
	if err != nil {
		return fmt.Errorf("stash: configuring stash-boxes: %w", err)
	}
	return nil
}

// ValidateStashBox asks the server whether it can reach a stash-box with the
// given credential, and returns what it says.
//
// The request is made by the *server*, so this tests the server's route to
// the box rather than this program's — which is the whole point when the two
// are on different machines.
func (c *Client) ValidateStashBox(ctx context.Context, endpoint, apiKey string) (valid bool, status string, err error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($input: StashBoxInput!) { validateStashBoxCredentials(input: $input) { valid status } }`,
		Variables: map[string]any{"input": map[string]any{
			"endpoint": endpoint, "api_key": apiKey, "name": "",
		}},
	})
	if err != nil {
		return false, "", fmt.Errorf("stash: validating stash-box %s: %w", endpoint, err)
	}
	var result struct {
		Validate struct {
			Valid  bool   `json:"valid"`
			Status string `json:"status"`
		} `json:"validateStashBoxCredentials"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false, "", fmt.Errorf("stash: decoding stash-box validation: %w", err)
	}
	return result.Validate.Valid, result.Validate.Status, nil
}

// SubmitSceneDraft sends a scene to a stash-box as a draft and returns the
// draft's id there.
//
// A draft is not an edit. It lands in the stash-box as a proposal that
// someone — usually the submitter — then turns into a create or a modify
// through the stash-box's own interface. Nothing changes upstream until they
// do.
func (c *Client) SubmitSceneDraft(ctx context.Context, sceneID, endpoint string) (draftID string, err error) {
	return c.submitDraft(ctx, "submitStashBoxSceneDraft", sceneID, endpoint)
}

// SubmitPerformerDraft sends a performer to a stash-box as a draft.
func (c *Client) SubmitPerformerDraft(ctx context.Context, performerID, endpoint string) (draftID string, err error) {
	return c.submitDraft(ctx, "submitStashBoxPerformerDraft", performerID, endpoint)
}

func (c *Client) submitDraft(ctx context.Context, mutation, id, endpoint string) (string, error) {
	if id == "" || endpoint == "" {
		return "", fmt.Errorf("stash: %s: an id and an endpoint are both required", mutation)
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `mutation($input: StashBoxDraftSubmissionInput!) { ` + mutation + `(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{
			"id": id, "stash_box_endpoint": endpoint,
		}},
	})
	if err != nil {
		return "", fmt.Errorf("stash: %s for %s: %w", mutation, id, err)
	}
	var result map[string]*string
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding draft id: %w", err)
	}
	if result[mutation] == nil {
		// The mutation is typed as a nullable ID, so a submission the box
		// declined is not an error at the GraphQL level.
		return "", fmt.Errorf("stash: %s for %s: the stash-box returned no draft id", mutation, id)
	}
	return *result[mutation], nil
}

// SubmitFingerprints sends the scenes' file fingerprints to a stash-box,
// against whatever entries they are already linked to there.
//
// Only linked scenes contribute: the stash id is what says which upstream
// scene the hashes belong to, so a scene with none is silently nothing to
// submit. ok is what the server reports.
func (c *Client) SubmitFingerprints(ctx context.Context, endpoint string, sceneIDs ...string) (ok bool, err error) {
	if endpoint == "" {
		return false, fmt.Errorf("stash: submitting fingerprints: no endpoint")
	}
	if len(sceneIDs) == 0 {
		return false, nil
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `mutation($input: StashBoxFingerprintSubmissionInput!) { submitStashBoxFingerprints(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{
			"scene_ids": sceneIDs, "stash_box_endpoint": endpoint,
		}},
	})
	if err != nil {
		return false, fmt.Errorf("stash: submitting fingerprints: %w", err)
	}
	var result struct {
		Submitted bool `json:"submitStashBoxFingerprints"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return false, fmt.Errorf("stash: decoding fingerprint submission: %w", err)
	}
	return result.Submitted, nil
}
