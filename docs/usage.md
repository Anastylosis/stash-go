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
    // bad or missing API key
case errors.As(err, &apiErr):
    // reached the resolver; apiErr.Messages says what it objected to
}
```

Branch on the type rather than the message text. The API key is stripped from
error strings before they leave the client, so these are safe to log.
