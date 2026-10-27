package reviewbriefing

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

// ErrUnknownPage is returned for a path that is not one of the briefing's
// pages.
var ErrUnknownPage = errors.New("not a page of this briefing")

// Bounds on what one rendered page may read. A built briefing with its
// diagrams embedded is a few hundred kilobytes.
const (
	maxFileBytes   = 8 << 20
	maxRenderBytes = 24 << 20
)

// mediaTypes are the types files are inlined as, by extension, as the
// extension inlines them.
var mediaTypes = map[string]string{
	".html": "text/html", ".htm": "text/html", ".css": "text/css", ".js": "text/javascript", ".mjs": "text/javascript",
	".json": "application/json", ".svg": "image/svg+xml", ".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".ico": "image/x-icon", ".md": "text/markdown",
	".txt": "text/plain", ".mmd": "text/plain", ".woff": "font/woff", ".woff2": "font/woff2", ".mp4": "video/mp4", ".webm": "video/webm",
}

var reference = regexp.MustCompile(`(?i)\b(src|href)\s*=\s*("([^"]*)"|'([^']*)')`)

// absolute matches a reference that names no file of the repository: a
// scheme, a protocol-relative or root-relative address, or an anchor.
var absolute = regexp.MustCompile(`(?i)^([a-z][a-z0-9+.-]*:|//|#|/)`)

// Render reads one page of the briefing from the checkout, with each file of
// the briefing it refers to by a relative src or href (a stylesheet, an
// image, a script) put in as a data: URL: the page is drawn in a sandbox
// that can read nothing else. Links to other pages are left as they are.
func Render(ctx context.Context, index *Index, page string) (string, error) {
	if !slices.ContainsFunc(index.Pages, func(p Page) bool { return p.Path == page }) {
		return "", ErrUnknownPage
	}
	content, err := readFile(index.Root, page)
	if err != nil {
		return "", err
	}
	budget := maxRenderBytes - len(content)
	inlined := map[string]string{}
	html := reference.ReplaceAllStringFunc(string(content), func(whole string) string {
		if ctx.Err() != nil {
			return whole
		}
		match := reference.FindStringSubmatch(whole)
		target, ok := resolve(match[3]+match[4], page)
		if !ok || isPage(target) || !slices.Contains(index.Files, target) {
			return whole
		}
		data, done := inlined[target]
		if !done {
			bytes, err := readFile(index.Root, target)
			if err != nil || len(bytes) > budget {
				return whole
			}
			budget -= len(bytes)
			data = "data:" + mediaType(target) + ";base64," + base64.StdEncoding.EncodeToString(bytes)
			inlined[target] = data
		}
		return match[1] + `="` + data + `"`
	})
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return html, nil
}

// resolve turns a reference made from page into a repository path, or false
// when it is absolute, empty, or climbs out of the repository.
func resolve(ref, page string) (string, bool) {
	if ref == "" || absolute.MatchString(ref) {
		return "", false
	}
	if cut := strings.IndexAny(ref, "?#"); cut >= 0 {
		ref = ref[:cut]
	}
	joined := path.Join(path.Dir(page), ref)
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", false
	}
	return joined, true
}

// readFile reads a briefing file from the working tree. The file must really
// be inside .review/: a link there that leads elsewhere is not followed.
func readFile(root, name string) ([]byte, error) {
	review, err := filepath.EvalSymlinks(filepath.Join(root, Dir))
	if err != nil {
		return nil, err
	}
	full, err := filepath.EvalSymlinks(filepath.Join(root, filepath.FromSlash(name)))
	if err != nil {
		return nil, err
	}
	if !strings.HasPrefix(full, review+string(filepath.Separator)) {
		return nil, fmt.Errorf("%s leads outside %s", name, Dir)
	}
	file, err := os.Open(full)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	content, err := io.ReadAll(io.LimitReader(file, maxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(content) > maxFileBytes {
		return nil, fmt.Errorf("%s is larger than %d MiB", name, maxFileBytes>>20)
	}
	return content, nil
}

func mediaType(name string) string {
	if media, ok := mediaTypes[strings.ToLower(path.Ext(name))]; ok {
		return media
	}
	return "application/octet-stream"
}
