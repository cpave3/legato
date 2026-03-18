package server

import (
	"context"
	"net"
	"net/http"

	"github.com/cpave3/legato/internal/service"
)

// Server is a minimal HTTP server wrapping BoardService.
type Server struct {
	svc    service.BoardService
	addr   string
	server *http.Server
}

// New creates a new server.
func New(svc service.BoardService, addr string) *Server {
	s := &Server{
		svc:  svc,
		addr: addr,
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/health", healthHandler(svc))
	s.server = &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	return s
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
	s.addr = ln.Addr().String()
	return s.server.Serve(ln)
}

// Stop gracefully shuts down the server.
func (s *Server) Stop(ctx context.Context) error {
	return s.server.Shutdown(ctx)
}
