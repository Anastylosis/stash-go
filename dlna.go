package stash

import (
	"context"
	"encoding/json"
	"fmt"
	"time"
)

// DLNAStatus is what the server's DLNA service is currently doing.
type DLNAStatus struct {
	Running bool `json:"running"`
	// Until is when the current state ends, and reads in whichever
	// direction Running points: while running it is when the service will
	// stop, while stopped it is when it will start. nil means the state has
	// no end — which is the usual case, since only a timed
	// [Client.EnableDLNA] or [Client.DisableDLNA] sets one.
	Until *string `json:"until"`
	// RecentIPAddresses are the addresses that have asked the service for
	// something lately, whether or not it answered. This is where the
	// address for [Client.AllowDLNAIP] comes from: a device is identified
	// by having just tried.
	RecentIPAddresses []string `json:"recentIPAddresses"`
	// AllowedIPAddresses are the temporary grants — the permanent list
	// lives in the configuration, under dlnaInterfaces and dlnaWhitelist.
	AllowedIPAddresses []DLNAIP `json:"allowedIPAddresses"`
}

// DLNAIP is one temporarily allowed address.
type DLNAIP struct {
	Address string `json:"ipAddress"`
	// Until is when the grant lapses, or nil for one made without a
	// duration, which lasts until the server restarts.
	Until *string `json:"until"`
}

// DLNAStatus reports whether the DLNA service is running, which addresses
// have been asking for it, and which are allowed to.
func (c *Client) DLNAStatus(ctx context.Context) (DLNAStatus, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ dlnaStatus { running until recentIPAddresses allowedIPAddresses { ipAddress until } } }`,
	})
	if err != nil {
		return DLNAStatus{}, fmt.Errorf("stash: reading DLNA status: %w", err)
	}
	var result struct {
		DLNAStatus DLNAStatus `json:"dlnaStatus"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return DLNAStatus{}, fmt.Errorf("stash: decoding DLNA status: %w", err)
	}
	return result.DLNAStatus, nil
}

// EnableDLNA starts the DLNA service for the given duration, or until it is
// disabled again when d is zero.
//
// None of this touches the configuration: a temporary enable is forgotten on
// restart, when the dlnaEnabled setting decides again.
func (c *Client) EnableDLNA(ctx context.Context, d time.Duration) error {
	return c.dlnaSwitch(ctx, "enableDLNA", "EnableDLNAInput", d, nil)
}

// DisableDLNA stops the DLNA service for the given duration, or until it is
// enabled again when d is zero.
func (c *Client) DisableDLNA(ctx context.Context, d time.Duration) error {
	return c.dlnaSwitch(ctx, "disableDLNA", "DisableDLNAInput", d, nil)
}

// AllowDLNAIP lets one address use the DLNA service for the given duration,
// or until the server restarts when d is zero. It is a temporary grant on top
// of the configured whitelist, not an addition to it.
//
// The address is a bare IP — the form [Client.DLNAStatus] reports in
// RecentIPAddresses, which is where a device that has just tried and been
// refused shows up.
func (c *Client) AllowDLNAIP(ctx context.Context, address string, d time.Duration) error {
	if address == "" {
		return fmt.Errorf("stash: allowing a DLNA address: no address")
	}
	return c.dlnaSwitch(ctx, "addTempDLNAIP", "AddTempDLNAIPInput", d, map[string]any{"address": address})
}

// DisallowDLNAIP revokes a grant made by [Client.AllowDLNAIP]. It says
// nothing about an address in the configured whitelist, which this cannot
// reach.
func (c *Client) DisallowDLNAIP(ctx context.Context, address string) error {
	if address == "" {
		return fmt.Errorf("stash: revoking a DLNA address: no address")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: RemoveTempDLNAIPInput!) { removeTempDLNAIP(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{"address": address}},
	})
	if err != nil {
		return fmt.Errorf("stash: revoking DLNA access for %s: %w", address, err)
	}
	return nil
}

// dlnaSwitch runs one of the four DLNA mutations, all of which take an
// optional duration and answer with a boolean.
func (c *Client) dlnaSwitch(ctx context.Context, mutation, inputType string, d time.Duration, input map[string]any) error {
	if input == nil {
		input = map[string]any{}
	}
	if d != 0 {
		// Stash counts in whole minutes, and an omitted duration means
		// "no expiry" — so a sub-minute one would silently become
		// permanent, which is the opposite of what it asked for.
		minutes := int(d / time.Minute)
		if minutes == 0 {
			return fmt.Errorf("stash: %s: %s is less than the minute Stash counts in; pass 0 for no expiry", mutation, d)
		}
		input["duration"] = minutes
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ` + inputType + `!) { ` + mutation + `(input: $input) }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return fmt.Errorf("stash: %s: %w", mutation, err)
	}
	return nil
}
