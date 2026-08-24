package stash

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
)

// A server that has not been set up yet has no database, so its schema
// version is null rather than zero — and zero is a real schema version.
func TestSystemStatusSetupHasNoDatabaseSchema(t *testing.T) {
	_, c := server(t, reply(`{"data":{"systemStatus":{"status":"SETUP","databaseSchema":null,
		"appSchema":85,"databasePath":null,"configPath":null}}}`))

	got, err := c.SystemStatus(context.Background())
	if err != nil {
		t.Fatalf("SystemStatus: %v", err)
	}
	if got.DatabaseSchema != nil {
		t.Errorf("DatabaseSchema = %v, want nil", *got.DatabaseSchema)
	}
	if got.Ready() {
		t.Error("Ready() = true for a server showing its setup wizard")
	}
}

// Ping succeeds against a server awaiting migration — answering
// NEEDS_MIGRATION is a successful answer. This is what tells them apart.
func TestSystemStatusNeedsMigrationIsNotReady(t *testing.T) {
	_, c := server(t, reply(`{"data":{"systemStatus":{"status":"NEEDS_MIGRATION","databaseSchema":74,
		"appSchema":85,"databasePath":"/root/.stash/stash-go.sqlite","configPath":"/root/.stash/config.yml"}}}`))

	got, err := c.SystemStatus(context.Background())
	if err != nil {
		t.Fatalf("SystemStatus: %v", err)
	}
	if got.Ready() {
		t.Error("Ready() = true with the database behind the binary")
	}
	if got.DatabaseSchema == nil || *got.DatabaseSchema != 74 || got.AppSchema != 85 {
		t.Errorf("schema = %v/%d, want 74/85", got.DatabaseSchema, got.AppSchema)
	}
	if got.DatabasePath != "/root/.stash/stash-go.sqlite" {
		t.Errorf("DatabasePath = %q", got.DatabasePath)
	}
}

func TestSystemStatusOKIsReady(t *testing.T) {
	_, c := server(t, reply(`{"data":{"systemStatus":{"status":"OK","databaseSchema":85,"appSchema":85}}}`))
	got, err := c.SystemStatus(context.Background())
	if err != nil {
		t.Fatalf("SystemStatus: %v", err)
	}
	if !got.Ready() || got.Status != SystemOK {
		t.Errorf("status = %q, Ready = %v", got.Status, got.Ready())
	}
}

// A binary built from source outside a release has no tag, and the hash is
// then the only thing identifying it.
func TestServerVersionKeepsTheHashWhenThereIsNoTag(t *testing.T) {
	_, c := server(t, reply(`{"data":{"version":{"version":"","hash":"a1b2c3d","build_time":"2026-01-01 00:00:00"}}}`))
	got, err := c.ServerVersion(context.Background())
	if err != nil {
		t.Fatalf("ServerVersion: %v", err)
	}
	if got.Hash != "a1b2c3d" || got.BuildTime != "2026-01-01 00:00:00" {
		t.Errorf("version = %+v", got)
	}
}

func TestLatestVersion(t *testing.T) {
	_, c := server(t, reply(`{"data":{"latestversion":{"shorthash":"deadbee","url":"https://example.test/stash"}}}`))
	hash, url, err := c.LatestVersion(context.Background())
	if err != nil {
		t.Fatalf("LatestVersion: %v", err)
	}
	if hash != "deadbee" || url != "https://example.test/stash" {
		t.Errorf("got (%q, %q)", hash, url)
	}
}

// Newest first is the order Stash keeps its ring in, and a caller reading
// "the last thing that happened" depends on it not being reversed here.
func TestLogsKeepTheServersOrder(t *testing.T) {
	_, c := server(t, reply(`{"data":{"logs":[
		{"time":"2026-01-01T00:00:02Z","level":"Error","message":"second"},
		{"time":"2026-01-01T00:00:01Z","level":"Info","message":"first"}]}}`))

	got, err := c.Logs(context.Background())
	if err != nil {
		t.Fatalf("Logs: %v", err)
	}
	if len(got) != 2 || got[0].Message != "second" || got[0].Level != "Error" {
		t.Errorf("logs = %+v", got)
	}
}

