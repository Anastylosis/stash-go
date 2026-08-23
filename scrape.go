package stash

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// StashBox is one stash-box the server is configured against — stashdb.org
// and its siblings, the shared metadata databases Stash matches against.
//
// The API key is deliberately not carried here. It is the server's
// credential for a third party, and a library handing it back invites it into
// a log line.
type StashBox struct {
	Endpoint string `json:"endpoint"`
	Name     string `json:"name"`
}

// StashBoxes returns the stash-boxes the server is configured against, in the
// order Stash holds them. A scrape needs one of these endpoints, and an empty
// result means nothing can be scraped from a stash-box at all.
func (c *Client) StashBoxes(ctx context.Context) ([]StashBox, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ configuration { general { stashBoxes { endpoint name } } } }`,
	})
	if err != nil {
		return nil, fmt.Errorf("stash: reading stash-boxes: %w", err)
	}
	var result struct {
		Configuration struct {
			General struct {
				StashBoxes []StashBox `json:"stashBoxes"`
			} `json:"general"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding stash-boxes: %w", err)
	}
	return result.Configuration.General.StashBoxes, nil
}

// ScrapedPerformer is a performer as a scraper describes one. Its fields are
// strings because that is what scrapers return, including the numeric ones —
// [ScrapedPerformer.Input] does the converting.
type ScrapedPerformer struct {
	Name           string   `json:"name"`
	Disambiguation string   `json:"disambiguation"`
	Gender         string   `json:"gender"`
	Birthdate      string   `json:"birthdate"`
	DeathDate      string   `json:"death_date"`
	Country        string   `json:"country"`
	Ethnicity      string   `json:"ethnicity"`
	EyeColor       string   `json:"eye_color"`
	HairColor      string   `json:"hair_color"`
	Height         string   `json:"height"`
	Weight         string   `json:"weight"`
	Measurements   string   `json:"measurements"`
	FakeTits       string   `json:"fake_tits"`
	CareerLength   string   `json:"career_length"`
	Tattoos        string   `json:"tattoos"`
	Piercings      string   `json:"piercings"`
	Details        string   `json:"details"`
	URLs           []string `json:"urls"`
	// Aliases is one comma-separated string, not a list. Stash's own input
	// wants a list, which is half of what Input is for.
	Aliases string `json:"aliases"`
	// Images are URLs, best first.
	Images []string `json:"images"`
	// RemoteSiteID is the performer's id at the source. For a stash-box that
	// is the stash id, which is what makes the result worth keeping.
	RemoteSiteID string `json:"remote_site_id"`
	// StoredID is set when Stash already has this performer, which saves
	// creating a second one under a name that differs by punctuation.
	StoredID string `json:"stored_id"`
}

// ScrapePerformers searches a stash-box through the server and returns what it
// found.
//
// query is matched against names, and a name search returns everything close
// to it — ten results for a common first name is normal, so a caller picking
// blindly will pick wrong. Passing a stash id instead returns the one
// performer it belongs to, which is the reliable way to use this when the id
// is already known.
//
// The server does the scraping, using the API key configured for that
// stash-box. A stash-box with no key configured returns nothing rather than
// an error.
func (c *Client) ScrapePerformers(ctx context.Context, endpoint, query string) ([]ScrapedPerformer, error) {
	if endpoint == "" || query == "" {
		return nil, fmt.Errorf("stash: scraping performer: endpoint and query are both required")
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($source: ScraperSourceInput!, $input: ScrapeSinglePerformerInput!) {
			scrapeSinglePerformer(source: $source, input: $input) {
				name disambiguation gender birthdate death_date country ethnicity
				eye_color hair_color height weight measurements fake_tits
				career_length tattoos piercings details urls aliases images
				remote_site_id stored_id } }`,
		Variables: map[string]any{
			"source": map[string]any{"stash_box_endpoint": endpoint},
			"input":  map[string]any{"query": query},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("stash: scraping performer %q: %w", query, err)
	}
	var result struct {
		ScrapeSinglePerformer []ScrapedPerformer `json:"scrapeSinglePerformer"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding scraped performer: %w", err)
	}
	return result.ScrapeSinglePerformer, nil
}

// ScrapedScene is a scene as a stash-box describes one.
type ScrapedScene struct {
	Title    string   `json:"title"`
	Code     string   `json:"code"`
	Date     string   `json:"date"`
	Details  string   `json:"details"`
	Director string   `json:"director"`
	URLs     []string `json:"urls"`
	// Image is a URL to the scene's cover.
	Image string `json:"image"`
	// Duration is in seconds, and is the check worth making before
	// believing a match: two scenes with the same code and wildly different
	// lengths are not the same scene.
	Duration int `json:"duration"`
	// RemoteSiteID is the scene's stash id.
	RemoteSiteID string             `json:"remote_site_id"`
	Studio       *ScrapedStudio     `json:"studio"`
	Performers   []ScrapedPerformer `json:"performers"`
	Tags         []ScrapedTag       `json:"tags"`
}

