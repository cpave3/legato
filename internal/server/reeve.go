package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ReeveSessions is the Reeve's sessions (service.ReeveService).
type ReeveSessions interface {
	Overview(ctx context.Context) (service.ReeveOverview, error)
	Start(ctx context.Context, in service.ReeveStart) (*store.ReeveSession, error)
	End(ctx context.Context, taskID string) error
	Resume(ctx context.Context, taskID string, in service.ReeveStart) (*store.ReeveSession, error)
	Delete(ctx context.Context, taskID string) error
	Authenticate(ctx context.Context, token string) (service.ReeveIdentity, error)
	Message(ctx context.Context, text, by string) error
	// StopAgent ends the current session when taskID is its agent's.
	StopAgent(ctx context.Context, taskID string) (bool, error)
}

type reeveKey struct{}

// ReeveCaller is the Reeve session a request was made for, when it came
// with a Reeve API token.
func ReeveCaller(ctx context.Context) (service.ReeveIdentity, bool) {
	id, ok := ctx.Value(reeveKey{}).(service.ReeveIdentity)
	return id, ok
}

// SetReeveService serves the Reeve's sessions and accepts its API tokens.
func (s *Server) SetReeveService(reeve ReeveSessions) { s.reeve = reeve }

func (s *Server) registerReeveRoutes(mux *routeMux) {
	mux.HandleFunc("GET /api/reeve", s.reeveOverviewHandler())
	mux.HandleFunc("POST /api/reeve/sessions", s.reeveStartHandler())
	mux.HandleFunc("POST /api/reeve/sessions/{task_id}/end", s.reeveEndHandler())
	mux.HandleFunc("POST /api/reeve/sessions/{task_id}/resume", s.reeveResumeHandler())
	mux.HandleFunc("DELETE /api/reeve/sessions/{task_id}", s.reeveDeleteHandler())
	mux.HandleFunc("POST /api/reeve/message", s.reeveMessageHandler())
	mux.HandleFunc("GET /api/reeve/sessions/{task_id}/change-sets", s.reeveChangeSetsHandler())
	mux.HandleFunc("POST /api/reeve/secrets/{approval_id}", s.reeveSecretHandler())
	mux.HandleFunc("GET /api/reeve/voice", s.reeveVoiceStatusHandler())
	mux.HandleFunc("GET "+voiceSessionPath, s.reeveVoiceSessionHandler())
	mux.HandleFunc("GET /api/reeve/engram", s.reeveEngramHandler())
	mux.HandleFunc("PUT /api/reeve/engram/seed", s.reeveSeedHandler())
	mux.HandleFunc("DELETE /api/reeve/engram/memories/{id}", s.reeveEngramRemoveHandler(ReeveEngramKeeper.RemoveMemory))
	mux.HandleFunc("DELETE /api/reeve/engram/threads/{id}", s.reeveEngramRemoveHandler(ReeveEngramKeeper.CloseThread))
}

// serveReeveToken lets a Reeve API token reach the read routes only: a
// change goes to the person as a change set, and never routes are refused
// whatever is approved.
func (s *Server) serveReeveToken(w http.ResponseWriter, r *http.Request, next http.Handler, token string) {
	if s.reeve == nil {
		writeIntegrationError(w, http.StatusUnauthorized, "unauthorized", "unknown Reeve token")
		return
	}
	id, err := s.reeve.Authenticate(r.Context(), token)
	if err != nil {
		writeIntegrationError(w, http.StatusUnauthorized, "unauthorized", "unknown or revoked Reeve token")
		return
	}
	switch info := ClassifyRequest(r); info.Class {
	case ClassRead:
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), reeveKey{}, id)))
	case ClassNever:
		writeIntegrationError(w, http.StatusForbidden, "never", "the Reeve may never "+lowerFirst(info.Description))
	default:
		writeIntegrationError(w, http.StatusForbidden, "needs_approval", info.Description+" is a "+string(info.Class)+" call: propose it as a change set for the person to approve")
	}
}

func lowerFirst(s string) string {
	if s == "" {
		return s
	}
	return strings.ToLower(s[:1]) + s[1:]
}

func (s *Server) haveReeve(w http.ResponseWriter) bool {
	if s.reeve == nil {
		s.writeError(w, http.StatusServiceUnavailable, "the Reeve is not available on this server")
		return false
	}
	return true
}

