// Package claudecode fetches Claude Code's native builds from Anthropic's
// release server, as Anthropic's installer (https://claude.ai/install.sh)
// does, and keeps versions of it in a folder of their own with a link to
// the one in use. It knows nothing of Legato, and never touches the
// operator's own installation (~/.local/bin/claude,
// ~/.local/share/claude).
package claudecode

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// DefaultBase is the release server the installer reads.
const DefaultBase = "https://downloads.claude.ai/claude-code-releases"

// Channels are the release channels the server publishes.
var Channels = []string{"stable", "latest"}

// versionPattern is what the installer accepts as a version, with the
// suffix kept to characters that are safe in a file name.
var versionPattern = regexp.MustCompile(`^[0-9]+\.[0-9]+\.[0-9]+(-[0-9A-Za-z.]+)?$`)

// ValidVersion reports whether v is a version such as 2.1.326.
func ValidVersion(v string) bool { return versionPattern.MatchString(v) }

// maxManifest bounds a manifest or a channel's answer.
const maxManifest = 1 << 20

// Releases reads Claude Code's release server.
type Releases struct {
	// Base is the server; "" for DefaultBase.
	Base string
	// Platform is the build to fetch, such as linux-x64; "" for this
	// machine's (Platform).
	Platform string
	// HTTP is the client; nil for http.DefaultClient.
	HTTP *http.Client
}

// Latest is the version a channel (stable or latest) points at.
func (r Releases) Latest(ctx context.Context, channel string) (string, error) {
	if channel != "stable" && channel != "latest" {
		return "", fmt.Errorf("unknown release channel %q: stable or latest", channel)
	}
	body, err := r.get(ctx, channel)
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(body))
	if !ValidVersion(v) {
		return "", fmt.Errorf("the release server's %s channel answered something that is not a version", channel)
	}
	return v, nil
}

// Fetch downloads a version's binary for the platform into dir as
// dir/<version>, checked against the release's manifest (its SHA-256 and
// size), and returns its path. Nothing is left in dir when it fails.
func (r Releases) Fetch(ctx context.Context, version, dir string) (string, error) {
	if !ValidVersion(version) {
		return "", fmt.Errorf("%q is not a Claude Code version", version)
	}
	platform := r.Platform
	if platform == "" {
		var err error
		if platform, err = Platform(); err != nil {
			return "", err
		}
	}
	raw, err := r.get(ctx, version+"/manifest.json")
	if err != nil {
		return "", err
	}
	var manifest struct {
		Platforms map[string]struct {
			Checksum string `json:"checksum"`
			Size     int64  `json:"size"`
		} `json:"platforms"`
	}
	if err := json.Unmarshal(raw, &manifest); err != nil {
		return "", fmt.Errorf("the manifest of Claude Code %s is not JSON: %w", version, err)
	}
	want, ok := manifest.Platforms[platform]
	if !ok || len(want.Checksum) != 64 || want.Size <= 0 {
		return "", fmt.Errorf("Claude Code %s has no build for %s", version, platform)
	}
	resp, err := r.do(ctx, version+"/"+platform+"/claude")
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, "."+version+".*.part")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	hash := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, hash), io.LimitReader(resp.Body, want.Size+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return "", fmt.Errorf("downloading Claude Code %s: %w", version, err)
	}
	if n != want.Size || hex.EncodeToString(hash.Sum(nil)) != strings.ToLower(want.Checksum) {
		return "", fmt.Errorf("Claude Code %s failed its checksum: the download is not the release", version)
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, version)
	if err := os.Rename(tmp.Name(), path); err != nil {
		return "", err
	}
	return path, nil
}

func (r Releases) get(ctx context.Context, path string) ([]byte, error) {
	resp, err := r.do(ctx, path)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, maxManifest))
}

func (r Releases) do(ctx context.Context, path string) (*http.Response, error) {
	base := r.Base
	if base == "" {
		base = DefaultBase
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(base, "/")+"/"+path, nil)
	if err != nil {
		return nil, err
	}
	client := r.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("the Claude Code release server answered %s for %s", resp.Status, path)
	}
	return resp, nil
}

// ErrUnsupportedPlatform is a machine Claude Code publishes no native build
// for.
var ErrUnsupportedPlatform = errors.New("Claude Code publishes no native build for this machine")

// Platform is this machine's build name, as the installer works it out:
// linux-x64, linux-arm64 (with -musl on a musl system), darwin-x64 or
// darwin-arm64.
func Platform() (string, error) {
	var arch string
	switch runtime.GOARCH {
	case "amd64":
		arch = "x64"
	case "arm64":
		arch = "arm64"
	default:
		return "", ErrUnsupportedPlatform
	}
	switch runtime.GOOS {
	case "darwin":
		return "darwin-" + arch, nil
	case "linux":
		for _, lib := range []string{"/lib/libc.musl-x86_64.so.1", "/lib/libc.musl-aarch64.so.1"} {
			if _, err := os.Stat(lib); err == nil {
				return "linux-" + arch + "-musl", nil
			}
		}
		return "linux-" + arch, nil
	}
	return "", ErrUnsupportedPlatform
}
