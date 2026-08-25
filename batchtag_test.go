package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStashBoxBatchTagNeedsAnEndpoint(t *testing.T) {
	_, c := server(t, reply(`{"data":{"stashBoxBatchTagTag":"7"}}`))

	if _, err := c.StashBoxBatchTag(context.Background(), BatchTagTags, BatchTagOptions{}); err == nil {
		t.Fatal("a batch tag with no endpoint was allowed")
	}
}

func TestStashBoxBatchTagRefusesAnUnknownTarget(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"x":"7"}}`))
	defer srv.Close()

	// The target names the mutation, so an unchecked value would be
	// interpolated straight into the query.
	_, err := NewClient(srv.URL).StashBoxBatchTag(context.Background(),
		BatchTagTarget("sceneDestroy(input:{id:1})#"), BatchTagOptions{Endpoint: "https://x.test/graphql"})
	if err == nil {
		t.Fatal("an arbitrary target was accepted")
	}
	if len(capt.reqs) != 0 {
		t.Errorf("sent %d requests", len(capt.reqs))
	}
}

func TestStashBoxBatchTagRefusesBothIDsAndNames(t *testing.T) {
	_, c := server(t, reply(`{"data":{"stashBoxBatchTagTag":"7"}}`))

	_, err := c.StashBoxBatchTag(context.Background(), BatchTagTags, BatchTagOptions{
		Endpoint: "https://x.test/graphql",
		IDs:      []string{"1"},
		Names:    []string{"a"},
	})
	if err == nil {
		t.Fatal("ids and names together were allowed")
	}
}

func TestStashBoxBatchTagSendsTheJob(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"stashBoxBatchTagTag":"41"}}`))
	defer srv.Close()

	job, err := NewClient(srv.URL).StashBoxBatchTag(context.Background(), BatchTagTags, BatchTagOptions{
		Endpoint:      "https://x.test/graphql",
		IDs:           []string{"1", "2"},
		ExcludeFields: []string{"name", "aliases"},
	})
	if err != nil {
		t.Fatalf("StashBoxBatchTag: %v", err)
	}
	if job != "41" {
		t.Errorf("job = %q", job)
	}
	if !strings.Contains(capt.reqs[0].Query, "stashBoxBatchTagTag") {
		t.Errorf("query = %q", capt.reqs[0].Query)
	}
	b, _ := json.Marshal(capt.reqs[0].Variables["input"])
	var in map[string]any
	_ = json.Unmarshal(b, &in)
	if in["stash_box_endpoint"] != "https://x.test/graphql" {
		t.Errorf("endpoint = %v", in["stash_box_endpoint"])
	}
	// Refresh is sent explicitly: its default decides whether a repeat run
	// revisits everything, and a reader of the request should see which.
	if in["refresh"] != false {
		t.Errorf("refresh = %v", in["refresh"])
	}
	if _, ok := in["names"]; ok {
		t.Error("sent an empty names list alongside ids")
	}
	if fields, _ := in["exclude_fields"].([]any); len(fields) != 2 {
		t.Errorf("exclude_fields = %v", in["exclude_fields"])
	}
}
