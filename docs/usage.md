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
`Organized`, `OCounter`), its `Tags`, `Performers`, `Studio` and `StashIDs`,
and its files.

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
`TagIDs`, `PerformerIDs`, `StudioID`, `Organized`, `StashIDs`, `CoverImage`.

The list fields (`URLs`, `TagIDs`, `PerformerIDs`, `StashIDs`) **replace** what
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
