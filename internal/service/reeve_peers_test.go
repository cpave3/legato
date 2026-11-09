package service

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/cpave3/legato/internal/engine/credential"
	"github.com/cpave3/legato/internal/engine/store"
)

type mapSecrets struct {
	mu     sync.Mutex
	values map[string][]byte
}

func (m *mapSecrets) Add(_ context.Context, name, _ string, value []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.values == nil {
		m.values = map[string][]byte{}
	}
	m.values[name] = value
	return nil
}

func (m *mapSecrets) Get(_ context.Context, ref credential.Ref) (credential.Secret, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	v, ok := m.values[ref.Path]
	if !ok {
		return credential.Secret{}, errors.New("no such secret")
	}
	return credential.Secret{Value: v}, nil
}

func (m *mapSecrets) Remove(_ context.Context, name string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.values, name)
	return nil
}

// instance is one Legato in a test: its store, its peer service, the
// messages its Reeve was given, and its web server's Reeve route.
type instance struct {
	id       string
	db       *store.Store
	peers    *ReevePeerService
	tokens   *IntegrationTokenService
	server   *httptest.Server
	mu       sync.Mutex
	received []string
}

func newInstance(t *testing.T, id, name string) *instance {
	t.Helper()
	db, err := store.New(filepath.Join(t.TempDir(), "legato.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	in := &instance{id: id, db: db, tokens: NewIntegrationTokenService(db)}
	in.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		identity, err := in.tokens.Authenticate(r.Context(), strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer "))
		if err != nil || !identity.Has(ScopeReeve) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		var msg PeerMessage
		json.NewDecoder(r.Body).Decode(&msg)
		answer, err := in.peers.Receive(r.Context(), identity, msg)
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		json.NewEncoder(w).Encode(answer)
	}))
	t.Cleanup(in.server.Close)
	in.peers = NewReevePeerService(ReevePeerOptions{
		Store: db, Tokens: in.tokens, Secrets: &mapSecrets{},
		Self: func(context.Context) ReeveSelf {
			return ReeveSelf{InstanceID: id, Name: name, Role: "standalone", Address: in.server.URL}
		},
		Deliver: func(_ context.Context, from PeerInfo, text string) error {
			in.mu.Lock()
			defer in.mu.Unlock()
			in.received = append(in.received, PeerFrame(from, text))
			return nil
		},
	})
	return in
}

// pairWith gives a a Reeve pairing with b, as the phrase exchange does: b's
// token for a, kept by a.
func pairWith(t *testing.T, a, b *instance, bName string) {
	t.Helper()
	ctx := context.Background()
	token, err := b.tokens.CreateReevePeer(ctx, "a", a.id)
	if err != nil {
		t.Fatal(err)
	}
	a.peers.opts.Secrets.Add(ctx, reevePeerSecret(b.id), "value", []byte(token))
	a.db.SaveReevePeer(ctx, store.ReevePeer{InstanceID: b.id, Name: bName, Via: PeerViaPairing, Address: b.server.URL, TokenSecret: reevePeerSecret(b.id)})
}

func TestAReeveMessagesAPairedOneAndIsAnsweredOverItsContact(t *testing.T) {
	ctx := context.Background()
	a := newInstance(t, "srv-aaaaaaaaaaaaaaa1", "laptop")
	b := newInstance(t, "srv-bbbbbbbbbbbbbbb2", "vm-1")
	pairWith(t, a, b, "vm-1")

	out, err := a.peers.Send(ctx, "vm-1", "why did the sync fail?")
	if err != nil || out.State != "delivered" {
		t.Fatalf("send: %+v, %v", out, err)
	}
	if len(b.received) != 1 || b.received[0] != "[from the Reeve on laptop · standalone] why did the sync fail?" {
		t.Fatalf("b received %v", b.received)
	}
	// b answers over the contact a sent with its first message.
	back, err := b.peers.Send(ctx, "laptop", "Jira answered 401: the token expired")
	if err != nil || back.State != "delivered" {
		t.Fatalf("answer: %+v, %v", back, err)
	}
	if len(a.received) != 1 || !strings.Contains(a.received[0], "Jira answered 401") {
		t.Fatalf("a received %v", a.received)
	}
}

