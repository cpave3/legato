package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func captureStderrWithCode(t *testing.T, run func() int) (string, int) {
	t.Helper()
	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w
	code := run()
	w.Close()
	os.Stderr = old
	buffer := new(bytes.Buffer)
	if _, err := buffer.ReadFrom(r); err != nil {
		t.Fatal(err)
	}
	r.Close()
	return buffer.String(), code
}

func TestParseCommandArgsRejectsUnknownAndDuplicateFlags(t *testing.T) {
	specs := map[string]flagSpec{"status": {}}
	if _, err := parseCommandArgs([]string{"--wat", "x"}, specs); err == nil || err.Code != "unknown_flag" {
		t.Fatalf("unknown flag error = %#v", err)
	}
	if _, err := parseCommandArgs([]string{"--status", "Doing", "--status=Done"}, specs); err == nil || err.Code != "duplicate_flag" {
		t.Fatalf("duplicate flag error = %#v", err)
	}
}

func TestReviewAndSwarmParsersRejectUnknownFlags(t *testing.T) {
	if _, _, _, err := parseReviewArgs([]string{"--wat", "value"}); err == nil {
		t.Fatal("review parser accepted unknown flag")
	}
	if _, _, _, err := parseMessageArgs([]string{"task-1", "message", "--wat"}); err == nil {
		t.Fatal("swarm message parser accepted unknown flag")
	}
	if _, _, err := parseSwarmCreateArgs([]string{"goal", "--wat"}); err == nil {
		t.Fatal("swarm create parser accepted unknown flag")
	}
}

