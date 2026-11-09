package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

// ProjectIdentity is a stable repository scope for project learning. Key is
// suitable for persistence; Label is its human-readable repository identity.
type ProjectIdentity struct {
	Key   string
	Label string
}

// ProjectIdentityResolver resolves repository scope without using branch or
// worktree paths as project identity.
type ProjectIdentityResolver struct {
	inspector CheckoutInspector
}

func NewProjectIdentityResolver(inspector CheckoutInspector) *ProjectIdentityResolver {
	return &ProjectIdentityResolver{inspector: inspector}
}

// Resolve prefers durable CodeContext identity, then identity inspected from
// the checkout, and finally the checkout's Git common directory.
func (r *ProjectIdentityResolver) Resolve(codeContext *store.CodeContext, directory string) (ProjectIdentity, error) {
	if codeContext != nil {
		if repo, ok := normalizeRepository(codeContext.Repo); ok {
			return repositoryIdentity(repo), nil
		}
	}

	var inspectErr error
	if r != nil && r.inspector != nil && strings.TrimSpace(directory) != "" {
		inspected, err := r.inspector.InspectCodeContext(directory)
		if err != nil {
			inspectErr = err
		} else if inspected != nil {
			if repo, ok := normalizeRepository(inspected.Repo); ok {
				return repositoryIdentity(repo), nil
			}
		}
	}

	commonDir, err := gitCommonDirectory(directory)
	if err == nil {
		digest := sha256.Sum256([]byte(commonDir))
		return ProjectIdentity{Key: "git-common-dir:sha256:" + hex.EncodeToString(digest[:])}, nil
	}
	if inspectErr != nil {
		return ProjectIdentity{}, fmt.Errorf("project identity unresolved: checkout inspection: %v; git common directory: %w", inspectErr, err)
	}
	return ProjectIdentity{}, fmt.Errorf("project identity unresolved: %w", err)
}

// ResolveProjectIdentity is a convenience wrapper around ProjectIdentityResolver.
func ResolveProjectIdentity(codeContext *store.CodeContext, directory string, inspector CheckoutInspector) (ProjectIdentity, error) {
	return NewProjectIdentityResolver(inspector).Resolve(codeContext, directory)
}

func repositoryIdentity(repo string) ProjectIdentity {
	return ProjectIdentity{Key: "repo:" + repo, Label: repo}
}

func normalizeRepository(raw string) (string, bool) {
	return config.NormalizeRepository(raw)
}

func gitCommonDirectory(directory string) (string, error) {
	if strings.TrimSpace(directory) == "" {
		return "", errors.New("checkout directory is empty")
	}
	cmd := exec.Command("git", "rev-parse", "--git-common-dir")
	cmd.Dir = directory
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("git rev-parse --git-common-dir: %w: %s", err, strings.TrimSpace(string(output)))
	}
	commonDir := strings.TrimSpace(string(output))
	if commonDir == "" {
		return "", errors.New("git returned an empty common directory")
	}
	if !filepath.IsAbs(commonDir) {
		commonDir = filepath.Join(directory, commonDir)
	}
	commonDir, err = filepath.Abs(commonDir)
	if err != nil {
		return "", fmt.Errorf("resolve Git common directory: %w", err)
	}
	commonDir = filepath.Clean(commonDir)
	if evaluated, evalErr := filepath.EvalSymlinks(commonDir); evalErr == nil {
		commonDir = evaluated
	}
	return commonDir, nil
}
