package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestMergeScenesRefusesSelfMerge(t *testing.T) {
	_, c := server(t, reply(`{"data":{"sceneMerge":{"id":"5"}}}`))

	err := c.MergeScenes(context.Background(), "5", []string{"6", "5"}, nil)
	if err == nil {
		t.Fatal("merging a scene into itself was allowed")
	}
	if !strings.Contains(err.Error(), "both source and destination") {
		t.Errorf("err = %v", err)
	}
}

func TestMergeScenesRefusesEmptyArguments(t *testing.T) {
	_, c := server(t, reply(`{"data":{"sceneMerge":{"id":"5"}}}`))

	if err := c.MergeScenes(context.Background(), "", []string{"6"}, nil); err == nil {
		t.Error("no destination was allowed")
	}
	if err := c.MergeScenes(context.Background(), "5", nil, nil); err == nil {
		t.Error("no sources was allowed")
	}
}

func TestMergeScenesAddressesValuesAtTheDestination(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"sceneMerge":{"id":"5"}}}`))
	defer srv.Close()

	title := "the surviving title"
	// The caller left values.ID empty, or wrong: an update aimed anywhere but
	// the destination would write onto a scene the merge is about to delete.
	err := NewClient(srv.URL).MergeScenes(context.Background(), "5", []string{"6"},
		&SceneUpdate{ID: "6", Title: &title})
	if err != nil {
		t.Fatalf("MergeScenes: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	values, _ := in["values"].(map[string]any)
	if values["id"] != "5" {
		t.Errorf("values.id = %v, want the destination", values["id"])
	}
	if values["title"] != title {
		t.Errorf("values.title = %v", values["title"])
	}
}

func TestDeleteSceneDefaultsToKeepingTheFile(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"sceneDestroy":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).DeleteScene(context.Background(), "5", DeleteOptions{}); err != nil {
		t.Fatalf("DeleteScene: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	// Sent explicitly rather than omitted: a caller reading the request should
	// see that nothing on disk is being touched.
	for _, key := range []string{"delete_file", "delete_generated", "destroy_file_entry"} {
		if in[key] != false {
			t.Errorf("%s = %v, want false", key, in[key])
		}
	}
	if in["id"] != "5" {
		t.Errorf("id = %v", in["id"])
	}
}

func TestDeleteScenesCarriesOptions(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"scenesDestroy":true}}`))
	defer srv.Close()

	err := NewClient(srv.URL).DeleteScenes(context.Background(), []string{"5", "6"},
		DeleteOptions{DeleteFile: true, DeleteGenerated: true})
	if err != nil {
		t.Fatalf("DeleteScenes: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	if in["delete_file"] != true || in["delete_generated"] != true || in["destroy_file_entry"] != false {
		t.Errorf("input = %v", in)
	}
	if ids, _ := in["ids"].([]any); len(ids) != 2 {
		t.Errorf("ids = %v", in["ids"])
	}
}

func TestDeleteScenesWithNoIDsSendsNothing(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"scenesDestroy":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).DeleteScenes(context.Background(), nil, DeleteOptions{DeleteFile: true}); err != nil {
		t.Fatalf("DeleteScenes: %v", err)
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests for an empty deletion", len(cap.reqs))
	}
}

func TestDeleteScenesRefusesAnEmptyID(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"scenesDestroy":true}}`))
	defer srv.Close()

	// An empty id in the list would delete whatever Stash resolves "" to,
	// alongside the ids that were meant.
	if err := NewClient(srv.URL).DeleteScenes(context.Background(), []string{"5", ""}, DeleteOptions{}); err == nil {
		t.Fatal("an empty id was allowed")
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests despite the bad id", len(cap.reqs))
	}
}

func TestMoveFilesRefusesRenamingSeveralAtOnce(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"moveFiles":true}}`))
	defer srv.Close()

	err := NewClient(srv.URL).MoveFiles(context.Background(), []string{"5", "6"},
		MoveTarget{Folder: "/library/kept", Basename: "one-name.mp4"})
	if err == nil {
		t.Fatal("renaming two files to one name was allowed")
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests", len(cap.reqs))
	}
}

func TestMoveFilesOmitsUnsetDestinationFields(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"moveFiles":true}}`))
	defer srv.Close()

	err := NewClient(srv.URL).MoveFiles(context.Background(), []string{"5"},
		MoveTarget{FolderID: "12"})
	if err != nil {
		t.Fatalf("MoveFiles: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	if in["destination_folder_id"] != "12" {
		t.Errorf("destination_folder_id = %v", in["destination_folder_id"])
	}
	// An empty destination_folder would be a move to the library root.
	if _, ok := in["destination_folder"]; ok {
		t.Error("sent an empty destination_folder")
	}
	if _, ok := in["destination_basename"]; ok {
		t.Error("sent an empty destination_basename")
	}
}

func TestMoveFilesNeedsADestination(t *testing.T) {
	_, c := server(t, reply(`{"data":{"moveFiles":true}}`))

	if err := c.MoveFiles(context.Background(), []string{"5"}, MoveTarget{}); err == nil {
		t.Fatal("a move with no destination was allowed")
	}
}

func TestDestroyFilesWithNoIDsSendsNothing(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"destroyFiles":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).DestroyFiles(context.Background()); err != nil {
		t.Fatalf("DestroyFiles: %v", err)
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests", len(cap.reqs))
	}
}

func TestAssignFileNeedsBothIDs(t *testing.T) {
	_, c := server(t, reply(`{"data":{"sceneAssignFile":true}}`))

	if err := c.AssignFile(context.Background(), "5", ""); err == nil {
		t.Error("no file id was allowed")
	}
	if err := c.AssignFile(context.Background(), "", "9"); err == nil {
		t.Error("no scene id was allowed")
	}
}

func TestFindSceneByHashPicksTheFieldForTheAlgorithm(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findSceneByHash":{"id":"5"}}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	for _, tc := range []struct{ algorithm, field string }{
		{"oshash", "oshash"},
		{"md5", "checksum"},
		{"checksum", "checksum"},
	} {
		if _, _, err := c.FindSceneByHash(context.Background(), tc.algorithm, "abc"); err != nil {
			t.Fatalf("FindSceneByHash(%s): %v", tc.algorithm, err)
		}
		in := inputOf(t, cap.reqs[len(cap.reqs)-1])
		if in[tc.field] != "abc" {
			t.Errorf("%s went to %v, want %s", tc.algorithm, in, tc.field)
		}
	}
}

func TestFindSceneByHashRejectsPhash(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findSceneByHash":null}}`))
	defer srv.Close()

	// phash is a similarity hash and this query is an exact lookup; accepting
	// it would put a name on a search that cannot be performed.
	_, _, err := NewClient(srv.URL).FindSceneByHash(context.Background(), "phash", "abc")
	if err == nil {
		t.Fatal("phash was accepted")
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests", len(cap.reqs))
	}
}

