# Contributing

## Cutting a release

Releases are tagged `vMAJOR.MINOR.PATCH`. Pushing the tag runs
[`.github/workflows/release.yml`](.github/workflows/release.yml), which
publishes the GitHub release and opens a linked Discussion in
*Announcements*.

```bash
git tag -a v0.6.0 -m "v0.6.0"
git push origin v0.6.0
```

There are no release assets: a Go module is consumed from its git tag
through the module proxy, never from a downloaded file. The release page
exists for the changelog.

The tag's annotation is rendered as a callout above the commit list — use it
for what the commits cannot say on their own:

```bash
git tag -a v0.6.1 -m "fixes caption decoding on Stash 0.28

No API change; a client built against v0.6.0 needs no edits."
git push origin v0.6.1
```

**A published version cannot be withdrawn.** Once the module proxy has
fetched a tag, that version is permanent — the fix for a bad one is a new
patch release, not a retraction. Worth a moment's thought before pushing the
tag, though not worth an approval gate: the sibling repos have one because
CI genuinely cannot run their smoke tests, and everything that matters here
runs on every commit.

Before tagging, run the live suite against a real Stash — CI cannot, because
there is no Stash on a runner:

```sh
STASH_URL=http://your-server:9999 STASH_API_KEY=… go test -tags integration ./...
```

## Tests

`go test ./...` must pass with **no outbound network**. Every test drives an
`httptest` stub; anything reaching a real server is a bug, and CI enforces it
with `offline-isolation`.

The integration suite (`-tags integration`) is read-only by contract: it
queries, and never creates, updates or deletes. `MetadataScan` is
deliberately not exercised there — a scan is an hours-long mutation against
someone's real library, so its request shape is pinned by unit tests and
checked against the server's schema instead.

## Adding to the surface

Two constraints are load-bearing, and both fail in ways that are hard to
attribute later.

**No dependencies.** Standard library only. It is a stated property of the
module and the reason it is safe to pull into anything.

**A field the server lacks fails the whole query**, not just that field. So a
new scene field goes in `SceneFields` only if every supported server
(Stash 0.30+) has it. Anything newer goes behind a probe — see
`sceneSelection` and `WithCaptions` for the shape: the option is off by
default, so a caller that does not want the field pays no introspection
request to find out it exists.

Prefer widening `Execute`'s reach over wrapping a call that is already easy
to write by hand. Wrapping earns its place when the shape is fiddly, the
failure mode is silent, or the result needs decoding into an existing type.

## Documentation

- [docs/usage.md](docs/usage.md) — the API and how to use it
- [docs/design.md](docs/design.md) — why the client is shaped this way
