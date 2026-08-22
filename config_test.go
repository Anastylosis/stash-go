package stash

import (
	"context"
	"testing"
)

func TestPluginSettingsDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"configuration":{"plugins":{
	  "moansubs":{"server_url":"https://moansubs.org","exact_mode":true,"hide_push_button":null},
	  "other":{"x":1}}}}}`))

	got, err := c.PluginSettings(context.Background(), "moansubs")
	if err != nil {
		t.Fatalf("PluginSettings: %v", err)
	}
	if got["server_url"] != "https://moansubs.org" {
		t.Errorf("server_url = %v", got["server_url"])
	}
	if got["exact_mode"] != true {
		t.Errorf("exact_mode = %v, want true", got["exact_mode"])
	}
	// A boolean the user never touched comes back as null, not false. A
	// caller asserting .(bool) gets false either way, but the key exists —
	// which is how "unset" is told from "absent".
	v, present := got["hide_push_button"]
	if !present || v != nil {
		t.Errorf("hide_push_button = %v (present=%v), want a present nil", v, present)
	}
}

// A plugin whose settings are all at their defaults is indistinguishable
// from one that is not installed, and both mean "nothing has been set" —
// so neither is an error.
func TestPluginSettingsUnknownPluginIsEmptyNotAnError(t *testing.T) {
	_, c := server(t, reply(`{"data":{"configuration":{"plugins":{"other":{"x":1}}}}}`))
	got, err := c.PluginSettings(context.Background(), "moansubs")
	if err != nil {
		t.Fatalf("PluginSettings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want an empty map", got)
	}
}

func TestPluginSettingsNoPluginsAtAll(t *testing.T) {
	_, c := server(t, reply(`{"data":{"configuration":{"plugins":null}}}`))
	got, err := c.PluginSettings(context.Background(), "moansubs")
	if err != nil {
		t.Fatalf("PluginSettings: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("got %v, want an empty map", got)
	}
}

func TestPluginSettingsSurfacesServerError(t *testing.T) {
	_, c := server(t, reply(`{"errors":[{"message":"not authorised"}]}`))
	if _, err := c.PluginSettings(context.Background(), "moansubs"); err == nil {
		t.Error("PluginSettings = nil error, want the server's rejection")
	}
}

func TestConfigurationDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"configuration":{"general":{"databasePath":"/db"},"plugins":{"a":{}}}}}`))
	got, err := c.Configuration(context.Background())
	if err != nil {
		t.Fatalf("Configuration: %v", err)
	}
	if _, ok := got["general"]; !ok {
		t.Errorf("configuration = %v, want a general section", got)
	}
}
