package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

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

var repositoryPart = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9._-]*[a-z0-9])?$`)

func normalizeRepository(raw string) (string, bool) {
	repo := strings.TrimSpace(raw)
	if repo == "" {
		return "", false
	}

	// Convert the common SCP-style Git remote into URL form before parsing.
	if strings.HasPrefix(repo, "git@") && !strings.Contains(repo, "://") {
		parts := strings.SplitN(strings.TrimPrefix(repo, "git@"), ":", 2)
		if len(parts) != 2 {
			return "", false
		}
		repo = parts[0] + "/" + parts[1]
	} else if strings.Contains(repo, "://") {
		u, err := url.Parse(repo)
		if err != nil || u.Hostname() == "" {
			return "", false
		}
		repo = u.Hostname() + "/" + strings.TrimPrefix(u.Path, "/")
	}

	repo = strings.TrimSuffix(strings.TrimSuffix(repo, "/"), ".git")
	parts := strings.Split(repo, "/")
	if len(parts) == 2 {
		parts = append([]string{"github.com"}, parts...)
	}
	if len(parts) != 3 {
		return "", false
	}
	for i, part := range parts {
		parts[i] = strings.ToLower(strings.TrimSpace(part))
		if !repositoryPart.MatchString(parts[i]) {
			return "", false
		}
	}
	return strings.Join(parts, "/"), true
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