func (s *Server) reeveError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, service.ErrReeveRunning), errors.Is(err, service.ErrReeveNotEnded):
		s.writeError(w, http.StatusConflict, err.Error())
	case errors.Is(err, store.ErrNotFound):
		s.writeError(w, http.StatusNotFound, "no such Reeve session")
	case errors.Is(err, service.ErrReeveSandboxed):
		s.writeError(w, http.StatusConflict, err.Error())
	default:
		s.writeError(w, http.StatusInternalServerError, err.Error())
	}
}

func (s *Server) reeveOverviewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeve(w) {
			return
		}
		overview, err := s.reeve.Overview(r.Context())
		if err != nil {
			s.reeveError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, overview)
	}
}

func decodeReeveStart(r *http.Request) service.ReeveStart {
	var in service.ReeveStart
	_ = json.NewDecoder(r.Body).Decode(&in)
	in.StartedBy = "web"
	return in
}

func (s *Server) reeveStartHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeve(w) {
			return
		}
		session, err := s.reeve.Start(r.Context(), decodeReeveStart(r))
		if err != nil {
			s.reeveError(w, err)
			return
		}
		s.NotifyAgentsChanged()
		s.writeJSON(w, http.StatusCreated, session)
	}
}

func (s *Server) reeveEndHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeve(w) {
			return
		}
		if err := s.reeve.End(r.Context(), r.PathValue("task_id")); err != nil {
			s.reeveError(w, err)
			return
		}
		s.NotifyAgentsChanged()
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ended"})
	}
}

func (s *Server) reeveResumeHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeve(w) {
			return
		}
		session, err := s.reeve.Resume(r.Context(), r.PathValue("task_id"), decodeReeveStart(r))
		if err != nil {
			s.reeveError(w, err)
			return
		}
		s.NotifyAgentsChanged()
		s.writeJSON(w, http.StatusOK, session)
	}
}

func (s *Server) reeveDeleteHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeve(w) {
			return
		}
		if err := s.reeve.Delete(r.Context(), r.PathValue("task_id")); err != nil {
			s.reeveError(w, err)
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
	}
}

// reeveMessageHandler serves POST /api/reeve/message {text}: into the
// running session, resuming a suspended one, or starting one with it.
func (s *Server) reeveMessageHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.haveReeve(w) {
			return
		}
		var in struct {
			Text string `json:"text"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil || strings.TrimSpace(in.Text) == "" {
			s.writeError(w, http.StatusBadRequest, "want {\"text\": …}")
			return
		}
		if err := s.reeve.Message(r.Context(), in.Text, "web"); err != nil {
			s.reeveError(w, err)
			return
		}
		s.NotifyAgentsChanged()
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
	}
}

// reeveTaskID is the task of the Reeve's current session, "" for none.
func (s *Server) reeveTaskID(ctx context.Context) string {
	if s.reeve == nil {
		return ""
	}
	overview, err := s.reeve.Overview(ctx)
	if err != nil || overview.Current == nil {
		return ""
	}
	return overview.Current.TaskID
}

// ReeveChangeSets lists a session's change sets (service.ChangeSetService).
type ReeveChangeSets interface {
	ChangeSetsOf(ctx context.Context, taskID string) ([]store.ReeveChangeSet, error)
}

// SetReeveChangeSets lists the Reeve's change sets for its conversation.
func (s *Server) SetReeveChangeSets(sets ReeveChangeSets) { s.reeveChangeSets = sets }

// ChangeSetLine is a change set as the conversation shows it: a compact
// line with its state.
type ChangeSetLine struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
	State  string `json:"state"`
	Calls  int    `json:"calls"`
	Ran    int    `json:"ran"`
}

func (s *Server) reeveChangeSetsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.reeveChangeSets == nil {
			s.writeJSON(w, http.StatusOK, map[string]any{"change_sets": []ChangeSetLine{}})
			return
		}
		sets, err := s.reeveChangeSets.ChangeSetsOf(r.Context(), r.PathValue("task_id"))
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		lines := make([]ChangeSetLine, 0, len(sets))
		for _, set := range sets {
			var calls []json.RawMessage
			_ = json.Unmarshal([]byte(set.Calls), &calls)
			var results []struct {
				State string `json:"state"`
			}
			_ = json.Unmarshal([]byte(set.Results), &results)
			line := ChangeSetLine{ID: set.ID, Reason: set.Reason, State: set.State, Calls: len(calls)}
			for _, res := range results {
				if res.State == "ran" {
					line.Ran++
				}
			}
			lines = append(lines, line)
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"change_sets": lines})
	}
}

// ReeveSecretKeeper stores a secret the person pasted for the Reeve
// (service.ReeveSecrets).
type ReeveSecretKeeper interface {
	Fulfil(ctx context.Context, approvalID, value string) (string, error)
}

// SetReeveSecrets lets the person answer the Reeve's requests for secrets.
func (s *Server) SetReeveSecrets(secrets ReeveSecretKeeper) { s.reeveSecrets = secrets }

// reeveSecretHandler serves POST /api/reeve/secrets/{approval_id} {value}:
// the value goes to the secret store, and the Reeve is given its name.
func (s *Server) reeveSecretHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if s.reeveSecrets == nil {
			s.writeError(w, http.StatusServiceUnavailable, "secrets cannot be stored here")
			return
		}
		var in struct {
			Value string `json:"value"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			s.writeError(w, http.StatusBadRequest, "want {\"value\": …}")
			return
		}
		ref, err := s.reeveSecrets.Fulfil(r.Context(), r.PathValue("approval_id"), in.Value)
		switch {
		case errors.Is(err, service.ErrSecretRequestClosed), errors.Is(err, store.ErrNotFound):
			s.writeError(w, http.StatusConflict, "this request for a secret is no longer open")
		case err != nil:
			s.writeError(w, http.StatusBadRequest, err.Error())
		default:
			s.writeJSON(w, http.StatusOK, map[string]string{"ref": ref})
		}
	}
}

