package service

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var testAzure = SandboxAzure{Credential: "azure-reader", ClientID: "b4c42806-dfd5-4a59-b6d3-6ae1c96d2166", TenantID: "7948632d-756d-40b1-8af2-1b9ca8b344b1"}

// azWrapperFixture is a real az that records each call with the secret it
// was given (and, on login, writes an account as az does), a legato whose
// credential exec answers as given, the wrapper, and a home of its own.
type azWrapperFixture struct {
	wrapper, calls, asked, home string
}

// legatoGives is a credential exec that hands the secret to the command.
const legatoGives = `while [ "$1" != "--" ]; do shift; done; shift; AZURE_CLIENT_SECRET=s3cret exec "$@"`

func newAzWrapperFixture(t *testing.T, legatoAnswer string) azWrapperFixture {
	t.Helper()
	dir := t.TempDir()
	f := azWrapperFixture{calls: filepath.Join(dir, "calls"), asked: filepath.Join(dir, "asked"), home: filepath.Join(dir, "home")}
	realAz := filepath.Join(dir, "az-real")
	_ = os.WriteFile(realAz, []byte("#!/bin/sh\n"+
		"echo \"$* secret=$AZURE_CLIENT_SECRET\" >> "+f.calls+"\n"+
		"if [ \"$1\" = login ]; then mkdir -p \"$HOME/.azure\"; printf '{\"subscriptions\": [{\"id\": \"t\", \"user\": {\"name\": \"b4c4\", \"type\": \"servicePrincipal\"}}]}' > \"$HOME/.azure/azureProfile.json\"; fi\n"), 0o755)
	legato := filepath.Join(dir, "legato")
	_ = os.WriteFile(legato, []byte("#!/bin/sh\necho \"$*\" >> "+f.asked+"\n"+legatoAnswer+"\n"), 0o755)
	f.wrapper = filepath.Join(dir, "wrapper", "az")
	if err := writeAzWrapper(f.wrapper, legato, testAzure); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_AZ_REAL", realAz)
	t.Setenv("HOME", f.home)
	t.Setenv("AZURE_CONFIG_DIR", "")
	return f
}

func (f azWrapperFixture) run(t *testing.T, args ...string) (string, error) {
	t.Helper()
	out, err := exec.Command(f.wrapper, args...).CombinedOutput()
	return string(out), err
}

func (f azWrapperFixture) lines(t *testing.T, file string) []string {
	t.Helper()
	got, _ := os.ReadFile(file)
	if strings.TrimSpace(string(got)) == "" {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(got)), "\n")
}

func TestTheAzWrapperSignsInAsTheAppBeforeTheFirstCommand(t *testing.T) {
	f := newAzWrapperFixture(t, legatoGives)
	// An account file az left with no account in it (after az logout).
	_ = os.MkdirAll(filepath.Join(f.home, ".azure"), 0o700)
	_ = os.WriteFile(filepath.Join(f.home, ".azure", "azureProfile.json"), []byte("\ufeff{\"subscriptions\": []}"), 0o600)

	if out, err := f.run(t, "group", "list", "-o", "table"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	asked := f.lines(t, f.asked)
	if len(asked) != 1 || !strings.HasPrefix(asked[0], "credential exec azure-reader --env AZURE_CLIENT_SECRET -- ") {
		t.Fatalf("asked = %q", asked)
	}
	calls := f.lines(t, f.calls)
	want := []string{
		"login --service-principal --username b4c42806-dfd5-4a59-b6d3-6ae1c96d2166 --tenant 7948632d-756d-40b1-8af2-1b9ca8b344b1 --password=s3cret --allow-no-subscriptions --output none secret=s3cret",
		// The secret reaches the az login only.
		"group list -o table secret=",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls = %q\nwant    %q", calls, want)
	}

	// Signed in now: the next command runs at once.
	if out, err := f.run(t, "account", "show"); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if asked := f.lines(t, f.asked); len(asked) != 1 {
		t.Fatalf("asked again: %q", asked)
	}
	if calls := f.lines(t, f.calls); calls[len(calls)-1] != "account show secret=" {
		t.Fatalf("calls = %q", calls)
	}
}

func TestTheAzWrapperLeavesSigningInAndHelpToAz(t *testing.T) {
	for _, args := range [][]string{{"login", "--use-device-code"}, {"logout"}, {"version"}, {"--version"}, {"help"}, {}, {"--help"}, {"group", "list", "--help"}, {"vm", "-h"}} {
		f := newAzWrapperFixture(t, legatoGives)
		if out, err := f.run(t, args...); err != nil {
			t.Fatalf("%q: %v %s", args, err, out)
		}
		if asked := f.lines(t, f.asked); len(asked) != 0 {
			t.Errorf("%q asked Legato: %q", args, asked)
		}
		if calls := f.lines(t, f.calls); len(calls) != 1 || calls[0] != strings.TrimSpace(strings.Join(args, " ")+" secret=") {
			t.Errorf("%q: calls = %q", args, calls)
		}
	}
}

func TestTheAzWrapperSaysToTryAgainWhileThePersonDecides(t *testing.T) {
	f := newAzWrapperFixture(t, `echo "legato: read with azure-reader: waiting for the person (pending ap-9)" >&2; exit 4`)
	out, err := f.run(t, "group", "list")
	if err == nil {
		t.Fatalf("exit 0 without signing in: %s", out)
	}
	if !strings.Contains(out, "pending ap-9") || !strings.Contains(out, "run this az command again") {
		t.Fatalf("out = %s", out)
	}
	if calls := f.lines(t, f.calls); len(calls) != 0 {
		t.Fatalf("az ran: %q", calls)
	}
}

func TestTheAzWrapperStopsWhenItCannotReachTheRealAz(t *testing.T) {
	f := newAzWrapperFixture(t, legatoGives)
	for _, real := range []string{"", f.wrapper} {
		t.Setenv("LEGATO_AZ_REAL", real)
		out, err := f.run(t, "group", "list")
		if exit, ok := err.(*exec.ExitError); !ok || exit.ExitCode() != 127 || !strings.Contains(out, "LEGATO_AZ_REAL") {
			t.Fatalf("real %q: %v %s", real, err, out)
		}
	}
}
