package dockerproxy

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Upstream is the daemon the proxy passes requests to: a Unix socket, or a
// TCP address without TLS.
type Upstream struct {
	Network string // "unix" or "tcp"
	Address string
}

// DefaultSocket is the daemon's socket when nothing names another.
const DefaultSocket = "unix:///var/run/docker.sock"

// ParseHost reads a DOCKER_HOST value.
func ParseHost(host string) (Upstream, error) {
	u, err := url.Parse(host)
	if err != nil {
		return Upstream{}, fmt.Errorf("docker host %q: %w", host, err)
	}
	switch u.Scheme {
	case "unix":
		path := u.Path
		if path == "" {
			path = u.Opaque
		}
		if !filepath.IsAbs(path) {
			return Upstream{}, fmt.Errorf("docker host %q: want an absolute socket path", host)
		}
		return Upstream{Network: "unix", Address: path}, nil
	case "tcp":
		if u.Host == "" {
			return Upstream{}, fmt.Errorf("docker host %q: want tcp://host:port", host)
		}
		return Upstream{Network: "tcp", Address: u.Host}, nil
	}
	return Upstream{}, fmt.Errorf("docker host %q: Legato's proxy speaks to a unix:// socket or a tcp:// address without TLS", host)
}

// FindHost is the daemon the person's own docker command uses: host when
// given, else DOCKER_HOST, else the current Docker context's (DOCKER_CONTEXT,
// or currentContext in ~/.docker/config.json), else the default socket.
func FindHost(host string, getenv func(string) string, home string) string {
	if host != "" {
		return host
	}
	if h := getenv("DOCKER_HOST"); h != "" {
		return h
	}
	configDir := getenv("DOCKER_CONFIG")
	if configDir == "" {
		configDir = filepath.Join(home, ".docker")
	}
	name := getenv("DOCKER_CONTEXT")
	if name == "" {
		var cfg struct {
			CurrentContext string `json:"currentContext"`
		}
		if data, err := os.ReadFile(filepath.Join(configDir, "config.json")); err == nil {
			_ = json.Unmarshal(data, &cfg)
		}
		name = cfg.CurrentContext
	}
	if name != "" && name != "default" {
		sum := sha256.Sum256([]byte(name))
		var meta struct {
			Endpoints map[string]struct {
				Host string `json:"Host"`
			} `json:"Endpoints"`
		}
		if data, err := os.ReadFile(filepath.Join(configDir, "contexts", "meta", hex.EncodeToString(sum[:]), "meta.json")); err == nil {
			if json.Unmarshal(data, &meta) == nil && meta.Endpoints["docker"].Host != "" {
				return meta.Endpoints["docker"].Host
			}
		}
	}
	return DefaultSocket
}

func (u Upstream) dial(ctx context.Context) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, u.Network, u.Address)
}

func (u Upstream) transport() *http.Transport {
	return &http.Transport{
		DialContext:         func(ctx context.Context, _, _ string) (net.Conn, error) { return u.dial(ctx) },
		MaxIdleConns:        8,
		IdleConnTimeout:     30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second,
		// Never compress: a stream must reach the client as it is written.
		DisableCompression: true,
	}
}

// Ping says whether the daemon answers.
func (u Upstream) Ping(ctx context.Context) error {
	client := &http.Client{Transport: u.transport(), Timeout: 5 * time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://docker/_ping", nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("the Docker daemon answered %s", resp.Status)
	}
	return nil
}

func (u Upstream) String() string {
	if u.Network == "unix" {
		return "unix://" + u.Address
	}
	return u.Network + "://" + strings.TrimPrefix(u.Address, "//")
}
