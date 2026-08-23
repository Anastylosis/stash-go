package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
)

func TestStashBoxesDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"configuration":{"general":{"stashBoxes":[
		{"endpoint":"https://example.test/graphql","name":"Example"}]}}}}`))

	boxes, err := c.StashBoxes(context.Background())
	if err != nil {
		t.Fatalf("StashBoxes: %v", err)
	}
	if len(boxes) != 1 || boxes[0].Endpoint != "https://example.test/graphql" {
		t.Errorf("boxes = %+v", boxes)
	}
}

// The API key is the server's credential for a third party, and a struct
// field for it is an invitation to log one.
func TestStashBoxesDoesNotCarryTheAPIKey(t *testing.T) {
	if _, ok := reflect.TypeOf(StashBox{}).FieldByName("APIKey"); ok {
		t.Error("StashBox has an APIKey field")
	}
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"configuration":{"general":{"stashBoxes":[]}}}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).StashBoxes(context.Background()); err != nil {
		t.Fatalf("StashBoxes: %v", err)
	}
	if strings.Contains(cap.reqs[0].Query, "api_key") {
		t.Errorf("the query asks for the API key: %s", cap.reqs[0].Query)
	}
}

func TestScrapePerformersDecodes(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"scrapeSinglePerformer":[{
		"name":"Example Performer","gender":"FEMALE","country":"US","height":"167",
		"aliases":"Alias One, Alias Two","images":["https://example.test/1","https://example.test/2"],
		"remote_site_id":"abc-123"}]}}`))
	defer srv.Close()

	got, err := NewClient(srv.URL).ScrapePerformers(context.Background(), "https://example.test/graphql", "abc-123")
	if err != nil {
		t.Fatalf("ScrapePerformers: %v", err)
	}
	if len(got) != 1 || got[0].RemoteSiteID != "abc-123" {
		t.Fatalf("scraped = %+v", got)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["source"])
	if !strings.Contains(string(b), "stash_box_endpoint") {
		t.Errorf("source = %s", b)
	}
}

func TestScrapePerformersRequiresBothArguments(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if _, err := c.ScrapePerformers(context.Background(), "", "x"); err == nil {
		t.Error("want an error without an endpoint")
	}
	if _, err := c.ScrapePerformers(context.Background(), "https://example.test/graphql", ""); err == nil {
		t.Error("want an error without a query")
	}
}

// The conversion is the fiddly part: strings to numbers, one comma-separated
// string to a list, and the first image out of many.
func TestScrapedPerformerInput(t *testing.T) {
	in := ScrapedPerformer{
		Name:         "Example Performer",
		Height:       "167 cm",
		Weight:       "55",
		Aliases:      "Alias One, Alias Two ,, ",
		Images:       []string{"https://example.test/1", "https://example.test/2"},
		RemoteSiteID: "abc-123",
	}.Input("https://example.test/graphql")

	if in.HeightCM != 167 {
		t.Errorf("HeightCM = %d, want 167", in.HeightCM)
	}
	if in.Weight != 55 {
		t.Errorf("Weight = %d, want 55", in.Weight)
	}
	if !reflect.DeepEqual(in.Aliases, []string{"Alias One", "Alias Two"}) {
		t.Errorf("Aliases = %q", in.Aliases)
	}
	if in.Image != "https://example.test/1" {
		t.Errorf("Image = %q, want the first", in.Image)
	}
	if len(in.StashIDs) != 1 || in.StashIDs[0].ID != "abc-123" {
		t.Errorf("StashIDs = %+v", in.StashIDs)
	}
}

func TestScrapedPerformerInputWithNothingToConvert(t *testing.T) {
	in := ScrapedPerformer{Name: "Example"}.Input("")
	if in.HeightCM != 0 || in.Weight != 0 || in.Aliases != nil || in.Image != "" {
		t.Errorf("input = %+v", in)
	}
	// No endpoint means no stash id to record it against, and a stash id
	// with an empty endpoint matches nothing later.
	if len(in.StashIDs) != 0 {
		t.Errorf("StashIDs = %+v, want none", in.StashIDs)
	}
}

func TestMeasureIgnoresJunk(t *testing.T) {
	for in, want := range map[string]int{"167": 167, "167 cm": 167, "167cm": 167, "": 0, "tall": 0, "-5": 0} {
		if got := measure(in); got != want {
			t.Errorf("measure(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestScrapeScenesDecodes(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"scrapeSingleScene":[{
		"title":"A Scene","code":"CODE1","date":"2010-07-21","duration":1479,
		"remote_site_id":"abc-123",
		"studio":{"name":"A Studio","stored_id":"3"},
		"performers":[{"name":"Someone","stored_id":"5"}],
		"tags":[{"name":"a tag"}]}]}}`))
	defer srv.Close()

	got, err := NewClient(srv.URL).ScrapeScenes(context.Background(), "https://example.test/graphql", "CODE1")
	if err != nil {
		t.Fatalf("ScrapeScenes: %v", err)
	}
	if len(got) != 1 || got[0].Duration != 1479 || got[0].Studio == nil || got[0].Studio.StoredID != "3" {
		t.Fatalf("scraped = %+v", got)
	}
	// stored_id is what says the box's performer is already in the library,
	// and is the difference between finding one and creating a second.
	if len(got[0].Performers) != 1 || got[0].Performers[0].StoredID != "5" {
		t.Errorf("performers = %+v", got[0].Performers)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	if !strings.Contains(string(b), `"query"`) {
		t.Errorf("input = %s", b)
	}
}

// Matching on the file's fingerprints rather than on text, which is a
// different input field and the difference between exact and fuzzy.
func TestScrapeSceneByIDSendsTheSceneID(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"scrapeSingleScene":[]}}`))
	defer srv.Close()

	got, err := NewClient(srv.URL).ScrapeSceneByID(context.Background(), "https://example.test/graphql", "36")
	if err != nil {
		t.Fatalf("ScrapeSceneByID: %v", err)
	}
	// An empty result is the ordinary answer for a library the box does not
	// cover, not a failure.
	if len(got) != 0 {
		t.Errorf("scraped = %+v", got)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	if !strings.Contains(string(b), `"scene_id":"36"`) {
		t.Errorf("input = %s", b)
	}
}

func TestScrapeSceneCallsNeedBothArguments(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if _, err := c.ScrapeScenes(context.Background(), "", "x"); err == nil {
		t.Error("want an error without an endpoint")
	}
	if _, err := c.ScrapeSceneByID(context.Background(), "https://example.test/graphql", ""); err == nil {
		t.Error("want an error without a scene id")
	}
}
