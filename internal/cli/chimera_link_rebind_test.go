package cli_test

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/cli"
	"github.com/cpave3/legato/internal/engine/broker"
	"github.com/cpave3/legato/internal/engine/hooks"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

const rebindTestInterval = 20 * time.Millisecond

// fakeChimera is the agent's own Chimera: it serves one session under a
// secret path, answers its context probe with contextStatus, and records the
// messages posted to it.
type fakeChimera struct {
	url           string
	sessionID     string
	contextStatus atomic.Int32
	probes        atomic.Int32
	mu            sync.Mutex
	messages      []string
}

func newFakeChimera(t *testing.T, sessionID string) *fakeChimera {
	t.Helper()
	f := &fakeChimera{sessionID: sessionID}
	f.contextStatus.Store(http.StatusOK)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /secret/v1/sessions/"+sessionID+"/context", func(w http.ResponseWriter, r *http.Request) {
		f.probes.Add(1)
		status := int(f.contextStatus.Load())
		if status != http.StatusOK {
			http.Error(w, "unavailable", status)
			return
		}
		_, _ = io.WriteString(w, `{"contextWindow":200000}`)
	})
	mux.HandleFunc("GET /secret/v1/sessions/"+sessionID, func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"id":"`+sessionID+`","status":"idle","cwd":"/repo"}`)
	})
	mux.HandleFunc("POST /secret/v1/sessions/"+sessionID+"/messages", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.messages = append(f.messages, string(body))
		f.mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	f.url = server.URL + "/secret"
	return f
}

func (f *fakeChimera) received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.messages...)
}

func (f *fakeChimera) start(seq int) string {
	return `{"seq":` + strconv.Itoa(seq) + `,"type":"session.start","sessionId":"` + f.sessionID + `","cwd":"/repo","serverUrl":"` + f.url + `","apiVersion":1}`
}

// typingTmux is a terminal that counts what is typed into it.
type typingTmux struct {
	*noopTmux
	sent atomic.Int32
}

func (m *typingTmux) SendKeys(string, string) error          { m.sent.Add(1); return nil }
func (m *typingTmux) SendKey(string, string) error           { m.sent.Add(1); return nil }
func (m *typingTmux) SendKeysLine(string, string) error      { m.sent.Add(1); return nil }
func (m *typingTmux) SendKeysMultiline(string, string) error { m.sent.Add(1); return nil }
func (m *typingTmux) typed() int32                           { return m.sent.Load() }

type linkRun struct {
	w    *io.PipeWriter
	done chan error
}

func startLink(t *testing.T, s *store.Store, opts cli.ChimeraLinkOptions) *linkRun {
	t.Helper()
	r, w := io.Pipe()
	opts.Stdin = r
	if opts.TaskID == "" {
		opts.TaskID = "link-task"
	}
	opts.RebindInterval = rebindTestInterval
	run := &linkRun{w: w, done: make(chan error, 1)}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { run.done <- cli.RunChimeraLink(ctx, s, opts) }()
	t.Cleanup(func() {
		cancel()
		_ = w.Close()
		<-run.done
	})
	return run
}

func (l *linkRun) write(t *testing.T, line string) {
	t.Helper()
	if _, err := io.WriteString(l.w, line+"\n"); err != nil {
		t.Fatal(err)
	}
}

func linkBinding(t *testing.T, s *store.Store) (sessionID, endpoint string, nativeOff bool) {
	t.Helper()
	a, err := s.GetAgentSessionByTaskID(context.Background(), "link-task")
	if err != nil {
		t.Fatal(err)
	}
	if a.ChimeraSessionID != nil {
		sessionID = *a.ChimeraSessionID
	}
	if a.ChimeraEndpoint != nil {
		endpoint = *a.ChimeraEndpoint
	}
	return sessionID, endpoint, a.NativeOff
}

func loseBinding(t *testing.T, s *store.Store, f *fakeChimera) {
	t.Helper()
	a, err := s.GetAgentSessionByTaskID(context.Background(), "link-task")
	if err != nil {
		t.Fatal(err)
	}
	if cleared, err := s.ClearChimeraDiscoveryForSession(context.Background(), a.ID, f.sessionID, f.url); err != nil || !cleared {
		t.Fatalf("clear = %v, %v", cleared, err)
	}
}

// stays waits several intervals and fails if cond stops holding.
func stays(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * rebindTestInterval)
	for time.Now().Before(deadline) {
		if !cond() {
			t.Fatalf("%s did not hold", what)
		}
		time.Sleep(rebindTestInterval / 2)
	}
}

// An idle agent, whose Chimera sends nothing more, gets its binding back
// from its own link once something cleared it.
func TestChimeraLinkRestoresALostBindingWhileTheAgentIsIdle(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	f := newFakeChimera(t, "conversation")
	link := startLink(t, s, cli.ChimeraLinkOptions{})
	link.write(t, f.start(1))
	waitFor(t, "first binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })

	loseBinding(t, s, f)

	waitFor(t, "restored binding", func() bool {
		id, endpoint, _ := linkBinding(t, s)
		return id == f.sessionID && endpoint == f.url
	})
}

// The operator switched the agent to its terminal: the link leaves it there.
func TestChimeraLinkNeverRestoresAnAgentSwitchedToItsTerminal(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	f := newFakeChimera(t, "conversation")
	link := startLink(t, s, cli.ChimeraLinkOptions{})
	link.write(t, f.start(1))
	waitFor(t, "first binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })
	a, _ := s.GetAgentSessionByTaskID(context.Background(), "link-task")
	if ok, err := s.KeepAgentSessionOnTmux(context.Background(), a.ID, f.sessionID, f.url); err != nil || !ok {
		t.Fatalf("downgrade = %v, %v", ok, err)
	}
	stays(t, "the switch to the terminal", func() bool {
		id, _, off := linkBinding(t, s)
		return id == "" && off
	})
}

// A session Chimera no longer serves is not brought back, and the link stops
// asking about it.
func TestChimeraLinkDoesNotResurrectAGoneSession(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	f := newFakeChimera(t, "conversation")
	link := startLink(t, s, cli.ChimeraLinkOptions{})
	link.write(t, f.start(1))
	waitFor(t, "first binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })

	f.contextStatus.Store(http.StatusNotFound)
	loseBinding(t, s, f)
	waitFor(t, "a probe", func() bool { return f.probes.Load() > 0 })
	stays(t, "the cleared binding", func() bool { id, _, _ := linkBinding(t, s); return id == "" })
	if n := f.probes.Load(); n != 1 {
		t.Fatalf("probed a gone session %d times", n)
	}
}

// A Chimera that is busy for a while is asked again, less often, and the
// binding comes back once it answers.
func TestChimeraLinkRestoresABindingOnceChimeraAnswersAgain(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	f := newFakeChimera(t, "conversation")
	link := startLink(t, s, cli.ChimeraLinkOptions{})
	link.write(t, f.start(1))
	waitFor(t, "first binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })

	f.contextStatus.Store(http.StatusServiceUnavailable)
	loseBinding(t, s, f)
	time.Sleep(12 * rebindTestInterval)
	if id, _, _ := linkBinding(t, s); id != "" {
		t.Fatal("bound without confirming the session")
	}
	if n := f.probes.Load(); n < 2 || n > 6 {
		t.Fatalf("probed %d times in 12 intervals; want a few, backing off", n)
	}
	f.contextStatus.Store(http.StatusOK)
	waitFor(t, "restored binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })
}

// A newer agent of the same task owns the task now: the old link never binds
// it, nor its own superseded row.
func TestChimeraLinkNeverBindsANewerAgentOfItsTask(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	ctx := context.Background()
	f := newFakeChimera(t, "conversation")
	link := startLink(t, s, cli.ChimeraLinkOptions{})
	link.write(t, f.start(1))
	waitFor(t, "first binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })
	old, _ := s.GetAgentSessionByTaskID(ctx, "link-task")
	loseBinding(t, s, f)
	newer, err := s.InsertAgentSessionReturningID(ctx, store.AgentSession{TaskID: "link-task", TmuxSession: "legato-link-task-2", AgentKind: "chimera", Command: "shell", Status: "running", StartedAt: "2999-01-01 00:00:00"})
	if err != nil {
		t.Fatal(err)
	}
	stays(t, "both rows unbound", func() bool {
		for _, id := range []int{old.ID, newer} {
			a, err := s.GetAgentSessionByID(ctx, id)
			if err != nil || a.ChimeraEndpoint != nil {
				return false
			}
		}
		return true
	})
}

// A replayed event after a restored binding is still dropped by its
// sequence, and cannot move the binding to another session.
func TestChimeraLinkKeepsItsSequenceAcrossARestoredBinding(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	f := newFakeChimera(t, "conversation")
	link := startLink(t, s, cli.ChimeraLinkOptions{})
	link.write(t, f.start(1))
	waitFor(t, "first binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })
	loseBinding(t, s, f)
	waitFor(t, "restored binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })

	link.write(t, `{"seq":1,"type":"session.start","sessionId":"intruder","cwd":"/repo","serverUrl":"http://127.0.0.1:1/x","apiVersion":1}`)
	stays(t, "the restored binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })
}

// Through the broker, as a sandboxed link reaches Legato: a lost binding is
// restored, and an outage of the broker meanwhile does not end the link.
func TestChimeraLinkRestoresABindingThroughTheBrokerAcrossAnOutage(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	const token = "lgb_chimera_rebind"
	if err := s.SetExecutionBrokerToken(context.Background(), "native-link-task", service.HashBrokerToken(token)); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "clb")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	socket := filepath.Join(dir, "b.sock")
	srv, err := broker.Listen(socket, cli.NewBrokerHandler(s, cli.BrokerDeps{}))
	if err != nil {
		t.Fatal(err)
	}
	f := newFakeChimera(t, "conversation")
	link := startLink(t, s, cli.ChimeraLinkOptions{ExecutionID: "native-link-task", Host: cli.BrokerBridgeHost(socket, token)})
	link.write(t, f.start(1))
	waitFor(t, "first binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })

	srv.Close()
	loseBinding(t, s, f)
	time.Sleep(5 * rebindTestInterval)
	select {
	case err := <-link.done:
		t.Fatalf("link ended during the outage: %v", err)
	default:
	}
	srv, err = broker.Listen(socket, cli.NewBrokerHandler(s, cli.BrokerDeps{}))
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()
	waitFor(t, "restored binding", func() bool {
		id, endpoint, _ := linkBinding(t, s)
		return id == f.sessionID && endpoint == f.url
	})
}

// The whole way round: a message sent while the binding is lost waits, and
// reaches the same Chimera session once the link restores it, never the
// terminal.
func TestAMessageSentWhileTheBindingIsLostReachesTheSameSession(t *testing.T) {
	s := newTestStore(t)
	seedChimeraLinkAgent(t, s)
	f := newFakeChimera(t, "conversation")
	link := startLink(t, s, cli.ChimeraLinkOptions{})
	link.write(t, f.start(1))
	waitFor(t, "first binding", func() bool { id, _, _ := linkBinding(t, s); return id == f.sessionID })

	terminal := &typingTmux{noopTmux: &noopTmux{sessions: map[string]bool{"legato-link-task": true}}}
	adapter := hooks.NewChimeraAdapter("legato")
	adapter.SetModDir(t.TempDir())
	agents := service.NewAgentService(s, terminal, t.TempDir(), service.AgentServiceOptions{
		Adapters: map[string]service.AIToolAdapter{"chimera": adapter}, DisablePeerAwareness: true,
	})

	loseBinding(t, s, f)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := agents.SendMessage(ctx, "link-task", "[legato test] still there?"); err != nil {
		t.Fatal(err)
	}
	got := f.received()
	if len(got) != 1 || !strings.Contains(got[0], "still there?") {
		t.Fatalf("Chimera received %q", got)
	}
	if terminal.typed() != 0 {
		t.Fatal("the message was typed into the terminal")
	}
}
