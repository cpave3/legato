package service

import (
	"os"
	"testing"

	"github.com/cpave3/legato/internal/engine/gitrepo"
)

// The test binary is also the askpass helper the repository cache's git
// runs, as legato is.
func TestMain(m *testing.M) {
	if gitrepo.IsAskPass(os.Getenv) {
		os.Exit(gitrepo.AskPass(os.Args[1:], os.Stdout, os.Stderr))
	}
	os.Exit(m.Run())
}
