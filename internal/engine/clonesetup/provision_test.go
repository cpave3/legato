package clonesetup_test

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/engine/clonesetup"
)

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func TestProvisionCopiesAndLinksFromTheMainCheckout(t *testing.T) {
	primary, clone := t.TempDir(), t.TempDir()
	write(t, primary, ".env.local", "SECRET=1")
	write(t, primary, "apps/web/.env.local", "WEB=1")
	write(t, primary, "local.settings.json", "{}")
	setup := clonesetup.Setup{Copy: []string{".env.local", "apps/web/.env.local"}, Link: []string{"local.settings.json"}}

	report, err := clonesetup.Provision(setup, primary, clone)
	if err != nil {
		t.Fatal(err)
	}

	if read(t, filepath.Join(clone, ".env.local")) != "SECRET=1" || read(t, filepath.Join(clone, "apps/web/.env.local")) != "WEB=1" {
		t.Error("files not copied")
	}
	target, err := os.Readlink(filepath.Join(clone, "local.settings.json"))
	if err != nil || target != filepath.Join(primary, "local.settings.json") {
		t.Errorf("link = %q (%v), want one to the main checkout's file", target, err)
	}
	// A copy is its own: changing it leaves the main checkout alone.
	write(t, clone, ".env.local", "CHANGED")
	if read(t, filepath.Join(primary, ".env.local")) != "SECRET=1" {
		t.Error("the copy is the main checkout's file")
	}
	if !reflect.DeepEqual(report.Linked, []string{filepath.Join(primary, "local.settings.json")}) {
		t.Errorf("linked = %v", report.Linked)
	}
}

func TestProvisionTakesGlobs(t *testing.T) {
	primary, clone := t.TempDir(), t.TempDir()
	write(t, primary, "apps/a/.env.local", "A")
	write(t, primary, "apps/b/.env.local", "B")

	if _, err := clonesetup.Provision(clonesetup.Setup{Copy: []string{"apps/*/.env.local"}}, primary, clone); err != nil {
		t.Fatal(err)
	}

	if read(t, filepath.Join(clone, "apps/a/.env.local")) != "A" || read(t, filepath.Join(clone, "apps/b/.env.local")) != "B" {
		t.Error("glob not copied")
	}
}

func TestProvisionLeavesWhatTheCloneHasAndSaysWhatIsMissing(t *testing.T) {
	primary, clone := t.TempDir(), t.TempDir()
	write(t, primary, "config.json", "main checkout's")
	write(t, clone, "config.json", "tracked")

	report, err := clonesetup.Provision(clonesetup.Setup{Copy: []string{"config.json", ".env.local"}}, primary, clone)
	if err != nil {
		t.Fatal(err)
	}

	if read(t, filepath.Join(clone, "config.json")) != "tracked" {
		t.Error("a file of the clone was replaced")
	}
	notes := strings.Join(report.Notes, "\n")
	if !strings.Contains(notes, "config.json") || !strings.Contains(notes, ".env.local") {
		t.Errorf("notes = %q, want both files named", notes)
	}
}

func TestProvisionStaysInsideTheMainCheckout(t *testing.T) {
	primary, clone, elsewhere := t.TempDir(), t.TempDir(), t.TempDir()
	write(t, elsewhere, "id_ed25519", "PRIVATE")
	if err := os.Symlink(filepath.Join(elsewhere, "id_ed25519"), filepath.Join(primary, ".env.local")); err != nil {
		t.Fatal(err)
	}

	report, _ := clonesetup.Provision(clonesetup.Setup{Copy: []string{".env.local"}, Link: []string{".env.local"}}, primary, clone)

	if _, err := os.Lstat(filepath.Join(clone, ".env.local")); err == nil {
		t.Errorf("a file outside the main checkout reached the clone (notes %q)", report.Notes)
	}
}

func TestProvisionWithoutAMainCheckoutDoesNothing(t *testing.T) {
	clone := t.TempDir()

	report, err := clonesetup.Provision(clonesetup.Setup{Copy: []string{".env.local"}}, "", clone)

	if err != nil || len(report.Notes) != 1 || !strings.Contains(report.Notes[0], "no main checkout") {
		t.Errorf("report %+v, err %v", report, err)
	}
}
