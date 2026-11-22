package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"strings"

	"gopkg.in/yaml.v3"

	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
)

// The workflow file API: the web editor reads a workflow's file, checks
// what is typed, and creates, saves, copies and deletes files. The file's
// bytes are the source of truth: they are written as sent, and a save
// names the digest of the file it replaces.

// workflowFolderList is every folder workflows are read from, in
// increasing precedence (workflowengine.Folders).
func (s *Server) workflowFolderList(ctx context.Context) []workflowengine.Folder {
	return workflowengine.Folders(legacyWorkflowDirPath(), s.workflowGlobalDir, s.repositories(ctx), s.workflowProjectDir)
}

// writableFolders are the folders a workflow can be created in: all but
// the legacy one.
func (s *Server) writableFolders(ctx context.Context) []workflowengine.Folder {
	var out []workflowengine.Folder
	for _, folder := range s.workflowFolderList(ctx) {
		if !folder.Legacy {
			out = append(out, folder)
		}
	}
	return out
}

func (s *Server) folderNamed(ctx context.Context, id string) (workflowengine.Folder, bool) {
	if id == "" {
		id = "global"
	}
	for _, folder := range s.writableFolders(ctx) {
		if folder.ID == id {
			return folder, true
		}
	}
	return workflowengine.Folder{}, false
}

func (s *Server) findWorkflow(ctx context.Context, name string) *workflowengine.Definition {
	for _, definition := range s.discoverWorkflowsIn(ctx).Definitions {
		if definition.Name == name {
			return definition
		}
	}
	return nil
}

// writeWorkflowError is a refusal with a code the editor acts on.
func (s *Server) writeWorkflowError(w http.ResponseWriter, status int, code, message string, extra map[string]any) {
	body := map[string]any{"error": message, "code": code}
	for key, value := range extra {
		body[key] = value
	}
	s.writeJSON(w, status, body)
}

// checked is a file as the editor sends it, checked: its definition when
// it has no errors, and its diagnostics.
func checked(data string) (*workflowengine.Definition, []workflowengine.Diagnostic) {
	definition, diagnostics := workflowengine.Check("workflow.yaml", []byte(data))
	if diagnostics == nil {
		diagnostics = []workflowengine.Diagnostic{}
	}
	return definition, diagnostics
}

// newlyActiveTriggers are the triggers after would start runs by itself
// here that before did not: this Legato's switch if it has one, else the
// file's enabled.
func (s *Server) newlyActiveTriggers(ctx context.Context, before, after *workflowengine.Definition) ([]string, error) {
	active := func(definition *workflowengine.Definition, kind string) (bool, error) {
		if definition == nil {
			return false, nil
		}
		for _, trigger := range definition.On.List() {
			if trigger.Kind != kind {
				continue
			}
			if s.workflowTriggers != nil {
				enabled, ok, err := s.workflowTriggers.TriggerSwitch(ctx, definition.Name, kind)
				if err != nil || ok {
					return enabled, err
				}
			}
			return trigger.EnabledByDefault, nil
		}
		return false, nil
	}
	triggers := []string{}
	for _, trigger := range after.On.List() {
		now, err := active(after, trigger.Kind)
		if err != nil {
			return nil, err
		}
		was, err := active(before, trigger.Kind)
		if err != nil {
			return nil, err
		}
		if now && !was {
			triggers = append(triggers, trigger.Kind)
		}
	}
	return triggers, nil
}

// confirmTriggers refuses a save that would start triggers without the
// operator's confirmation, and says which.
func (s *Server) confirmTriggers(w http.ResponseWriter, r *http.Request, before, after *workflowengine.Definition, confirmed bool) bool {
	triggers, err := s.newlyActiveTriggers(r.Context(), before, after)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return false
	}
	if len(triggers) > 0 && !confirmed {
		s.writeWorkflowError(w, http.StatusConflict, "confirm_triggers",
			"this save switches on "+strings.Join(triggers, ", ")+": the trigger will start runs by itself. Save again with confirm_triggers to go ahead",
			map[string]any{"triggers": triggers})
		return false
	}
	return true
}

