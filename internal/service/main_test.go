package service

import (
	"os"
	"testing"

	"github.com/cpave3/legato/internal/engine/gitrepo"
	"github.com/cpave3/legato/internal/testenv"
)

// The test binary is also the askpass helper the repository cache's git
// runs, as legato is.
func TestMain(m *testing.M) {
	if gitrepo.IsAskPass(os.Getenv) {
		os.Exit(gitrepo.AskPass(os.Args[1:], os.Stdout, os.Stderr))
	}
	// The wall tests' launcher (`legato sandbox exec` on macOS) and their
	// helper inside the sandbox.
	if os.Getenv(wallLauncherEnv) == "1" && len(os.Args) > 3 && os.Args[1] == "sandbox" && os.Args[2] == "exec" {
		os.Exit(wallLaunch(os.Args[3:]))
	}
	if mode := os.Getenv(wallHelperEnv); mode != "" {
		os.Exit(wallHelper(mode, os.Args[1:]))
	}
	// Away from the agent session the tests may run in: its GitHub
	// token, its gh wrapper's real gh, its broker.
	os.Exit(testenv.Run(m.Run))
}