func TestFindSceneByHashReportsAbsence(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findSceneByHash":null}}`))

	scene, found, err := c.FindSceneByHash(context.Background(), "oshash", "abc")
	if err != nil {
		t.Fatalf("FindSceneByHash: %v", err)
	}
	if found || scene != nil {
		t.Errorf("found=%v scene=%v for a null result", found, scene)
	}
}

func TestFindFileTreatsNotFoundAsAbsence(t *testing.T) {
	// findFile is declared non-null, so Stash reports a missing file as an
	// error rather than a null.
	_, c := server(t, reply(`{"errors":[{"message":"file not found"}],"data":null}`))

	file, found, err := c.FindFileByPath(context.Background(), `Z:\gone.mp4`)
	if err != nil {
		t.Fatalf("FindFileByPath: %v", err)
	}
	if found || file != nil {
		t.Errorf("found=%v file=%v", found, file)
	}
}

func TestFindFileKeepsOtherErrors(t *testing.T) {
	_, c := server(t, reply(`{"errors":[{"message":"not authorized"}],"data":null}`))

	if _, _, err := c.FindFile(context.Background(), "5"); err == nil {
		t.Fatal("an unrelated error was reported as a missing file")
	}
}

func TestFindFileDecodesVideoFields(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findFile":{
		"id":"55","basename":"a.wmv","path":"Z:\\lib\\a.wmv","size":1293140719,
		"duration":2533.4,"width":1280,"height":720,"video_codec":"vc1",
		"fingerprints":[{"type":"oshash","value":"abc"},{"type":"phash","value":"def"}]}}}`))

	file, found, err := c.FindFile(context.Background(), "55")
	if err != nil || !found {
		t.Fatalf("FindFile: %v, found=%v", err, found)
	}
	if file.Size != 1293140719 || file.Width != 1280 {
		t.Errorf("file = %+v", file)
	}
	if hash, ok := file.Fingerprint("phash"); !ok || hash != "def" {
		t.Errorf("phash = %q, %v", hash, ok)
	}
}

func TestSetFingerprintsSendsAList(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"fileSetFingerprints":true}}`))
	defer srv.Close()

	err := NewClient(srv.URL).SetFingerprints(context.Background(), "55",
		[]Fingerprint{{Type: "oshash", Value: "abc"}})
	if err != nil {
		t.Fatalf("SetFingerprints: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	list, _ := in["fingerprints"].([]any)
	if len(list) != 1 {
		t.Fatalf("fingerprints = %v", in["fingerprints"])
	}
	first, _ := list[0].(map[string]any)
	if first["type"] != "oshash" || first["value"] != "abc" {
		t.Errorf("fingerprint = %v", first)
	}
}

func TestSetFingerprintsSendsAnEmptyListNotNull(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"fileSetFingerprints":true}}`))
	defer srv.Close()

	// Clearing every fingerprint is a legitimate ask; a null would fail the
	// non-null input instead of doing it.
	if err := NewClient(srv.URL).SetFingerprints(context.Background(), "55", nil); err != nil {
		t.Fatalf("SetFingerprints: %v", err)
	}
	in := inputOf(t, cap.reqs[0])
	list, ok := in["fingerprints"].([]any)
	if !ok || len(list) != 0 {
		t.Errorf("fingerprints = %v", in["fingerprints"])
	}
}

func TestFindScenesByPathRegexPassesThePatternAsQ(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"findScenesByPathRegex":{"count":66,"scenes":[{"id":"55"}]}}}`))
	defer srv.Close()

	scenes, total, err := NewClient(srv.URL).FindScenesByPathRegex(context.Background(), `S\d\dE\d\d`, 1, 3)
	if err != nil {
		t.Fatalf("FindScenesByPathRegex: %v", err)
	}
	if total != 66 || len(scenes) != 1 {
		t.Errorf("total=%d scenes=%d", total, len(scenes))
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["filter"])
	var filter map[string]any
	_ = json.Unmarshal(b, &filter)
	if filter["q"] != `S\d\dE\d\d` {
		t.Errorf("q = %v", filter["q"])
	}
	if filter["per_page"] != float64(3) {
		t.Errorf("per_page = %v", filter["per_page"])
	}
}

func TestFindScenesByPathRegexNeedsAPattern(t *testing.T) {
	_, c := server(t, reply(`{"data":{"findScenesByPathRegex":{"count":0,"scenes":[]}}}`))

	if _, _, err := c.FindScenesByPathRegex(context.Background(), "", 1, 10); err == nil {
		t.Fatal("an empty pattern was allowed")
	}
}