// workflowFileHandler serves /api/workflows/{name}/file: GET the file's
// bytes, PUT {yaml, digest, confirm_triggers} to save over it, DELETE
// {digest} to remove it.
func (s *Server) workflowFileHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		name := r.PathValue("name")
		definition := s.findWorkflow(ctx, name)
		if definition == nil {
			s.writeWorkflowError(w, http.StatusNotFound, "not_found", "no workflow "+name, nil)
			return
		}
		folders := s.workflowFolderList(ctx)
		info := workflowengine.FileFor(definition, folders)
		switch r.Method {
		case http.MethodGet:
			s.readWorkflowFile(w, definition, info)
		case http.MethodPut:
			s.saveWorkflowFile(w, r, definition, info)
		case http.MethodDelete:
			s.deleteWorkflowFile(w, r, definition, info)
		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func (s *Server) readWorkflowFile(w http.ResponseWriter, definition *workflowengine.Definition, info workflowengine.FileInfo) {
	out := map[string]any{"name": definition.Name, "path": info.Path, "folder": info.Folder, "editable": info.Editable, "reason": info.Reason}
	if info.Path == "" {
		// A built-in: its definition as YAML, to read or copy.
		data, err := yaml.Marshal(definition)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out["yaml"], out["digest"] = string(data), ""
		s.writeJSON(w, http.StatusOK, out)
		return
	}
	data, err := os.ReadFile(info.Path)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out["yaml"], out["digest"] = string(data), workflowengine.FileDigest(data)
	s.writeJSON(w, http.StatusOK, out)
}

func (s *Server) saveWorkflowFile(w http.ResponseWriter, r *http.Request, before *workflowengine.Definition, info workflowengine.FileInfo) {
	var body struct {
		YAML            string `json:"yaml"`
		Digest          string `json:"digest"`
		ConfirmTriggers bool   `json:"confirm_triggers"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		s.writeError(w, http.StatusBadRequest, `want {"yaml": "…", "digest": "sha256:…"}`)
		return
	}
	if !info.Editable {
		s.writeWorkflowError(w, http.StatusForbidden, "not_editable", info.Reason, nil)
		return
	}
	after, diagnostics := checked(body.YAML)
	if after == nil {
		s.writeWorkflowError(w, http.StatusUnprocessableEntity, "invalid", diagnostics[0].Message, map[string]any{"diagnostics": diagnostics})
		return
	}
	if after.Name != before.Name {
		s.writeWorkflowError(w, http.StatusUnprocessableEntity, "rename",
			"a workflow's name is fixed once it is made: keep name: "+before.Name+", or save it as a new workflow to give it another", nil)
		return
	}
	if !s.confirmTriggers(w, r, before, after, body.ConfirmTriggers) {
		return
	}
	data := []byte(body.YAML)
	if err := workflowengine.ReplaceFile(info.Path, data, body.Digest); errors.Is(err, workflowengine.ErrStaleFile) {
		current, _ := os.ReadFile(info.Path)
		s.writeWorkflowError(w, http.StatusConflict, "stale", "the file changed since it was opened: reload it, or keep editing and save again",
			map[string]any{"yaml": string(current), "digest": workflowengine.FileDigest(current)})
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged})
	s.writeJSON(w, http.StatusOK, map[string]any{"name": after.Name, "path": info.Path, "folder": info.Folder, "digest": workflowengine.FileDigest(data), "diagnostics": diagnostics})
}

func (s *Server) deleteWorkflowFile(w http.ResponseWriter, r *http.Request, definition *workflowengine.Definition, info workflowengine.FileInfo) {
	var body struct {
		Digest string `json:"digest"`
	}
	if json.NewDecoder(r.Body).Decode(&body) != nil {
		s.writeError(w, http.StatusBadRequest, `want {"digest": "sha256:…"}`)
		return
	}
	if !info.Editable {
		s.writeWorkflowError(w, http.StatusForbidden, "not_editable", info.Reason, nil)
		return
	}
	if err := workflowengine.RemoveFile(info.Path, body.Digest); errors.Is(err, workflowengine.ErrStaleFile) {
		s.writeWorkflowError(w, http.StatusConflict, "stale", "the file changed since it was opened: reload it before deleting it", nil)
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// The switches go with the workflow, unless a file it shadowed keeps
	// the name alive.
	remains := s.findWorkflow(r.Context(), definition.Name) != nil
	if !remains && s.workflowTriggers != nil {
		if err := s.workflowTriggers.ForgetTriggers(r.Context(), definition.Name); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged})
	s.writeJSON(w, http.StatusOK, map[string]any{"name": definition.Name, "removed": info.Path, "remains": remains})
}

// workflowFilesHandler serves POST /api/workflow-files {yaml, folder,
// confirm_triggers}: a new workflow file, never over a workflow of that
// name in any folder or built in.
func (s *Server) workflowFilesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			YAML            string `json:"yaml"`
			Folder          string `json:"folder"`
			ConfirmTriggers bool   `json:"confirm_triggers"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			s.writeError(w, http.StatusBadRequest, `want {"yaml": "…", "folder": "global"}`)
			return
		}
		ctx := r.Context()
		folder, ok := s.folderNamed(ctx, body.Folder)
		if !ok {
			s.writeWorkflowError(w, http.StatusBadRequest, "folder", "no workflows folder "+body.Folder+" to write to", nil)
			return
		}
		definition, diagnostics := checked(body.YAML)
		if definition == nil {
			s.writeWorkflowError(w, http.StatusUnprocessableEntity, "invalid", diagnostics[0].Message, map[string]any{"diagnostics": diagnostics})
			return
		}
		if err := workflowengine.ValidName(definition.Name); err != nil {
			s.writeWorkflowError(w, http.StatusUnprocessableEntity, "invalid", err.Error(), map[string]any{"diagnostics": diagnostics})
			return
		}
		if s.findWorkflow(ctx, definition.Name) != nil {
			s.writeWorkflowError(w, http.StatusConflict, "name_taken", "a workflow named "+definition.Name+" exists: choose another name", nil)
			return
		}
		if !s.confirmTriggers(w, r, nil, definition, body.ConfirmTriggers) {
			return
		}
		s.createWorkflowFile(w, folder, definition.Name, []byte(body.YAML), diagnostics)
	}
}

