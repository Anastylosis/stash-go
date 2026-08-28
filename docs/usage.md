# Usage

```go
import "github.com/Anastylosis/stash-go"
```

The import path ends in `stash-go`; the package is `stash`.

**Connecting** · [the client](#connecting) · [inside a plugin](#inside-a-stash-plugin) · [your HTTP client](#bring-your-own-http-client)

**Scenes** · [reading](#reading-scenes) · [files and fingerprints](#files-and-fingerprints) · [filtering](#filtering) · [by date](#filtering-by-date) · [the whole library](#whole-library) · [writing](#writing-scenes) · [covers](#cover-images) · [clearing a list](#clearing-a-list) · [media URLs](#media-the-api-will-not-hand-over)

**People and places** · [tags, performers, studios](#tags-performers-and-studios) · [performer details](#performers-and-where-their-details-come-from) · [reading and changing](#reading-changing-and-removing-them) · [deleting and merging](#deleting-and-merging) · [find by stash id](#find-by-stash-id-not-by-name) · [studios and tags](#studios-and-tags)

**Tasks and jobs** · [tasks](#tasks) · [the ones that write](#the-ones-that-write) · [stopping](#stopping)

**Stash-box** · [scraping a scene](#scraping-a-scene) · [scraping a stash-box](#scraping-a-stash-box)

**The rest** · [saved filters](#saved-filters) · [adding tags](#adding-tags-without-replacing-them) · [plugins and packages](#plugins-and-packages) · [interface settings](#interface-settings) · [backup](#backing-up-the-database) · [older servers](#older-servers) · [anything not wrapped](#anything-not-wrapped) · [errors](#errors)

---

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
`Organized`, `OCounter`), its `Tags`, `Performers`, `Studio`, `StashIDs`,
`Galleries` and `Captions`, its `Groups` membership, the playback counters
(`PlayCount`, `PlayDuration`, `LastPlayedAt`, `ResumeTime`), and its files.

`Captions` lists the subtitle tracks Stash found beside the video. They are
read-only over GraphQL — Stash discovers them by scanning, so a caption cannot
be attached over the API, only written to disk and picked up by a scan.
`LanguageCode` is the bare subtag Stash parsed off the filename, so `clip.pt.srt`
gives `pt` and a regional tag like `clip.pt-BR.srt` is not a caption at all as
far as Stash is concerned.

`Groups` is the scene's membership of what Stash called movies before 0.28.
`SceneIndex` is its place within one, and is nil for a group that does not
order its scenes — nil rather than zero, because zero is a real index.

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

`Tier` classes a file by its pixel dimensions rather than by Stash's
`resolution` label, which libraries get wrong (720x404 labelled HD, square
cover art labelled 8K). Bands are on the longer side, so 4K is not 1080-tier
and a portrait file counts by its height:

```go
if f.Tier() == stash.Tier1080 { // 1920 <= longer side < 2560
    // a genuine Full HD copy, not a 4K one
}
t := stash.TierOf(3840, 2160) // stash.Tier4K
```

A scene has more than one file when Stash has attached re-detected duplicates
to it, which is the case deduplication tools care about — see
[Deduplication, deletion and files](#deduplication-deletion-and-files).

`ParseFilename` reads date, title and performers out of a basename that
follows the convention `YYYY-MM-DD_Performers-Title_Resolution.ext` — Stash's
own scan-time parser doesn't handle multiple performers or a dashed title.
It reports `false`, not an error, for anything else:

```go
pf, ok := stash.ParseFilename("2024-12-15_Some.Performer-A.Long.Title_1080p.mp4")
// pf.Date == "2024-12-15", pf.Title == "A Long Title", pf.Performers == []string{"Some Performer"}
```

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

To mirror incrementally, keep the newest `updated_at` you have seen and ask
only for what changed since:

```go
changed, _, err := c.FindScenes(ctx, stash.SceneFilter{UpdatedAfter: since}, 1, 100)
```

`UpdatedAfter` is exclusive and takes RFC 3339 or Stash's own
`"2006-01-02 15:04:05"`.

To find scenes lacking stash-box metadata:

```go
no := false
scenes, _, err := c.FindScenes(ctx, stash.SceneFilter{HasStashID: &no}, 1, 100)
```

### Scenes with more than one file

```go
multi := true
scenes, total, err := c.FindScenes(ctx, stash.SceneFilter{MultiFile: &multi}, 1, 100)
```

Stash attaches a re-detected file to the scene that already holds its hash
rather than making a second scene, so these are the duplicates that never
became duplicates — invisible to `FindDuplicateScenes`, which compares scenes
to each other. `false` selects the ordinary single-file scenes; leaving it nil
asks about neither.

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

A backup is only worth having if it is intact, and the two ways a downloaded
one arrives broken are both silent. A proxy or login page answering the
download URL writes HTML into a file called `local.sqlite`; a connection
dropped mid-transfer writes a prefix of the database, which opens fine and is
missing the end of the library. `DownloadVerifiedBackup` catches both from the
SQLite header alone — the magic string, and a page count the file's length has
to agree with — and only then gives the file the server's name. It refuses
`IncludeBlobs`: a server that keeps blobs on disk answers that request with a
zip of database and blobs, which is not a SQLite file and cannot be checked.

```go
m, err := c.DownloadVerifiedBackup(ctx, stash.BackupOptions{}, "backups")
if errors.Is(err, stash.ErrNotSQLite) || errors.Is(err, stash.ErrTruncatedBackup) {
	// nothing was kept; the .part file is gone
}
// backups/local.sqlite.85.20260101_000000
// backups/local.sqlite.85.20260101_000000.manifest.json
fmt.Println(m.File, m.Bytes, m.SHA256, m.Server.Version, m.Server.SceneCount)
```

The manifest beside the file records its size and SHA-256 and which server it
came from — version, schema, OS, database path, scene count — because a file
called `local.sqlite` says nothing about which Stash it belongs to. A server
that is not `Ready()` is refused before anything is downloaded. `VerifySQLite`
is the check on its own, for a file you already have.

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

## Studios and tags

The same shape as performers: a full record from the dedicated queries, a
partial update that only sends what you set, a separate call to empty a field,
and a merge for the duplicates every library grows.

```go
studio, found, err := c.FindStudioByID(ctx, id)
err = c.UpdateStudio(ctx, stash.StudioInput{ID: id, Details: "corrected"})
err = c.ClearStudioFields(ctx, id, "aliases")

tag, found, err := c.FindTagByID(ctx, id)
err = c.MergeTags(ctx, keepID, []string{foldedAwayID}, nil)
```

A studio or tag reached **through a scene** carries only `ID` and `Name`: the
shared scene selection asks for no more, because a page of scenes should not
drag a full record along for each one. The queries above fill the rest in.

`Parents` and `Children` on a tag, and `ParentStudio` on a studio, are one
level deep. A hierarchy queried in full would carry the whole tree on every
member of it.

`MergeTags` moves everything the sources were on to the destination and
deletes them. The fourth argument is applied to the destination as part of the
merge — the place to keep a source's better name, since afterwards there is
nothing to copy from. Not reversible.

`Aliases`, `URLs`, `TagIDs`, `ParentIDs` and `ChildIDs` **replace** what is
stored rather than adding to it. Read first and send the union if adding is
what you meant.

## Tasks

Every task is a background job and returns an id for `FindJob`.

```go
job, err := c.MetadataGenerate(ctx, stash.GenerateOptions{
    Sprites: true, Phashes: true, SceneIDs: []string{"36"},
})
```

`MetadataGenerate` produces what a scan does not: covers, sprites and
perceptual hashes. Those three are what other work depends on — a scene with
no sprite cannot be read for a title card, and one with no phash cannot be
matched against a stash-box.

**Every flag defaults to off**, the same as `ScanOptions`, because generating
across a library is hours of work and gigabytes of output. `SceneIDs` and
`Paths` are omitted when empty rather than sent as empty lists: Stash reads an
absent scope as "the whole library", so the two are different requests.

Without `Overwrite`, Stash skips what it already has, which is what makes a
second run cheap.

### The ones that write

Three of these change data rather than producing files, and each has a way of
being worse than it looks:

- **`MetadataIdentify`** matches scenes against a stash-box and applies what it
  finds. Whether it overwrites existing fields is decided by rules configured
  on the server, which this call cannot see.
- **`MetadataClean`** removes the records of files that are no longer on disk.
  An unmounted drive presents as a library whose files were all deleted. Use
  `DryRun` first.
- **`MetadataAutoTag`** attaches performers, studios and tags to scenes whose
  *path* contains their name. That is a guess about filenames; on a library
  with a performer called "Angel" it is a bad one. `"*"` means all of a kind,
  which is what the UI's button sends, and a call with no lists at all is
  refused rather than started as a job that does nothing.

### Stopping

```go
err := c.StopJob(ctx, job)
err := c.StopAllJobs(ctx)
```

Both return without waiting. The job moves to `STOPPING` and then to
`CANCELLED`, which `JobStatus.Done()` counts as terminal — treating it as
still-running is how a poll loop becomes a hang.

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

### Clearing a list

`SceneUpdate` omits empty fields, which is what makes a partial update safe —
an unset field leaves the stored value alone. The cost is that it cannot clear
a list: an empty `StashIDs` is indistinguishable from "do not touch the stash
ids", so removing the last one silently does nothing.

```go
err := c.SetSceneStashIDs(ctx, sceneID, nil)   // actually clears them
```

## Deduplication, deletion and files

This is the one part of the API that destroys things. The calls are shaped so
that the destructive choice has to be typed out, and nothing here is
reversible.

### Finding the duplicate

```go
scene, found, err := c.FindSceneByHash(ctx, "oshash", hash)
```

An exact lookup: `oshash` (or `md5`/`checksum`) names one file, so one scene.
`phash` is rejected — it is a similarity hash, and this query cannot do
similarity.

Similarity is the other call:

```go
groups, err := c.FindDuplicateScenes(ctx, 4, 1.0)
```

The server groups scenes by perceptual hash and hands back each group whole.
`distance` is how far two hashes may differ: 0 demands identical ones, 4 finds
re-encodes and resolution changes, and past 8 the matches stop being worth
reviewing.

`durationDiff` is the argument that decides whether the result is usable. It
bounds how far apart two runtimes may be, in seconds, and it is the strong
half of the test — phashes collide across unrelated videos often enough to
matter, but a collision that also agrees on length rarely does. Leave it out
and nothing fails; you simply get a much noisier answer. Pass a negative value
to mean it deliberately.

When the question is about paths rather than hashes:

```go
scenes, total, err := c.FindScenesByPathRegex(ctx, `S\d\dE\d\d`, 1, 100)
```

The server evaluates the pattern, in Go's regexp syntax, against the whole
path. `SceneFilter.PathContains` covers a plain substring; this is for
everything a substring cannot ask.

### Merging

```go
err := c.MergeScenes(ctx, keepID, []string{foldedAwayID}, &stash.SceneUpdate{
    Title: &betterTitle,
}, stash.MergeOptions{PlayHistory: true, OHistory: true})
```

The sources' files move to the destination and the source scenes are deleted.
No video is touched on disk. The destination's own metadata wins, so the
fourth argument is where a source's better title or date goes — afterwards
there is nothing left to copy from. The `ID` on that update is overwritten
with the destination's, because an update aimed anywhere else would write onto
a scene the merge is about to delete.

A source that is also the destination is refused rather than passed on: Stash
would fold the scene into itself and delete it.

Stash does not union the rest either: a tag or performer only a source had is
gone with the source unless the values put it on the destination. `Union`
computes those values from the scenes themselves, per field, and reports the
stash IDs it had to drop because two scenes claimed different remote entries
on the same stash-box:

```go
values, conflicts := stash.Union(keep, sources, stash.DefaultUnionPolicy())
for _, c := range conflicts {
    log.Printf("%s: kept %s, dropped %s", c.Endpoint, c.Kept, c.Dropped)
}
err := c.MergeScenes(ctx, keep.ID, ids, &values, stash.MergeOptions{})
```

The default policy keeps the destination's scalars unless they are empty,
unions the lists, takes the highest rating and marks the result organized if
any copy was. Each field's `FieldPolicy` can be set otherwise; the zero policy
touches nothing. The update comes back partial, and zero when there is nothing
to carry over.

`MergeOptions` is what the merge carries besides the files. Both fields
default to false, matching Stash's own default, which discards the sources'
watch history. When the scenes really are the same content that is the wrong
default: the times it was watched belong to the copy being kept.

### Deleting

```go
err := c.DeleteScene(ctx, id, stash.DeleteOptions{})
err := c.DeleteScenes(ctx, ids, stash.DeleteOptions{DeleteFile: true})
```

A zero `DeleteOptions` removes the database record and leaves the video where
it is — the recoverable choice, since the next scan finds it again. The three
fields each go further:

| Field | What it does |
| --- | --- |
| `DeleteFile` | Deletes the video from disk. No undo, no wastebasket. |
| `DeleteGenerated` | Removes sprites, previews and covers. Regenerable from the video. |
| `DestroyFileEntry` | Forgets the file record too, so a rescan re-adds it. |

`DestroyFileEntry` is the subtle one. Without it Stash remembers the file and
will *not* re-add it on the next scan — right when deleting a duplicate whose
video is still on disk under another scene, wrong when the file is gone and
you may restore it later.

### Moving and renaming

```go
err := c.MoveFiles(ctx, fileIDs, stash.MoveTarget{FolderID: "12"})
err := c.MoveFiles(ctx, []string{fileID}, stash.MoveTarget{Basename: "better name.mp4"})
```

A real move on the filesystem, with Stash's records updated to match. The
destination has to be inside a configured library path or Stash refuses.
`Basename` with more than one file is refused here, because it would give them
all the same name.

### Reattaching a file

```go
err := c.AssignFile(ctx, sceneID, fileID)
```

Moves a file to another scene. This is how a file Stash matched to the wrong
scene is put right without deleting anything.

```go
err := c.SetPrimaryFile(ctx, sceneID, fileID)
```

Chooses which of a scene's own files it streams, and whose resolution and
codec it reports as its own. Not the same call: `AssignFile` moves a file
between scenes, this reorders the files one scene already has, and the file
must already belong to it.

A merge is the usual reason to reach for it. Afterwards the destination holds
every file the sources brought, in no particular order — naming the best one
before destroying the rest is what stops a 4K scene from reporting itself as
the 540p copy that happened to sort first.

### Files directly

```go
file, found, err := c.FindFile(ctx, fileID)
file, found, err := c.FindFileByPath(ctx, `Z:\library\a.mp4`)
```

The path must be exactly as Stash stored it, separators included — on a
Windows server, backslashes. A path Stash does not know comes back as
`found == false`; Stash reports it as a GraphQL error, because `findFile` is
declared non-null, and that one error is translated back into absence.

```go
err := c.SetFingerprints(ctx, fileID, fingerprints)
```

Replaces, not merges: a hash the file had and this call omits is dropped. Read
the file first and append if adding one is what you meant.

### Deleting files

Stash has two mutations here and they are not synonyms. The names suggest the
opposite of what they do, so both are wrapped under names that match Stash's
own, and the difference is spelled out rather than left to the reader:

```go
err := c.DeleteFiles(ctx, fileIDs...)    // deletes the videos from disk
err := c.DestroyFiles(ctx, fileIDs...)   // forgets the records, keeps the videos
```

`DeleteFiles` is permanent. `DestroyFiles` is the reversible one — Stash's own
description is "deletes file entries from the database without deleting the
files from the filesystem", so the next scan finds those files again and
re-adds them. Reach for it when you want Stash to forget a file you are about
to move yourself, and expect it to come back otherwise.

`DeleteScene` without `DeleteFile` is the equivalent at the scene level.

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

## Administering the server

### How big is the library

```go
stats, err := c.LibraryStats(ctx)
```

The server's own tallies, answered from the database in one query — sizing a
library this way costs one round trip where counting the same thing through
`FindScenes` pages through every scene.

`ScenesSize` is bytes and `ScenesDuration` seconds, both `float64` because
that is what the schema declares and a large library overflows the precision
an int would give them. The counts cover what Stash has indexed, which is not
what is on disk: a file the last scan never reached is not in them.

### What state is it in

`Ping` succeeds against a server that is showing its setup wizard or refusing
to open a database it is too new for — answering "I am not ready" is a
successful answer. `SystemStatus` is how the two are told apart:

```go
st, err := c.SystemStatus(ctx)
if !st.Ready() {
    // st.Status is SystemSetup or SystemNeedsMigration
}
```

`AppSchema` being ahead of `DatabaseSchema` *is* the `NEEDS_MIGRATION`
condition. `DatabaseSchema` is nil on a server with no database yet, which is
what `SETUP` means — nil rather than zero, because zero is a real schema
version.

The struct carries the fields every supported server has. Stash has added
others since — the operating system, the working and home directories, the
resolved ffmpeg and ffprobe paths — and naming one here would fail the whole
query against a server without it. `Execute` reaches those.

`ServerVersion` is `Version` with the build hash and time attached; a binary
built from source outside a release has an empty version and a hash that
identifies it. `LatestVersion` asks the *server* to fetch the newest release
from GitHub, so it fails when the server has no route out — which is not the
same thing as this program having none.

### Logs

```go
entries, err := c.Logs(ctx)   // newest first
```

This is not the log file. Stash keeps a bounded in-memory ring of the last few
hundred entries and serves that, so a server restarted since the event has
nothing to say about it and a busy one has already dropped it. For anything
that must not be missed, read the file `logFile` names. There is no way to
follow the log from here: new entries arrive over a GraphQL subscription,
which is a websocket this package does not open.

### General settings

The same read-what-you-name shape as the interface settings, for the section
behind Settings > System:

```go
cfg, err := c.GeneralConfig(ctx, "databasePath", "blobsPath", "logFile")
err = c.ConfigureGeneral(ctx, map[string]any{"logLevel": "Debug"})
```

Two things here bite harder than they do elsewhere. A **list-valued field is
replaced, not extended** — sending one entry of `stashes` makes it the only
library path Stash has, and `SetStashBoxes` exists because the same trap costs
API keys. And several of these fields are how the server reaches its own data:
point `databasePath` or `generatedPath` somewhere new and Stash starts afresh
there rather than moving anything.

`StashBoxConfigs` is the way to read `stashBoxes`; it is the one field in the
section that carries credentials.

### The API key

```go
key, err := c.GenerateAPIKey(ctx)
c = stash.NewClient(url, stash.WithAPIKey(key))
```

There is one key per Stash, so generating a new one invalidates the old — the
one this client is authenticating with included, which stops working the
moment the mutation returns. The new key does not apply itself. `ClearAPIKey`
removes it without issuing another.

The returned key is a credential in a variable, and nothing redacts it for
you: the scrubbing applies to the key a client was *built* with, not to one it
has just been handed.

### Migrations and maintenance

`Migrate` is the one a `NEEDS_MIGRATION` server is waiting for. It is
irreversible — a migrated database cannot be opened by the Stash that wrote it
— which is what `backupPath` is for, and it runs synchronously rather than as
a job, so the default 30s HTTP timeout gives up long before a large library
finishes while the server carries on regardless.

```go
c := stash.NewClient(url, stash.WithAPIKey(key), stash.WithHTTPClient(&http.Client{}))
err := c.Migrate(ctx, `/root/.stash/pre-migration.sqlite`)
```

The rest are background jobs, returning an id for `FindJob`:

| call | what it is for |
|---|---|
| `MigrateBlobs` | move blob data between the database and the filesystem after `blobsPath` changes |
| `MigrateHashNaming` | rename generated files from MD5 to oshash naming |
| `MigrateSceneScreenshots` | read an old Stash's loose screenshot files in as scene covers |
| `DownloadFFMpeg` | have the server fetch its own ffmpeg and ffprobe |
| `OptimiseDatabase` | vacuum and reindex |

`MigrateBlobs` takes `deleteOld`. Leaving it false writes the data to its new
home without removing it from the old one, which is the undoable way round.

`AnonymiseDatabase` writes a copy with every name, path, URL and free-text
field stripped — the thing a bug report attaches. It keeps the shape of a
library and none of what it is a library of, and it is a copy: nothing about
the live database changes. `DownloadAnonymisedDatabase` streams it here
instead of leaving it on the server, with the same transfer caveats as
`DownloadBackup`.

### DLNA

```go
st, err := c.DLNAStatus(ctx)
err = c.EnableDLNA(ctx, 2*time.Hour)          // 0 means until disabled
err = c.AllowDLNAIP(ctx, "192.168.1.20", 0)   // 0 means until the server restarts
```

None of this touches the configuration: a temporary enable is forgotten on
restart, when `dlnaEnabled` decides again, and the grants sit on top of the
configured whitelist rather than joining it. `DisallowDLNAIP` revokes one it
made; it can say nothing about a configured address.

`Status.Until` reads in whichever direction `Running` points — while running
it is when the service stops, while stopped it is when it starts.

Stash counts durations in whole minutes, and an *absent* duration means "no
expiry". A duration under a minute is therefore refused rather than truncated
to zero, which would silently mean forever.

`RecentIPAddresses` is where the address for `AllowDLNAIP` comes from: a
device that has just tried and been refused is identified by having tried.

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

### Reading, changing and removing them

```go
p, found, err := c.FindPerformerByID(ctx, id)
list, count, err := c.FindPerformers(ctx,
    stash.PerformerFilter{Gender: "FEMALE", HasScenes: &no}, 1, 100)
```

A performer reached through a scene carries only `ID` and `Name` — the shared
scene selection asks for nothing more, because a page of scenes would
otherwise drag a full record along for every credit. These two fill the rest
in.

`UpdatePerformer` is partial in the same way `UpdateScene` is: only the fields
you set are sent.

```go
details := "Corrected."
err := c.UpdatePerformer(ctx, stash.PerformerUpdate{ID: id, Details: &details})
```

The pointers are what separate "set this to zero" from "leave it alone" — a
`Rating100` of 0 and a `Favorite` of false are real values, not absences. And
as with scenes, a partial update cannot *empty* a field, because empty and
absent look identical on the wire:

```go
err := c.ClearPerformerFields(ctx, id, "birthdate", "alias_list")
```

`Aliases`, `URLs`, `TagIDs` and `StashIDs` on an update **replace** what is
stored rather than adding to it. Read the performer first and send the union
if adding is what you meant.

### Deleting and merging

```go
err := c.DeletePerformer(ctx, id)
err := c.DeletePerformers(ctx, ids...)
err := c.MergePerformers(ctx, keepID, []string{foldedAwayID}, nil)
```

`DeletePerformers` is all or nothing: Stash checks every id first, and one
that does not exist fails the call with nothing deleted. That bites after a
merge, which has already removed its sources.

`MergePerformers` moves the sources' scenes to the destination and deletes
them. The destination's own fields win, so the fourth argument is where a
source's better name or birthdate goes — after the merge the sources are gone
and there is nothing to copy from. None of this is reversible.

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
found, err := c.ScrapeScenes(ctx, endpoint, "CODE1234")   // by text
```

`ScrapeSceneByID` matches on the file's own fingerprints, which is exact.
`ScrapeScenes` matches on whatever the stash-box searches — titles, in
practice — so it returns the studio's *other* scenes when it does not have the
one asked for. One result is not the same as the right result: check something
about it, ideally something the stash-box did not supply, before believing it.

An empty result is the ordinary answer for a library the stash-box does not
cover, not a failure.

Identifying a whole library one scene at a time is the slow way round:

```go
batch := sceneIDs[:20]
candidates, err := c.ScrapeMultiScenes(ctx, endpoint, batch)
for i, forScene := range candidates {
    // forScene is what the stash-box offered for batch[i]; empty means no match.
}
```

The result is parallel to the ids given — one entry per scene, in order, empty
where the stash-box recognised nothing — so the two slices can be walked
together. Stash queries the stash-box once per scene either way and paces
itself by its own `max_requests_per_minute`, so the batch size is a question
of how much work a failed request loses, not of politeness. Twenty or so is
comfortable.

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

## Which Stash this targets

**This targets Stash 0.31.1, schema 85** — the version the live suite runs
against, and the only one anything has been checked on. Older servers are not
supported: the selection sets name fields directly, and a field the schema
lacks fails the whole query rather than itself.

The wrapped calls do not probe for anything — they name what 0.31.1 has.
`Supports` is for the other direction: a query of your own, through `Execute`,
against a field you are not sure of.

```go
if ok, err := c.Supports(ctx, "groups"); err == nil && ok {
    // safe to name it in your own query
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
query := `query($q: String) { sceneWall(q: $q) { ` + stash.SceneFields + ` } }`
data, err := c.Execute(ctx, query, map[string]any{"q": "beach"})

var result struct {
    Scenes []stash.Scene `json:"sceneWall"`
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
