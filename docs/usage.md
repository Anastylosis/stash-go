# Usage

```go
import "github.com/Anastylosis/stash-go"
```

The import path ends in `stash-go`; the package is `stash`.

## Connecting

```go
c := stash.NewClient("http://localhost:9999", stash.WithAPIKey(key))

if err := c.Ping(ctx); err != nil {
    return fmt.Errorf("stash unreachable: %w", err)
}
```

`baseURL` is the server root — `/graphql` is appended. Omit `WithAPIKey` for an
instance with authentication disabled.

### Inside a Stash plugin

Stash hands a plugin process a session cookie in `server_connection`, and a
plugin has no API key unless the operator configured one:

```go
c := stash.NewClient(url, stash.WithCookie(cookie))
```

Pass both when you have both — the API key wins. Session cookies expire
mid-run, which on a long task fails partway through rather than at startup.

### Bring your own HTTP client

```go
c := stash.NewClient(url, stash.WithHTTPClient(myRetryingClient))
```

The default is a plain `http.Client` with a 30s timeout and **no retry**. If
your program already has a transport with backoff and pooling, pass it — the
library will not fight it.

## Reading scenes

```go
scene, found, err := c.FindScene(ctx, "42")
```

`found` is false when no scene has that ID. That is not an error.

Every scene query returns the same selection set: the scene's own metadata
(`Title`, `Code`, `Details`, `Director`, `Date`, `URLs`, `Rating100`,
`Organized`, `OCounter`), its `Tags`, `Performers`, `Studio`, `StashIDs` and
`Galleries`, and its files.

`scene.HasStashID()` reports whether stash-box metadata is attached.

### Files and fingerprints

`Scene.Files` carries the full video-file record — path, size, dimensions,
codecs, bitrate, frame rate and content hashes:

```go
if f := scene.PrimaryFile(); f != nil {
    fmt.Println(f.Path, f.Width, f.Height, f.Size)
    if hash, ok := f.Fingerprint("phash"); ok {
        // perceptual hash, for duplicate detection
    }
}
```

`PrimaryFile` is the first file, the one Stash treats as canonical; it returns
nil for a scene with no files. `Fingerprint` looks up a hash by type
(`"oshash"`, `"phash"`, `"md5"`).

A scene has more than one file when Stash has attached re-detected duplicates
to it, which is the case deduplication tools care about.

### Filtering by date

```go
has := false
undated, _, err := c.FindScenes(ctx, stash.SceneFilter{HasDate: &has}, 1, 100)

recent, _, err := c.FindScenes(ctx,
    stash.SceneFilter{DateAfter: "2009-01-01", DateBefore: "2010-01-01"}, 1, 100)
```

`DateBefore` and `DateAfter` are exclusive at both ends, and combine into one
range — Stash takes a single date criterion, so sending them as two filters
would silently keep only the last. A scene with no date matches neither bound:
an absent date is not an early one.

### Filtering

```go
scenes, total, err := c.FindScenes(ctx, stash.SceneFilter{
    StudioName:   "Example Studio",
    PathContains: "/archive/",
}, 1, 100)
```

Pages are 1-based, sorted by path so paging stays stable.

**Naming a performer or studio that does not exist is an error**, not an empty
page:

```go
_, _, err := c.FindScenes(ctx, stash.SceneFilter{PerformerName: "Typo"}, 1, 100)
if errors.Is(err, stash.ErrPerformerNotFound) {
    // a typo, not "nothing matched"
}
```

Stash itself answers a bad name with zero results and no error, which is why
this distinction is worth an extra lookup.

To find scenes lacking stash-box metadata:

```go
no := false
scenes, _, err := c.FindScenes(ctx, stash.SceneFilter{HasStashID: &no}, 1, 100)
```

### Whole library

```go
scenes, err := c.FindAllScenes(ctx, stash.SceneFilter{}, func(fetched, total int) {
    fmt.Printf("\r%d/%d", fetched, total)
})
```

This is slow on a large instance — a 61k-scene library is several minutes and
hundreds of requests. Pass a cancellable context; on cancellation you get what
was collected so far *and* `ctx.Err()`, so partial work is not thrown away.

## Writing scenes

Only the fields you set are sent:

