package stash

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
)

// MergeOptions says what a merge carries across besides the files.
//
// Both default to false, which is Stash's own default and discards the
// sources' watch history. A deduplicating tool usually wants both: the copy
// being kept is the same content, so the times it was watched belong to it.
type MergeOptions struct {
	// PlayHistory folds the sources' play timestamps into the
	// destination's.
	PlayHistory bool
	// OHistory folds the sources' o timestamps into the destination's.
	OHistory bool
}

// MergeScenes folds the source scenes into the destination and deletes them,
// moving their files across.
//
// The destination keeps its own metadata; values is applied to it as part of
// the merge, which is where a source's better title or date goes — afterwards
// the sources are gone and there is nothing left to copy from. Stash does not
// union metadata by itself, so values is the whole of what survives from a
// source: compute it before calling.
//
// This deletes database records, not files on disk: the sources' files are
// reattached to the destination. Deleting a file is [Client.DeleteScene] with
// DeleteFile set, or [Client.DestroyFiles].
//
// Not reversible.
func (c *Client) MergeScenes(ctx context.Context, destinationID string, sourceIDs []string, values *SceneUpdate, opts MergeOptions) error {
	if destinationID == "" {
		return fmt.Errorf("stash: merging scenes: no destination")
	}
	if len(sourceIDs) == 0 {
		return fmt.Errorf("stash: merging scenes: no sources")
	}
	for _, id := range sourceIDs {
		if id == destinationID {
			// Stash would fold the destination into itself and delete it.
			return fmt.Errorf("stash: merging scenes: %s is both source and destination", id)
		}
	}
	input := map[string]any{"source": sourceIDs, "destination": destinationID}
	if opts.PlayHistory {
		input["play_history"] = true
	}
	if opts.OHistory {
		input["o_history"] = true
	}
	if values != nil {
		v := *values
		v.ID = destinationID
		input["values"] = v
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: SceneMergeInput!) { sceneMerge(input: $input) { id } }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return fmt.Errorf("stash: merging %d scenes into %s: %w", len(sourceIDs), destinationID, err)
	}
	return nil
}

// DeleteOptions says how far a scene deletion reaches.
//
// Every field defaults to false, which removes only the scene record and
// leaves the video where it is. That is the recoverable choice: a scan finds
// the file again. The others are not.
type DeleteOptions struct {
	// DeleteFile removes the video from disk. There is no undo, and Stash
	// does not move it to a wastebasket.
	DeleteFile bool
	// DeleteGenerated removes the sprites, previews and covers Stash made
	// for it. Those can be regenerated from the video, so this is only
	// destructive alongside DeleteFile.
	DeleteGenerated bool
	// DestroyFileEntry removes the file's row as well as the scene's. Without
	// it Stash remembers the file and will not re-add it on the next scan,
	// which is what you want when deleting a duplicate whose video is still
	// on disk under another scene — and what you do not want when the file
	// is gone and you may restore it later.
	DestroyFileEntry bool
}

func (o DeleteOptions) fields() map[string]any {
	return map[string]any{
		"delete_file":        o.DeleteFile,
		"delete_generated":   o.DeleteGenerated,
		"destroy_file_entry": o.DestroyFileEntry,
	}
}

// DeleteScene removes one scene.
//
// With a zero [DeleteOptions] this removes the database record only and the
// video stays on disk. Set DeleteFile and the video is deleted, permanently.
func (c *Client) DeleteScene(ctx context.Context, id string, opts DeleteOptions) error {
	if id == "" {
		return fmt.Errorf("stash: deleting scene: no id")
	}
	input := opts.fields()
	input["id"] = id
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: SceneDestroyInput!) { sceneDestroy(input: $input) }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return fmt.Errorf("stash: deleting scene %s: %w", id, err)
	}
	return nil
}

// DeleteScenes removes several scenes in one request, on the same terms as
// [Client.DeleteScene].
func (c *Client) DeleteScenes(ctx context.Context, ids []string, opts DeleteOptions) error {
	if len(ids) == 0 {
		return nil
	}
	for _, id := range ids {
		if id == "" {
			return fmt.Errorf("stash: deleting scenes: an id is empty")
		}
	}
	input := opts.fields()
	input["ids"] = ids
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: ScenesDestroyInput!) { scenesDestroy(input: $input) }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return fmt.Errorf("stash: deleting %d scenes: %w", len(ids), err)
	}
	return nil
}

// SetPrimaryFile chooses which of a scene's files is the primary one — the
// file Stash streams, and the one whose resolution and codec the scene
// reports as its own.
//
// This is not [Client.AssignFile]: that moves a file between scenes, while
// this reorders the files a scene already has. A scene left with several
// files after a merge is the usual reason to call it, picking the best of
// them before the rest are destroyed.
//
// The file must already belong to the scene; Stash rejects the update
// otherwise.
func (c *Client) SetPrimaryFile(ctx context.Context, sceneID, fileID string) error {
	if sceneID == "" || fileID == "" {
		return fmt.Errorf("stash: setting the primary file: a scene id and a file id are both required")
	}
	if err := c.UpdateScene(ctx, SceneUpdate{ID: sceneID, PrimaryFileID: &fileID}); err != nil {
		return fmt.Errorf("stash: making file %s primary on scene %s: %w", fileID, sceneID, err)
	}
	return nil
}

