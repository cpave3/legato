package cli_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/swarm"
	"github.com/cpave3/legato/internal/testenv"
)

func TestMain(m *testing.M) {
	os.Exit(testenv.Run(m.Run))
}

func TestCLIEnvironmentIsIsolated(t *testing.T) {
	assertUnder(t, config.ResolveDBPath(&config.Config{}), os.Getenv("XDG_DATA_HOME"))
	assertUnder(t, ipc.SocketPath(), os.Getenv("XDG_RUNTIME_DIR"))

	legatoHome, err := swarm.LegatoHome()
	if err != nil {
		t.Fatal(err)
	}
	if legatoHome != os.Getenv("LEGATO_HOME") {
		t.Errorf("Legato home = %q, want isolated path %q", legatoHome, os.Getenv("LEGATO_HOME"))
	}
}

func assertUnder(t *testing.T, path, root string) {
	t.Helper()
	relative, err := filepath.Rel(root, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		t.Errorf("path %q is outside isolated root %q", path, root)
	}
}

func newTestIPCServer(t *testing.T, path string, callback func(ipc.Message)) *ipc.Server {
	t.Helper()
	server, err := ipc.NewServer(path, callback)
	if errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.EACCES) {
		t.Skipf("Unix sockets unavailable in restricted test environment: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	return server
}