func TestGeneralConfigAsksForWhatItWasGiven(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"configuration":{"general":{"databasePath":"/db","logFile":"/log"}}}}`))
	defer srv.Close()

	got, err := NewClient(srv.URL).GeneralConfig(context.Background(), "databasePath", "logFile")
	if err != nil {
		t.Fatalf("GeneralConfig: %v", err)
	}
	if got["logFile"] != "/log" {
		t.Errorf("logFile = %v", got["logFile"])
	}
	if q := cap.reqs[0].Query; !strings.Contains(q, "general { databasePath logFile }") {
		t.Errorf("query = %q", q)
	}
}

// The fields are spliced into the query, so anything that is not a field
// name has to be refused before it gets there.
func TestGeneralConfigRefusesAnythingButFieldNames(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	for _, bad := range []string{"", "a b", "x { y }", "logFile)", "2fast"} {
		if _, err := c.GeneralConfig(context.Background(), bad); err == nil {
			t.Errorf("GeneralConfig(%q) = nil error, want a refusal", bad)
		}
	}
	if _, err := c.GeneralConfig(context.Background()); err == nil {
		t.Error("GeneralConfig() with no fields = nil error, want a refusal")
	}
}

// Only the keys given are sent: everything else is left as the server has it.
func TestConfigureGeneralSendsOnlyWhatItWasGiven(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"configureGeneral":{"__typename":"ConfigResult"}}}`))
	defer srv.Close()

	err := NewClient(srv.URL).ConfigureGeneral(context.Background(), map[string]any{"logLevel": "Debug"})
	if err != nil {
		t.Fatalf("ConfigureGeneral: %v", err)
	}
	in := sentInput(t, cap.reqs[0])
	if len(in) != 1 || in["logLevel"] != "Debug" {
		t.Errorf("input = %v, want only logLevel", in)
	}
}

func TestConfigureGeneralWithNothingMakesNoRequest(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).ConfigureGeneral(context.Background(), nil); err != nil {
		t.Fatalf("ConfigureGeneral: %v", err)
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests for an empty change", len(cap.reqs))
	}
}

func TestGenerateAPIKeyReturnsTheNewKey(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"generateAPIKey":"eyJhbGciOi.new.key"}}`))
	defer srv.Close()

	key, err := NewClient(srv.URL, WithAPIKey("old")).GenerateAPIKey(context.Background())
	if err != nil {
		t.Fatalf("GenerateAPIKey: %v", err)
	}
	if key != "eyJhbGciOi.new.key" {
		t.Errorf("key = %q", key)
	}
	if got := sentInput(t, cap.reqs[0])["clear"]; got != false {
		t.Errorf("clear = %v, want false", got)
	}
}

func TestClearAPIKeySaysSo(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"generateAPIKey":""}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).ClearAPIKey(context.Background()); err != nil {
		t.Fatalf("ClearAPIKey: %v", err)
	}
	if got := sentInput(t, cap.reqs[0])["clear"]; got != true {
		t.Errorf("clear = %v, want true", got)
	}
}

func TestLibraryStatsDecodesCounts(t *testing.T) {
	_, c := server(t, reply(`{"data":{"stats":{
		"scene_count":61218,"scenes_size":7.3812687965143e+13,"scenes_duration":9.274805869e+07,
		"image_count":0,"images_size":0,"gallery_count":0,
		"performer_count":4307,"studio_count":1933,"group_count":0,"tag_count":4853,
		"total_o_count":2,"total_play_count":1073,"total_play_duration":77306.408,"scenes_played":1019
	}}}`))

	got, err := c.LibraryStats(context.Background())
	if err != nil {
		t.Fatalf("LibraryStats: %v", err)
	}
	if got.SceneCount != 61218 {
		t.Errorf("SceneCount = %d, want 61218", got.SceneCount)
	}
	// Sizes and durations are Float in the schema and big enough to arrive
	// in exponent notation — decoding either into an int would truncate.
	if got.ScenesSize != 7.3812687965143e+13 {
		t.Errorf("ScenesSize = %v", got.ScenesSize)
	}
	if got.ScenesDuration != 9.274805869e+07 {
		t.Errorf("ScenesDuration = %v", got.ScenesDuration)
	}
	if got.PerformerCount != 4307 || got.StudioCount != 1933 || got.TagCount != 4853 {
		t.Errorf("entity counts = %+v", got)
	}
	if got.TotalPlayDuration != 77306.408 || got.ScenesPlayed != 1019 {
		t.Errorf("playback totals = %+v", got)
	}
}
