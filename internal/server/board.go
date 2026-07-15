package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// ---------- GET /api/board ----------

func (s *Server) boardHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}

		ctx := context.Background()
		view := parseWorkspaceView(r.URL.Query().Get("workspace"))

		columns, err := s.board.ListColumns(ctx)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to list columns")
			return
		}

		resp := BoardResponse{
			Columns: make([]ColumnResponse, len(columns)),
		}

		allTaskIDs := make([]string, 0, 64)
		type ref struct{ col, card int }
		cardRefs := make(map[string]ref)

		for i, col := range columns {
			cards, err := s.board.ListCardsByWorkspace(ctx, col.Name, view)
			if err != nil {
				s.writeError(w, http.StatusInternalServerError, "failed to list cards")
				return
			}
			cardResp := make([]CardResponse, len(cards))
			for j, c := range cards {
				cardResp[j] = CardResponse{
					ID:             c.ID,
					Title:          c.Title,
					Priority:       c.Priority,
					IssueType:      c.IssueType,
					Status:         c.Status,
					Provider:       c.Provider,
					HasWarning:     c.HasWarning,
					WorkspaceName:  c.WorkspaceName,
					WorkspaceColor: c.WorkspaceColor,
				}
				allTaskIDs = append(allTaskIDs, c.ID)
				cardRefs[c.ID] = ref{col: i, card: j}
			}
			resp.Columns[i] = ColumnResponse{Name: col.Name, Cards: cardResp}
		}

		// durations
		if s.agents != nil && len(allTaskIDs) > 0 {
			durations, _ := s.agents.GetTaskDurations(ctx, allTaskIDs)
			for id, d := range durations {
				if r, ok := cardRefs[id]; ok {
					resp.Columns[r.col].Cards[r.card].WorkingSeconds = d.Working.Seconds()
					resp.Columns[r.col].Cards[r.card].WaitingSeconds = d.Waiting.Seconds()
				}
			}
		}

		// agent states
		if s.agents != nil && len(allTaskIDs) > 0 {
			agents, _ := s.agents.ListAgents(ctx)
			agentMap := make(map[string]service.AgentSession, len(agents))
			for _, a := range agents {
				agentMap[a.TaskID] = a
			}
			for _, id := range allTaskIDs {
				if r, ok := cardRefs[id]; ok {
					if a, exists := agentMap[id]; exists && a.Status == "running" {
						resp.Columns[r.col].Cards[r.card].AgentActive = true
						resp.Columns[r.col].Cards[r.card].AgentState = a.Activity
					}
				}
			}
		}

		// Review tour badges.
		if reviews, ok := s.reviews.(interface {
			ReviewBadgeStates(context.Context) (map[string]service.ReviewBadgeState, error)
		}); ok {
			if states, stateErr := reviews.ReviewBadgeStates(ctx); stateErr == nil {
				for id, state := range states {
					if ref, exists := cardRefs[id]; exists {
						resp.Columns[ref.col].Cards[ref.card].ReviewReady = state.Ready
						resp.Columns[ref.col].Cards[ref.card].ReviewUnreviewed = state.Unreviewed
					}
				}
			}
		}

		// PR metadata
		for _, id := range allTaskIDs {
			if r, ok := cardRefs[id]; ok {
				s.enrichPRMeta(ctx, id, &resp.Columns[r.col].Cards[r.card])
			}
		}

		// swarm stats
		if s.swarm != nil {
			for _, id := range allTaskIDs {
				if r, ok := cardRefs[id]; ok {
					if stats := computeSwarmStats(ctx, s.swarm, id); stats != nil {
						resp.Columns[r.col].Cards[r.card].SwarmStats = stats
					}
				}
			}
		}

		// workspaces
		workspaces, err := s.board.ListWorkspaces(ctx)
		if err == nil {
			resp.Workspaces = make([]WorkspaceResponse, len(workspaces))
			for i, w := range workspaces {
				resp.Workspaces[i] = WorkspaceResponse{ID: w.ID, Name: w.Name, Color: w.Color}
			}
		}

		s.writeJSON(w, http.StatusOK, resp)
	}
}

