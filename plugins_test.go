package stash

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestPluginsDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"plugins":[
		{"id":"example-plugin","name":"Example Plugin","description":"An example plugin","url":"https://example.test","version":"1.2.3","enabled":true},
		{"id":"other","name":"Other","enabled":false}]}}`))

	got, err := c.Plugins(context.Background())
	if err != nil {
		t.Fatalf("Plugins: %v", err)
	}
	if len(got) != 2 || got[0].ID != "example-plugin" || !got[0].Enabled || got[1].Enabled {
		t.Errorf("plugins = %+v", got)
	}
}

func TestSetPluginsEnabledSendsTheMap(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"setPluginsEnabled":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).SetPluginsEnabled(context.Background(), map[string]bool{"example-plugin": true}); err != nil {
		t.Fatalf("SetPluginsEnabled: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["m"])
	if string(b) != `{"example-plugin":true}` {
		t.Errorf("enabledMap = %s", b)
	}
}

// An empty map is a request to change nothing, and Stash need not hear about
// it.
func TestSetPluginsEnabledEmptyMakesNoRequest(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).SetPluginsEnabled(context.Background(), nil); err != nil {
		t.Fatalf("SetPluginsEnabled: %v", err)
	}
	if len(cap.reqs) != 0 {
		t.Errorf("sent %d requests, want none", len(cap.reqs))
	}
}

func TestInterfaceConfigAsksForTheNamedFields(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"configuration":{"interface":{"javascript":"x","javascriptEnabled":true}}}}`))
	defer srv.Close()

	got, err := NewClient(srv.URL).InterfaceConfig(context.Background(), "javascript", "javascriptEnabled")
	if err != nil {
		t.Fatalf("InterfaceConfig: %v", err)
	}
	if got["javascript"] != "x" || got["javascriptEnabled"] != true {
		t.Errorf("config = %+v", got)
	}
	if !strings.Contains(cap.reqs[0].Query, "javascript javascriptEnabled") {
		t.Errorf("query = %s", cap.reqs[0].Query)
	}
}

// Field names are spliced into the query, so they have to be field names.
func TestInterfaceConfigRefusesAnythingButAFieldName(t *testing.T) {
	for _, bad := range []string{"", "javascript css", "javascript }", "1st", "a-b", "a{b}"} {
		_, c := server(t, reply(`{"data":{}}`))
		if _, err := c.InterfaceConfig(context.Background(), bad); err == nil {
			t.Errorf("InterfaceConfig(%q): want an error", bad)
		}
	}
	_, c := server(t, reply(`{"data":{}}`))
	if _, err := c.InterfaceConfig(context.Background()); err == nil {
		t.Error("InterfaceConfig with no fields: want an error")
	}
}

func TestConfigureInterfaceSendsOnlyWhatItWasGiven(t *testing.T) {
	cap := &capture{}
	srv := httptest.NewServer(cap.handler(`{"data":{"configureInterface":{"__typename":"ConfigInterfaceResult"}}}`))
	defer srv.Close()

	err := NewClient(srv.URL).ConfigureInterface(context.Background(), map[string]any{"javascriptEnabled": true})
	if err != nil {
		t.Fatalf("ConfigureInterface: %v", err)
	}
	b, _ := json.Marshal(cap.reqs[0].Variables["input"])
	if string(b) != `{"javascriptEnabled":true}` {
		t.Errorf("input = %s, want only the key given", b)
	}
}
