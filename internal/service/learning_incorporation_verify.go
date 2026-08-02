package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

var errUnsafeIncorporationTarget = errors.New("unsafe incorporation target")

// VerifyIncorporationTarget verifies a pending incorporation from local,
// deterministic state and records the verdict. explicitConfirm is deliberately
// used only by destinations whose result cannot be inferred from repository
// state (currently test_or_lint).
func (s *LearningService) VerifyIncorporationTarget(ctx context.Context, id string, revision int, repositoryRoot string, explicitConfirm bool) (*store.LearningIncorporation, error) {
	if revision <= 0 {
		return nil, LearningValidationError("revision must be a positive integer")
	}
	inc, err := s.store.GetLearningIncorporation(ctx, id)
	if err != nil {
		return nil, err
	}
	if inc.Revision != revision || inc.Status != "verification_pending" {
		return nil, ErrLearningConflict
	}

	passed, _, targetDigest, err := s.verifyIncorporationTarget(ctx, inc, repositoryRoot, explicitConfirm)
	if err != nil {
		if errors.Is(err, errUnsafeIncorporationTarget) {
			return nil, LearningValidationError("%v", err)
		}
		return nil, err
	}
	if err := s.store.VerifyLearningIncorporationTarget(ctx, id, revision, passed, targetDigest); err != nil {
		if errors.Is(err, store.ErrConflict) {
			return nil, ErrLearningConflict
		}
		return nil, err
	}
	verified, err := s.store.GetLearningIncorporation(ctx, id)
	if err == nil {
		s.publish(verified.LearningID, "verification_completed")
	}
	return verified, err
}

// verifyIncorporationTarget returns a human-readable decision summary and, for
// file-backed destinations, the digest of the content that actually passed.
func (s *LearningService) verifyIncorporationTarget(ctx context.Context, inc *store.LearningIncorporation, repositoryRoot string, explicitConfirm bool) (bool, string, string, error) {
	destination := strings.ToLower(strings.TrimSpace(inc.Destination))
	switch destination {
	case "existing_source", "documentation", "agents_file", "claude_file":
		path, err := verifiedRepositoryFile(repositoryRoot, inc.Target, nil)
		if err != nil {
			if errors.Is(err, errUnsafeIncorporationTarget) {
				return false, "", "", err
			}
			return false, err.Error(), "", nil
		}
		return verifyFileTargetEvidence(path, inc)
	case "skill", "named_skill", "workflow", "named_workflow":
		roots := []string{".claude/skills", ".opencode/skills"}
		if destination == "workflow" || destination == "named_workflow" {
			roots = []string{".legato/workflows"}
		}
		path, err := verifiedNamedResource(repositoryRoot, inc.Target, roots)
		if err != nil {
			if errors.Is(err, errUnsafeIncorporationTarget) {
				return false, "", "", err
			}
			return false, err.Error(), "", nil
		}
		return verifyFileTargetEvidence(path, inc)
	case "prompt_profile":
		view, err := s.store.GetLearningView(ctx, inc.LearningID)
		if err != nil {
			return false, "learning cannot be read", "", err
		}
		approved := strings.TrimSpace(inc.Target)
		if approved == "" {
			return false, "prompt profile approved wording is required", "", nil
		}
		if approved != strings.TrimSpace(view.Learning.Claim) {
			return false, "prompt profile wording does not match the approved learning", "", nil
		}
		return true, "approved prompt profile wording matches", "", nil
	case "test_or_lint":
		if !explicitConfirm {
			return false, "test or lint result requires explicit confirmation", "", nil
		}
		return true, "test or lint result explicitly confirmed", "", nil
	default:
		return false, "destination requires trusted user verification", "", nil
	}
}

