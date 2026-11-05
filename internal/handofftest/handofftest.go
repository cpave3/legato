// Package handofftest runs two Legatos in one Go test: a main instance and a
// worker, each with its own SQLite database and real services, the worker's
// web server on an httptest TLS server. Handoff tests act through the main's
// services, the ones the web UI calls, and assert on both sides.
//
// Use it from a test:
//
//	clock := handofftest.NewClock(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
//	worker := handofftest.StartWorker(t, handofftest.WorkerOptions{Name: "vm-1", Clock: clock})
//	main := handofftest.NewMain(t, handofftest.MainOptions{Name: "workstation", Clock: clock})
//	paired := handofftest.Pair(t, main, worker) // the main's record of the worker
//
// Pair does what a person does: it opens a main pairing request on the
// worker over HTTP (as Settings › Main instances › Pair a main instance
// does) and enters its phrase in the main's Add a worker (WorkerService.Pair).
// main.Workers.Client(ctx, paired.InstanceID) then calls the worker with the
// main's handoff-only token over the worker's pinned certificate.
//
// worker.Stop closes its web server and database, standing in for a worker
// that went away; worker.Restart opens the same database again and serves
// it at the same address with the same certificate, so the main's record
// still reaches it. WorkerOptions.Wire is called with the worker's services
// before it serves, at start and at each restart: later tickets give the
// worker's server their routes there (and a fake GitHub, when they need
// one). Agents start in a FakeTmux, never in a real tmux.
package handofftest

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// Clock is a clock a test moves by hand, shared by both instances.
type Clock struct {
	mu  sync.Mutex
	now time.Time
}

// NewClock starts a clock at t.
func NewClock(t time.Time) *Clock { return &Clock{now: t} }

// Now is the clock's time.
func (c *Clock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

// Advance moves the clock on by d.
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

// WorkerOptions configure a worker.
type WorkerOptions struct {
	// Name is the worker's name, as a main lists it ("vm-1" when empty).
	Name string
	// Clock is the worker's clock; a new one at 2026-11-05 09:00 UTC when nil.
	Clock *Clock
	// NoSandbox makes the worker say it cannot run agents in the sandbox.
	NoSandbox bool
	// Wire is called with the worker's services before it serves, at start
	// and after each restart.
	Wire func(w *Worker)
}

// Worker is a worker instance: its database, services and web server.
type Worker struct {
	t    testing.TB
	opts WorkerOptions

	Dir    string
	DBPath string
	Clock  *Clock

	Store   *store.Store
	Bus     *events.Bus
	Board   service.BoardService
	Agents  service.AgentService
	Tmux    *FakeTmux
	Tokens  *service.IntegrationTokenService
	Pairing *service.PairingService
	Server  *server.Server

	// HTTP is the TLS server; URL its address (https://127.0.0.1:port),
	// which stays the same across restarts.
	HTTP *httptest.Server
	URL  string

	webToken string
	addr     string
}

// StartWorker starts a worker; t's cleanup stops it.
func StartWorker(t testing.TB, opts WorkerOptions) *Worker {
	t.Helper()
	if opts.Name == "" {
		opts.Name = "vm-1"
	}
	if opts.Clock == nil {
		opts.Clock = NewClock(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
	}
	dir := t.TempDir()
	w := &Worker{t: t, opts: opts, Dir: dir, DBPath: filepath.Join(dir, "worker.db"), Clock: opts.Clock, Tmux: NewFakeTmux(), webToken: "worker-web-token"}
	w.start()
	seedColumns(t, w.Store)
	t.Cleanup(w.Stop)
	return w
}

// start opens the database, builds the services and serves them.
func (w *Worker) start() {
	w.t.Helper()
	db, err := store.New(w.DBPath)
	if err != nil {
		w.t.Fatalf("worker database: %v", err)
	}
	w.Store = db
	w.Bus = events.New()
	w.Board = service.NewBoardService(db, w.Bus)
	w.Agents = service.NewAgentService(db, w.Tmux, w.Dir)
	w.Tokens = service.NewIntegrationTokenService(db)

	instanceID := func(ctx context.Context) (string, error) { return service.InstanceID(ctx, db) }
	srv := server.New(w.Board, w.Agents, w.Tmux, "")
	srv.SetAuthToken(w.webToken)
	secrets := service.PairingSecrets{
		WebToken:      srv.AuthToken,
		Name:          w.opts.Name,
		CACertificate: func() (string, error) { return w.caPEM(), nil },
		Mains:         w.Tokens,
		InstanceID:    instanceID,
	}
	w.Pairing = service.NewPairingService(secrets.Secret)
	w.Pairing.SetClock(w.Clock.Now)
	w.Pairing.AllowMains()
	srv.SetPairingService(w.Pairing)
	srv.SetIntegrationAuth(w.Tokens)
	srv.SetMainInstances(w.Tokens)
	srv.SetInstanceID(instanceID)
	sandbox := !w.opts.NoSandbox
	srv.SetSandboxAvailable(func() bool { return sandbox })
	w.Server = srv
	if w.opts.Wire != nil {
		w.opts.Wire(w)
	}

	w.HTTP = httptest.NewUnstartedServer(srv.Handler())
	if w.addr != "" {
		ln, err := listenAgain(w.addr)
		if err != nil {
			w.t.Fatalf("worker address %s: %v", w.addr, err)
		}
		w.HTTP.Listener.Close()
		w.HTTP.Listener = ln
	}
	w.HTTP.StartTLS()
	w.addr = w.HTTP.Listener.Addr().String()
	w.URL = "https://" + w.addr
}

// listenAgain listens on an address a stopped worker used, which the system
// may take a moment to give back.
func listenAgain(addr string) (net.Listener, error) {
	var err error
	for range 50 {
		var ln net.Listener
		if ln, err = net.Listen("tcp", addr); err == nil {
			return ln, nil
		}
		time.Sleep(20 * time.Millisecond)
	}
	return nil, err
}

// caPEM is the certificate a main must trust for the worker: the httptest
// server's own, which is its own CA. It is the same at every restart.
func (w *Worker) caPEM() string {
	if w.HTTP == nil || w.HTTP.Certificate() == nil {
		return ""
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: w.HTTP.Certificate().Raw}))
}

