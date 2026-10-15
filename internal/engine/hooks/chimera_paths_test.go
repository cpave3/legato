package hooks

import (
	"os/exec"
	"strings"
	"testing"
)

func TestChimeraIsGrantedEachPathWithItsOwnFlag(t *testing.T) {
	a := NewChimeraAdapter("legato")

	flags := a.PathGrantFlags([]string{"/srv/My Vault", "/srv/it's here"}, []string{"/home/me/Daily Notes"})

	// Run the flags through a real shell to see the arguments Chimera gets.
	out, err := exec.Command("sh", "-c", `printf '%s\n'`+flags).Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	want := []string{
		"--add-read-path", "/srv/My Vault",
		"--add-read-path", "/srv/it's here",
		"--add-write-path", "/home/me/Daily Notes",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("arguments = %q, want %q", got, want)
	}
}
