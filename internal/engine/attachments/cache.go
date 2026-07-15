package attachments

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Metadata identifies a remote attachment and the properties used for caching.
type Metadata struct {
	ID       string `json:"id"`
	Filename string `json:"filename"`
	MimeType string `json:"mime_type"`
	Size     int64  `json:"size"`
}

// Local describes an attachment available to local callers.
type Local struct {
	Metadata
	Path string `json:"path"`
}

// Downloader opens a remote attachment stream.
type Downloader interface {
	DownloadAttachment(ctx context.Context, id string) (io.ReadCloser, error)
}

// Cache reconciles remote attachments into a deterministic filesystem tree.
type Cache struct {
	root    string
	maxSize int64
}

// NewCache creates a cache rooted at root. maxSize <= 0 disables the size limit.
func NewCache(root string, maxSize int64) *Cache {
	return &Cache{root: root, maxSize: maxSize}
}

// DefaultRoot returns the deterministic per-user temporary attachment root.
func DefaultRoot() string {
	if dir := os.Getenv("XDG_RUNTIME_DIR"); dir != "" {
		return filepath.Join(dir, "legato", "jira-attachments")
	}
	return filepath.Join(os.TempDir(), fmt.Sprintf("legato-%d", os.Getuid()), "jira-attachments")
}

// Reconcile makes the local image set match remote metadata.
func (c *Cache) Reconcile(ctx context.Context, ticketID string, remote []Metadata, downloader Downloader) error {
	dir := c.ticketDir(ticketID)
	wanted := make(map[string]Local)
	for _, item := range remote {
		if !strings.HasPrefix(strings.ToLower(item.MimeType), "image/") {
			continue
		}
		if c.maxSize > 0 && item.Size > c.maxSize {
			return fmt.Errorf("attachment %s exceeds maximum size of %d bytes", item.Filename, c.maxSize)
		}
		item.Filename = sanitizeFilename(item.Filename)
		wanted[item.ID] = Local{Metadata: item, Path: filepath.Join(dir, sanitizeFilename(item.ID)+"-"+item.Filename)}
	}

	current, err := c.List(ticketID)
	if err != nil {
		return err
	}
	currentByID := make(map[string]Local, len(current))
	for _, item := range current {
		currentByID[item.ID] = item
	}

	if len(wanted) > 0 {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	for id, item := range wanted {
		old, exists := currentByID[id]
		if exists && old.Size == item.Size {
			if info, statErr := os.Stat(old.Path); statErr == nil && info.Size() == item.Size {
				item.Path = old.Path
				wanted[id] = item
				continue
			}
		}
		if err := c.download(ctx, item, downloader); err != nil {
			return err
		}
	}
	for id, item := range currentByID {
		if _, ok := wanted[id]; !ok {
			if err := os.Remove(item.Path); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	if len(wanted) == 0 {
		return c.Remove(ticketID)
	}

	manifest := make([]Local, 0, len(wanted))
	for _, item := range wanted {
		manifest = append(manifest, item)
	}
	sort.Slice(manifest, func(i, j int) bool { return manifest[i].ID < manifest[j].ID })
	return writeJSONAtomic(filepath.Join(dir, "manifest.json"), manifest)
}

// List returns cached attachments whose manifest is present.
func (c *Cache) List(ticketID string) ([]Local, error) {
	data, err := os.ReadFile(filepath.Join(c.ticketDir(ticketID), "manifest.json"))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var items []Local
	if err := json.Unmarshal(data, &items); err != nil {
		return nil, fmt.Errorf("read attachment manifest: %w", err)
	}
	return items, nil
}

// Remove deletes all cached attachments for a ticket.
func (c *Cache) Remove(ticketID string) error {
	err := os.RemoveAll(c.ticketDir(ticketID))
	if err != nil {
		return fmt.Errorf("remove attachment cache for %s: %w", ticketID, err)
	}
	return nil
}

func (c *Cache) download(ctx context.Context, item Local, downloader Downloader) error {
	body, err := downloader.DownloadAttachment(ctx, item.ID)
	if err != nil {
		return fmt.Errorf("download attachment %s: %w", item.Filename, err)
	}
	defer body.Close()

	tmp, err := os.CreateTemp(filepath.Dir(item.Path), ".attachment-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	limit := io.Reader(body)
	if c.maxSize > 0 {
		limit = io.LimitReader(body, c.maxSize+1)
	}
	written, copyErr := io.Copy(tmp, limit)
	closeErr := tmp.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	if c.maxSize > 0 && written > c.maxSize {
		return fmt.Errorf("attachment %s exceeds maximum size of %d bytes", item.Filename, c.maxSize)
	}
	if item.Size >= 0 && written != item.Size {
		return fmt.Errorf("attachment %s size mismatch: got %d, want %d", item.Filename, written, item.Size)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return err
	}
	return os.Rename(tmpName, item.Path)
}

func (c *Cache) ticketDir(ticketID string) string {
	return filepath.Join(c.root, sanitizeFilename(ticketID))
}

func sanitizeFilename(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	name = strings.Map(func(r rune) rune {
		if r == '/' || r == '\\' || r == 0 {
			return '_'
		}
		return r
	}, name)
	if name == "" || name == "." {
		return "attachment"
	}
	return name
}

func writeJSONAtomic(path string, value any) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".manifest-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o600); err != nil {
		return err
	}
	return os.Rename(name, path)
}
