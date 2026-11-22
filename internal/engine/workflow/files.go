package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Folder is a folder workflow files are read from: the legacy folder,
// the global one, a repository's checkout or the project.
type Folder struct {
	ID     string `json:"id"`
	Label  string `json:"label"`
	Path   string `json:"path"`
	Legacy bool   `json:"legacy,omitempty"`
}

// Folders lists the workflow folders in increasing precedence: legacy,
// global, each repository preset's <primary_dir>/.legato/workflows in
// order, then the project. A folder named twice is listed once, where it
// takes precedence. Empty paths are left out.
func Folders(legacy, global string, repositories []RepositoryPreset, project string) []Folder {
	var folders []Folder
	add := func(folder Folder) {
		if folder.Path == "" {
			return
		}
		folder.Path = filepath.Clean(folder.Path)
		for i, existing := range folders {
			if existing.Path == folder.Path {
				folders = append(folders[:i], folders[i+1:]...)
				break
			}
		}
		folders = append(folders, folder)
	}
	add(Folder{ID: "legacy", Label: "Legacy folder", Path: legacy, Legacy: true})
	add(Folder{ID: "global", Label: "Your workflows", Path: global})
	for _, preset := range repositories {
		if strings.TrimSpace(preset.PrimaryDir) == "" {
			continue
		}
		add(Folder{ID: "repo:" + preset.Name, Label: preset.Name, Path: filepath.Join(preset.PrimaryDir, ".legato", "workflows")})
	}
	add(Folder{ID: "project", Label: "This project", Path: project})
	return folders
}

// DiscoverFolders loads the definitions of folders, listed as Folders
// lists them: a later folder's workflow replaces an earlier one of the same
// name, which Shadows then names.
func DiscoverFolders(folders []Folder) DiscoveryResult {
	paths := make([]string, len(folders))
	for i, folder := range folders {
		paths[i] = folder.Path
	}
	return DiscoverPaths(paths...)
}

// FileInfo is where a definition's file is, and whether the web UI may
// write it.
type FileInfo struct {
	Path     string `json:"path,omitempty"`
	Folder   string `json:"folder,omitempty"`
	Editable bool   `json:"editable"`
	Reason   string `json:"reason,omitempty"`
}

// FileFor finds a definition's file among folders. A built-in has none; a
// file in no folder, or a link to a file outside its folder, is read-only.
func FileFor(definition *Definition, folders []Folder) FileInfo {
	source := definition.Source
	if source == "" || strings.HasPrefix(source, "builtin:") {
		return FileInfo{Reason: "a built-in workflow has no file: copy it to your workflows to change it"}
	}
	info := FileInfo{Path: source}
	dir := filepath.Dir(filepath.Clean(source))
	var folder *Folder
	for i := range folders {
		if folders[i].Path == dir {
			folder = &folders[i]
		}
	}
	if folder == nil {
		info.Reason = "the file is in no workflows folder"
		return info
	}
	info.Folder = folder.ID
	if within, err := insideFolder(source, folder.Path); err != nil || !within {
		info.Reason = "the file is a link to somewhere outside its folder"
		return info
	}
	info.Editable = true
	return info
}

// insideFolder reports whether path is a regular file in folder, links
// resolved.
func insideFolder(path, folder string) (bool, error) {
	stat, err := os.Lstat(path)
	if err != nil {
		return false, err
	}
	if !stat.Mode().IsRegular() {
		return false, nil
	}
	realFolder, err := filepath.EvalSymlinks(folder)
	if err != nil {
		return false, err
	}
	realPath, err := filepath.EvalSymlinks(path)
	if err != nil {
		return false, err
	}
	return filepath.Dir(realPath) == realFolder, nil
}

// ErrStaleFile refuses a save over a file that changed since it was read.
var ErrStaleFile = errors.New("the file changed since it was read")

// FileDigest names a file's content, for a save to say what it replaces.
func FileDigest(data []byte) string {
	sum := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// ValidName refuses what cannot be a workflow name: it is a file name too.
func ValidName(name string) error {
	if !workflowNamePattern.MatchString(name) {
		return fmt.Errorf("%q is not a workflow name: use lower-case letters, digits, '.', '-' and '_'", name)
	}
	return nil
}

// CreateFile writes data to dir as <name>.workflow.yaml, all at once and
// never over a file: one there is ErrWorkflowNameTaken.
func CreateFile(dir, name string, data []byte) (string, error) {
	if err := ValidName(name); err != nil {
		return "", err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, name+".workflow.yaml")
	temp, err := writeTemp(dir, data, 0o644)
	if err != nil {
		return "", err
	}
	defer os.Remove(temp)
	// A link fails on a file that exists, as a rename would not.
	if err := os.Link(temp, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("%w: %s", ErrWorkflowNameTaken, path)
		}
		return "", err
	}
	return path, nil
}

// ReplaceFile writes data over path, all at once, when the file still has
// digest; otherwise ErrStaleFile. Its mode is kept.
func ReplaceFile(path string, data []byte, digest string) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if FileDigest(current) != digest {
		return ErrStaleFile
	}
	stat, err := os.Stat(path)
	if err != nil {
		return err
	}
	temp, err := writeTemp(filepath.Dir(path), data, stat.Mode().Perm())
	if err != nil {
		return err
	}
	if err := os.Rename(temp, path); err != nil {
		os.Remove(temp)
		return err
	}
	return nil
}

// RemoveFile removes path when the file still has digest; otherwise
// ErrStaleFile.
func RemoveFile(path, digest string) error {
	current, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if FileDigest(current) != digest {
		return ErrStaleFile
	}
	return os.Remove(path)
}

func writeTemp(dir string, data []byte, mode os.FileMode) (string, error) {
	file, err := os.CreateTemp(dir, ".workflow-*.tmp")
	if err != nil {
		return "", err
	}
	name := file.Name()
	_, err = file.Write(data)
	if err == nil {
		err = file.Sync()
	}
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Chmod(name, mode)
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}
