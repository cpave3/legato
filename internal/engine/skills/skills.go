// Package skills reads a folder of agent skills: one folder per skill, each
// holding a SKILL.md whose YAML frontmatter gives its name and description,
// beside any files the skill uses. It knows nothing of Legato or of the agent
// tools that load skills. See docs/ai/skills.md.
package skills

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// FileName is the file that makes a folder a skill.
const FileName = "SKILL.md"

// Skill is one folder of a skills folder.
type Skill struct {
	// Name is the folder's name: what a skill is known and kept by.
	Name string `json:"name"`
	// Description is the frontmatter's description, which an agent reads
	// to decide when to use the skill.
	Description string `json:"description"`
	// Path is the folder as listed: a link to a skill stays a link.
	Path string `json:"path"`
	// Err says why the skill cannot be used; empty when it can.
	Err string `json:"error,omitempty"`
}

// OK reports whether the skill can be given to an agent.
func (s Skill) OK() bool { return s.Err == "" }

// namePattern is the Agent Skills format's rule for a name: lowercase
// letters, digits and hyphens, at most 64 characters.
var namePattern = regexp.MustCompile(`^[a-z0-9]+(-[a-z0-9]+)*$`)

const maxNameLength = 64

// Scan lists the skills in dir, sorted by name. A folder that is not a usable
// skill is listed with its Err; files, and folders whose names start with a
// dot, are not listed. A dir that does not exist holds no skills.
func Scan(dir string) ([]Skill, error) {
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Skill
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)
		// Stat follows a link, so a skill linked in from a checkout counts.
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		list = append(list, read(name, path))
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list, nil
}

func read(name, path string) Skill {
	skill := Skill{Name: name, Path: path}
	fail := func(format string, args ...any) Skill {
		skill.Err = fmt.Sprintf(format, args...)
		return skill
	}
	if len(name) > maxNameLength || !namePattern.MatchString(name) {
		return fail("the folder's name must be lowercase letters, digits and hyphens, at most %d characters", maxNameLength)
	}
	content, err := os.ReadFile(filepath.Join(path, FileName))
	if errors.Is(err, os.ErrNotExist) {
		return fail("no %s in the folder", FileName)
	}
	if err != nil {
		return fail("reading %s: %v", FileName, err)
	}
	front, err := frontmatter(content)
	if err != nil {
		return fail("%s: %v", FileName, err)
	}
	if front.Name == "" {
		return fail("%s: the frontmatter has no name", FileName)
	}
	if front.Name != name {
		return fail("%s: the frontmatter's name %q is not the folder's name", FileName, front.Name)
	}
	skill.Description = strings.TrimSpace(front.Description)
	if skill.Description == "" {
		return fail("%s: the frontmatter has no description", FileName)
	}
	return skill
}

type header struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
}

// frontmatter reads the YAML block a SKILL.md starts with, between two lines
// of three dashes.
func frontmatter(content []byte) (header, error) {
	content = bytes.TrimPrefix(content, []byte("\xef\xbb\xbf"))
	text := strings.ReplaceAll(string(content), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return header{}, errors.New("no frontmatter: the file must start with a line of three dashes")
	}
	rest := text[len("---\n"):]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return header{}, errors.New("the frontmatter has no closing line of three dashes")
	}
	var h header
	if err := yaml.Unmarshal([]byte(rest[:end]), &h); err != nil {
		return header{}, fmt.Errorf("the frontmatter is not valid YAML: %v", err)
	}
	return h, nil
}

// MaxBytes is the most a skill's files may hold for Copy: a skill is
// instructions and small scripts, and every agent started gets a copy.
const MaxBytes = 32 << 20

// Copy copies a skill's folder to dst, which must not exist. Only what is
// inside the skill's folder is copied: a link to a file inside it is copied
// as that file, while a link that leads out of the folder, to a folder, or
// nowhere is left out, so a skill cannot carry another file of the host
// into an agent's sandbox. Files keep their executable bit. A skill holding
// more than MaxBytes is refused, and nothing is left at dst.
func Copy(s Skill, dst string) error {
	return copyLimited(s, dst, MaxBytes)
}

func copyLimited(s Skill, dst string, limit int64) (err error) {
	root, err := os.OpenRoot(s.Path)
	if err != nil {
		return err
	}
	defer root.Close()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	if err := os.Mkdir(dst, 0o755); err != nil {
		return err
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(dst)
		}
	}()
	fsys := root.FS()
	var total int64
	return fs.WalkDir(fsys, ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == "." {
			return nil
		}
		target := filepath.Join(dst, filepath.FromSlash(path))
		if entry.IsDir() {
			return os.Mkdir(target, 0o755)
		}
		// Stat follows a link, inside the root only.
		info, err := fs.Stat(fsys, path)
		if err != nil || !info.Mode().IsRegular() {
			return nil
		}
		total += info.Size()
		if total > limit {
			return fmt.Errorf("skill %s is larger than %d MiB", s.Name, limit>>20)
		}
		mode := os.FileMode(0o644)
		if info.Mode().Perm()&0o111 != 0 {
			mode = 0o755
		}
		return copyFile(fsys, path, target, mode)
	})
}

func copyFile(fsys fs.FS, path, target string, mode os.FileMode) error {
	in, err := fsys.Open(path)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(target, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
