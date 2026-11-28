package claudecode

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// ErrNoVersion is a folder with no version in use yet.
var ErrNoVersion = errors.New("no Claude Code version is in use yet")

// Folder is where a program keeps its own Claude Code versions:
//
//	versions/<version>  each binary, as fetched or adopted
//	claude              a link to the version in use; its name is claude,
//	                    because the process name of what runs through it is
//	                    the link's name, and tools that find Claude Code by
//	                    process name look for claude
//	last-good           the version that was in use before the current one
//	held                a version rolled back from, not to be offered again
//	lock                held while versions change
type Folder struct {
	Dir string
}

// VersionsDir holds the binaries.
func (f Folder) VersionsDir() string { return filepath.Join(f.Dir, "versions") }

// Command is the link to run.
func (f Folder) Command() string { return filepath.Join(f.Dir, "claude") }

func (f Folder) versionPath(v string) (string, error) {
	if !ValidVersion(v) {
		return "", fmt.Errorf("%q is not a Claude Code version", v)
	}
	return filepath.Join(f.VersionsDir(), v), nil
}

// Has reports whether a version's binary is in the folder.
func (f Folder) Has(v string) bool {
	path, err := f.versionPath(v)
	if err != nil {
		return false
	}
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

// Current is the version the link points at, whether or not its file is
// still there; ErrNoVersion when there is no link.
func (f Folder) Current() (string, error) {
	target, err := os.Readlink(f.Command())
	if errors.Is(err, os.ErrNotExist) {
		return "", ErrNoVersion
	}
	if err != nil {
		return "", err
	}
	v := filepath.Base(target)
	if !ValidVersion(v) {
		return "", fmt.Errorf("%s points at %s, which is not a version", f.Command(), target)
	}
	return v, nil
}

// SetCurrent points the link at a version in the folder. The link is made
// under another name and renamed over the old one, so a launch never finds
// it missing.
func (f Folder) SetCurrent(v string) error {
	if !f.Has(v) {
		return fmt.Errorf("Claude Code %s is not in %s", v, f.VersionsDir())
	}
	tmp := filepath.Join(f.Dir, ".claude.link")
	os.Remove(tmp)
	if err := os.Symlink(filepath.Join("versions", v), tmp); err != nil {
		return err
	}
	if err := os.Rename(tmp, f.Command()); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// LastGood is the version kept to roll back to; "" for none.
func (f Folder) LastGood() (string, error) { return f.readMark("last-good") }

// SetLastGood records it.
func (f Folder) SetLastGood(v string) error { return f.writeMark("last-good", v) }

// Held is a version rolled back from; "" for none.
func (f Folder) Held() (string, error) { return f.readMark("held") }

// SetHeld records it; "" clears it.
func (f Folder) SetHeld(v string) error { return f.writeMark("held", v) }

func (f Folder) readMark(name string) (string, error) {
	raw, err := os.ReadFile(filepath.Join(f.Dir, name))
	if errors.Is(err, os.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(raw))
	if !ValidVersion(v) {
		return "", nil
	}
	return v, nil
}

func (f Folder) writeMark(name, v string) error {
	path := filepath.Join(f.Dir, name)
	if v == "" {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return nil
	}
	if !ValidVersion(v) {
		return fmt.Errorf("%q is not a Claude Code version", v)
	}
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(v+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// Adopt puts a copy of an installed binary in the folder as version v: a
// hard link when both are on one file system, else a copy. The original is
// left alone, and removing it later leaves the folder's.
func (f Folder) Adopt(src, v string) error {
	dst, err := f.versionPath(v)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(f.VersionsDir(), 0o755); err != nil {
		return err
	}
	tmp := filepath.Join(f.VersionsDir(), "."+v+".adopt")
	os.Remove(tmp)
	if err := os.Link(src, tmp); err != nil {
		if err := copyFile(src, tmp); err != nil {
			os.Remove(tmp)
			return err
		}
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// InstalledVersion reads a command path (such as the claude on PATH) as a
// native install: the file it leads to lies in a folder named versions and
// is named for its version, as the installer lays out
// ~/.local/share/claude/versions/<version>.
func InstalledVersion(command string) (path, version string, ok bool) {
	resolved, err := filepath.EvalSymlinks(command)
	if err != nil {
		return "", "", false
	}
	v := filepath.Base(resolved)
	if filepath.Base(filepath.Dir(resolved)) != "versions" || !ValidVersion(v) {
		return "", "", false
	}
	if info, err := os.Stat(resolved); err != nil || !info.Mode().IsRegular() {
		return "", "", false
	}
	return resolved, v, true
}

// Lock holds the folder's lock until the returned function is called, for
// every process that changes it.
func (f Folder) Lock() (func(), error) {
	file, err := f.openLock()
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return func() { file.Close() }, nil
}

// TryLock takes the lock only if nobody holds it.
func (f Folder) TryLock() (func(), bool, error) {
	file, err := f.openLock()
	if err != nil {
		return nil, false, err
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return func() { file.Close() }, true, nil
}

func (f Folder) openLock() (*os.File, error) {
	if err := os.MkdirAll(f.Dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(f.Dir, "lock"), os.O_CREATE|os.O_RDWR, 0o644)
}
