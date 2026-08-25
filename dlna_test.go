package stash

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestDLNAStatusDecodes(t *testing.T) {
	_, c := server(t, reply(`{"data":{"dlnaStatus":{"running":true,"until":"2026-01-01T12:00:00Z",
		"recentIPAddresses":["192.168.1.20","192.168.1.21"],
		"allowedIPAddresses":[{"ipAddress":"192.168.1.20","until":null}]}}}`))

	got, err := c.DLNAStatus(context.Background())
	if err != nil {
		t.Fatalf("DLNAStatus: %v", err)
	}
	if !got.Running || got.Until == nil || *got.Until != "2026-01-01T12:00:00Z" {
		t.Errorf("status = %+v", got)
	}
	if len(got.RecentIPAddresses) != 2 {
		t.Errorf("recent = %v", got.RecentIPAddresses)
	}
	// A grant made without a duration lasts until the server restarts, and
	// says so by having no expiry rather than one in the past.
	if len(got.AllowedIPAddresses) != 1 || got.AllowedIPAddresses[0].Until != nil {
		t.Errorf("allowed = %+v", got.AllowedIPAddresses)
	}
	if got.AllowedIPAddresses[0].Address != "192.168.1.20" {
		t.Errorf("address = %q", got.AllowedIPAddresses[0].Address)
	}
}

// Stash counts in minutes, and an absent duration means "no expiry".
func TestDLNADurationsBecomeMinutes(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"enableDLNA":true}}`))
	defer srv.Close()
	c := NewClient(srv.URL)

	if err := c.EnableDLNA(context.Background(), 2*time.Hour); err != nil {
		t.Fatalf("EnableDLNA: %v", err)
	}
	if got := sentInput(t, capt.reqs[0])["duration"]; got != float64(120) {
		t.Errorf("duration = %v, want 120 minutes", got)
	}

	if err := c.EnableDLNA(context.Background(), 0); err != nil {
		t.Fatalf("EnableDLNA: %v", err)
	}
	if _, present := sentInput(t, capt.reqs[1])["duration"]; present {
		t.Error("a zero duration sent a duration; it means no expiry, which is the field being absent")
	}
}

// Truncating 30s to zero minutes would turn "for half a minute" into
// "forever", which is the opposite of what was asked.
func TestDLNARefusesADurationShorterThanAMinute(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"enableDLNA":true}}`))
	defer srv.Close()

	err := NewClient(srv.URL).EnableDLNA(context.Background(), 30*time.Second)
	if err == nil {
		t.Fatal("EnableDLNA(30s) = nil error, want a refusal")
	}
	if !strings.Contains(err.Error(), "minute") {
		t.Errorf("error = %q, want it to name the unit", err)
	}
	if len(capt.reqs) != 0 {
		t.Error("the refused call still reached the server")
	}
}

func TestDisableDLNA(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"disableDLNA":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).DisableDLNA(context.Background(), 15*time.Minute); err != nil {
		t.Fatalf("DisableDLNA: %v", err)
	}
	if !strings.Contains(capt.reqs[0].Query, "disableDLNA") {
		t.Errorf("query = %q", capt.reqs[0].Query)
	}
	if got := sentInput(t, capt.reqs[0])["duration"]; got != float64(15) {
		t.Errorf("duration = %v", got)
	}
}

func TestAllowDLNAIPSendsAddressAndDuration(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"addTempDLNAIP":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).AllowDLNAIP(context.Background(), "192.168.1.20", time.Hour); err != nil {
		t.Fatalf("AllowDLNAIP: %v", err)
	}
	in := sentInput(t, capt.reqs[0])
	if in["address"] != "192.168.1.20" || in["duration"] != float64(60) {
		t.Errorf("input = %v", in)
	}
}

func TestDisallowDLNAIP(t *testing.T) {
	capt := &capture{}
	srv := httptest.NewServer(capt.handler(`{"data":{"removeTempDLNAIP":true}}`))
	defer srv.Close()

	if err := NewClient(srv.URL).DisallowDLNAIP(context.Background(), "192.168.1.20"); err != nil {
		t.Fatalf("DisallowDLNAIP: %v", err)
	}
	if got := sentInput(t, capt.reqs[0])["address"]; got != "192.168.1.20" {
		t.Errorf("address = %v", got)
	}
}

func TestDLNAAddressCallsNeedAnAddress(t *testing.T) {
	_, c := server(t, reply(`{"data":{}}`))
	if err := c.AllowDLNAIP(context.Background(), "", 0); err == nil {
		t.Error("AllowDLNAIP(\"\") = nil error")
	}
	if err := c.DisallowDLNAIP(context.Background(), ""); err == nil {
		t.Error("DisallowDLNAIP(\"\") = nil error")
	}
}
