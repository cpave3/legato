package service

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// chimeraAttachToken represents a single @path (read) or #path (write) token.
type chimeraAttachToken struct {
	Kind     string // "read" or "write"
	Raw      string // original path text as typed
	Absolute string // resolved absolute path
}

// A path runs to the next whitespace, or is written in double quotes
// (@"My Notes/a.md") or with its spaces escaped (@My\ Notes/a.md) when it has
// spaces in it, as Chimera's TUI reads it.
var chimeraAttachPattern = regexp.MustCompile(`(^|\s)([@#])(?:"([^"\n]+)"|((?:\\\s|\S)+))`)

var chimeraEscapedSpace = regexp.MustCompile(`\\(\s)`)

// parseChimeraAttachments detects @path (read) and #path (write) tokens in
// user input, mirroring Chimera TUI semantics. Relative paths resolve against
// cwd; ~/ expands via homeDir. Markdown headings and generated paste/image
// placeholders are skipped.
func parseChimeraAttachments(input, cwd, homeDir string) []chimeraAttachToken {
	var tokens []chimeraAttachToken
	for _, match := range chimeraAttachPattern.FindAllStringSubmatchIndex(input, -1) {
		prefixStart, prefixEnd := match[4], match[5]
		prefix := input[prefixStart:prefixEnd]
		var rawPath string
		if match[6] >= 0 {
			rawPath = input[match[6]:match[7]]
		} else {
			rawPath = chimeraEscapedSpace.ReplaceAllString(input[match[8]:match[9]], "$1")
		}

		if !isChimeraAttachmentPath(rawPath) {
			continue
		}
		if prefix == "#" && strings.HasPrefix(rawPath, "#") {
			continue
		}
		if prefix == "#" && (isPlaceholder(rawPath) || isPastePlaceholder(input, match[0], rawPath)) {
			continue
		}

		kind := "read"
		if prefix == "#" {
			kind = "write"
		}

		var resolved string
		switch {
		case strings.HasPrefix(rawPath, "~/"):
			resolved = filepath.Join(homeDir, rawPath[2:])
		case filepath.IsAbs(rawPath):
			resolved = rawPath
		default:
			resolved = filepath.Join(cwd, rawPath)
		}
		tokens = append(tokens, chimeraAttachToken{Kind: kind, Raw: rawPath, Absolute: resolved})
	}
	return tokens
}

func isChimeraAttachmentPath(rawPath string) bool {
	if rawPath == "" {
		return false
	}
	first := rawPath[0]
	return first == '.' || first == '/' || first == '~' || first == '_' || first == '-' ||
		(first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z') || (first >= '0' && first <= '9')
}

func isPlaceholder(rawPath string) bool {
	return regexp.MustCompile(`^\d+\]$`).MatchString(rawPath)
}

func isPastePlaceholder(input string, matchStart int, rawPath string) bool {
	if !regexp.MustCompile(`^\d+,$`).MatchString(rawPath) {
		return false
	}
	return strings.HasSuffix(input[:matchStart+1], "[Pasted text ")
}

const (
	chimeraAttachMaxFileLines  = 2000
	chimeraAttachMaxFileBytes  = 100 * 1024
	chimeraAttachMaxDirEntries = 200
)

// readChimeraAttachment reads a bounded snapshot of a file or directory.
// Files are line-numbered, capped at 2000 lines / 100 KiB. Directories list
// entries one per line with / on subdirectories, capped at 200 entries.
func readChimeraAttachment(absPath string) (string, error) {
	info, err := os.Stat(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("missing: %s", absPath)
		}
		return "", err
	}

	if info.IsDir() {
		return readChimeraDir(absPath)
	}
	return readChimeraFile(absPath)
}

func readChimeraFile(absPath string) (string, error) {
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err
	}
	if len(data) > chimeraAttachMaxFileBytes {
		data = data[:chimeraAttachMaxFileBytes]
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	var lines []string
	for scanner.Scan() {
		lines = append(lines, scanner.Text())
		if len(lines) >= chimeraAttachMaxFileLines {
			break
		}
	}
	for i, line := range lines {
		lines[i] = fmt.Sprintf("%d\t%s", i+1, line)
	}
	return strings.Join(lines, "\n"), nil
}

func readChimeraDir(absPath string) (string, error) {
	entries, err := os.ReadDir(absPath)
	if err != nil {
		return "", err
	}
	capped := entries
	if len(capped) > chimeraAttachMaxDirEntries {
		capped = capped[:chimeraAttachMaxDirEntries]
	}
	var lines []string
	for _, entry := range capped {
		name := entry.Name()
		if entry.IsDir() {
			name += "/"
		}
		lines = append(lines, name)
	}
	return strings.Join(lines, "\n"), nil
}
