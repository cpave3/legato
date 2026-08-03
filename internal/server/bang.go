package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/service"
)

const (
	bangTimeout  = 30 * time.Second
	bangMaxBytes = 1024 * 1024 // 1 MiB
)

func (s *Server) chimeraBangHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		taskID := r.PathValue("task_id")
		if taskID == "" {
			http.Error(w, "task_id required", http.StatusBadRequest)
			return
		}

		var req struct {
			Command string `json:"command"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
		if req.Command == "" {
			http.Error(w, "command required", http.StatusBadRequest)
			return
		}

		endpoint, sessionID, err := s.getChimeraEndpoint(r, taskID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusServiceUnavailable)
			return
		}

		client := chimera.NewClient(endpoint)
		snap, err := client.GetSession(r.Context(), sessionID)
		if err != nil {
			http.Error(w, "chimera snapshot: "+err.Error(), http.StatusBadGateway)
			return
		}
		if snap.CWD == "" {
			http.Error(w, "chimera session has no cwd", http.StatusBadGateway)
			return
		}

		environments, ok := s.agents.(service.AgentEnvironmentProvider)
		if !ok {
			http.Error(w, "agent environment is unavailable", http.StatusServiceUnavailable)
			return
		}
		env, err := environments.GetAgentEnvironment(r.Context(), taskID)
		if err != nil {
			http.Error(w, "agent environment: "+err.Error(), http.StatusServiceUnavailable)
			return
		}
		result := executeBang(r.Context(), req.Command, snap.CWD, env)

		formatted := formatBangResult(req.Command, result)
		_ = client.PostMessageAppend(context.Background(), sessionID, formatted)

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	}
}

type bangResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	TimedOut bool   `json:"timed_out"`
}

func executeBang(ctx context.Context, command, cwd string, env []string) bangResult {
	ctx, cancel := context.WithTimeout(ctx, bangTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, "sh", "-c", command)
	cmd.Dir = cwd
	cmd.Env = append(os.Environ(), env...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	result := bangResult{
		Stdout:   truncateOutput(stdout.String()),
		Stderr:   truncateOutput(stderr.String()),
		ExitCode: 0,
	}
	if ctx.Err() != nil {
		result.TimedOut = true
		result.ExitCode = -1
	} else if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			result.ExitCode = exitErr.ExitCode()
		} else {
			result.ExitCode = 1
		}
	}
	return result
}

func truncateOutput(s string) string {
	if len(s) > bangMaxBytes {
		return s[:bangMaxBytes] + "\n(truncated: output exceeded 1 MiB limit)"
	}
	return s
}

func formatBangResult(command string, result bangResult) string {
	summary := fmt.Sprintf("!%s (exit %d%s)", command, result.ExitCode, func() string {
		if result.TimedOut {
			return ", timed out"
		}
		return ""
	}())
	lines := []string{summary}
	if result.Stdout != "" {
		lines = append(lines, result.Stdout)
	}
	if result.Stderr != "" {
		lines = append(lines, result.Stderr)
	}
	if result.Stdout == "" && result.Stderr == "" {
		lines = append(lines, "(no output)")
	}
	return strings.Join(lines, "\n")
}

var _ = io.EOF
