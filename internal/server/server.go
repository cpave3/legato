package server

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/macros"
	"github.com/cpave3/legato/internal/server/static"
	"github.com/cpave3/legato/internal/service"
)

// Server is the HTTP/WebSocket server for Legato's web UI.
type Server struct {
	board            service.BoardService
	agents           service.AgentService
	swarm            SwarmService
	sync             service.SyncService
	prTracking       service.PRTrackingService
	tmux             service.TmuxManager
	bus              *events.Bus
	addr             string
	workDir          string // default CWD for agent spawn
	server           *http.Server
	hub              *Hub
	streams          *streamManager
	tlsCert          string
	tlsKey           string
	caCertPath       string
	authToken        string
	macros           []macros.Macro
	sparklineWindow  time.Duration
	sparklineBuckets int
}

// New creates a new server. agents and tmux may be nil (agent endpoints will return empty results).
// For swarm support, use NewWithSwarm.
func New(board service.BoardService, agents service.AgentService, tmux service.TmuxManager, addr string) *Server {
	return NewWithSwarm(board, agents, tmux, addr, nil, nil, "")
}

// NewWithSwarm creates a new server with swarm and event bus support.
// agents, tmux, swarm and bus may be nil.
func NewWithSwarm(board service.BoardService, agents service.AgentService, tmux service.TmuxManager, addr string, swarm SwarmService, bus *events.Bus, workDir string) *Server {
	sm := newStreamManager(tmux)
	s := &Server{
		board:   board,
		agents:  agents,
		swarm:   swarm,
		tmux:    tmux,
		bus:     bus,
		addr:    addr,
		workDir: workDir,
		hub:     newHub(),
		streams: sm,
	}
	// When a pipe-pane stream ends (shell exit), reconcile DB state
	// and notify all web clients so dead agents update in the sidebar.
	sm.onStreamEnd = func(agentID string) {
		if agents != nil {
			_ = agents.ReconcileSessions(context.Background())
		}
		s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(board))
	mux.HandleFunc("/api/agents", s.agentsHandler())
	mux.HandleFunc("/api/agents/spawn", s.spawnAgentHandler())
	mux.HandleFunc("/api/agents/kill", s.killAgentHandler())
	mux.HandleFunc("/api/tasks", s.tasksHandler())
	mux.HandleFunc("/api/tasks/search", s.searchTasksHandler())
	mux.HandleFunc("/api/tasks/{id}/archive", s.archiveTaskHandler())
	mux.HandleFunc("/api/tasks/{id}/link-pr", s.linkPRHandler())
	mux.HandleFunc("/api/tasks/{id}/pr-preview", s.prPreviewHandler())
	mux.HandleFunc("/api/tasks/{id}/unlink-pr", s.unlinkPRHandler())
	mux.HandleFunc("/api/tasks/{id}", s.taskHandler())
	mux.HandleFunc("/api/board", s.boardHandler())
	mux.HandleFunc("/api/board/archive-done", s.archiveDoneHandler())
	mux.HandleFunc("/api/workspaces", s.workspacesHandler())
	mux.HandleFunc("/api/settings", s.settingsHandler())
	mux.HandleFunc("/api/ca-cert", s.caCertHandler())
	mux.HandleFunc("/api/adapters", s.adaptersHandler())
	mux.HandleFunc("/api/macros", s.macrosHandler())
	mux.HandleFunc("/api/remote/search", s.remoteSearchHandler())
	mux.HandleFunc("/api/remote/import", s.remoteImportHandler())
	mux.HandleFunc("/api/repo/detect", s.detectRepoHandler())
	// Swarm endpoints
	mux.HandleFunc("/api/swarm/start", s.swarmStartHandler())
	mux.HandleFunc("/api/swarm/dispatch", s.swarmDispatchHandler())
	mux.HandleFunc("/api/swarm/message", s.swarmMessageHandler())
	mux.HandleFunc("/api/swarm/broadcast", s.swarmBroadcastHandler())
	mux.HandleFunc("/api/swarm/close", s.swarmCloseHandler())
	mux.HandleFunc("/api/swarm/finish", s.swarmFinishHandler())
	mux.HandleFunc("/api/swarm/next-step", s.swarmNextStepHandler())
	mux.HandleFunc("/api/swarm/status/", s.swarmStatusHandler())
	mux.HandleFunc("/api/swarm/inbox/", s.swarmInboxHandler())
	mux.HandleFunc("/api/swarm/pending-plan/", s.swarmPendingPlanHandler())
	mux.HandleFunc("/api/swarm/pending-plans", s.swarmPendingPlansHandler())
	mux.HandleFunc("/ws", s.wsHandler())

	// SPA fallback — serve embedded frontend for all non-API paths.
	if fsys := static.DistFS(); fsys != nil {
		mux.HandleFunc("/", spaHandler(fsys))
	}

	s.server = &http.Server{
		Addr:    addr,
		Handler: s.authMiddleware(corsMiddleware(mux)),
		// Disable HTTP/2 — WebSocket upgrades require the HTTP/1.1
		// Connection: Upgrade mechanism which doesn't exist in HTTP/2.
		TLSNextProto: make(map[string]func(*http.Server, *tls.Conn, http.Handler)),
	}
	return s
}

