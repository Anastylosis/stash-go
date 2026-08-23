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