// ScrapedStudio is a studio as a scraper describes one. StoredID is set when
// Stash already has it, which saves looking it up by a name that may differ.
type ScrapedStudio struct {
	Name         string `json:"name"`
	StoredID     string `json:"stored_id"`
	RemoteSiteID string `json:"remote_site_id"`
	URL          string `json:"url"`
}

// ScrapedTag is a tag as a scraper describes one.
type ScrapedTag struct {
	Name     string `json:"name"`
	StoredID string `json:"stored_id"`
}

// ScrapeScenes searches a stash-box for scenes through the server.
//
// Passing a scene id Stash already knows — see [Client.ScrapeSceneByID] —
// matches on the file's fingerprints, which is exact. A text query matches on
// whatever the stash-box searches, so it returns near-misses too: check
// something about each result before believing it, because "one result" is
// not the same as "the right one".
func (c *Client) ScrapeScenes(ctx context.Context, endpoint, query string) ([]ScrapedScene, error) {
	if endpoint == "" || query == "" {
		return nil, fmt.Errorf("stash: scraping scene: endpoint and query are both required")
	}
	return c.scrapeScenes(ctx, endpoint, map[string]any{"query": query}, query)
}

// ScrapeSceneByID asks a stash-box about a scene Stash already has, matched
// on the file's fingerprints rather than on any text.
//
// An empty result is the ordinary answer for a library the stash-box does not
// cover, not a failure.
func (c *Client) ScrapeSceneByID(ctx context.Context, endpoint, sceneID string) ([]ScrapedScene, error) {
	if endpoint == "" || sceneID == "" {
		return nil, fmt.Errorf("stash: scraping scene: endpoint and scene id are both required")
	}
	return c.scrapeScenes(ctx, endpoint, map[string]any{"scene_id": sceneID}, sceneID)
}

const scrapedSceneFields = `
	title code date details director urls image duration remote_site_id
	studio { name stored_id remote_site_id url }
	performers { name gender remote_site_id stored_id images country birthdate }
	tags { name stored_id }`

func (c *Client) scrapeScenes(ctx context.Context, endpoint string, input map[string]any, what string) ([]ScrapedScene, error) {
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($source: ScraperSourceInput!, $input: ScrapeSingleSceneInput!) {
			scrapeSingleScene(source: $source, input: $input) {` + scrapedSceneFields + `} }`,
		Variables: map[string]any{
			"source": map[string]any{"stash_box_endpoint": endpoint},
			"input":  input,
		},
	})
	if err != nil {
		return nil, fmt.Errorf("stash: scraping scene %q: %w", what, err)
	}
	var result struct {
		ScrapeSingleScene []ScrapedScene `json:"scrapeSingleScene"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding scraped scene: %w", err)
	}
	return result.ScrapeSingleScene, nil
}

// Input converts a scraped performer into something [Client.CreatePerformerFrom]
// can take, with endpoint naming the stash-box it came from so the stash id
// is recorded against the right one.
//
// The conversions are the fiddly part and the reason this exists: heights and
// weights arrive as strings and sometimes with a unit attached, aliases
// arrive comma-separated where the input wants a list, and the first image is
// the one to keep.
func (p ScrapedPerformer) Input(endpoint string) PerformerInput {
	in := PerformerInput{
		Name:           p.Name,
		Disambiguation: p.Disambiguation,
		Gender:         p.Gender,
		Birthdate:      p.Birthdate,
		DeathDate:      p.DeathDate,
		Country:        p.Country,
		Ethnicity:      p.Ethnicity,
		EyeColor:       p.EyeColor,
		HairColor:      p.HairColor,
		HeightCM:       measure(p.Height),
		Weight:         measure(p.Weight),
		Measurements:   p.Measurements,
		FakeTits:       p.FakeTits,
		CareerLength:   p.CareerLength,
		Tattoos:        p.Tattoos,
		Piercings:      p.Piercings,
		Details:        p.Details,
		URLs:           p.URLs,
		Aliases:        splitAliases(p.Aliases),
	}
	if len(p.Images) > 0 {
		in.Image = p.Images[0]
	}
	if p.RemoteSiteID != "" && endpoint != "" {
		in.StashIDs = []StashID{{Endpoint: endpoint, ID: p.RemoteSiteID}}
	}
	return in
}

// measure reads the leading number out of a scraped measurement. "167",
// "167 cm" and "167cm" all mean the same thing, and a scraper picks whichever
// the source used.
func measure(s string) int {
	s = strings.TrimSpace(s)
	end := 0
	for end < len(s) && s[end] >= '0' && s[end] <= '9' {
		end++
	}
	if end == 0 {
		return 0
	}
	n, err := strconv.Atoi(s[:end])
	if err != nil {
		return 0
	}
	return n
}

func splitAliases(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}