```go
title := "Corrected Title"
organized := true

err := c.UpdateScene(ctx, stash.SceneUpdate{
    ID:        scene.ID,
    Title:     &title,
    Organized: &organized,
})
```

Everything unset is left as it was. The pointer is what distinguishes "set this
to empty" (`&""`) from "do not touch this" (`nil`).

Settable: `Title`, `Code`, `Details`, `Director`, `Date`, `Rating100`, `URLs`,
`TagIDs`, `PerformerIDs`, `StudioID`, `GalleryIDs`, `Organized`, `StashIDs`,
`CoverImage`.

The list fields (`URLs`, `TagIDs`, `PerformerIDs`, `GalleryIDs`, `StashIDs`) **replace** what
is there rather than adding to it, so union them with the scene's current
values first if that is what you mean. Being slices, they are omitted when nil
and cannot express "clear this list" — use `Execute` for that.

### Cover images

`CoverImage` takes a data URI you have already produced:

```go
img := "data:image/jpeg;base64," + base64.StdEncoding.EncodeToString(jpeg)
update.CoverImage = &img
```

The library will not fetch a URL for you — see [design.md](design.md).

## Tags, performers and studios

Metadata pushes need IDs, and the entity may not exist yet:

```go
tagID, err := c.EnsureTag(ctx, "Cowgirl")
perfID, err := c.EnsurePerformer(ctx, "Someone")
studioID, err := c.EnsureStudio(ctx, "Example Studio")
```

`EnsureTag` checks **aliases** before creating. Stash treats aliases as
first-class, so a tag may already exist under a name you do not know — creating
blindly makes a duplicate that then has to be merged by hand.

The narrower operations are available when you need them:

```go
id, found, err := c.FindTag(ctx, name)
id, found, err := c.FindTagByAlias(ctx, name)
id, err := c.CreateTag(ctx, name)
```

## Backing up the database

`DownloadBackup` backs up the server's database and streams it to a writer:

```go
f, err := os.Create("local.sqlite.backup")
name, n, err := c.DownloadBackup(ctx, stash.BackupOptions{IncludeBlobs: true}, f)
```

`name` is what the server called the backup — `local.sqlite.85.20260101_000000`,
carrying the schema version and the timestamp — which is worth keeping as the
filename rather than inventing one.

Two things about the transfer are easy to get wrong:

- **The HTTP client's timeout covers all of it.** A database is hundreds of
  megabytes on a real library, and the default client's 30s timeout applies to
  the whole stream, not just the response headers. Pass `WithHTTPClient(&http.Client{})`
  and bound the run with `ctx` instead. A transfer killed by the client's
  timeout says so, and names the option.
- **`WithMaxResponseBytes` does not apply.** That cap protects a caller
  decoding a GraphQL response into memory; this is a stream to a writer you
  chose.

`BackupDatabase` is the other half: it leaves the backup on the server and
returns the path it wrote.

```go
path, err := c.BackupDatabase(ctx, stash.BackupOptions{})
// C:\Users\you\.stash\local.sqlite.85.20260101_000000
```

That path is in the *server's* notation, and against a Windows-hosted Stash it
is a drive letter and backslashes — nothing the calling machine can open, and
nothing `path/filepath` on a Unix host will parse correctly. It is a string to
show a human, not a path to act on.

`IncludeBlobs` means nothing on a server that keeps blobs in the database,
which is what an empty `blobsPath` in the configuration means: there they are
part of the file either way. It matters where blobs live on the filesystem.

Stash does not delete the temporary file it served the download from — it
clears that directory on restart — so backing up on a schedule leaves copies
on the server's temp volume.

## Saved filters

A saved filter is what appears in Stash's sidebar, and its criteria are **not
written the way a query's are**. Stash accepts the query notation, stores it,
and the filter then does nothing in the UI:

```
query:  "date": {"modifier": "NOT_NULL", "value": ""}
saved:  "date": {"modifier": "NOT_NULL", "value": {"value": ""}}

query:  "tags": {"modifier": "EXCLUDES", "value": ["4"], "depth": 0}
saved:  "tags": {"modifier": "EXCLUDES",
                 "value": {"depth": 0, "items": [{"id": 4, "label": "HD%20Available"}]}}
```

