package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func inputOf(t *testing.T, req graphqlRequest) map[string]any {
	t.Helper()
	b, err := json.Marshal(req.Variables["input"])
	if err != nil {
		t.Fatalf("marshalling input: %v", err)
	}
	var out map[string]any
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("decoding input: %v", err)
	}
	return out
}

func TestCreatePerformerFromSendsEverythingGiven(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerCreate":{"id":"9"}}}`))
	defer srv.Close()

	id, err := NewClient(srv.URL).CreatePerformerFrom(context.Background(), PerformerInput{
		Name:     "Example Performer",
		Gender:   "FEMALE",
		Country:  "US",
		HeightCM: 167,
		Aliases:  []string{"Alias One", "Alias Two"},
		Image:    "https://example.test/images/1",
		StashIDs: []StashID{{Endpoint: "https://example.test/graphql", ID: "abc-123"}},
	})
	if err != nil {
		t.Fatalf("CreatePerformerFrom: %v", err)
	}
	if id != "9" {
		t.Errorf("id = %q, want 9", id)
	}

	in := inputOf(t, cap.reqs[0])
	if in["name"] != "Example Performer" || in["height_cm"] != float64(167) {
		t.Errorf("input = %+v", in)
	}
	ids, _ := in["stash_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("stash_ids = %v", in["stash_ids"])
	}
	if got := ids[0].(map[string]any)["stash_id"]; got != "abc-123" {
		t.Errorf("stash_id = %v", got)
	}
}

// Sending an empty field is not the same as not sending it: Stash stores the
// empty string, and a performer ends up with a birthdate of "".
func TestCreatePerformerFromOmitsWhatItWasNotGiven(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"performerCreate":{"id":"1"}}}`))
	defer srv.Close()

	if _, err := NewClient(srv.URL).CreatePerformerFrom(context.Background(),
		PerformerInput{Name: "Only A Name"}); err != nil {
		t.Fatalf("CreatePerformerFrom: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	if len(in) != 1 {
		t.Errorf("sent %d fields, want only name: %+v", len(in), in)
	}
	// Zero is a real height nobody has, and sending it would be a lie.
	if _, ok := in["height_cm"]; ok {
		t.Error("height_cm was sent as zero")
	}
}

func TestCreatePerformerFromRefusesAnEmptyName(t *testing.T) {
	_, c := server(t, reply(`{"data":{"performerCreate":{"id":"1"}}}`))
	if _, err := c.CreatePerformerFrom(context.Background(), PerformerInput{}); err == nil {
		t.Error("CreatePerformerFrom with no name: want an error")
	}
}

func TestFindPerformerByStashID(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findPerformers":{"performers":[{"id":"42"}]}}}`))
	defer srv.Close()

	id, found, err := NewClient(srv.URL).FindPerformerByStashID(context.Background(),
		"https://example.test/graphql", "abc-123")
	if err != nil {
		t.Fatalf("FindPerformerByStashID: %v", err)
	}
	if !found || id != "42" {
		t.Errorf("got (%q, %v)", id, found)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["f"])
	if !strings.Contains(string(b), "abc-123") || !strings.Contains(string(b), "EQUALS") {
		t.Errorf("filter = %s", b)
	}
}

func TestFindPerformerByStashIDNotFound(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findPerformers":{"performers":[]}}}`))
	id, found, err := c.FindPerformerByStashID(context.Background(), "https://example.test/graphql", "nope")
	if err != nil {
		t.Fatalf("FindPerformerByStashID: %v", err)
	}
	if found || id != "" {
		t.Errorf("got (%q, %v), want empty", id, found)
	}
}

func TestFindPerformerByStashIDRefusesAnEmptyID(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if _, _, err := c.FindPerformerByStashID(context.Background(), "https://example.test/graphql", ""); err == nil {
		t.Error("want an error for an empty stash id")
	}
}
