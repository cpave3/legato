package service

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
)

type FileRemoveExecutor struct{}

func NewFileRemoveExecutor() *FileRemoveExecutor { return &FileRemoveExecutor{} }
func (e *FileRemoveExecutor) Type() string       { return "file.remove" }

func (e *FileRemoveExecutor) Start(_ context.Context, step ResolvedWorkflowStep) (WorkflowExecutionResult, error) {
	workingDir, err := workflowString(step.With, "working_dir", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	relative, err := workflowString(step.With, "path", true)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if filepath.IsAbs(relative) || filepath.Clean(relative) != relative || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return WorkflowExecutionResult{}, fmt.Errorf("path must stay within the working directory")
	}
	root, err := filepath.EvalSymlinks(workingDir)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	target := filepath.Join(root, relative)
	resolved, err := filepath.EvalSymlinks(target)
	if os.IsNotExist(err) {
		return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": false}}, nil
	}
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	pathRel, err := filepath.Rel(root, resolved)
	if err != nil || pathRel == ".." || strings.HasPrefix(pathRel, ".."+string(filepath.Separator)) {
		return WorkflowExecutionResult{}, fmt.Errorf("path escapes the working directory")
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return WorkflowExecutionResult{}, err
	}
	if !info.Mode().IsRegular() {
		return WorkflowExecutionResult{}, fmt.Errorf("path is not a regular file")
	}
	if err := os.Remove(resolved); err != nil {
		return WorkflowExecutionResult{}, err
	}
	return WorkflowExecutionResult{Status: "succeeded", Outputs: map[string]any{"removed": true}}, nil
}

func (e *FileRemoveExecutor) Cancel(context.Context, store.WorkflowStepAttempt) error { return nil }