func parseWorkspaceView(raw string) store.WorkspaceView {
	switch raw {
	case "", "all":
		return store.WorkspaceView{Kind: store.ViewAll}
	case "unassigned":
		return store.WorkspaceView{Kind: store.ViewUnassigned}
	default:
		if id, err := strconv.Atoi(raw); err == nil {
			return store.WorkspaceView{Kind: store.ViewWorkspace, WorkspaceID: id}
		}
		return store.WorkspaceView{Kind: store.ViewAll}
	}
}

func (s *Server) enrichPRMeta(ctx context.Context, taskID string, card *CardResponse) {
	detail, err := s.board.GetCard(ctx, taskID)
	if err != nil || detail == nil || detail.PRMeta == nil {
		return
	}
	card.PRNumber = detail.PRMeta.PRNumber
	card.PRCheckStatus = detail.PRMeta.CheckStatus
	card.PRReviewDecision = detail.PRMeta.ReviewDecision
	card.PRCommentCount = detail.PRMeta.CommentCount
	card.PRIsDraft = detail.PRMeta.IsDraft
}

func computeSwarmStats(ctx context.Context, swarm SwarmService, parentID string) *SwarmStatsResponse {
	subs, err := swarm.ListSubtaskInfos(ctx, parentID)
	if err != nil || len(subs) == 0 {
		return nil
	}
	stats := &SwarmStatsResponse{Total: len(subs)}
	for _, sub := range subs {
		switch sub.Status {
		case "done":
			stats.Done++
		case "reporting":
			stats.InReview++
		case "in_progress":
			stats.Building++
		case "queued":
			stats.Queued++
		case "cancelled":
			stats.Rejected++
		}
	}
	return stats
}

// ---------- GET /api/workspaces ----------

func (s *Server) workspacesHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		ctx := context.Background()
		workspaces, err := s.board.ListWorkspaces(ctx)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, "failed to list workspaces")
			return
		}
		resp := make([]WorkspaceResponse, len(workspaces))
		for i, w := range workspaces {
			resp[i] = WorkspaceResponse{ID: w.ID, Name: w.Name, Color: w.Color}
		}
		s.writeJSON(w, http.StatusOK, resp)
	}
}

// ---------- GET /api/tasks/{id} ----------

func (s *Server) taskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := r.PathValue("id")
		if id == "" {
			s.writeError(w, http.StatusBadRequest, "task id is required")
			return
		}
		ctx := context.Background()

		switch r.Method {
		case http.MethodGet:
			if r.URL.Query().Get("format") == "full" {
				text, err := s.board.ExportCardContext(ctx, id, service.ExportFormatFull)
				if err != nil {
					if errors.Is(err, store.ErrNotFound) {
						s.writeError(w, http.StatusNotFound, "task not found")
						return
					}
					s.writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
				w.Header().Set("Content-Type", "text/plain")
				w.WriteHeader(http.StatusOK)
				_, _ = w.Write([]byte(text))
				return
			}

			detail, err := s.board.GetCard(ctx, id)
			if err != nil {
				if errors.Is(err, store.ErrNotFound) {
					s.writeError(w, http.StatusNotFound, "task not found")
					return
				}
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			if detail == nil {
				s.writeError(w, http.StatusNotFound, "task not found")
				return
			}
			s.writeJSON(w, http.StatusOK, cardDetailToResponse(detail))

		case http.MethodPatch:
			body, err := io.ReadAll(r.Body)
			if err != nil {
				s.writeError(w, http.StatusBadRequest, "failed to read body")
				return
			}
			var req struct {
				Title       *string `json:"title,omitempty"`
				Description *string `json:"description,omitempty"`
				WorkspaceID *int    `json:"workspace_id,omitempty"`
				Column      *string `json:"column,omitempty"`
			}
			if err := json.Unmarshal(body, &req); err != nil {
				s.writeError(w, http.StatusBadRequest, "invalid JSON")
				return
			}
			var raw map[string]json.RawMessage
			_ = json.Unmarshal(body, &raw)

			if req.Title != nil {
				if err := s.board.UpdateTaskTitle(ctx, id, *req.Title); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						s.writeError(w, http.StatusNotFound, "task not found")
						return
					}
					s.writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
			if req.Description != nil {
				if err := s.board.UpdateTaskDescription(ctx, id, *req.Description); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						s.writeError(w, http.StatusNotFound, "task not found")
						return
					}
					s.writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
			if _, ok := raw["workspace_id"]; ok {
				if err := s.board.UpdateTaskWorkspace(ctx, id, req.WorkspaceID); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						s.writeError(w, http.StatusNotFound, "task not found")
						return
					}
					s.writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
			if req.Column != nil {
				if err := s.board.MoveCard(ctx, id, *req.Column); err != nil {
					if errors.Is(err, store.ErrNotFound) {
						s.writeError(w, http.StatusNotFound, "task not found")
						return
					}
					s.writeError(w, http.StatusInternalServerError, err.Error())
					return
				}
			}
			s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

		case http.MethodDelete:
			if err := s.board.DeleteTask(ctx, id); err != nil {
				if errors.Is(err, store.ErrNotFound) {
					s.writeError(w, http.StatusNotFound, "task not found")
					return
				}
				s.writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})

		default:
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		}
	}
}