// Stop closes the web server and the database: the worker is unreachable.
// Stopping a stopped worker does nothing.
func (w *Worker) Stop() {
	if w.HTTP != nil {
		w.HTTP.Close()
		w.HTTP = nil
	}
	if w.Store != nil {
		w.Store.Close()
		w.Store = nil
	}
}

// Restart stops the worker if it runs, then starts it again on the same
// database, at the same address, with the same certificate.
func (w *Worker) Restart() {
	w.t.Helper()
	w.Stop()
	w.start()
}

// InstanceID is the worker's instance ID.
func (w *Worker) InstanceID() string {
	w.t.Helper()
	return mustInstanceID(w.t, w.Store)
}

// WebToken is the worker's full web token, which its browsers and phones
// use. A main never holds it.
func (w *Worker) WebToken() string { return w.webToken }

// Client is an HTTP client that trusts the worker's certificate.
func (w *Worker) Client() *http.Client { return w.HTTP.Client() }

// Do sends a request to the worker's web server with token (none when
// empty) and returns the response.
func (w *Worker) Do(method, path, token, body string) *http.Response {
	w.t.Helper()
	req, err := http.NewRequest(method, w.URL+path, strings.NewReader(body))
	if err != nil {
		w.t.Fatal(err)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := w.Client().Do(req)
	if err != nil {
		w.t.Fatalf("%s %s: %v", method, path, err)
	}
	w.t.Cleanup(func() { resp.Body.Close() })
	return resp
}

// MainOptions configure a main instance.
type MainOptions struct {
	// Name is the name the main offers for itself on a worker
	// ("workstation" when empty).
	Name string
	// Clock is the main's clock; a new one at 2026-11-05 09:00 UTC when nil.
	Clock *Clock
}

// Main is a main instance: its database and services. It has no web
// server: tests call its services as its web UI does.
type Main struct {
	t testing.TB

	Dir   string
	Clock *Clock
	Name  string

	Store   *store.Store
	Bus     *events.Bus
	Board   service.BoardService
	Secrets *service.SecretStoreService
	Workers *service.WorkerService
}

// NewMain makes a main instance; t's cleanup closes it.
func NewMain(t testing.TB, opts MainOptions) *Main {
	t.Helper()
	if opts.Name == "" {
		opts.Name = "workstation"
	}
	if opts.Clock == nil {
		opts.Clock = NewClock(time.Date(2026, 11, 5, 9, 0, 0, 0, time.UTC))
	}
	dir := t.TempDir()
	db, err := store.New(filepath.Join(dir, "main.db"))
	if err != nil {
		t.Fatalf("main database: %v", err)
	}
	t.Cleanup(func() { db.Close() })
	seedColumns(t, db)
	m := &Main{t: t, Dir: dir, Clock: opts.Clock, Name: opts.Name, Store: db, Bus: events.New()}
	m.Board = service.NewBoardService(db, m.Bus)
	// The key file source works anywhere: no keyring, no systemd-creds.
	m.Secrets = service.NewSecretStoreService(db, credential.MasterKeyOptions{DataDir: dir, Source: "file"})
	m.Workers = service.NewWorkerService(db, service.WorkerServiceOptions{Secrets: m.Secrets, MainName: opts.Name, Now: opts.Clock.Now})
	return m
}

// InstanceID is the main's instance ID.
func (m *Main) InstanceID() string {
	m.t.Helper()
	return mustInstanceID(m.t, m.Store)
}

// OpenMainPairing opens a main pairing request on the worker, as Settings ›
// Main instances › Pair a main instance does, and returns its phrase.
func (w *Worker) OpenMainPairing() []string {
	w.t.Helper()
	st, err := w.Pairing.OpenFor(service.PairingMain)
	if err != nil {
		w.t.Fatalf("opening a main pairing request: %v", err)
	}
	return st.Phrase
}

// Pair pairs main with worker as a person does: a main request opened on
// the worker, its phrase entered in the main's Add a worker under the
// main's name. It returns the main's record of the worker.
func Pair(t testing.TB, main *Main, worker *Worker) service.Worker {
	t.Helper()
	phrase := worker.OpenMainPairing()
	paired, err := main.Workers.Pair(context.Background(), service.WorkerPairRequest{
		Address: worker.URL, Phrase: strings.Join(phrase, " "), Name: main.Name,
	})
	if err != nil {
		t.Fatalf("pairing %s with %s: %v", main.Name, worker.opts.Name, err)
	}
	return paired
}

func mustInstanceID(t testing.TB, db *store.Store) string {
	t.Helper()
	id, err := service.InstanceID(context.Background(), db)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// seedColumns gives a board Legato's default columns, as the setup wizard
// does on a first run.
func seedColumns(t testing.TB, db *store.Store) {
	t.Helper()
	for i, c := range config.DefaultColumns() {
		statuses, _ := json.Marshal(c.RemoteStatuses)
		if err := db.CreateColumnMapping(context.Background(), store.ColumnMapping{ColumnName: c.Name, RemoteStatuses: string(statuses), SortOrder: i}); err != nil {
			t.Fatal(err)
		}
	}
}
