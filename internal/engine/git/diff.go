package git

import (
	"strconv"
	"strings"
)

// FileStatus classifies how a file changed within a diff.
type FileStatus string

const (
	FileModified FileStatus = "modified"
	FileAdded    FileStatus = "added"
	FileDeleted  FileStatus = "deleted"
	FileRenamed  FileStatus = "renamed"
	FileBinary   FileStatus = "binary"
)

// LineKind classifies a single line within a hunk.
type LineKind string

const (
	LineContext LineKind = "ctx"
	LineAdded   LineKind = "add"
	LineDeleted LineKind = "del"
)

// Line is one line of a hunk with its old/new line numbers (0 when the line
// does not exist on that side).
type Line struct {
	Kind  LineKind `json:"kind"`
	OldNo int      `json:"old_no"`
	NewNo int      `json:"new_no"`
	Text  string   `json:"text"`
}

// Hunk is one @@-delimited region of a file diff.
type Hunk struct {
	Header string `json:"header"`
	Lines  []Line `json:"lines"`
}

// FileDiff is the parsed diff of a single file. It is the interchange format
// between the diff engine and both UIs (rendered directly in the TUI, JSON-
// encoded for the web).
type FileDiff struct {
	OldPath string     `json:"old_path"`
	NewPath string     `json:"new_path"`
	Status  FileStatus `json:"status"`
	Hunks   []Hunk     `json:"hunks"`
}

// ParseUnifiedDiff parses `git diff` / `git show` output into per-file
// structures. Unrecognized preamble lines are skipped.
func ParseUnifiedDiff(text string) []FileDiff {
	var files []FileDiff
	var cur *FileDiff
	var hunk *Hunk
	var oldNo, newNo int

	flushHunk := func() {
		if cur != nil && hunk != nil {
			cur.Hunks = append(cur.Hunks, *hunk)
		}
		hunk = nil
	}
	flushFile := func() {
		flushHunk()
		if cur != nil {
			files = append(files, *cur)
		}
		cur = nil
	}

	for _, raw := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(raw, "diff --git "):
			flushFile()
			cur = &FileDiff{Status: FileModified}
			cur.OldPath, cur.NewPath = parseDiffGitPaths(raw)
		case cur == nil:
			continue
		case hunk == nil && strings.HasPrefix(raw, "new file mode"):
			cur.Status = FileAdded
		case hunk == nil && strings.HasPrefix(raw, "deleted file mode"):
			cur.Status = FileDeleted
		case hunk == nil && strings.HasPrefix(raw, "rename from "):
			cur.Status = FileRenamed
			cur.OldPath = strings.TrimPrefix(raw, "rename from ")
		case hunk == nil && strings.HasPrefix(raw, "rename to "):
			cur.NewPath = strings.TrimPrefix(raw, "rename to ")
		case hunk == nil && strings.HasPrefix(raw, "Binary files "):
			cur.Status = FileBinary
		case strings.HasPrefix(raw, "@@"):
			flushHunk()
			header := raw
			if idx := strings.Index(raw[2:], "@@"); idx >= 0 {
				header = raw[:idx+4]
			}
			hunk = &Hunk{Header: header}
			oldNo, newNo = parseHunkStarts(header)
		case hunk != nil && strings.HasPrefix(raw, "+"):
			hunk.Lines = append(hunk.Lines, Line{Kind: LineAdded, NewNo: newNo, Text: raw[1:]})
			newNo++
		case hunk != nil && strings.HasPrefix(raw, "-"):
			hunk.Lines = append(hunk.Lines, Line{Kind: LineDeleted, OldNo: oldNo, Text: raw[1:]})
			oldNo++
		case hunk != nil && strings.HasPrefix(raw, " "):
			hunk.Lines = append(hunk.Lines, Line{Kind: LineContext, OldNo: oldNo, NewNo: newNo, Text: raw[1:]})
			oldNo++
			newNo++
		}
	}
	flushFile()
	return files
}

// parseDiffGitPaths extracts old/new paths from a "diff --git a/x b/y" line.
func parseDiffGitPaths(line string) (oldPath, newPath string) {
	rest := strings.TrimPrefix(line, "diff --git ")
	parts := strings.SplitN(rest, " b/", 2)
	if len(parts) != 2 {
		return rest, rest
	}
	return strings.TrimPrefix(parts[0], "a/"), parts[1]
}

// parseHunkStarts extracts the old/new starting line numbers from a hunk
// header like "@@ -12,4 +13,6 @@".
func parseHunkStarts(header string) (oldStart, newStart int) {
	fields := strings.Fields(header)
	for _, f := range fields {
		if strings.HasPrefix(f, "-") {
			oldStart = parseStart(f[1:])
		} else if strings.HasPrefix(f, "+") {
			newStart = parseStart(f[1:])
		}
	}
	return oldStart, newStart
}

func parseStart(spec string) int {
	if idx := strings.Index(spec, ","); idx >= 0 {
		spec = spec[:idx]
	}
	n, _ := strconv.Atoi(spec)
	return n
}