// SetTLS configures TLS certificate paths. Call before Start/Serve.
func (s *Server) SetTLS(certFile, keyFile string) {
	s.tlsCert = certFile
	s.tlsKey = keyFile
}

// SetCACertPath sets the path to the CA certificate for download.
func (s *Server) SetCACertPath(path string) {
	s.caCertPath = path
}

// SetAuthToken sets the bearer token required for all API/WebSocket requests.
// Call before Start/Serve. If empty, auth is disabled (open access).
func (s *Server) SetAuthToken(token string) {
	s.authToken = token
}

// SetSyncService sets the optional sync service for remote search/import endpoints.
func (s *Server) SetSyncService(svc service.SyncService) {
	s.sync = svc
}

// SetPRTrackingService sets the optional PR tracking service for link-pr endpoints.
func (s *Server) SetPRTrackingService(svc service.PRTrackingService) {
	s.prTracking = svc
}

// corsMiddleware adds CORS headers to all responses so the PWA served from
// one legato instance can talk to another. Wildcard origin is acceptable
// because the server runs on a local network with self-signed TLS — the CA
// trust requirement is the real access control.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PATCH, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware checks for a valid bearer token on all requests except
// GET /health (monitoring) and OPTIONS (CORS preflight). WebSocket upgrades
// use ?token= query param since browsers can't set headers on WS connections.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// No token configured — auth disabled.
		if s.authToken == "" {
			next.ServeHTTP(w, r)
			return
		}

		// Exempt endpoints.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/health" && r.Method == http.MethodGet {
			next.ServeHTTP(w, r)
			return
		}

		// Static assets (SPA shell) are public — auth is enforced on API/WS only.
		if !strings.HasPrefix(r.URL.Path, "/api/") && r.URL.Path != "/ws" {
			next.ServeHTTP(w, r)
			return
		}

		// WebSocket: token in query param.
		if r.URL.Path == "/ws" {
			token := r.URL.Query().Get("token")
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
			return
		}

		// REST: Authorization: Bearer <token>
		auth := r.Header.Get("Authorization")
		if strings.HasPrefix(auth, "Bearer ") {
			token := strings.TrimPrefix(auth, "Bearer ")
			if subtle.ConstantTimeCompare([]byte(token), []byte(s.authToken)) == 1 {
				next.ServeHTTP(w, r)
				return
			}
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
	})
}