// ReeveEngramKeeper is the Reeve's engram in Settings
// (service.ReeveEngramService).
type ReeveEngramKeeper interface {
	View(ctx context.Context) (service.ReeveEngramView, error)
	ProposeSeed(ctx context.Context, seed, by string) (*service.Approval, error)
	RemoveMemory(ctx context.Context, id string) error
	CloseThread(ctx context.Context, id string) error
}

// SetReeveEngram serves the Reeve's engram: its seed, memories and threads.
func (s *Server) SetReeveEngram(e ReeveEngramKeeper) { s.reeveEngram = e }

func (s *Server) reeveEngramReady(w http.ResponseWriter) bool {
	if s.reeveEngram == nil {
		s.writeError(w, http.StatusServiceUnavailable, "this server does not keep the Reeve's engram")
		return false
	}
	return true
}

// reeveEngramHandler serves GET /api/reeve/engram.
func (s *Server) reeveEngramHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.reeveEngramReady(w) {
			return
		}
		view, err := s.reeveEngram.View(r.Context())
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, view)
	}
}

// reeveSeedHandler serves PUT /api/reeve/engram/seed {seed}: one of the
// Reeve's own rules, so it answers 202 with the approval to sign, or 409
// with no device enrolled.
func (s *Server) reeveSeedHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.reeveEngramReady(w) {
			return
		}
		var in struct {
			Seed string `json:"seed"`
		}
		if json.NewDecoder(r.Body).Decode(&in) != nil {
			s.writeError(w, http.StatusBadRequest, "want {\"seed\": …}")
			return
		}
		approval, err := s.reeveEngram.ProposeSeed(r.Context(), in.Seed, "web")
		switch {
		case errors.Is(err, service.ErrNoDeviceEnrolled):
			s.writeError(w, http.StatusConflict, err.Error())
		case err != nil:
			s.writeError(w, http.StatusBadRequest, err.Error())
		default:
			s.writeJSON(w, http.StatusAccepted, map[string]any{"status": "needs_signature", "approval_id": approval.ID})
		}
	}
}

// reeveEngramRemoveHandler serves DELETE /api/reeve/engram/memories/{id}
// and /threads/{id}: a memory is deleted, a thread closed.
func (s *Server) reeveEngramRemoveHandler(remove func(ReeveEngramKeeper, context.Context, string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.reeveEngramReady(w) {
			return
		}
		if err := remove(s.reeveEngram, r.Context(), r.PathValue("id")); err != nil {
			s.writeError(w, http.StatusBadRequest, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}
