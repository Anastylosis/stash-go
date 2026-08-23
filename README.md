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
- **The API key never reaches an error string.** Some GraphQL middlewares echo
  the request back on auth failure, and those messages get logged.
- **Sentinel errors where Stash answers with silence.** A filter naming a
  performer that does not exist returns an empty result set, so a typo looks
  exactly like a genuine no-match. `ErrPerformerNotFound`, `ErrStudioNotFound`
  and `ErrTagNotFound` make the difference visible.
- **Partial updates that cannot clear a field by accident** — and separate
  calls for when clearing is what you meant.

## What it covers

Scenes, performers, studios and tags; the files and fingerprints behind a
scene; saved filters; plugins and the package manager; the metadata tasks and
the jobs they run; database backup; and submitting to a stash-box.

That is **23 of Stash's 62 queries and 37 of its 125 mutations**. The rest is
mostly galleries, images, groups and markers — whole object types this has
never needed, and writing them untested would be worse than leaving them out.
`Execute` reaches anything not wrapped, using the same transport, auth and
error handling.

The notable gap for anyone doing library maintenance is **deduplication**:
`sceneMerge`, `sceneDestroy` and `moveFiles` are not here yet, though
`MergePerformers` and `MergeTags` are.

## Which Stash it works against

**Verified against Stash 0.31.1 (schema 85).** That is the version the live
suite runs on, and the only one anything has been checked against.

Older servers are *likely* to work and are not tested. Two things found while
building this are the reason for the hedge: Stash's date criterion requires a
`value` even when the modifier ignores it, and `career_start` is declared
`String` although it holds a year. Shapes like those drift between releases,
and a mismatch is not subtle — GraphQL fails the **whole** query when asked
for a field the schema lacks, so one wrong field costs the entire response.

Where a field is known to vary, ask first:

```go
if ok, _ := c.Supports(ctx, "captions"); ok {
    // safe to include it
}
```

Introspection runs once per client and is cached. `Scene.captions` is the
field this matters most for, so it has an option of its own — off by default,
because honouring it costs an introspection request a caller who does not want
captions should not pay:

```go
c := stash.NewClient(url, stash.WithCaptions())
```

With the option set and the field absent, scene queries run unchanged and
`Scene.Captions` stays nil rather than erroring.

## Three things that surprise people

**A partial update cannot empty a field.** `SceneUpdate` sends only what you
set, so an unset `Title` leaves the stored one alone — which is what makes it
safe, and what makes `Title: ""` mean "leave it" rather than "clear it".

```go
title := "Corrected"
err := c.UpdateScene(ctx, stash.SceneUpdate{ID: id, Title: &title})
err = c.ClearSceneFields(ctx, id, "title")   // actually empties it
```

`ClearPerformerFields`, `ClearStudioFields` and `ClearTagFields` do the same
for the others, sending the empty value each field's type wants.

**List fields replace rather than add.** `SceneUpdate.TagIDs` overwrites a
scene's tags, so adding one through it means a read-modify-write that loses
whatever arrived in between. `AddSceneTags` and `AddScenePerformers` use
Stash's own ADD mode instead, for many scenes in one request.

**Captions are read-only.** A subtitle is attached by writing a sidecar next
to the video and making Stash scan for it; there is no mutation that attaches
one.

```go
job, err := c.MetadataScan(ctx, stash.ScanOptions{Paths: []string{dir}})
```

Scanning and generating are background jobs, so they return an id rather than
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

Every generate flag defaults to off, which is *not* Stash's own default. A
generate across a library is hours of work and gigabytes of output, so ask for
what you want:

```go
job, err := c.MetadataGenerate(ctx, stash.GenerateOptions{
    Sprites: true, Phashes: true, SceneIDs: []string{id},
})
```

## Documentation

- [docs/usage.md](docs/usage.md) — the API, call by call
- [docs/design.md](docs/design.md) — why it is shaped this way, and what it
  deliberately does not do
- [CONTRIBUTING.md](CONTRIBUTING.md) — cutting a release, and the constraints
  any addition has to respect

## Tests

`go test ./...` needs no network — every test drives an `httptest` stub, and
CI enforces that nothing reaches out.

Two properties are asserted across **every** method that talks to the server,
so anything added later is covered the moment it joins the list: that it
reports a problem rather than returning a zero value and no error, against a
GraphQL error, an HTTP 500, an HTTP 401 and a body that is not JSON; and that
it never lets the API key into an error string.

A second suite runs against a real server. It is read-only by contract: it
queries, and never creates, updates or deletes.

```sh
STASH_URL=http://your-server:9999 STASH_API_KEY=… go test -tags integration ./...
```

Without a reachable server the whole suite skips.

The calls that write are therefore **not** exercised live — scans, generates,
backups, package installs and the entity mutations are pinned by unit tests
and checked against the server's own schema introspection instead. A scan is
an hours-long mutation against somebody's real library; a test should not
start one.

## Status

Useful and in production against one library, not finished.

Solid in the sense that matters: the surface it has is tested, the error paths
with it, and three real bugs found by using it are fixed. Incomplete in that
half the API is not here, and only one Stash version has ever been checked.

Versions follow `vMAJOR.MINOR.PATCH`. Everything since v0.7.0 has been
additive — no call has changed its signature or its behaviour.

## License

Copyright (C) 2026 Wasylq

GPL-3.0-only.
