package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

const maxWorkflowOutputBytes = 10 << 20

type WorkflowOutputRequest struct {
	RunID, StepID, AttemptID, TaskID, WorkingDir string
	Declared                                     map[string]workflowengine.Output
	Values, Artifacts                            map[string]string
}

type WorkflowOutputCollector struct {
	store   *store.Store
	dataDir string
}

func NewWorkflowOutputCollector(s *store.Store, dataDir string) *WorkflowOutputCollector {
	return &WorkflowOutputCollector{store: s, dataDir: dataDir}
}

func (c *WorkflowOutputCollector) Collect(ctx context.Context, req WorkflowOutputRequest) (map[string]any, error) {
	result := make(map[string]any, len(req.Declared))
	for key, spec := range req.Declared {
		if spec.Type == "file" {
			relative, ok := req.Artifacts[key]
			if !ok {
				return nil, fmt.Errorf("artifact output %q is required", key)
			}
			artifact, err := c.collectFile(ctx, req, key, spec, relative)
			if err != nil {
				return nil, fmt.Errorf("artifact %q: %w", key, err)
			}
			result[key] = map[string]any{"artifact_id": artifact.ID, "path": artifact.ManagedPath, "media_type": artifact.MediaType, "digest": artifact.Digest}
			continue
		}
		raw, ok := req.Values[key]
		if !ok {
			return nil, fmt.Errorf("output %q is required", key)
		}
		value, err := parseWorkflowOutput(spec.Type, raw)
		if err != nil {
			return nil, fmt.Errorf("output %q: %w", key, err)
		}
		result[key] = value
	}
	for key := range req.Values {
		if _, ok := req.Declared[key]; !ok {
			return nil, fmt.Errorf("output %q is not declared", key)
		}
	}
	for key := range req.Artifacts {
		if _, ok := req.Declared[key]; !ok {
			return nil, fmt.Errorf("artifact %q is not declared", key)
		}
	}
	return result, nil
}

func parseWorkflowOutput(valueType, raw string) (any, error) {
	switch valueType {
	case "string":
		return raw, nil
	case "integer":
		value, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			return nil, fmt.Errorf("must be an integer")
		}
		return value, nil
	case "boolean":
		value, err := strconv.ParseBool(raw)
		if err != nil {
			return nil, fmt.Errorf("must be a boolean")
		}
		return value, nil
	case "json":
		if len(raw) > maxWorkflowOutputBytes {
			return nil, fmt.Errorf("JSON exceeds size limit")
		}
		var value any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, fmt.Errorf("invalid JSON: %w", err)
		}
		if workflowJSONDepth(value) > 32 {
			return nil, fmt.Errorf("JSON exceeds depth limit")
		}
		return value, nil
	default:
		return nil, fmt.Errorf("unsupported type %q", valueType)
	}
}

func workflowJSONDepth(value any) int {
	max := 0
	switch value := value.(type) {
	case map[string]any:
		for _, child := range value {
			if depth := workflowJSONDepth(child); depth > max {
				max = depth
			}
		}
	case []any:
		for _, child := range value {
			if depth := workflowJSONDepth(child); depth > max {
				max = depth
			}
		}
	default:
		return 1
	}
	return max + 1
}

func (c *WorkflowOutputCollector) collectFile(ctx context.Context, req WorkflowOutputRequest, key string, spec workflowengine.Output, relative string) (*store.WorkflowArtifact, error) {
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path must stay within the working directory")
	}
	root, err := filepath.EvalSymlinks(req.WorkingDir)
	if err != nil {
		return nil, err
	}
	source, err := filepath.EvalSymlinks(filepath.Join(root, relative))
	if err != nil {
		return nil, err
	}
	pathRel, err := filepath.Rel(root, source)
	if err != nil || pathRel == ".." || strings.HasPrefix(pathRel, ".."+string(filepath.Separator)) {
		return nil, fmt.Errorf("path escapes the working directory")
	}
	info, err := os.Stat(source)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("path is not a regular file")
	}
	if info.Size() > maxWorkflowOutputBytes {
		return nil, fmt.Errorf("file exceeds size limit")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return nil, err
	}
	digest := sha256.Sum256(data)
	artifactID := workflowID("wfa-")
	destination := filepath.Join(c.dataDir, "workflows", req.RunID, req.StepID, req.AttemptID, artifactID+filepath.Ext(source))
	if err := os.MkdirAll(filepath.Dir(destination), 0o700); err != nil {
		return nil, err
	}
	tmp, err := os.CreateTemp(filepath.Dir(destination), ".artifact-*")
	if err != nil {
		return nil, err
	}
	if _, err = io.Copy(tmp, strings.NewReader(string(data))); err == nil {
		err = tmp.Chmod(0o600)
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(tmp.Name(), destination)
	} else {
		_ = os.Remove(tmp.Name())
	}
	if err != nil {
		return nil, err
	}
	mediaType := spec.MediaType
	if mediaType == "" {
		mediaType = "application/octet-stream"
	}
	artifact := &store.WorkflowArtifact{ID: artifactID, RunID: req.RunID, StepID: req.StepID, AttemptID: req.AttemptID, TaskID: req.TaskID, OutputKey: key, ManagedPath: destination, OriginalPath: relative, MediaType: mediaType, SizeBytes: info.Size(), Digest: "sha256:" + hex.EncodeToString(digest[:])}
	if err := c.store.InsertWorkflowArtifact(ctx, *artifact); err != nil {
		_ = os.Remove(destination)
		return nil, err
	}
	return artifact, nil
}
