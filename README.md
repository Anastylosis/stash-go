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
- **Captions, when the server has them.** `WithCaptions()` adds
  `Scene.Captions` to the scene queries, probing once to check the field
  exists rather than failing every query on a server that lacks it.
- **Plugin settings.** `PluginSettings("your-plugin")` reads what the user
  configured in the Stash UI, which is how a Go plugin gets its own config.
- **Tasks and their jobs.** `MetadataScan` starts a scan — the only way to
  make Stash notice a file that appeared on disk — and `FindJob` follows it.
- **An escape hatch.** `Execute` runs any query against the same transport, so
  an unwrapped corner of the schema does not mean starting over — and
  `SceneFields` drops the standard selection set into your own query, so the
  result still decodes into `Scene`.

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

`Scene.captions` is the field this matters most for, so it has an option of
its own. It is off by default: honouring it costs an introspection request,
and a caller that does not want captions should not pay for one.

```go
c := stash.NewClient(url, stash.WithCaptions())
```

With the option set and the field absent, scene queries run unchanged and
`Scene.Captions` stays nil rather than erroring.

## Partial updates are non-destructive

`SceneUpdate` sends only the fields you set. An unset `Title` leaves the
existing title alone rather than clearing it.

```go
title := "Corrected"
err := c.UpdateScene(ctx, stash.SceneUpdate{ID: id, Title: &title})
```

## Scanning, and why it matters for subtitles

Captions are read-only in GraphQL. A subtitle is attached by writing a
sidecar next to the video and making Stash scan for it — there is no
mutation that attaches one.

```go
job, err := c.MetadataScan(ctx, stash.ScanOptions{Paths: []string{dir}})
```

Every generate flag defaults to off. That is *not* Stash's own default, which
remembers whatever was ticked last in the UI — a library call that quietly
started generating covers, previews and sprites across a library would be an
expensive surprise. Ask for what you want:

```go
stash.ScanOptions{Paths: []string{dir}, GeneratePhashes: true}
```

Scanning is a background job, so `MetadataScan` returns an id rather than
waiting:

```go
for {
    j, found, err := c.FindJob(ctx, job)
    if err != nil || !found || j.Status.Done() {
        break
    }
    time.Sleep(2 * time.Second)
}
```

`Status.Done()` covers all three terminal states — treating `CANCELLED` as
still-running turns that loop into a hang.

## Documentation

- [docs/usage.md](docs/usage.md) — the API, paginating large libraries, and
  ensuring tags/performers/studios exist
- [docs/design.md](docs/design.md) — why the client is shaped this way, and
  what it deliberately does not do
- [CONTRIBUTING.md](CONTRIBUTING.md) — cutting a release, and the two
  constraints any addition has to respect

## Tests

`go test ./...` needs no network — every test drives an `httptest` stub.

A second suite runs against a real server. It is read-only: it queries, and
never creates, updates or deletes anything.

```sh
STASH_URL=http://your-server:9999 STASH_API_KEY=… go test -tags integration ./...
```

Without a reachable server the whole suite skips.

## Status

Early. The surface covers scenes with their files and captions, the
tag/performer/studio entities that metadata pushes need, plugin settings, and
the scan/job pair. Operations for deduplication work — merging scenes, moving
and deleting files — are next; `Execute` covers them meanwhile.

`MetadataScan` is the one call the live suite does not exercise: a scan is an
hours-long mutation against a real library, not something a test should start.
Its request shape is pinned by unit tests and checked against the server's own
schema introspection.

## License

Copyright (C) 2026 Wasylq

GPL-3.0-only.