func TestReadTextInputPreservesMultilineStdin(t *testing.T) {
	got, err := readTextInput("", false, "-", true, bytes.NewBufferString("one\n\ntwo\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got != "one\n\ntwo\n" {
		t.Fatalf("description = %q", got)
	}
}

func TestAgentPeersJSONListsOnlySameWorktreeAgents(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	repo := initCLIRepo(t)
	other := initCLIRepo(t)
	ctx := context.Background()
	for i, peer := range []struct{ task, execution, dir string }{{"caller", "exec-caller", repo}, {"peer", "exec-peer", repo}, {"other", "exec-other", other}} {
		execution := store.Execution{ID: peer.execution, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, ExecutionTaskID: peer.task, TaskTitle: peer.task, WorkingDir: peer.dir, StartedAt: time.Now().Add(time.Duration(i) * time.Second)}
		if err := s.CreateExecution(ctx, execution); err != nil {
			t.Fatal(err)
		}
		sessionID, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: peer.task, TmuxSession: "legato-" + peer.task, Command: "shell", Status: "running"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.LinkExecutionAgentSession(ctx, peer.execution, sessionID); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	t.Setenv("LEGATO_TASK_ID", "caller")
	t.Setenv("LEGATO_EXECUTION_ID", "exec-caller")
	fake := &contractTmux{alive: map[string]bool{"legato-caller": true, "legato-peer": true, "legato-other": true}}
	oldFactory := newAgentContractTmux
	newAgentContractTmux = func() (service.TmuxManager, error) { return fake, nil }
	t.Cleanup(func() { newAgentContractTmux = oldFactory })

	out := captureStdout(t, func() int { return runCLI([]string{"agent", "peers", "--json"}) })
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Peers []struct {
				TaskID string `json:"task_id"`
			} `json:"peers"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if !envelope.OK || len(envelope.Data.Peers) != 1 || envelope.Data.Peers[0].TaskID != "peer" {
		t.Fatalf("output = %s", out)
	}
}

func initCLIRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "init", "-q")
	cmd.Dir = dir
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	return dir
}

func TestAgentMessageJSONDeliversToAddressedTask(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if err := s.CreateExecution(ctx, store.Execution{ID: "exec-peer", Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, ExecutionTaskID: "peer", TaskTitle: "peer", WorkingDir: initCLIRepo(t), StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	sessionID, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "peer", TmuxSession: "legato-peer", Command: "shell", Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.LinkExecutionAgentSession(ctx, "exec-peer", sessionID); err != nil {
		t.Fatal(err)
	}
	s.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	fake := &contractTmux{alive: map[string]bool{"legato-peer": true}}
	oldFactory := newAgentContractTmux
	newAgentContractTmux = func() (service.TmuxManager, error) { return fake, nil }
	t.Cleanup(func() { newAgentContractTmux = oldFactory })

	out := captureStdout(t, func() int { return runCLI([]string{"agent", "message", "peer", "please coordinate", "--json"}) })
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Delivered bool   `json:"delivered"`
			TaskID    string `json:"task_id"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if !envelope.OK || !envelope.Data.Delivered || envelope.Data.TaskID != "peer" {
		t.Fatalf("output = %s", out)
	}
	if got := fake.messages["legato-peer"]; len(got) != 1 || got[0] != "please coordinate" {
		t.Fatalf("messages = %#v", fake.messages)
	}
}

func TestAgentBroadcastJSONReportsDeliveredAndFailedPeers(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	repo := initCLIRepo(t)
	ctx := context.Background()
	for i, taskID := range []string{"caller", "healthy", "failed"} {
		executionID := "exec-" + taskID
		if err := s.CreateExecution(ctx, store.Execution{ID: executionID, Kind: store.ExecutionInteractive, Status: store.ExecutionRunning, ExecutionTaskID: taskID, TaskTitle: taskID, WorkingDir: repo, StartedAt: time.Now().Add(time.Duration(i) * time.Second)}); err != nil {
			t.Fatal(err)
		}
		sessionID, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: taskID, TmuxSession: "legato-" + taskID, Command: "shell", Status: "running"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.LinkExecutionAgentSession(ctx, executionID, sessionID); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	t.Setenv("LEGATO_TASK_ID", "caller")
	t.Setenv("LEGATO_EXECUTION_ID", "exec-caller")
	fake := &contractTmux{
		alive:      map[string]bool{"legato-caller": true, "legato-healthy": true, "legato-failed": true},
		sendErrors: map[string]error{"legato-failed": errors.New("delivery failed")},
	}
	oldFactory := newAgentContractTmux
	newAgentContractTmux = func() (service.TmuxManager, error) { return fake, nil }
	t.Cleanup(func() { newAgentContractTmux = oldFactory })

	out := captureStdout(t, func() int { return runCLI([]string{"agent", "broadcast", "touching agent.go", "--json"}) })
	var envelope struct {
		OK   bool `json:"ok"`
		Data struct {
			Delivered []string `json:"delivered"`
			Failed    []struct {
				TaskID string `json:"task_id"`
			} `json:"failed"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("decode output: %v\n%s", err, out)
	}
	if !envelope.OK || len(envelope.Data.Delivered) != 1 || envelope.Data.Delivered[0] != "healthy" || len(envelope.Data.Failed) != 1 || envelope.Data.Failed[0].TaskID != "failed" {
		t.Fatalf("output = %s", out)
	}
	if got := fake.messages["legato-healthy"]; len(got) != 1 || got[0] != "touching agent.go" {
		t.Fatalf("messages = %#v", fake.messages)
	}
}

type contractTmux struct {
	alive      map[string]bool
	messages   map[string][]string
	sendErrors map[string]error
}

func (f *contractTmux) Spawn(string, string, int, int, ...string) error { return nil }
func (f *contractTmux) Kill(string) error                               { return nil }
func (f *contractTmux) Capture(string) (string, error)                  { return "", nil }
func (f *contractTmux) CaptureWithEscapes(string) (string, error)       { return "", nil }
func (f *contractTmux) Attach(string) *exec.Cmd                         { return exec.Command("true") }
func (f *contractTmux) ListSessions() ([]string, error) {
	var sessions []string
	for name, alive := range f.alive {
		if alive {
			sessions = append(sessions, name)
		}
	}
	return sessions, nil
}
func (f *contractTmux) IsAlive(name string) (bool, error) { return f.alive[name], nil }
func (f *contractTmux) SendKeys(string, string) error     { return nil }
func (f *contractTmux) SendKey(string, string) error      { return nil }
func (f *contractTmux) SendKeysLine(string, string) error { return nil }
func (f *contractTmux) SendKeysMultiline(name, payload string) error {
	if !f.alive[name] {
		return fmt.Errorf("session %s unavailable", name)
	}
	if err := f.sendErrors[name]; err != nil {
		return err
	}
	if f.messages == nil {
		f.messages = map[string][]string{}
	}
	f.messages[name] = append(f.messages[name], payload)
	return nil
}
func (f *contractTmux) SendKeysShellCommand(string, string) error { return nil }
func (f *contractTmux) PipeOutput(string) (io.Reader, func(), error) {
	return strings.NewReader(""), func() {}, nil
}
func (f *contractTmux) SetEnv(string, string, string) error      { return nil }
func (f *contractTmux) SetOption(string, string, string) error   { return nil }
func (f *contractTmux) PaneCommands() (map[string]string, error) { return nil, nil }

func TestTaskCommandRejectsUnsupportedConfiguredDatabaseDriver(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  driver: mysql\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)

	out, code := captureStderrWithCode(t, func() int {
		return runCLI([]string{"--json", "task", "list"})
	})
	if code != exitEnvironment {
		t.Fatalf("exit code = %d, want %d", code, exitEnvironment)
	}
	var envelope struct {
		OK    bool `json:"ok"`
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if envelope.OK || envelope.Error.Code != "database_unavailable" || envelope.Error.Message != `database driver "mysql" is not supported` {
		t.Fatalf("envelope = %#v", envelope)
	}
}

func TestTaskCreateUsesConfiguredWorkspaceDefaultAndPreservesOverrides(t *testing.T) {
	tests := []struct {
		name          string
		args          []string
		wantWorkspace string
	}{
		{name: "omitted workspace uses default", args: []string{"task", "create", "Default task", "--json"}, wantWorkspace: "Personal"},
		{name: "explicit workspace wins", args: []string{"task", "create", "Work task", "--workspace", "Work", "--json"}, wantWorkspace: "Work"},
		{name: "explicit none stays unassigned", args: []string{"task", "create", "Unassigned task", "--workspace", "none", "--json"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dbPath := filepath.Join(t.TempDir(), "legato.db")
			s, err := store.New(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			if err := s.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: "Backlog"}); err != nil {
				t.Fatal(err)
			}
			s.Close()

			configPath := filepath.Join(t.TempDir(), "config.yaml")
			configYAML := "db:\n  path: " + dbPath + "\nworkspaces:\n  - name: Work\n  - name: Personal\ndefault_workspace: Personal\n"
			if err := os.WriteFile(configPath, []byte(configYAML), 0o600); err != nil {
				t.Fatal(err)
			}
			t.Setenv("LEGATO_CONFIG", configPath)

			out := captureStdout(t, func() int { return runCLI(tt.args) })
			var envelope struct {
				OK   bool `json:"ok"`
				Data struct {
					ID string `json:"id"`
				} `json:"data"`
			}
			if err := json.Unmarshal([]byte(out), &envelope); err != nil {
				t.Fatalf("JSON: %v\n%s", err, out)
			}
			if !envelope.OK || envelope.Data.ID == "" {
				t.Fatalf("envelope = %#v", envelope)
			}

			s, err = store.New(dbPath)
			if err != nil {
				t.Fatal(err)
			}
			defer s.Close()
			workspaces, err := s.ListWorkspaces(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			var wantID *int
			for _, workspace := range workspaces {
				if workspace.Name == tt.wantWorkspace {
					id := workspace.ID
					wantID = &id
				}
			}
			created, err := s.GetTask(context.Background(), envelope.Data.ID)
			if err != nil {
				t.Fatal(err)
			}
			if tt.wantWorkspace == "" {
				if created.WorkspaceID != nil {
					t.Fatalf("WorkspaceID = %v, want nil", created.WorkspaceID)
				}
			} else if wantID == nil || created.WorkspaceID == nil || *created.WorkspaceID != *wantID {
				t.Fatalf("WorkspaceID = %v, want %v", created.WorkspaceID, wantID)
			}
		})
	}
}

func TestTaskUpdateJSONUsesEnvironmentTaskAndDescriptionFile(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "legato.db")
	s, err := store.New(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	for i, name := range []string{"Backlog", "Doing"} {
		if err := s.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: name, SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.CreateTask(context.Background(), store.Task{ID: "task-1", Title: "Before", Status: "Backlog"}); err != nil {
		t.Fatal(err)
	}
	s.Close()

	configPath := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(configPath, []byte("db:\n  path: "+dbPath+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	descriptionPath := filepath.Join(t.TempDir(), "description.md")
	const description = "# Details\n\nPreserve this newline.\n"
	if err := os.WriteFile(descriptionPath, []byte(description), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LEGATO_CONFIG", configPath)
	t.Setenv("LEGATO_TASK_ID", "task-1")

	out := captureStdout(t, func() int {
		return runCLI([]string{"--json", "task", "update", "--status", "Doing", "--description-file", descriptionPath})
	})
	var envelope struct {
		OK      bool   `json:"ok"`
		Command string `json:"command"`
		Data    struct {
			Status        string `json:"status"`
			DescriptionMD string `json:"description_md"`
		} `json:"data"`
	}
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatalf("JSON: %v\n%s", err, out)
	}
	if !envelope.OK || envelope.Command != "task.update" || envelope.Data.Status != "Doing" || envelope.Data.DescriptionMD != description {
		t.Fatalf("envelope = %#v", envelope)
	}
}