func cardDetailToResponse(d *service.CardDetail) CardDetailResponse {
	resp := CardDetailResponse{
		ID:              d.ID,
		Title:           d.Title,
		DescriptionMD:   d.DescriptionMD,
		Status:          d.Status,
		Priority:        d.Priority,
		Provider:        d.Provider,
		RemoteID:        d.RemoteID,
		RemoteMeta:      d.RemoteMeta,
		WorkspaceID:     d.WorkspaceID,
		SwarmActiveStep: d.SwarmActiveStep,
		SwarmStepNames:  d.SwarmStepNames,
		CreatedAt:       d.CreatedAt.Format(time.RFC3339),
		UpdatedAt:       d.UpdatedAt.Format(time.RFC3339),
	}
	if d.PRMeta != nil {
		resp.PRMeta = &PRMetaResponse{
			Repo:           d.PRMeta.Repo,
			Branch:         d.PRMeta.Branch,
			PRNumber:       d.PRMeta.PRNumber,
			PRURL:          d.PRMeta.PRURL,
			State:          d.PRMeta.State,
			IsDraft:        d.PRMeta.IsDraft,
			ReviewDecision: d.PRMeta.ReviewDecision,
			CheckStatus:    d.PRMeta.CheckStatus,
			CommentCount:   d.PRMeta.CommentCount,
		}
	}
	return resp
}

// ---------- POST /api/tasks/{id}/archive ----------

func (s *Server) archiveTaskHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		id := r.PathValue("id")
		ctx := context.Background()
		if err := s.board.ArchiveTask(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "task not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------- POST /api/board/archive-done ----------

func (s *Server) archiveDoneHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		ctx := context.Background()
		count, err := s.board.ArchiveDoneCards(ctx)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, ArchiveDoneResponse{Archived: count})
	}
}

// ---------- GET /api/tasks/search ----------

func (s *Server) searchTasksHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(q) < 2 {
			s.writeJSON(w, http.StatusOK, []CardResponse{})
			return
		}
		ctx := context.Background()
		cards, err := s.board.SearchCards(ctx, q)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp := make([]CardResponse, len(cards))
		for i, c := range cards {
			resp[i] = CardResponse{
				ID:        c.ID,
				Title:     c.Title,
				Priority:  c.Priority,
				IssueType: c.IssueType,
				Status:    c.Status,
			}
		}
		s.writeJSON(w, http.StatusOK, resp)
	}
}

// ---------- POST /api/tasks/{id}/link-pr ----------