Booleans are stringly typed there too — `organized` is the string `"false"`.
`SaveSceneFilter` writes the second notation from the same [SceneFilter] that
queries with the first, so the filter is described once:

```go
id, err := c.SaveSceneFilter(ctx, "Made before 2010",
    stash.SceneFilter{DateBefore: "2010-01-01"},
    &stash.FindFilter{Sort: "date", Direction: "ASC", PerPage: 100})
```

Saving under a name that already exists updates that filter rather than adding
a second of the same name, and carries its `ui_options` across — Stash allows
the duplicate, and a program run twice should not leave two identical entries
in someone's sidebar.

`SavedFilters`, `FindSavedFilter` and `DestroySavedFilter` are the rest.

## Adding tags without replacing them

`SceneUpdate.TagIDs` **replaces** a scene's tags. Adding one through it means
reading the current list, appending and writing it back — which drops any tag
added in between, and drops all of them if the read is skipped.

```go
err := c.AddSceneTags(ctx, []string{tagID}, sceneIDs...)
```

Stash's bulk update takes an ADD mode instead, applied to every scene named in
one request. `RemoveSceneTags` is the same the other way; removing a tag a
scene does not have is not an error.

## Media the API will not hand over

A scene carries URLs to things GraphQL does not return as data — the sprite
sheet, its WebVTT, the cover, the stream — and those routes want the same
credential as `/graphql`.

```go
var sprite bytes.Buffer
contentType, n, err := c.Fetch(ctx, scene.Paths.Sprite, &sprite)
```

`Fetch` applies the credential and re-roots the URL the way `DownloadBackup`
does, so one the server built from a proxied request still resolves. Stash
generates sprites, previews and covers lazily, so a missing one comes back as
`ErrNotFound` rather than a status code to unwrap — that is an ordinary state
of a scene, not a failure to stop for.

## Plugins and packages

The package manager, which arrived in Stash 0.25, installs plugins and
scrapers from an index. A package is identified by id *and* source, because
two indexes may both offer an id:

```go
sources, err := c.PackageSources(ctx, stash.PackagePlugin)
available, err := c.AvailablePackages(ctx, stash.PackagePlugin, sources[0].URL)

job, err := c.InstallPackages(ctx, stash.PackagePlugin, pkg.Spec())
```

`AvailablePackages` makes the *server* fetch that index over the internet, so
it is slower than it looks and fails when the server is offline rather than
when you are.

