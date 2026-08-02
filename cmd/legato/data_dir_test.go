package main

import (
	"context"
	"io"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/attachments"
)

type attachmentDownloader struct{}

func (attachmentDownloader) DownloadAttachment(context.Context, string) (io.ReadCloser, error) {
	return io.NopCloser(strings.NewReader("image")), nil
}

func TestRuntimeFileRootsUseExplicitDataDir(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "shared-data")
	cfg := &config.Config{DataDir: dataDir, DB: config.DBConfig{Path: filepath.Join(t.TempDir(), "database", "legato.db")}}

	cache := attachments.NewCache(attachmentRoot(cfg), 0)
	if err := cache.Reconcile(context.Background(), "TASK-1", []attachments.Metadata{{ID: "1", Filename: "image.png", MimeType: "image/png", Size: 5}}, attachmentDownloader{}); err != nil {
		t.Fatal(err)
	}
	cached, err := cache.List("TASK-1")
	if err != nil {
		t.Fatal(err)
	}
	if len(cached) != 1 || !strings.HasPrefix(cached[0].Path, filepath.Join(dataDir, "attachments")) {
		t.Fatalf("cached attachments = %#v", cached)
	}
	if got := workflowArtifactRoot(cfg); got != filepath.Join(dataDir, "artifacts") {
		t.Fatalf("artifact root = %q", got)
	}
}
