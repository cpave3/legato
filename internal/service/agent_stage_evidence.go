package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/chimera"
	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
)

// WatchedAgent is one running native agent whose event stream is followed:
// a slot of a task, with its execution and the folder it works in.
type WatchedAgent struct {
	// TaskID is the card's task; Slot the task's slot (store.SlotMain or a
	// named slot such as SlotReviewer).
	TaskID, Slot  string
	ExecutionID   string
	ExecutionKind string
	AgentKind     string
	// WorkingDir is the execution's working folder, "" when unknown.
	WorkingDir string
}

// AgentID is the agent's ID as lists name it: the task ID for its main
// slot, <task>_<slot> for a named one.
func (a WatchedAgent) AgentID() string { return AgentSlotKey(a.TaskID, a.Slot) }

// Place is the agent's stage place: its execution's for a workflow agent,
// else its task's slot.
func (a WatchedAgent) Place() WorkStagePlace {
	place := TaskPlace(a.TaskID, a.Slot)
	if a.ExecutionKind == store.ExecutionWorkflowAgent {
		place.PlaceExecutionID = a.ExecutionID
	}
	return place
}

func watchedAgentOf(s store.AgentSession) WatchedAgent {
	slot := s.Slot
	if slot == "" {
		slot = store.SlotMain
	}
	return WatchedAgent{
		TaskID: s.TaskID, Slot: slot, ExecutionID: s.ExecutionID, ExecutionKind: s.ExecutionKind,
		AgentKind: s.AgentKind, WorkingDir: s.WorkingDir,
	}
}

// editTools are the tool names, as native streams carry them, of calls that
// change files: the mapped names of a main loop (Claude Code's Edit and
// Write, Codex's fileChange as patch, Chimera's own), and Claude Code's raw
// names, which a subagent's calls keep.
var editTools = map[string]bool{
	"edit": true, "write": true, "patch": true,
	"Edit": true, "Write": true, "MultiEdit": true, "NotebookEdit": true,
}

// EditedPaths lists the files a native event says the agent is changing:
// a tool_call_start of an edit tool, the agent's own or a subagent's
// (inside subagent_event). Paths are as the tool named them, relative ones
// included. Nil for any other event.
func EditedPaths(event chimera.EventEnvelope) []string {
	if event.Type != "tool_call_start" && event.Type != "subagent_event" {
		return nil
	}
	var call struct {
		Type  string          `json:"type"`
		Name  string          `json:"name"`
		Args  map[string]any  `json:"args"`
		Event json.RawMessage `json:"event"`
	}
	if json.Unmarshal(event.Data, &call) != nil {
		return nil
	}
	if call.Type == "subagent_event" || event.Type == "subagent_event" {
		if len(call.Event) == 0 {
			return nil
		}
		return EditedPaths(chimera.EventEnvelope{Type: "tool_call_start", Data: call.Event})
	}
	if call.Type != "" && call.Type != "tool_call_start" {
		return nil
	}
	if !editTools[call.Name] {
		return nil
	}
	return pathsOfEdit(call.Args)
}

func pathsOfEdit(args map[string]any) []string {
	var paths []string
	add := func(v any) {
		if s, ok := v.(string); ok && strings.TrimSpace(s) != "" {
			paths = append(paths, strings.TrimSpace(s))
		}
	}
	for _, key := range []string{"path", "file_path", "notebook_path"} {
		add(args[key])
	}
	if changes, ok := args["changes"].([]any); ok {
		for _, change := range changes {
			if entry, ok := change.(map[string]any); ok {
				add(entry["path"])
				add(entry["movePath"])
			}
		}
	}
	for _, key := range []string{"patch", "input"} {
		if text, ok := args[key].(string); ok {
			paths = append(paths, patchFiles(text)...)
		}
	}
	return paths
}

// patchFileHeaders open a file's section in an apply_patch text.
var patchFileHeaders = []string{"*** Add File: ", "*** Update File: ", "*** Delete File: ", "*** Move to: "}

// patchFiles lists the files an apply_patch text changes.
func patchFiles(text string) []string {
	var files []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimRight(line, "\r")
		for _, header := range patchFileHeaders {
			if strings.HasPrefix(line, header) {
				if file := strings.TrimSpace(strings.TrimPrefix(line, header)); file != "" {
					files = append(files, file)
				}
			}
		}
	}
	return files
}

// InsideWorkingDir says whether path is a file inside dir: a relative path
// is read from dir, and dir itself is not inside. Without a dir nothing is.
func InsideWorkingDir(path, dir string) bool {
	if dir == "" || path == "" {
		return false
	}
	return relativeInside(path, dir) != ""
}

// relativeInside is path relative to dir when it is inside it, else "".
func relativeInside(path, dir string) string {
	dir = filepath.Clean(dir)
	if !filepath.IsAbs(path) {
		path = filepath.Join(dir, path)
	}
	rel, err := filepath.Rel(dir, filepath.Clean(path))
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return ""
	}
	return rel
}

// editReason is what the operator reads beside an Implementing stage set
// by edits: the first file inside dir, and how many more; "" when none is.
func editReason(paths []string, dir string) string {
	var inside []string
	for _, path := range paths {
		if rel := relativeInside(path, dir); rel != "" {
			inside = append(inside, filepath.ToSlash(rel))
		}
	}
	switch len(inside) {
	case 0:
		return ""
	case 1:
		return "edited " + inside[0]
	default:
		return fmt.Sprintf("edited %s and %d more", inside[0], len(inside)-1)
	}
}

// ObserveNativeEvent takes one event of a watched agent's stream: an edit
// inside its working folder is evidence (Implementing). It is the approval
// watcher's listener, so it never blocks for long.
func (s *WorkStageService) ObserveNativeEvent(agent WatchedAgent, event chimera.EventEnvelope) {
	paths := EditedPaths(event)
	if len(paths) == 0 || agent.TaskID == "" {
		return
	}
	reason := editReason(paths, agent.WorkingDir)
	if reason == "" {
		return
	}
	at := time.Now()
	if event.Timestamp > 0 {
		at = time.UnixMilli(event.Timestamp)
	}
	ctx := context.Background()
	readOnly, _ := s.store.ExecutionReadOnly(ctx, agent.ExecutionID)
	if _, err := s.EvidenceAt(ctx, agent.Place(), agent.ExecutionID, reason, readOnly, at); err != nil {
		log.Printf("stage evidence for %s: %v", agent.AgentID(), err)
	}
}

// RecordEditEvidence records, from a process apart from the host's (the
// broker's hook handler), that the agent of session edited path: evidence
// when path is inside its working folder (dir when the session names
// none). It tells running Legatos over IPC, as ReportWorkStage does.
func RecordEditEvidence(ctx context.Context, db *store.Store, session store.AgentSession, path, dir string) (bool, error) {
	if session.WorkingDir != "" {
		dir = session.WorkingDir
	}
	reason := editReason([]string{path}, dir)
	if reason == "" {
		return false, nil
	}
	readOnly, err := db.ExecutionReadOnly(ctx, session.ExecutionID)
	if err != nil {
		return false, err
	}
	place := SessionPlace(session)
	changed, err := NewWorkStageService(db, nil).EvidenceAt(ctx, place, session.ExecutionID, reason, readOnly, time.Now())
	if err != nil || !changed {
		return changed, err
	}
	current, err := db.GetWorkStage(ctx, place)
	if err != nil || current == nil {
		return changed, err
	}
	ipc.Broadcast(StageIPCMessage(*current))
	return true, nil
}
