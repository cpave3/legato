// Package clonesetup reads how a repository wants a fresh clone made ready
// (its dependencies installed, its local files in place) and does it. The
// steps come from the repository's own .legato.toml, else from the
// post_create hooks and provisioning of its .yggdrasil.toml. It knows
// nothing of Legato beyond the names of those files.
package clonesetup

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/pelletier/go-toml/v2"
)

// The files a repository describes its setup in, in the order they are
// looked for.
const (
	LegatoFile    = ".legato.toml"
	YggdrasilFile = ".yggdrasil.toml"
)

// ErrNoSetup is a repository with neither file.
var ErrNoSetup = errors.New("the repository has no " + LegatoFile + " or " + YggdrasilFile)

// Setup is what makes a fresh clone ready.
type Setup struct {
	// File is the file it was read from.
	File string
	// Run are shell commands, run in order in the clone.
	Run []string
	// Copy and Link are paths, relative to the clone, of files taken from
	// the repository's main checkout: copied, or linked to.
	Copy []string
	Link []string
}

// Empty reports whether the setup has nothing to do.
func (s Setup) Empty() bool { return len(s.Run) == 0 && len(s.Copy) == 0 && len(s.Link) == 0 }

type legatoFile struct {
	Setup struct {
		Run  []string `toml:"run"`
		Copy []string `toml:"copy"`
		Link []string `toml:"link"`
	} `toml:"setup"`
}

type yggdrasilFile struct {
	Provision struct {
		Copy []string `toml:"copy"`
		Link []string `toml:"link"`
	} `toml:"provision"`
	Hooks struct {
		PostCreate []string `toml:"post_create"`
	} `toml:"hooks"`
}

// Load reads the setup of the clone at dir: its .legato.toml, else its
// .yggdrasil.toml (whose post_create hooks are the commands, and whose
// other sections are left alone). ErrNoSetup when it has neither.
func Load(dir string) (Setup, error) {
	data, err := os.ReadFile(filepath.Join(dir, LegatoFile))
	if err == nil {
		var f legatoFile
		dec := toml.NewDecoder(bytes.NewReader(data))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&f); err != nil {
			return Setup{}, fmt.Errorf("%s: %w", LegatoFile, err)
		}
		return checked(Setup{File: LegatoFile, Run: f.Setup.Run, Copy: f.Setup.Copy, Link: f.Setup.Link})
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return Setup{}, err
	}
	data, err = os.ReadFile(filepath.Join(dir, YggdrasilFile))
	if errors.Is(err, fs.ErrNotExist) {
		return Setup{}, ErrNoSetup
	}
	if err != nil {
		return Setup{}, err
	}
	var f yggdrasilFile
	if err := toml.Unmarshal(data, &f); err != nil {
		return Setup{}, fmt.Errorf("%s: %w", YggdrasilFile, err)
	}
	return checked(Setup{File: YggdrasilFile, Run: f.Hooks.PostCreate, Copy: f.Provision.Copy, Link: f.Provision.Link})
}

// checked refuses a file to copy or link that is not inside the clone.
func checked(s Setup) (Setup, error) {
	for _, paths := range [][]string{s.Copy, s.Link} {
		for _, p := range paths {
			if err := relative(p); err != nil {
				return Setup{}, fmt.Errorf("%s: %w", s.File, err)
			}
		}
	}
	return s, nil
}

func relative(p string) error {
	clean := filepath.Clean(p)
	if p == "" || filepath.IsAbs(p) || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return fmt.Errorf("%q is not a path inside the repository", p)
	}
	return nil
}