func (s *Server) linkPRHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.prTracking == nil {
			s.writeError(w, http.StatusServiceUnavailable, "pr tracking service not available")
			return
		}
		id := r.PathValue("id")
		var req struct {
			Owner    string `json:"owner"`
			Repo     string `json:"repo"`
			PRNumber int    `json:"pr_number"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.Owner == "" || req.Repo == "" || req.PRNumber <= 0 {
			s.writeError(w, http.StatusBadRequest, "owner, repo and pr_number are required")
			return
		}
		ctx := context.Background()
		if err := s.prTracking.LinkPR(ctx, id, req.Owner, req.Repo, req.PRNumber); err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------- GET /api/tasks/{id}/pr-preview ----------

func (s *Server) prPreviewHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.prTracking == nil {
			s.writeError(w, http.StatusServiceUnavailable, "pr tracking service not available")
			return
		}
		owner := r.URL.Query().Get("owner")
		repo := r.URL.Query().Get("repo")
		numberStr := r.URL.Query().Get("number")
		if owner == "" || repo == "" || numberStr == "" {
			s.writeError(w, http.StatusBadRequest, "owner, repo and number are required")
			return
		}
		number, err := strconv.Atoi(numberStr)
		if err != nil || number <= 0 {
			s.writeError(w, http.StatusBadRequest, "invalid pr number")
			return
		}
		status, err := s.prTracking.FetchPRByNumber(owner, repo, number)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, PRPreviewResponse{
			Number:         status.Number,
			URL:            status.URL,
			State:          status.State,
			IsDraft:        status.IsDraft,
			CheckStatus:    status.CheckStatus,
			ReviewDecision: status.ReviewDecision,
			CommentCount:   status.CommentCount,
			HeadBranch:     status.HeadBranch,
			Title:          status.Title,
		})
	}
}

// ---------- POST /api/tasks/{id}/unlink-pr ----------

func (s *Server) unlinkPRHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.prTracking == nil {
			s.writeError(w, http.StatusServiceUnavailable, "pr tracking service not available")
			return
		}
		id := r.PathValue("id")
		ctx := context.Background()
		if err := s.prTracking.UnlinkBranch(ctx, id); err != nil {
			if errors.Is(err, store.ErrNotFound) {
				s.writeError(w, http.StatusNotFound, "task not found")
				return
			}
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	}
}

// ---------- GET /api/remote/search ----------

func (s *Server) remoteSearchHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.sync == nil {
			s.writeError(w, http.StatusServiceUnavailable, "sync service not available")
			return
		}
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		if len(q) < 2 {
			s.writeJSON(w, http.StatusOK, []RemoteSearchResultResponse{})
			return
		}
		ctx := context.Background()
		results, err := s.sync.SearchRemote(ctx, q)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp := make([]RemoteSearchResultResponse, len(results))
		for i, r := range results {
			resp[i] = RemoteSearchResultResponse{
				ID:        r.ID,
				Summary:   r.Summary,
				Status:    r.Status,
				Priority:  r.Priority,
				IssueType: r.IssueType,
			}
		}
		s.writeJSON(w, http.StatusOK, resp)
	}
}

// ---------- POST /api/remote/import ----------

func (s *Server) remoteImportHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.sync == nil {
			s.writeError(w, http.StatusServiceUnavailable, "sync service not available")
			return
		}
		var req struct {
			TicketID    string `json:"ticket_id"`
			WorkspaceID *int   `json:"workspace_id,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			s.writeError(w, http.StatusBadRequest, "invalid JSON")
			return
		}
		if req.TicketID == "" {
			s.writeError(w, http.StatusBadRequest, "ticket_id is required")
			return
		}
		ctx := context.Background()
		card, err := s.sync.ImportRemoteTask(ctx, req.TicketID, req.WorkspaceID)
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		resp := CardResponse{
			ID:       card.ID,
			Title:    card.Title,
			Priority: card.Priority,
			Status:   card.Status,
		}
		s.writeJSON(w, http.StatusCreated, resp)
	}
}

// ---------- GET /api/repo/detect ----------

func (s *Server) detectRepoHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			s.writeError(w, http.StatusMethodNotAllowed, "method not allowed")
			return
		}
		if s.prTracking == nil {
			s.writeError(w, http.StatusServiceUnavailable, "pr tracking service not available")
			return
		}
		owner, repo, err := s.prTracking.DetectRepo()
		if err != nil || owner == "" || repo == "" {
			s.writeError(w, http.StatusNotFound, "repo not detectable")
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]string{"owner": owner, "repo": repo})
	}
}
