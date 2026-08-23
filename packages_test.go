package stash

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAvailablePackagesDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"availablePackages":[
		{"package_id":"example-plugin","name":"Example Plugin","version":"1.2.3-abcdef0","date":"2026-01-01 00:00:00",
		 "sourceURL":"https://example.test/index.yml",
		 "metadata":{"description":"An example plugin"},
		 "requires":[{"package_id":"example-dependency"}]}]}}`))

	pkgs, err := c.AvailablePackages(context.Background(), PackagePlugin, "https://example.test/index.yml")
	if err != nil {
		t.Fatalf("AvailablePackages: %v", err)
	}
	if len(pkgs) != 1 {
		t.Fatalf("got %d packages, want 1", len(pkgs))
	}
	p := pkgs[0]
	if p.ID != "example-plugin" || p.Version != "1.2.3-abcdef0" {
		t.Errorf("package = %+v", p)
	}
	if p.Description() != "An example plugin" {
		t.Errorf("Description() = %q", p.Description())
	}
	// Requirements are not installed for you, so a caller has to be able to
	// see them before deciding the install is finished.
	if len(p.Requires) != 1 || p.Requires[0].ID != "example-dependency" {
		t.Errorf("Requires = %+v", p.Requires)
	}
	if got := p.Spec(); got.ID != "example-plugin" || got.SourceURL != "https://example.test/index.yml" {
		t.Errorf("Spec() = %+v", got)
	}
}

func TestDescriptionOfPackageWithoutMetadata(t *testing.T) {
	if got := (Package{}).Description(); got != "" {
		t.Errorf("Description() = %q, want empty", got)
	}
}

func TestInstallPackagesReturnsJobID(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"installPackages":"7"}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	id, err := c.InstallPackages(context.Background(), PackagePlugin,
		PackageSpec{ID: "example-plugin", SourceURL: "https://example.test/index.yml"})
	if err != nil {
		t.Fatalf("InstallPackages: %v", err)
	}
	if id != "7" {
		t.Errorf("job id = %q, want 7", id)
	}
	if got := cap.reqs[0].Variables["type"]; got != "Plugin" {
		t.Errorf("type = %v, want Plugin", got)
	}
}

// Stash matches a package on id *and* source. A spec missing either matches
// nothing, and the job then runs and does nothing — a success that installed
// no software.
func TestInstallPackagesRejectsIncompleteSpec(t *testing.T) {
	for _, spec := range []PackageSpec{
		{ID: "example-plugin"},
		{SourceURL: "https://example.test/index.yml"},
	} {
		_, c := server(t, reply(`{"data":{"installPackages":"1"}}`))
		if _, err := c.InstallPackages(context.Background(), PackagePlugin, spec); err == nil {
			t.Errorf("InstallPackages(%+v): want an error", spec)
		}
	}
}

func TestInstallPackagesRejectsEmptyList(t *testing.T) {
	_, c := server(t, reply(`{"data":{"installPackages":"1"}}`))
	if _, err := c.InstallPackages(context.Background(), PackagePlugin); err == nil {
		t.Error("InstallPackages with no packages: want an error")
	}
	if _, err := c.UninstallPackages(context.Background(), PackagePlugin); err == nil {
		t.Error("UninstallPackages with no packages: want an error")
	}
}

// updatePackages reads an absent list as "everything installed", which is a
// legitimate request rather than the do-nothing job the other two would run.
func TestUpdatePackagesAcceptsEmptyList(t *testing.T) {
	_, c := server(t, reply(`{"data":{"updatePackages":"9"}}`))
	id, err := c.UpdatePackages(context.Background(), PackagePlugin)
	if err != nil {
		t.Fatalf("UpdatePackages: %v", err)
	}
	if id != "9" {
		t.Errorf("job id = %q, want 9", id)
	}
}

// A server four releases too old otherwise answers with a bare "Cannot query
// field", which reads like a bug in this library.
func TestPackageCallsExplainAMissingPackageManager(t *testing.T) {
	body := `{"errors":[{"message":"Cannot query field \"installPackages\" on type \"Mutation\".","extensions":{"code":"GRAPHQL_VALIDATION_FAILED"}}]}`
	_, c := server(t, reply(body))

	_, err := c.InstallPackages(context.Background(), PackagePlugin,
		PackageSpec{ID: "example-plugin", SourceURL: "https://example.test/index.yml"})
	if !errors.Is(err, ErrNoPackageManager) {
		t.Fatalf("err = %v, want ErrNoPackageManager", err)
	}
	// The server's own words survive the wrapping.
	if !strings.Contains(err.Error(), "Cannot query field") {
		t.Errorf("err = %v, want the server's message kept", err)
	}
}

func TestOtherAPIErrorsAreNotMistakenForAnOldServer(t *testing.T) {
	_, c := server(t, reply(`{"errors":[{"message":"unauthorized"}]}`))
	_, err := c.InstallPackages(context.Background(), PackagePlugin,
		PackageSpec{ID: "example-plugin", SourceURL: "https://example.test/index.yml"})
	if errors.Is(err, ErrNoPackageManager) {
		t.Errorf("err = %v, want it left alone", err)
	}
}

func TestPackageSourcesQueriesThePerTypeField(t *testing.T) {
	for _, tc := range []struct {
		t     PackageType
		field string
	}{{PackagePlugin, "pluginPackageSources"}, {PackageScraper, "scraperPackageSources"}} {
		var query string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			b, _ := io.ReadAll(r.Body)
			query = string(b)
			_, _ = io.WriteString(w, `{"data":{"configuration":{"general":{"`+tc.field+`":[{"name":"Community","url":"https://example.test/index.yml","local_path":"community"}]}}}}`)
		}))
		c := NewClient(srv.URL)
		got, err := c.PackageSources(context.Background(), tc.t)
		srv.Close()
		if err != nil {
			t.Fatalf("PackageSources(%s): %v", tc.t, err)
		}
		if !strings.Contains(query, tc.field) {
			t.Errorf("PackageSources(%s) asked for the wrong field: %s", tc.t, query)
		}
		if len(got) != 1 || got[0].URL != "https://example.test/index.yml" {
			t.Errorf("sources = %+v", got)
		}
	}
}
