package stash

import (
	"context"
	"encoding/json"
	"fmt"
)

// PackageType selects which of Stash's two package managers a call talks to.
type PackageType string

// PackageType values, one per package manager Stash exposes.
const (
	PackagePlugin  PackageType = "Plugin"
	PackageScraper PackageType = "Scraper"
)

// PackageSource is one index a package manager installs from, as configured
// in the server's settings.
type PackageSource struct {
	Name string `json:"name"`
	// URL of the index. It is also the sourceURL a [PackageSpec] has to
	// carry: Stash identifies a package by id *and* source, because two
	// indexes may both offer an id.
	URL       string `json:"url"`
	LocalPath string `json:"local_path"`
}

// Package is one entry in a package index, or one thing already installed.
type Package struct {
	ID        string `json:"package_id"`
	Name      string `json:"name"`
	Version   string `json:"version"`
	Date      string `json:"date"`
	SourceURL string `json:"sourceURL"`
	// Requires names the packages this one needs. Stash does not install
	// them for you — an install that leaves a requirement unmet succeeds and
	// the plugin then fails at runtime.
	Requires []Package `json:"requires"`
	// Metadata is the index's free-form block, which in practice carries a
	// "description". Free-form is why it is not modelled.
	Metadata map[string]any `json:"metadata"`
}

// Description returns the package's description, or "" when the index gives
// none.
func (p Package) Description() string {
	if s, ok := p.Metadata["description"].(string); ok {
		return s
	}
	return ""
}

// Spec returns the [PackageSpec] that names this package for an install or
// uninstall.
func (p Package) Spec() PackageSpec {
	return PackageSpec{ID: p.ID, SourceURL: p.SourceURL}
}

// PackageSpec names one package to install or uninstall. Both fields are
// required: an id alone is ambiguous across sources.
type PackageSpec struct {
	ID        string `json:"id"`
	SourceURL string `json:"sourceURL"`
}

const packageFields = `package_id name version date sourceURL metadata requires { package_id }`

// PackageSources returns the indexes the server is configured to install
// from. A [PackageSpec] needs one of these URLs.
func (c *Client) PackageSources(ctx context.Context, t PackageType) ([]PackageSource, error) {
	field := "pluginPackageSources"
	if t == PackageScraper {
		field = "scraperPackageSources"
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `{ configuration { general { ` + field + ` { name url local_path } } } }`,
	})
	if err != nil {
		return nil, fmt.Errorf("stash: reading package sources: %w", err)
	}
	var result struct {
		Configuration struct {
			General map[string][]PackageSource `json:"general"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding package sources: %w", err)
	}
	return result.Configuration.General[field], nil
}

// InstalledPackages returns what is installed, whether or not its source is
// still configured.
func (c *Client) InstalledPackages(ctx context.Context, t PackageType) ([]Package, error) {
	return c.packages(ctx, "installedPackages",
		`query($type: PackageType!) { installedPackages(type: $type) {`+packageFields+`} }`,
		map[string]any{"type": string(t)})
}

// AvailablePackages returns what the index at sourceURL offers.
//
// The server fetches that index over the internet when the call is made, so
// this is slower than it looks and fails when the server is offline — not
// when this program is.
func (c *Client) AvailablePackages(ctx context.Context, t PackageType, sourceURL string) ([]Package, error) {
	return c.packages(ctx, "availablePackages",
		`query($type: PackageType!, $source: String!) { availablePackages(type: $type, source: $source) {`+packageFields+`} }`,
		map[string]any{"type": string(t), "source": sourceURL})
}

func (c *Client) packages(ctx context.Context, field, query string, vars map[string]any) ([]Package, error) {
	data, err := c.do(ctx, graphqlRequest{Query: query, Variables: vars})
	if err != nil {
		return nil, fmt.Errorf("stash: reading %s: %w", field, err)
	}
	var result map[string][]Package
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, fmt.Errorf("stash: decoding %s: %w", field, err)
	}
	return result[field], nil
}

// InstallPackages installs packages and returns the id of the job doing it.
// It does not wait; follow the job with [Client.FindJob].
//
// Stash downloads each package from its source and unpacks it into the
// server's plugin or scraper directory. A package already installed is
// reinstalled at the index's current version, so this is also how an update
// is forced.
//
// Requirements are not resolved: a package whose Requires names something
// absent installs anyway, and fails when it runs. Check [Package.Requires]
// first.
func (c *Client) InstallPackages(ctx context.Context, t PackageType, specs ...PackageSpec) (jobID string, err error) {
	return c.packageJob(ctx, "installPackages", t, specs)
}

// UninstallPackages removes packages and returns the id of the job doing it.
//
// It deletes the package's directory on the server. Anything a plugin wrote
// inside its own directory goes with it.
func (c *Client) UninstallPackages(ctx context.Context, t PackageType, specs ...PackageSpec) (jobID string, err error) {
	return c.packageJob(ctx, "uninstallPackages", t, specs)
}

// UpdatePackages updates packages, or every installed package of that type
// when no spec is given, and returns the id of the job doing it.
func (c *Client) UpdatePackages(ctx context.Context, t PackageType, specs ...PackageSpec) (jobID string, err error) {
	return c.packageJob(ctx, "updatePackages", t, specs)
}

func (c *Client) packageJob(ctx context.Context, mutation string, t PackageType, specs []PackageSpec) (string, error) {
	// updatePackages takes a nullable list and reads null as "everything";
	// the other two require one, and an empty list there is a job that
	// installs nothing. Refuse it rather than start one.
	if len(specs) == 0 && mutation != "updatePackages" {
		return "", fmt.Errorf("stash: %s: no packages given", mutation)
	}
	for _, s := range specs {
		if s.ID == "" || s.SourceURL == "" {
			// Stash matches on both, and a spec missing either silently
			// matches nothing — the job runs and does nothing.
			return "", fmt.Errorf("stash: %s: package spec needs both an id and a sourceURL, got %+v", mutation, s)
		}
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `mutation($type: PackageType!, $packages: [PackageSpecInput!]!) { ` +
			mutation + `(type: $type, packages: $packages) }`,
		Variables: map[string]any{"type": string(t), "packages": specs},
	})
	if err != nil {
		return "", fmt.Errorf("stash: %s: %w", mutation, err)
	}
	var result map[string]string
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("stash: decoding %s job id: %w", mutation, err)
	}
	return result[mutation], nil
}