Install, update and uninstall are background jobs and return an id for
[`FindJob`](#tasks). Two things they do not do:

- **Resolve requirements.** A package whose `Requires` names something absent
  installs anyway, and fails when it runs. Check the field.
- **Reject a spec that matches nothing.** Stash matches on id and source
  together, so a spec missing either runs a job that installs nothing and
  reports success. `InstallPackages` refuses one rather than let that happen.

Against a server older than 0.25 every call here returns `ErrNoPackageManager`
wrapping the server's own message, rather than a bare "Cannot query field"
that reads like a bug in this library.

`Plugins` is the separate question of what the server has *loaded*: a plugin
installed from a source appears in both lists, one dropped into the plugins
directory by hand appears only in `Plugins`, and one whose files will not
parse appears in neither.

```go
err := c.SetPluginsEnabled(ctx, map[string]bool{"example": true})
```

## Interface settings

`ConfigureInterface` writes interface settings and leaves the rest alone, the
same way `SceneUpdate` does. `InterfaceConfig` reads back the fields you name:

```go
current, err := c.InterfaceConfig(ctx, "javascript", "javascriptEnabled")
err = c.ConfigureInterface(ctx, map[string]any{"javascriptEnabled": true})
```

The caller names the fields because the section is large and version-dependent,
and one field the schema lacks fails the whole query — asking only for what you
are about to write means a field you do not use cannot break you.

Custom JavaScript runs in every browser that opens that Stash. Read it before
writing it, and keep what is there.

## Performers, and where their details come from

`EnsurePerformer` takes a name. `CreatePerformerFrom` takes everything Stash
will store about one, and omits what it was not given — an empty field is not
the same as an absent one, and sending `birthdate: ""` stores an empty
birthdate.

```go
id, err := c.CreatePerformerFrom(ctx, stash.PerformerInput{
    Name:     "Example Performer",
    Gender:   "FEMALE",
    HeightCM: 167,
    Image:    "https://example.test/images/1",
    StashIDs: []stash.StashID{{Endpoint: endpoint, ID: "abc-123"}},
})
```

`Image` may be a URL or a `data:` URI. Given a URL, the *server* fetches it,
so one only the calling machine can reach will not work.

### Find by stash id, not by name

```go
id, found, err := c.FindPerformerByStashID(ctx, endpoint, "abc-123")
```

`FindPerformer` matches a name, and a name is neither unique nor stable — two
performers share one, one performer changes theirs, a scraper writes it with
different punctuation. A stash-box id is the same string forever, which makes
it the check worth making before creating anything.

### Scraping a scene

```go
found, err := c.ScrapeSceneByID(ctx, endpoint, sceneID)   // by the file's fingerprints
found, err := c.ScrapeScenes(ctx, endpoint, "MILF1773")   // by text
```

`ScrapeSceneByID` matches on the file's own fingerprints, which is exact.
`ScrapeScenes` matches on whatever the stash-box searches — titles, in
practice — so it returns the studio's *other* scenes when it does not have the
one asked for. One result is not the same as the right result: check something
about it, ideally something the stash-box did not supply, before believing it.

An empty result is the ordinary answer for a library the stash-box does not
cover, not a failure.

### Scraping a stash-box

```go
boxes, err := c.StashBoxes(ctx)
found, err := c.ScrapePerformers(ctx, boxes[0].Endpoint, "abc-123")
id, err := c.CreatePerformerFrom(ctx, found[0].Input(boxes[0].Endpoint))
```

The server does the scraping, with the API key configured for that stash-box;
one with no key returns nothing rather than an error. `StashBox` deliberately
does not carry that key — it is the server's credential for a third party, and
a field for it is an invitation to log one.

`query` is matched against names, so a name returns everything close to it and
a caller picking the first result picks wrong. **Passing a stash id returns
the one performer it belongs to**, which is the reliable way to use this.

`Input` does the converting: heights and weights arrive as strings and
sometimes with a unit attached, aliases arrive comma-separated where the input
wants a list, and the first image is the one to keep.

## Older servers

Stash 0.20 or newer is required: the shared selection set asks for the
`files { … }` record introduced there. Everything in it has existed since.

Asking for a field the schema lacks fails the **whole** query, not just that
field:

```go
if ok, err := c.Supports(ctx, "captions"); err == nil && ok {
    // safe to include it
}
```

Introspection runs once per client and is cached.

`Version` reports the server's version string if you need to gate on it
directly.

## Anything not wrapped

```go
data, err := c.Execute(ctx, `query($id: ID!) {
    findScene(id: $id) { id sceneStreams { url mime_type } }
}`, map[string]any{"id": id})
```

`Execute` returns the raw `data` object with the same transport, auth, size
limits and error handling as the typed methods.

When your query returns scenes, paste in `SceneFields` and decode into `Scene`
rather than declaring a parallel type and field list that drift apart:

```go
query := `query { findDuplicateScenes(distance: 4) { ` + stash.SceneFields + ` } }`
data, err := c.Execute(ctx, query, nil)

var result struct {
    Groups [][]stash.Scene `json:"findDuplicateScenes"`
}
err = json.Unmarshal(data, &result)
```

## Errors

```go
var apiErr *stash.APIError
var httpErr *stash.HTTPError

switch {
case errors.As(err, &httpErr) && httpErr.StatusCode == 401:
    // bad or missing API key; httpErr.Body has the server's own message
case errors.As(err, &apiErr):
    // reached the resolver. Each entry keeps the GraphQL `path` and
    // `extensions`, so you can branch on the failing field or the server's
    // error code rather than on message text:
    for _, e := range apiErr.Errors {
        if e.Extensions["code"] == "GRAPHQL_VALIDATION_FAILED" {
            // asked for a field this server's schema lacks
        }
    }
    // apiErr.Messages() when the structure is not needed
}
```

Branch on the type rather than the message text. The API key is stripped from
error strings before they leave the client, so these are safe to log.
