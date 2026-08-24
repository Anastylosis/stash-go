# Design

Why the client is shaped this way, and what it deliberately does not do.

## It does not own the HTTP client

`WithHTTPClient` takes yours. The library ships a plain `http.Client` with a 30s
timeout and no retry.

A client library that bakes in its own retry policy fights the program using it.
One caller already has a pooled transport with backoff; another is inside a
request handler where a retry means a timeout; a third needs a proxy. None of
that is the Stash client's business, and all of it is expressible as an
`*http.Client`.

The cost is that the default is naive. That is the right default: obviously
insufficient beats subtly wrong.

## No dependencies

Standard library only, and intended to stay that way. A client for one service
should not drag a GraphQL framework into someone's build.

The queries are string constants. That is less elegant than a generated client,
but a generated client from Stash's full schema is enormous, regenerates on
every upstream change, and would make this package a code-generation project
rather than a library. Hand-written queries also make the wire format reviewable
in a diff.

## Errors are typed, not formatted

GraphQL returns HTTP 200 with an `errors` array. Distinguishing "your key is
wrong" from "this server is too old for that field" by substring-matching an
error message is unreliable, so both surface as values:

| type | meaning |
|---|---|
| `*APIError` | request reached the resolver; `Messages` holds what it said |
| `*HTTPError` | non-2xx; `StatusCode` holds it |

### Sentinel errors for missing filter targets

Stash answers a filter naming a non-existent performer with **zero scenes and
no error**. A typo, a stray trailing space and a genuinely empty result are
indistinguishable — a caller exits 0 having done nothing, and reports success.

`FindScenes` resolves those names to IDs first and returns
`ErrPerformerNotFound` / `ErrStudioNotFound` when the lookup finds nothing. It
costs an extra round trip per named filter, which is worth it to turn a silent
no-op into an error.

### The API key is redacted from error text

Some GraphQL middlewares echo the request back on an auth failure, so the key
can appear inside `errors[].message`. Error strings get logged, printed to
stderr, and pasted into issues. Every message is scrubbed before it leaves the
client.

## Schema capability probing

GraphQL fails the **entire** query when it is asked for a field the schema does
not have. Against an older Stash, one optional field turns a working import into
a total failure with a message about the field, not about the version.

`Supports` introspects `Scene`'s field list once per client and caches it, so a
caller can ask before it commits to a selection set. The alternative — issue the
query, parse the failure, retry without the field — is slower and guesses at
error text.

## One selection set, and where it stops

Every scene query sends the same field list. Per-call selection sets would let
each caller ask for exactly what it uses, at the cost of a query-building API,
a Scene type full of fields that may or may not be populated, and no single
place to check what this package requires of a server.

So the list is fixed, and the rule for adding to it is that the field must be
cheap and belong to the scene itself. Scene scalars (`code`, `director`,
`rating100`, `o_counter`), the video-file record including fingerprints,
captions, group membership and the playback counters all qualify — a consumer
doing duplicate detection needs hashes and resolutions, and one pushing
metadata pays a few hundred bytes per scene it ignores.

The rule used to have a second half — the field had to exist on every
supported server — and that is what kept `groups`, `play_count` and `captions`
out. Pinning support to one current release retired it. Captions in particular
were behind an introspection probe and an opt-in option for exactly this
reason; both are gone, and the field is simply in the set.

**The target is Stash 0.31.1, schema 85** — the release this is built and
tested against. Older servers are not supported and not worked around: a field
this asks for by name may have been renamed or absent, and GraphQL fails the
whole query rather than the field. `Supports` remains for a consumer that
wants to ask before reaching for something through `Execute`.

The set is exported as `SceneFields` so a consumer writing its own query still
decodes into `Scene` completely. A field list copied by hand is a field list
that drifts: the type gains a field, the copy does not, and the value is
silently zero.

## Partial updates

`SceneUpdate`'s optional fields are pointers and slices with `omitempty`, so an
unset field is absent from the JSON rather than sent as a zero value.

This is load-bearing. A metadata push that sets a title must not blank the
details, and the difference between "set this to empty" and "do not touch this"
is exactly the difference between a nil pointer and a pointer to `""`.

## What it does not do

**It does not download cover images.** An earlier version of this code fetched
an arbitrary scraped URL and base64-encoded it into the mutation. That needs
SSRF validation, a size cap, and a policy for expired signed CDN links — all
application policy, none of it about talking to Stash. `SceneUpdate.CoverImage`
takes a data URI the caller has already produced.

**It does not define an interface for "media manager".** Go's convention is that
consumers declare the interface they need, so a program that wants to swap Stash
for something else writes the two or three methods it actually calls and accepts
anything satisfying them. A shared interface package would be a coordination
point with no benefit.

**It does not wrap the whole schema yet.** The covered surface is scenes and
their files, the performer/studio/tag entities, saved filters, plugins and
packages, the metadata tasks, backup, stash-box submission, and the server's
own administration — 32 of 62 queries and 60 of 125 mutations.

The whole API is the goal, so the remainder is a to-do list rather than a
boundary. It is mostly one shape: galleries, images, groups and markers are
about half of what is left, object types nothing has needed and therefore
nothing has tested. That is the constraint, not appetite — an untested wrapper
is worse than none, because a caller cannot tell the two apart until it fails.
`Execute` runs any query against the same transport, auth and error handling,
so an unwrapped corner is an inconvenience rather than a wall.

**It does not hand you raw SQL.** Stash exposes `querySQL` and `execSQL`.
A Go client offering arbitrary SQL against somebody's library is a footgun
with no matching benefit, and `Execute` already covers the escape-hatch case
that motivates them.

**It does not set up a Stash.** The `setup` mutation writes the configuration
for a server that has none, naming directories on the server's filesystem that
a client cannot see, validate or create. `SystemStatus` reports `SETUP` so a
program can say what is wrong and stop, which is the useful half.

**It does not follow the log.** New entries arrive over a GraphQL
subscription: a websocket, a second transport, and a reconnect-and-backoff
policy of its own — in a package whose entire transport story is "pass your
own `http.Client`". `Logs` reads the ring buffer the server already keeps.

**It does not claim to support every Stash version.** Everything here is
verified against 0.31.1, schema 85. Older servers are likely to work and are
untested — see the version note in the README for why the hedge is real rather
than boilerplate.

## Pagination

`FindScenes` sorts by `path` ascending. Any stable sort would do; an unstable
one silently drops and repeats rows across pages as the server reorders.

`FindAllScenes` pages at 100. On a 61k-scene library that is ~613 requests and
several minutes, which is why it takes a progress callback and why cancellation
returns the partial result alongside `ctx.Err()` rather than discarding the work.