func TestAnAnswerToAPrimeThatCannotBeReachedWaitsForItsNextMessage(t *testing.T) {
	ctx := context.Background()
	a := newInstance(t, "srv-aaaaaaaaaaaaaaa1", "laptop")
	b := newInstance(t, "srv-bbbbbbbbbbbbbbb2", "vm-1")
	pairWith(t, a, b, "vm-1")
	a.peers.Send(ctx, "vm-1", "hello")

	// The laptop goes behind NAT: its address no longer answers.
	row, _ := b.db.GetReevePeer(ctx, a.id)
	row.Address = "http://127.0.0.1:1"
	b.db.SaveReevePeer(ctx, *row)
	if out, err := b.peers.Send(ctx, "laptop", "the fix is ready"); err != nil || out.State != "queued" {
		t.Fatalf("answer to an unreachable prime: %+v, %v", out, err)
	}
	if _, err := a.peers.Send(ctx, "vm-1", "anything new?"); err != nil {
		t.Fatal(err)
	}
	if len(a.received) != 1 || !strings.Contains(a.received[0], "the fix is ready") {
		t.Fatalf("a received %v", a.received)
	}
}

func TestATokenWithoutTheReeveScopeIsRefused(t *testing.T) {
	ctx := context.Background()
	a := newInstance(t, "srv-aaaaaaaaaaaaaaa1", "laptop")
	b := newInstance(t, "srv-bbbbbbbbbbbbbbb2", "vm-1")
	mainToken, _ := b.tokens.CreateMain(ctx, "laptop", a.id)
	if err := b.tokens.SetMainReeve(ctx, a.id, false); err != nil {
		t.Fatal(err)
	}
	a.peers.opts.Secrets.Add(ctx, reevePeerSecret(b.id), "value", []byte(mainToken))
	a.db.SaveReevePeer(ctx, store.ReevePeer{InstanceID: b.id, Name: "vm-1", Via: PeerViaPairing, Address: b.server.URL, TokenSecret: reevePeerSecret(b.id)})
	if _, err := a.peers.Send(ctx, "vm-1", "hello"); err == nil {
		t.Fatal("a main whose Reeve may not message got through")
	}
	if mains, _ := b.tokens.ListMains(ctx); len(mains) != 1 || mains[0].Reeve {
		t.Fatalf("mains listed %+v, want the Reeve off", mains)
	}
	if err := b.tokens.SetMainReeve(ctx, a.id, true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.peers.Send(ctx, "vm-1", "hello"); err != nil {
		t.Fatalf("with the scope back: %v", err)
	}
}

func TestAPeersMessageGoesInOrStartsOrWaitsOrIsRefused(t *testing.T) {
	svc, db, agents, _ := newReeveFixture(t)
	ctx := context.Background()
	from := PeerInfo{InstanceID: "srv-aaaaaaaaaaaaaaa1", Name: "laptop", Role: "main"}

	if err := svc.PeerMessage(ctx, from, "hi", PeerRefuse); !errors.Is(err, ErrPeerRefused) {
		t.Fatalf("refuse: %v", err)
	}
	if err := svc.PeerMessage(ctx, from, "hi", PeerAsk); !errors.Is(err, ErrPeerAsk) {
		t.Fatalf("ask: %v", err)
	}
	items, _ := NewReevePeerAttentionAdapter(db, svc).Snapshot(ctx)
	if len(items) != 1 || items[0].Title != "The Reeve on laptop wants to talk" {
		t.Fatalf("attention = %+v", items)
	}
	if err := svc.LetPeerTalk(ctx, from.InstanceID); err != nil {
		t.Fatal(err)
	}
	current, _ := db.CurrentReeveSession(ctx)
	if current == nil || !strings.Contains(agents.spawned[0].LaunchPrompt, "[from the Reeve on laptop · main] hi") {
		t.Fatalf("let it talk: %+v / %v", current, agents.spawned)
	}
	svc.End(ctx, current.TaskID)
	if err := svc.PeerMessage(ctx, from, "again", PeerStart); err != nil {
		t.Fatal(err)
	}
	current, _ = db.CurrentReeveSession(ctx)
	if current == nil || current.StartedBy != "peer:"+from.InstanceID {
		t.Fatalf("start: %+v", current)
	}
}
