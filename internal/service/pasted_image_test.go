package service

import (
	"os"
	"path/filepath"
	"testing"
)

// A pasted image is a file Legato itself stored for a task. An agent tool is
// told it may read such a file without asking, so the test of what one is
// must not be talked round.
func TestIsPastedImage(t *testing.T) {
	temp := t.TempDir()
	t.Setenv("TMPDIR", temp)
	dir := PastedImageDir("task-1")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	image := filepath.Join(dir, "pasted-1.png")
	if err := os.WriteFile(image, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	other := PastedImageDir("task-2")
	if err := os.MkdirAll(other, 0o700); err != nil {
		t.Fatal(err)
	}
	theirs := filepath.Join(other, "pasted-1.png")
	if err := os.WriteFile(theirs, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	secret := filepath.Join(temp, "secret.txt")
	if err := os.WriteFile(secret, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A link inside the folder to a file outside it.
	link := filepath.Join(dir, "link.png")
	if err := os.Symlink(secret, link); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}

	cases := map[string]struct {
		taskID string
		path   string
		want   bool
	}{
		"the task's own pasted image":         {"task-1", image, true},
		"the same file, written the long way": {"task-1", filepath.Join(dir, "folder", "..", "pasted-1.png"), true},
		"another task's pasted image":         {"task-1", theirs, false},
		"a path that climbs out":              {"task-1", filepath.Join(dir, "..", "..", "..", "secret.txt"), false},
		"a link that leads out":               {"task-1", link, false},
		"a folder":                            {"task-1", filepath.Join(dir, "folder"), false},
		"the folder itself":                   {"task-1", dir, false},
		"a file that is not there":            {"task-1", filepath.Join(dir, "gone.png"), false},
		"a relative path":                     {"task-1", "pasted-1.png", false},
		"a file elsewhere":                    {"task-1", secret, false},
		"no task":                             {"", image, false},
		"a task ID that is not a name":        {"../task-1", image, false},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			if got := IsPastedImage(tc.taskID, tc.path); got != tc.want {
				t.Errorf("IsPastedImage(%q, %q) = %v, want %v", tc.taskID, tc.path, got, tc.want)
			}
		})
	}
}

func TestValidPastedImageTaskID(t *testing.T) {
	for _, id := range []string{"fekz3ove", "REX-1234", "st-0123456789", "a.b_c-d"} {
		if !ValidPastedImageTaskID(id) {
			t.Errorf("%q should be a valid task ID", id)
		}
	}
	for _, id := range []string{"", "..", "a/b", "with space", ".hidden", "-flag"} {
		if ValidPastedImageTaskID(id) {
			t.Errorf("%q should not be a valid task ID", id)
		}
	}
}
