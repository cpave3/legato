package clonesetup

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
)

// Report says what Provision did.
type Report struct {
	// Copied and Linked are the main checkout's files that were copied or
	// linked to, as absolute paths there.
	Copied, Linked []string
	// Notes are what it left alone, and why.
	Notes []string
}

// Provision copies and links the setup's files from the repository's main
// checkout (primary) into the clone, as Yggdrasil does for a new worktree.
// A file the clone has already is left alone, and one the main checkout
// lacks is noted. Both folders are opened as roots, so nothing outside
// either is read or written, whatever links they hold. Without a main
// checkout there is nothing to take files from.
func Provision(s Setup, primary, clone string) (Report, error) {
	var r Report
	if len(s.Copy) == 0 && len(s.Link) == 0 {
		return r, nil
	}
	if primary == "" {
		r.Notes = append(r.Notes, "no main checkout of the repository is set, so no files were copied or linked")
		return r, nil
	}
	from, err := os.OpenRoot(primary)
	if err != nil {
		return r, fmt.Errorf("opening the main checkout %s: %w", primary, err)
	}
	defer from.Close()
	to, err := os.OpenRoot(clone)
	if err != nil {
		return r, fmt.Errorf("opening the clone %s: %w", clone, err)
	}
	defer to.Close()

	each := func(patterns []string, do func(name string) error) {
		for _, pattern := range patterns {
			names, err := fs.Glob(from.FS(), path.Clean(filepath.ToSlash(pattern)))
			if err != nil || len(names) == 0 {
				r.Notes = append(r.Notes, fmt.Sprintf("%s: not in the main checkout", pattern))
				continue
			}
			for _, name := range names {
				if _, err := to.Lstat(name); err == nil {
					r.Notes = append(r.Notes, fmt.Sprintf("%s: the clone has its own, kept", name))
					continue
				}
				if err := do(name); err != nil {
					r.Notes = append(r.Notes, fmt.Sprintf("%s: %v", name, err))
				}
			}
		}
	}
	each(s.Copy, func(name string) error {
		if err := copyFile(from, to, name); err != nil {
			return err
		}
		r.Copied = append(r.Copied, filepath.Join(primary, name))
		return nil
	})
	each(s.Link, func(name string) error {
		// Only a file that is inside the main checkout, links followed.
		f, err := from.Open(name)
		if err != nil {
			return err
		}
		f.Close()
		if err := mkdirs(to, path.Dir(name)); err != nil {
			return err
		}
		target := filepath.Join(primary, filepath.FromSlash(name))
		if err := to.Symlink(target, name); err != nil {
			return err
		}
		r.Linked = append(r.Linked, target)
		return nil
	})
	return r, nil
}

func copyFile(from, to *os.Root, name string) error {
	src, err := from.Open(name)
	if err != nil {
		return err
	}
	defer src.Close()
	info, err := src.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("not a file")
	}
	if err := mkdirs(to, path.Dir(name)); err != nil {
		return err
	}
	dst, err := to.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, info.Mode().Perm())
	if err != nil {
		return err
	}
	if _, err := io.Copy(dst, src); err != nil {
		dst.Close()
		_ = to.Remove(name)
		return err
	}
	return dst.Close()
}

func mkdirs(root *os.Root, dir string) error {
	if dir == "." || dir == "" {
		return nil
	}
	return root.MkdirAll(dir, 0o755)
}