func verifiedRepositoryFile(repositoryRoot, target string, allowedRoots []string) (string, error) {
	root, err := safeRepositoryRoot(repositoryRoot)
	if err != nil {
		return "", err
	}
	path, reason, ok := resolveLocalTarget(root, target)
	if !ok {
		return "", fmt.Errorf("%w: %s", errUnsafeIncorporationTarget, reason)
	}
	if len(allowedRoots) > 0 {
		allowed := false
		for _, relativeRoot := range allowedRoots {
			if pathWithin(filepath.Join(root, filepath.FromSlash(relativeRoot)), path) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("%w: target is outside allowed resource roots", errUnsafeIncorporationTarget)
		}
	}
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("target missing")
	}
	if err != nil {
		return "", fmt.Errorf("target cannot be read")
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("target is not a regular file")
	}
	return path, nil
}

func verifiedNamedResource(repositoryRoot, target string, roots []string) (string, error) {
	target = strings.TrimSpace(target)
	if target == "" || filepath.IsAbs(target) || strings.ContainsAny(target, "\x00#") || strings.Contains(target, "://") {
		return "", fmt.Errorf("%w: target is not a safe named resource", errUnsafeIncorporationTarget)
	}
	// A path is accepted only when it is already under one of the safe roots.
	if strings.ContainsAny(target, `/\\`) {
		return verifiedRepositoryFile(repositoryRoot, target, roots)
	}
	root, err := safeRepositoryRoot(repositoryRoot)
	if err != nil {
		return "", err
	}
	for _, safeRoot := range roots {
		base := filepath.Join(root, filepath.FromSlash(safeRoot))
		candidates := []string{filepath.Join(base, target), filepath.Join(base, target+".yaml"), filepath.Join(base, target+".yml")}
		for _, candidate := range candidates {
			info, statErr := os.Stat(candidate)
			if statErr == nil {
				if info.IsDir() {
					for _, name := range []string{"SKILL.md", "skill.md"} {
						file := filepath.Join(candidate, name)
						if fileInfo, fileErr := os.Stat(file); fileErr == nil && fileInfo.Mode().IsRegular() {
							return file, nil
						}
					}
				} else if info.Mode().IsRegular() {
					return candidate, nil
				}
			}
		}
	}
	return "", fmt.Errorf("named resource missing")
}

func verifyFileTargetEvidence(path string, inc *store.LearningIncorporation) (bool, string, string, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return false, "target cannot be read", "", nil
	}
	actualDigest := sha256Digest(content)
	matchesDigest := func(expected string) (bool, error) {
		value := strings.TrimPrefix(strings.TrimSpace(expected), "sha256:")
		if len(value) != sha256.Size*2 {
			return false, fmt.Errorf("target digest is not a valid sha256 digest")
		}
		if _, err := hex.DecodeString(value); err != nil {
			return false, fmt.Errorf("target digest is not a valid sha256 digest")
		}
		return strings.EqualFold("sha256:"+value, actualDigest), nil
	}

	if strings.TrimSpace(inc.TargetDigest) != "" {
		matches, err := matchesDigest(inc.TargetDigest)
		if err != nil {
			return false, err.Error(), "", nil
		}
		if !matches {
			return false, "target digest mismatch", "", nil
		}
		return true, "target exists and digest matches", actualDigest, nil
	}

	kind := strings.ToLower(strings.TrimSpace(inc.VerificationKind))
	switch kind {
	case "exact_content", "content":
		if string(content) != inc.VerificationResult {
			return false, "target content mismatch", "", nil
		}
		return true, "target exists and exact content matches", actualDigest, nil
	case "digest", "sha256":
		expected := strings.TrimSpace(inc.VerificationResult)
		if !strings.HasPrefix(expected, "sha256:") {
			expected = "sha256:" + expected
		}
		matches, err := matchesDigest(expected)
		if err != nil {
			return false, err.Error(), "", nil
		}
		if !matches {
			return false, "target digest mismatch", "", nil
		}
		return true, "target exists and digest matches", actualDigest, nil
	default:
		return true, "target exists", actualDigest, nil
	}
}

// sha256Digest is kept small and local so callers/tests can produce the same
// canonical value without depending on platform path behavior.
func sha256Digest(content []byte) string {
	digest := sha256.Sum256(content)
	return "sha256:" + hex.EncodeToString(digest[:])
}
