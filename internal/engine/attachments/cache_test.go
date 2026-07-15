package attachments

import (
	"context"
	"io"
	"os"
	"strings"
	"testing"
)

type fakeDownloader struct {
	data  map[string]string
	calls []string
}

func (f *fakeDownloader) DownloadAttachment(_ context.Context, id string) (io.ReadCloser, error) {
	f.calls = append(f.calls, id)
	return io.NopCloser(strings.NewReader(f.data[id])), nil
}

func TestCacheReconcileDownloadsRepairsAndRemovesImages(t *testing.T) {
	cache := NewCache(t.TempDir(), 1024)
	dl := &fakeDownloader{data: map[string]string{"1": "image-one", "2": "image-two"}}
	remote := []Metadata{
		{ID: "1", Filename: "../screen.png", MimeType: "image/png", Size: 9},
		{ID: "text", Filename: "notes.txt", MimeType: "text/plain", Size: 4},
	}

	if err := cache.Reconcile(context.Background(), "TEST-1", remote, dl); err != nil {
		t.Fatalf("first reconcile: %v", err)
	}
	items, err := cache.List("TEST-1")
	if err != nil || len(items) != 1 {
		t.Fatalf("items = %+v, err = %v", items, err)
	}
	if !strings.HasSuffix(items[0].Path, "1-screen.png") {
		t.Fatalf("unsafe or unexpected path: %s", items[0].Path)
	}

	if err := cache.Reconcile(context.Background(), "TEST-1", remote, dl); err != nil {
		t.Fatalf("unchanged reconcile: %v", err)
	}
	if len(dl.calls) != 1 {
		t.Fatalf("unchanged attachment downloaded again: %v", dl.calls)
	}

	if err := os.Remove(items[0].Path); err != nil {
		t.Fatal(err)
	}
	if err := cache.Reconcile(context.Background(), "TEST-1", remote, dl); err != nil {
		t.Fatalf("repair reconcile: %v", err)
	}
	if len(dl.calls) != 2 {
		t.Fatalf("missing attachment was not restored: %v", dl.calls)
	}

	replaced := []Metadata{{ID: "2", Filename: "screen.png", MimeType: "image/png", Size: 9}}
	if err := cache.Reconcile(context.Background(), "TEST-1", replaced, dl); err != nil {
		t.Fatalf("replacement reconcile: %v", err)
	}
	if _, err := os.Stat(items[0].Path); !os.IsNotExist(err) {
		t.Fatalf("removed attachment still exists: %v", err)
	}
}

func TestCacheRejectsOversizedAttachment(t *testing.T) {
	cache := NewCache(t.TempDir(), 4)
	dl := &fakeDownloader{data: map[string]string{"1": "12345"}}
	err := cache.Reconcile(context.Background(), "TEST-1", []Metadata{{ID: "1", Filename: "big.png", MimeType: "image/png", Size: 5}}, dl)
	if err == nil || !strings.Contains(err.Error(), "exceeds maximum") {
		t.Fatalf("error = %v", err)
	}
}
