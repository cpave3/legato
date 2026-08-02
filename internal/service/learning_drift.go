package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

// IncorporationDriftResult describes the result of checking one verified
// incorporation. Outcome is one of unchanged, stale, or unverifiable.
type IncorporationDriftResult struct {
	IncorporationID string `json:"incorporation_id"`
	LearningID      string `json:"learning_id"`
	Target          string `json:"target,omitempty"`
	Outcome         string `json:"outcome"`
	Reason          string `json:"reason,omitempty"`
}

// CheckIncorporationDrift manually checks verified incorporations with local
// file targets. It never follows a target outside repositoryRoot. Semantic and
// unsafe references are reported as unverifiable and left untouched.
func (s *LearningService) CheckIncorporationDrift(ctx context.Context, repositoryRoot string) ([]IncorporationDriftResult, error) {
	root, err := safeRepositoryRoot(repositoryRoot)
	if err != nil {
		return nil, err
	}
	incorporations, err := s.store.ListVerifiedLearningIncorporations(ctx)
	if err != nil {
		return nil, err
	}
	results := make([]IncorporationDriftResult, 0, len(incorporations))
	for _, inc := range incorporations {
		result := IncorporationDriftResult{IncorporationID: inc.ID, LearningID: inc.LearningID, Target: inc.Target}
		path, reason, ok := resolveLocalTarget(root, inc.Target)
		destination := strings.ToLower(strings.TrimSpace(inc.Destination))
		if destination == "skill" || destination == "named_skill" || destination == "workflow" || destination == "named_workflow" {
			roots := []string{".claude/skills", ".opencode/skills"}
			if destination == "workflow" || destination == "named_workflow" {
				roots = []string{".legato/workflows"}
			}
			if resolved, resolveErr := verifiedNamedResource(root, inc.Target, roots); resolveErr == nil {
				path, reason, ok = resolved, "", true
			} else if errors.Is(resolveErr, errUnsafeIncorporationTarget) {
				path, reason, ok = "", resolveErr.Error(), false
			}
		}
		if !ok {
			result.Outcome, result.Reason = "unverifiable", reason
			results = append(results, result)
			continue
		}

		info, statErr := os.Stat(path)
		staleReason := ""
		switch {
		case errors.Is(statErr, os.ErrNotExist):
			staleReason = "target missing"
		case statErr != nil:
			result.Outcome, result.Reason = "unverifiable", "target cannot be read"
		case !info.Mode().IsRegular():
			result.Outcome, result.Reason = "unverifiable", "target is not a regular file"
		case inc.TargetDigest != "":
			matches, digestErr := targetDigestMatches(path, inc.TargetDigest)
			if digestErr != nil {
				result.Outcome, result.Reason = "unverifiable", digestErr.Error()
			} else if !matches {
				staleReason = "target digest mismatch"
			} else {
				result.Outcome = "unchanged"
			}
		default:
			result.Outcome = "unchanged"
		}

		if staleReason != "" {
			if err := s.store.MarkLearningIncorporationStale(ctx, inc.ID, inc.Revision); err != nil {
				if errors.Is(err, store.ErrConflict) {
					return nil, ErrLearningConflict
				}
				return nil, err
			}
			result.Outcome, result.Reason = "stale", staleReason
			s.publish(inc.LearningID, "incorporation_stale")
		}
		results = append(results, result)
	}
	return results, nil
}

func safeRepositoryRoot(repositoryRoot string) (string, error) {
	if strings.TrimSpace(repositoryRoot) == "" {
		return "", LearningValidationError("repository root is required")
	}
	absolute, err := filepath.Abs(repositoryRoot)
	if err != nil {
		return "", fmt.Errorf("resolve repository root: %w", err)
	}
	root, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", LearningValidationError("repository root cannot be resolved: %v", err)
	}
	info, err := os.Stat(root)
	if err != nil || !info.IsDir() {
		return "", LearningValidationError("repository root is not a directory")
	}
	return filepath.Clean(root), nil
}

func resolveLocalTarget(root, target string) (string, string, bool) {
	target = strings.TrimSpace(target)
	if target == "" || filepath.IsAbs(target) || strings.Contains(target, "://") || strings.ContainsAny(target, "#\x00") {
		return "", "target is not a concrete local file", false
	}
	clean := filepath.Clean(filepath.FromSlash(target))
	if clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.VolumeName(clean) != "" {
		return "", "target is not a safe repository-relative path", false
	}
	candidate := filepath.Join(root, clean)

	// Eval the target when it exists, or its nearest existing ancestor when it
	// does not. This prevents both existing and dangling symlinks from escaping.
	ancestor := candidate
	for {
		if _, err := os.Lstat(ancestor); err == nil {
			resolved, err := filepath.EvalSymlinks(ancestor)
			if err != nil {
				return "", "target path cannot be resolved safely", false
			}
			if !pathWithin(root, resolved) {
				return "", "target resolves outside repository root", false
			}
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", "target path cannot be resolved safely", false
		}
		parent := filepath.Dir(ancestor)
		if parent == ancestor || !pathWithin(root, parent) {
			return "", "target path cannot be resolved safely", false
		}
		ancestor = parent
	}
	return candidate, "", true
}

func pathWithin(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

func targetDigestMatches(path, expected string) (bool, error) {
	expected = strings.TrimSpace(expected)
	value := strings.TrimPrefix(expected, "sha256:")
	if len(value) != sha256.Size*2 {
		return false, fmt.Errorf("target digest is not a valid sha256 digest")
	}
	if _, err := hex.DecodeString(value); err != nil {
		return false, fmt.Errorf("target digest is not a valid sha256 digest")
	}
	file, err := os.Open(path)
	if err != nil {
		return false, fmt.Errorf("target cannot be read")
	}
	defer file.Close()
	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return false, fmt.Errorf("target cannot be read")
	}
	return strings.EqualFold(value, hex.EncodeToString(digest.Sum(nil))), nil
}
