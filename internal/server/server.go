package server

import (
	"context"
	"crypto/subtle"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"

	"github.com/cpave3/legato/internal/server/static"
	"github.com/cpave3/legato/internal/service"
)

// Server is the HTTP/WebSocket server for Legato's web UI.
type Server struct {
	board      service.BoardService
	agents     service.AgentService
	tmux       service.TmuxManager
	addr       string
	server     *http.Server
	hub        *Hub
	streams    *streamManager
	tlsCert    string
	tlsKey     string
	caCertPath string
	authToken  string
}

// New creates a new server. agents and tmux may be nil (agent endpoints will return empty results).
func New(board service.BoardService, agents service.AgentService, tmux service.TmuxManager, addr string) *Server {
	sm := newStreamManager(tmux)
	s := &Server{
		board:  board,
		agents: agents,
		tmux:   tmux,
		addr:    addr,
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
	mux.HandleFunc("/api/settings", s.settingsHandler())
	mux.HandleFunc("/api/ca-cert", s.caCertHandler())
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

// corsMiddleware adds CORS headers to all responses so the PWA served from
// one legato instance can talk to another. Wildcard origin is acceptable
// because the server runs on a local network with self-signed TLS — the CA
// trust requirement is the real access control.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
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
		fmt.Fprintf(w, `{"ca_cert_available":%t}`, hasCACert)
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

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
