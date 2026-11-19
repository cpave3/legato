package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/service"
)

func TestAFirstHeadlessStartPrintsTheChecksAndAPairingPhrase(t *testing.T) {
	var out bytes.Buffer
	report := service.DoctorReport{Checks: []service.DoctorCheck{
		{ID: "tmux", Title: "tmux", Required: true, Result: service.DoctorMissing, Detail: "tmux not found", Fix: "Install tmux"},
	}}
	open := func() (service.PairingStatus, error) {
		return service.PairingStatus{Phrase: []string{"astronaut", "malformed", "mozzarella", "pneumonia"}, ExpiresAt: time.Now().Add(5 * time.Minute)}, nil
	}
	headlessWelcome(&out, report, open, "https://worker:3080")
	text := out.String()
	for _, want := range []string{"Install tmux", "astronaut malformed mozzarella pneumonia", "https://worker:3080/settings"} {
		if !strings.Contains(text, want) {
			t.Errorf("missing %q in:\n%s", want, text)
		}
	}
}

func TestAHeadlessStartWithoutPairingStillPrintsTheChecks(t *testing.T) {
	var out bytes.Buffer
	headlessWelcome(&out, service.DoctorReport{}, func() (service.PairingStatus, error) { return service.PairingStatus{}, errors.New("no pairing") }, "http://worker:3080")
	if !strings.Contains(out.String(), "Every required check passed") || !strings.Contains(out.String(), "legato pair") {
		t.Fatalf("%s", out.String())
	}
}
