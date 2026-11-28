package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeReleases serves a release server the way downloads.claude.ai lays one
// out: <channel> as text, <version>/manifest.json, <version>/<platform>/claude.
type fakeReleases struct {
	channels map[string]string
	binaries map[string][]byte // version → the linux-x64 binary
	// checksum, when set, is the manifest's checksum in place of the real one.
	checksum string
	requests []string
}

func (f *fakeReleases) server(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.requests = append(f.requests, r.URL.Path)
		parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")
		switch {
		case len(parts) == 1:
			if v, ok := f.channels[parts[0]]; ok {
				fmt.Fprintln(w, v)
				return
			}
		case len(parts) == 2 && parts[1] == "manifest.json":
			bin, ok := f.binaries[parts[0]]
			if !ok {
				break
			}
			sum := sha256.Sum256(bin)
			checksum := hex.EncodeToString(sum[:])
			if f.checksum != "" {
				checksum = f.checksum
			}
			fmt.Fprintf(w, `{"version":%q,"platforms":{"linux-x64":{"binary":"claude","checksum":%q,"size":%d}}}`, parts[0], checksum, len(bin))
			return
		case len(parts) == 3 && parts[1] == "linux-x64" && parts[2] == "claude":
			if bin, ok := f.binaries[parts[0]]; ok {
				_, _ = w.Write(bin)
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestLatestReadsAReleaseChannel(t *testing.T) {
	f := &fakeReleases{channels: map[string]string{"stable": "2.1.326", "latest": "2.1.332"}}
	r := Releases{Base: f.server(t).URL}
	for channel, want := range map[string]string{"stable": "2.1.326", "latest": "2.1.332"} {
		got, err := r.Latest(context.Background(), channel)
		if err != nil || got != want {
			t.Fatalf("Latest(%s) = %q, %v; want %q", channel, got, err, want)
		}
	}
}

func TestLatestRefusesWhatIsNotAVersion(t *testing.T) {
	f := &fakeReleases{channels: map[string]string{"stable": "<html>blocked</html>", "latest": ""}}
	r := Releases{Base: f.server(t).URL}
	for _, channel := range []string{"stable", "latest", "beta"} {
		if got, err := r.Latest(context.Background(), channel); err == nil {
			t.Fatalf("Latest(%s) = %q, want an error", channel, got)
		}
	}
}

func TestLatestRefusesAnUnknownChannel(t *testing.T) {
	r := Releases{Base: "http://127.0.0.1:1"}
	if _, err := r.Latest(context.Background(), "../x"); err == nil {
		t.Fatal("want an error for a channel that is not stable or latest")
	}
}

func TestFetchPutsAVerifiedBinaryInTheFolder(t *testing.T) {
	f := &fakeReleases{binaries: map[string][]byte{"2.1.326": []byte("#!/bin/sh\necho 2.1.326\n")}}
	r := Releases{Base: f.server(t).URL, Platform: "linux-x64"}
	dir := t.TempDir()
	path, err := r.Fetch(context.Background(), "2.1.326", dir)
	if err != nil {
		t.Fatal(err)
	}
	if path != filepath.Join(dir, "2.1.326") {
		t.Fatalf("path = %s", path)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm()&0o100 == 0 {
		t.Fatalf("binary not executable: %v %v", info, err)
	}
	if got, _ := os.ReadFile(path); string(got) != "#!/bin/sh\necho 2.1.326\n" {
		t.Fatalf("binary = %q", got)
	}
}

func TestFetchRefusesABinaryWhoseChecksumDiffers(t *testing.T) {
	f := &fakeReleases{binaries: map[string][]byte{"2.1.326": []byte("tampered")}, checksum: strings.Repeat("0", 64)}
	r := Releases{Base: f.server(t).URL, Platform: "linux-x64"}
	dir := t.TempDir()
	if _, err := r.Fetch(context.Background(), "2.1.326", dir); err == nil || !strings.Contains(err.Error(), "checksum") {
		t.Fatalf("err = %v, want a checksum error", err)
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Fatalf("left %d files behind", len(entries))
	}
}

func TestFetchRefusesWhatIsNotAVersion(t *testing.T) {
	r := Releases{Base: "http://127.0.0.1:1", Platform: "linux-x64"}
	for _, v := range []string{"../2.1.326", "latest", "", "2.1"} {
		if _, err := r.Fetch(context.Background(), v, t.TempDir()); err == nil {
			t.Fatalf("Fetch(%q) did not fail", v)
		}
	}
}

func TestFetchReportsAPlatformTheReleaseLacks(t *testing.T) {
	f := &fakeReleases{binaries: map[string][]byte{"2.1.326": []byte("x")}}
	r := Releases{Base: f.server(t).URL, Platform: "linux-riscv"}
	if _, err := r.Fetch(context.Background(), "2.1.326", t.TempDir()); err == nil || !strings.Contains(err.Error(), "linux-riscv") {
		t.Fatalf("err = %v, want one naming the platform", err)
	}
}
