package stash

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFindNamedReturnsFirstMatch(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findTags":{"tags":[{"id":"7","name":"anal"}]}}}`))
	id, found, err := c.FindTag(context.Background(), "anal")
	if err != nil || !found {
		t.Fatalf("FindTag: %v, found=%v", err, found)
	}
	if id != "7" {
		t.Errorf("id = %q, want 7", id)
	}
}

func TestFindNamedEmptyIsNotAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findStudios":{"studios":[]}}}`))
	id, found, err := c.FindStudio(context.Background(), "nope")
	if err != nil {
		t.Fatalf("FindStudio: %v", err)
	}
	if found || id != "" {
		t.Errorf("got (%q, %v), want an empty id and found=false", id, found)
	}
}

func TestCreateReturnsID(t *testing.T) {
	_, c := server(t, reply(`{"data":{"performerCreate":{"id":"42"}}}`))
	id, err := c.CreatePerformer(context.Background(), "Someone")
	if err != nil {
		t.Fatalf("CreatePerformer: %v", err)
	}
	if id != "42" {
		t.Errorf("id = %q, want 42", id)
	}
}

func TestCreateWithoutIDIsAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"studioCreate":{"id":""}}}`))
	if _, err := c.CreateStudio(context.Background(), "X"); err == nil {
		t.Fatal("want an error when the server returns no id")
	}
}

// EnsureTag must check aliases before creating: Stash treats aliases as
// first-class, so a tag can already exist under a name the caller does not
// know, and creating blindly makes a duplicate.
func TestEnsureTagChecksAliasBeforeCreating(t *testing.T) {
	var queries []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		queries = append(queries, string(body))
		switch {
		case strings.Contains(string(body), "aliases"):
			_, _ = io.WriteString(w, `{"data":{"findTags":{"tags":[{"id":"9","name":"canonical"}]}}}`)
		default:
			_, _ = io.WriteString(w, `{"data":{"findTags":{"tags":[]}}}`)
		}
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL).EnsureTag(context.Background(), "synonym")
	if err != nil {
		t.Fatalf("EnsureTag: %v", err)
	}
	if id != "9" {
		t.Errorf("id = %q, want the alias match 9", id)
	}
	if len(queries) != 2 {
		t.Fatalf("made %d queries, want 2 (name then alias)", len(queries))
	}
	for _, q := range queries {
		if strings.Contains(q, "tagCreate") {
			t.Error("EnsureTag created a duplicate despite an alias match")
		}
	}
}

func TestEnsureCreatesWhenAbsent(t *testing.T) {
	created := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "studioCreate") {
			created = true
			_, _ = io.WriteString(w, `{"data":{"studioCreate":{"id":"new"}}}`)
			return
		}
		_, _ = io.WriteString(w, `{"data":{"findStudios":{"studios":[]}}}`)
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL).EnsureStudio(context.Background(), "Fresh")
	if err != nil {
		t.Fatalf("EnsureStudio: %v", err)
	}
	if !created || id != "new" {
		t.Errorf("created=%v id=%q, want true/new", created, id)
	}
}

func TestEnsureDoesNotCreateWhenPresent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), "performerCreate") {
			t.Error("EnsurePerformer created a performer that already existed")
		}
		_, _ = io.WriteString(w, `{"data":{"findPerformers":{"performers":[{"id":"5","name":"X"}]}}}`)
	}))
	defer srv.Close()

	id, err := NewClient(srv.URL).EnsurePerformer(context.Background(), "X")
	if err != nil {
		t.Fatalf("EnsurePerformer: %v", err)
	}
	if id != "5" {
		t.Errorf("id = %q, want 5", id)
	}
}