func (s *Server) settingsHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		hasCACert := s.caCertPath != ""
		resp := struct {
			CaCertAvailable bool   `json:"ca_cert_available"`
			WorkingDir      string `json:"working_dir"`
		}{
			CaCertAvailable: hasCACert,
			WorkingDir:      s.workDir,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *Server) adaptersHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		defaultAdapter := ""
		adapters := []string{}
		if s.agents != nil {
			defaultAdapter = s.agents.DefaultAdapter()
			adapters = s.agents.RegisteredAdapters()
		}
		resp := struct {
			Adapters []string `json:"adapters"`
			Default  string   `json:"default"`
		}{
			Adapters: adapters,
			Default:  defaultAdapter,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func (s *Server) caCertHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if s.caCertPath == "" {
			http.Error(w, "no CA certificate configured", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/x-pem-file")
		w.Header().Set("Content-Disposition", `attachment; filename="legato-ca.pem"`)
		http.ServeFile(w, r, s.caCertPath)
	}
}

// Handler returns the HTTP handler (useful for testing).
func (s *Server) Handler() http.Handler {
	return s.server.Handler
}

// Start begins listening. Blocks until the server is stopped or encounters an error.
func (s *Server) Start() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve accepts connections on an existing listener. Use this when the
// caller has already bound the port (e.g. to probe availability).
func (s *Server) Serve(ln net.Listener) error {
	s.addr = ln.Addr().String()
	if s.tlsCert != "" && s.tlsKey != "" {
		return s.server.ServeTLS(ln, s.tlsCert, s.tlsKey)
	}
	return s.server.Serve(ln)
}

// Addr returns the server's listen address (useful after :0 binding).
func (s *Server) Addr() string {
	return s.addr
}

// NotifyAgentsChanged broadcasts an agents_changed message to all WebSocket clients.
// Call this from IPC message handlers when agent state changes.
func (s *Server) NotifyAgentsChanged() {
	s.hub.Broadcast(WSMessage{Type: MsgAgentsChanged})
}

// StartSwarmEvents subscribes to swarm events from the event bus and broadcasts
// them to all connected WebSocket clients. Call once after server creation.
func (s *Server) StartSwarmEvents() {
	if s.bus == nil {
		return
	}

	go func() {
		ch := s.bus.Subscribe(events.EventPlanProposed)
		for ev := range ch {
			if p, ok := ev.Payload.(events.PlanProposedPayload); ok {
				// Persist so web clients can discover the plan after a cold load.
				if s.swarm != nil {
					_ = s.swarm.InsertPendingPlan(context.Background(), p.ParentTaskID, p.PlanPath, p.ReplySocket)
				}

				s.hub.Broadcast(WSMessage{
					Type:         MsgPlanProposed,
					ParentTaskID: p.ParentTaskID,
					PlanPath:     p.PlanPath,
					ReplySocket:  p.ReplySocket,
				})
			}
		}
	}()

	go func() {
		ch := s.bus.Subscribe(events.EventSwarmChanged)
		for ev := range ch {
			if p, ok := ev.Payload.(events.SwarmChangedPayload); ok {
				// clear pending plan on terminal status transitions
				if p.NewStatus == "plan_applied" || p.NewStatus == "rejected" {
					if s.swarm != nil {
						_ = s.swarm.DeletePendingPlan(context.Background(), p.ParentTaskID)
					}
				}

				s.hub.Broadcast(WSMessage{
					Type:         MsgSwarmChanged,
					ParentTaskID: p.ParentTaskID,
					SubtaskID:    p.SubtaskID,
					Status:       p.NewStatus,
				})
			}
		}
	}()
}

// StartBoardEvents subscribes to card-related events and broadcasts
// cards_changed to all connected WebSocket clients.
func (s *Server) StartBoardEvents() {
	if s.bus == nil {
		return
	}
	go func() {
		ch := s.bus.Subscribe(events.EventCardsRefreshed)
		for range ch {
			s.hub.Broadcast(WSMessage{Type: MsgCardsChanged})
		}
	}()
	go func() {
		ch := s.bus.Subscribe(events.EventCardMoved)
		for range ch {
			s.hub.Broadcast(WSMessage{Type: MsgCardsChanged})
		}
	}()
	go func() {
		ch := s.bus.Subscribe(events.EventCardUpdated)
		for range ch {
			s.hub.Broadcast(WSMessage{Type: MsgCardsChanged})
		}
	}()
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
