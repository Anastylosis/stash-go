# stash-go

[![CI](https://github.com/Anastylosis/stash-go/actions/workflows/ci.yml/badge.svg)](https://github.com/Anastylosis/stash-go/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/Anastylosis/stash-go/branch/master/graph/badge.svg)](https://codecov.io/gh/Anastylosis/stash-go)
[![Go Reference](https://pkg.go.dev/badge/github.com/Anastylosis/stash-go.svg)](https://pkg.go.dev/github.com/Anastylosis/stash-go)
[![Go Report Card](https://goreportcard.com/badge/github.com/Anastylosis/stash-go)](https://goreportcard.com/report/github.com/Anastylosis/stash-go)

A Go client for the GraphQL API of a running [Stash](https://stashapp.cc)
server.

Stash ships a Go client for its *plugins* (`pkg/plugin/util`), but it is wired
to localhost and shaped for that use. Anything talking to a Stash instance from
outside ends up hand-rolling the transport, auth and error handling. This is
that layer, written once.

```go
import "github.com/Anastylosis/stash-go"

c := stash.NewClient("http://localhost:9999", stash.WithAPIKey(key))

scenes, err := c.FindAllScenes(ctx, stash.SceneFilter{StudioName: "Example"}, nil)
```

## What it gives you

- **No dependencies.** Standard library only.
- **Your HTTP client.** `WithHTTPClient` takes whatever retry, backoff, proxy
  and timeout behaviour your program already uses. The library imposes none.
- **Typed errors.** `*APIError` carries the GraphQL messages, `*HTTPError` the
  status — so you can branch on a schema mismatch versus a bad key without
  matching on error text.
- **Sentinel errors for missing filter targets.** Stash answers "no such
  performer" with an empty result set, so a typo looks exactly like a genuine
  no-match. `ErrPerformerNotFound` and `ErrStudioNotFound` make the difference
  visible.
- **The API key never reaches an error string.** Some GraphQL middlewares echo
  the request back on auth failure; those messages get logged.
- **Scenes with their files.** Path, size, resolution, codecs and content
  fingerprints come back with every scene, so duplicate detection does not have
  to re-query for them.
- **An escape hatch.** `Execute` runs any query against the same transport, so
  an unwrapped corner of the schema does not mean starting over.

## Schema differences between versions

Stash 0.20 or newer is required.

GraphQL fails the **whole** query when asked for a field the schema lacks — one
unknown field costs the entire response, not just that field. Against an older
server that turns into a confusing total failure.

```go
if ok, _ := c.Supports(ctx, "captions"); ok {
    // safe to ask for it
}
```

Introspection runs once per client and is cached.

## Partial updates are non-destructive

`SceneUpdate` sends only the fields you set. An unset `Title` leaves the
existing title alone rather than clearing it.

```go
title := "Corrected"
err := c.UpdateScene(ctx, stash.SceneUpdate{ID: id, Title: &title})
```

## Documentation

- [docs/usage.md](docs/usage.md) — the API, paginating large libraries, and
  ensuring tags/performers/studios exist
- [docs/design.md](docs/design.md) — why the client is shaped this way, and
  what it deliberately does not do

## Tests

`go test ./...` needs no network — every test drives an `httptest` stub.

A second suite runs against a real server. It is read-only: it queries, and
never creates, updates or deletes anything.

```sh
STASH_URL=http://your-server:9999 STASH_API_KEY=… go test -tags integration ./...
```

Without a reachable server the whole suite skips.

## Status

Early. The surface covers scenes with their files, and the
tag/performer/studio entities that metadata pushes need. Operations for
deduplication work — merging scenes, moving and deleting files — are next;
`Execute` covers them meanwhile.

## License

GPL-3.0-only.