// AssignFile attaches an existing file to a scene, moving it from whatever
// scene held it.
//
// This is how a file that Stash matched to the wrong scene is put right
// without deleting anything.
func (c *Client) AssignFile(ctx context.Context, sceneID, fileID string) error {
	if sceneID == "" || fileID == "" {
		return fmt.Errorf("stash: assigning a file: a scene id and a file id are both required")
	}
	_, err := c.do(ctx, graphqlRequest{
		Query: `mutation($input: AssignSceneFileInput!) { sceneAssignFile(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{
			"scene_id": sceneID, "file_id": fileID,
		}},
	})
	if err != nil {
		return fmt.Errorf("stash: assigning file %s to scene %s: %w", fileID, sceneID, err)
	}
	return nil
}

// MoveTarget says where [Client.MoveFiles] should put them: a folder Stash
// already knows by id, or a path, and optionally a new name.
//
// Renaming one file at a time is what Basename is for; moving several with a
// Basename set would give them all the same name, so it is refused.
type MoveTarget struct {
	FolderID string
	Folder   string
	Basename string
}

// MoveFiles moves files on disk and updates Stash to match.
//
// This is a real move: the video changes place in the filesystem. Stash needs
// the destination to be inside a configured library path, or it will refuse
// rather than move a file somewhere it cannot see.
func (c *Client) MoveFiles(ctx context.Context, fileIDs []string, to MoveTarget) error {
	if len(fileIDs) == 0 {
		return nil
	}
	if to.FolderID == "" && to.Folder == "" && to.Basename == "" {
		return fmt.Errorf("stash: moving files: no destination given")
	}
	if to.Basename != "" && len(fileIDs) > 1 {
		return fmt.Errorf("stash: moving files: a basename renames one file, and %d were given", len(fileIDs))
	}
	input := map[string]any{"ids": fileIDs}
	for key, v := range map[string]string{
		"destination_folder_id": to.FolderID,
		"destination_folder":    to.Folder,
		"destination_basename":  to.Basename,
	} {
		if v != "" {
			input[key] = v
		}
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($input: MoveFilesInput!) { moveFiles(input: $input) }`,
		Variables: map[string]any{"input": input},
	})
	if err != nil {
		return fmt.Errorf("stash: moving %d file(s): %w", len(fileIDs), err)
	}
	return nil
}

// DestroyFiles deletes files from disk and removes their records.
//
// Permanent, and it says so plainly because the name does not: this is not
// "forget about these files", it is "delete these videos". [Client.DeleteScene]
// without DeleteFile is the reversible option.
func (c *Client) DestroyFiles(ctx context.Context, fileIDs ...string) error {
	if len(fileIDs) == 0 {
		return nil
	}
	_, err := c.do(ctx, graphqlRequest{
		Query:     `mutation($ids: [ID!]!) { destroyFiles(ids: $ids) }`,
		Variables: map[string]any{"ids": fileIDs},
	})
	if err != nil {
		return fmt.Errorf("stash: destroying %d file(s): %w", len(fileIDs), err)
	}
	return nil
}

// FindSceneByHash finds the scene holding a file with this fingerprint.
//
// Exact, unlike matching on a title or a path: the hash names one file. Which
// algorithm to pass depends on what the library has — "oshash" is computed on
// every scan, "phash" only when generated.
func (c *Client) FindSceneByHash(ctx context.Context, algorithm, hash string) (scene *Scene, found bool, err error) {
	if algorithm == "" || hash == "" {
		return nil, false, fmt.Errorf("stash: finding scene by hash: an algorithm and a hash are both required")
	}
	// The input names one field per algorithm rather than taking the name as
	// a value, so the field is chosen here.
	field := map[string]string{"oshash": "oshash", "checksum": "checksum", "md5": "checksum"}[algorithm]
	if field == "" {
		return nil, false, fmt.Errorf("stash: finding scene by hash: %q is not oshash or checksum", algorithm)
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($input: SceneHashInput!) { findSceneByHash(input: $input) {` + SceneFields + `} }`,
		Variables: map[string]any{"input": map[string]any{
			field: hash,
		}},
	})
	if err != nil {
		return nil, false, fmt.Errorf("stash: finding scene by %s %s: %w", algorithm, hash, err)
	}
	var result struct {
		FindSceneByHash *Scene `json:"findSceneByHash"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("stash: decoding scene: %w", err)
	}
	if result.FindSceneByHash == nil {
		return nil, false, nil
	}
	return result.FindSceneByHash, true, nil
}

