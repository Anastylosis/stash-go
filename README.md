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
scene; the merge, delete and move calls behind deduplication; saved filters;
plugins and the package manager; the metadata tasks and the jobs they run;
database backup; submitting to a stash-box; and administering the server
itself — status, logs, general and interface settings, the API key, the
database migrations and DLNA.

That is **32 of Stash's 62 queries and 60 of its 125 mutations**.

The goal is the whole API. What is left is not a boundary, it is a to-do list,
and it is mostly one shape: galleries, images, groups and markers account for
16 of the 30 remaining queries and 33 of the 65 remaining mutations — object
types nothing has needed yet, so nothing has been written and tested for them.
The rest is the URL scrapers, a handful of per-object bulk updates, the
playback and o-counter calls, and whole-library import/export.

Two things are deliberate rather than pending. Raw SQL (`querySQL`, `execSQL`)
will not be wrapped: arbitrary SQL against somebody's library is a footgun with
no matching benefit. Neither will `setup`, which configures a server that has
none, naming directories a client cannot see or validate.

Until a call is wrapped, `Execute` reaches it with the same transport, auth and
error handling as the typed methods.

## Which Stash it works against

**Stash 0.31.1 (schema 85).** That is what this targets, what the live suite
runs against, and the only version anything has been checked on.

Older servers are not supported. This is a deliberate simplification rather
than an untested hedge: the selection sets name fields directly, and GraphQL
fails the **whole** query when asked for one the schema lacks, so a renamed
field costs the entire response rather than itself. Shapes do drift — Stash's
date criterion requires a `value` even when the modifier ignores it, and
`career_start` is declared `String` although it holds a year — and chasing
them across releases bought less than pinning to one did.

Where a field is known to vary, ask first:

```go
if ok, _ := c.Supports(ctx, "groups"); ok {
    // safe to name it in your own Execute query
}
```

Introspection runs once per client and is cached. It is there for a consumer
reaching past the wrapped surface with `Execute`, not for the wrapped calls
themselves — those name what the target server has. `Scene.captions` used to
be gated behind it and an opt-in option; both are gone, and captions are in
the shared selection set with everything else.


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
backups, package installs, the entity mutations, and the administration calls
that migrate, anonymise or replace the API key are pinned by unit tests, and
the live suite checks the shapes they send against the server's own schema
instead. A scan is an hours-long mutation against somebody's real library; a
test should not start one, and nothing should replace a running server's
credential to prove it can.

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