func (s *Server) createWorkflowFile(w http.ResponseWriter, folder workflowengine.Folder, name string, data []byte, diagnostics []workflowengine.Diagnostic) {
	path, err := workflowengine.CreateFile(folder.Path, name, data)
	if errors.Is(err, workflowengine.ErrWorkflowNameTaken) {
		s.writeWorkflowError(w, http.StatusConflict, "name_taken", err.Error(), nil)
		return
	} else if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.hub.Broadcast(WSMessage{Type: MsgWorkflowChanged})
	s.writeJSON(w, http.StatusCreated, map[string]any{"name": name, "path": path, "folder": folder.ID, "digest": workflowengine.FileDigest(data), "diagnostics": diagnostics})
}

// workflowFileCheckHandler serves POST /api/workflow-files/check {yaml}:
// the definition (when it has no errors), its graph and every diagnostic.
// It writes nothing.
func (s *Server) workflowFileCheckHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			YAML string `json:"yaml"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			s.writeError(w, http.StatusBadRequest, `want {"yaml": "…"}`)
			return
		}
		definition, diagnostics := checked(body.YAML)
		out := map[string]any{"definition": nil, "graph": nil, "diagnostics": diagnostics}
		if definition != nil {
			out["definition"], out["graph"] = definition, workflowengine.BuildGraph(definition)
		}
		s.writeJSON(w, http.StatusOK, out)
	}
}

// workflowCopyHandler serves POST /api/workflows/{name}/copy {folder}: a
// built-in workflow written to a folder as a file of the same name, which
// then replaces the built-in.
func (s *Server) workflowCopyHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		var body struct {
			Folder string `json:"folder"`
		}
		if r.ContentLength != 0 && json.NewDecoder(r.Body).Decode(&body) != nil {
			s.writeError(w, http.StatusBadRequest, `want {"folder": "global"}`)
			return
		}
		ctx := r.Context()
		name := r.PathValue("name")
		definition := s.findWorkflow(ctx, name)
		if definition == nil {
			s.writeWorkflowError(w, http.StatusNotFound, "not_found", "no workflow "+name, nil)
			return
		}
		if !strings.HasPrefix(definition.Source, "builtin:") {
			s.writeWorkflowError(w, http.StatusBadRequest, "not_builtin", name+" has a file already: edit it", nil)
			return
		}
		folder, ok := s.folderNamed(ctx, body.Folder)
		if !ok {
			s.writeWorkflowError(w, http.StatusBadRequest, "folder", "no workflows folder "+body.Folder+" to write to", nil)
			return
		}
		data, err := yaml.Marshal(definition)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		data = append([]byte("# A copy of Legato's built-in "+name+" workflow, which this file replaces.\n"), data...)
		copied, diagnostics := checked(string(data))
		if copied == nil {
			s.writeWorkflowError(w, http.StatusInternalServerError, "invalid", "the built-in does not make a valid file: "+diagnostics[0].Message, nil)
			return
		}
		s.createWorkflowFile(w, folder, name, data, diagnostics)
	}
}