// FindScenesByPathRegex finds scenes whose file path matches a regular
// expression, one page at a time, and reports the total match count.
//
// The pattern is evaluated by the server, in Go's regexp syntax, against the
// full path. This is the call for questions a filter cannot ask — "which
// scenes still live under the old naming scheme" — where [SceneFilter]'s
// PathContains only does a substring.
func (c *Client) FindScenesByPathRegex(ctx context.Context, pattern string, page, perPage int) ([]Scene, int, error) {
	if pattern == "" {
		return nil, 0, fmt.Errorf("stash: finding scenes by path: no pattern")
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($filter: FindFilterType) { findScenesByPathRegex(filter: $filter) { count scenes {` + SceneFields + `} } }`,
		Variables: map[string]any{"filter": map[string]any{
			"q": pattern, "page": page, "per_page": perPage,
			"sort": "path", "direction": "ASC",
		}},
	})
	if err != nil {
		return nil, 0, fmt.Errorf("stash: finding scenes matching %q (page %d): %w", pattern, page, err)
	}
	var result struct {
		Found struct {
			Count  int     `json:"count"`
			Scenes []Scene `json:"scenes"`
		} `json:"findScenesByPathRegex"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, 0, fmt.Errorf("stash: decoding scenes: %w", err)
	}
	return result.Found.Scenes, result.Found.Count, nil
}

// FileFields is the selection set [File] decodes from, exported for the same
// reason as [SceneFields].
const FileFields = `
  id
  basename
  path
  size
  mod_time
  fingerprints { type value }
  ... on VideoFile {
    format
    width
    height
    duration
    video_codec
    audio_codec
    frame_rate
    bit_rate
  }`

// FindFileByPath looks up one file by its path on disk.
//
// The path must be exactly as Stash stored it, separators included — on a
// Windows server that means backslashes. found is false when no such file is
// known, which is not an error.
func (c *Client) FindFileByPath(ctx context.Context, path string) (file *File, found bool, err error) {
	if path == "" {
		return nil, false, fmt.Errorf("stash: finding file: no path")
	}
	return c.findFile(ctx, "path", path)
}

// FindFile looks up one file by id.
func (c *Client) FindFile(ctx context.Context, id string) (file *File, found bool, err error) {
	if id == "" {
		return nil, false, fmt.Errorf("stash: finding file: no id")
	}
	return c.findFile(ctx, "id", id)
}

func (c *Client) findFile(ctx context.Context, by, value string) (*File, bool, error) {
	argType := "ID"
	if by == "path" {
		argType = "String"
	}
	data, err := c.do(ctx, graphqlRequest{
		Query: `query($v: ` + argType + `) { findFile(` + by + `: $v) {` + FileFields + `} }`,
		Variables: map[string]any{
			"v": value,
		},
	})
	if err != nil {
		if isFileNotFound(err) {
			return nil, false, nil
		}
		return nil, false, fmt.Errorf("stash: finding file by %s %s: %w", by, value, err)
	}
	var result struct {
		FindFile *File `json:"findFile"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, false, fmt.Errorf("stash: decoding file: %w", err)
	}
	if result.FindFile == nil {
		return nil, false, nil
	}
	return result.FindFile, true, nil
}

// isFileNotFound reports the one error findFile raises for a file that simply
// is not there.
//
// Unlike findScene, findFile is declared non-null, so Stash cannot answer
// "no such file" with a null and raises an error instead. Matching the message
// is the only way to tell that apart from a real failure, so the match is kept
// exact: anything else, including a differently worded not-found from a future
// version, stays an error rather than being silently reported as absent.
func isFileNotFound(err error) bool {
	var apiErr *APIError
	if !errors.As(err, &apiErr) || len(apiErr.Errors) != 1 {
		return false
	}
	return apiErr.Errors[0].Message == "file not found"
}

// SetFingerprints replaces a file's fingerprints.
//
// Replaces, not merges: a hash the file had and this call omits is dropped.
// Read the file first and append if you mean to add one.
func (c *Client) SetFingerprints(ctx context.Context, fileID string, fingerprints []Fingerprint) error {
	if fileID == "" {
		return fmt.Errorf("stash: setting fingerprints: no file id")
	}
	list := make([]map[string]any, 0, len(fingerprints))
	for _, fp := range fingerprints {
		list = append(list, map[string]any{"type": fp.Type, "value": fp.Value})
	}
	_, err := c.do(ctx, graphqlRequest{
		Query: `mutation($input: FileSetFingerprintsInput!) { fileSetFingerprints(input: $input) }`,
		Variables: map[string]any{"input": map[string]any{
			"id": fileID, "fingerprints": list,
		}},
	})
	if err != nil {
		return fmt.Errorf("stash: setting fingerprints on file %s: %w", fileID, err)
	}
	return nil
}
