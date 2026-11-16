package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

type testClock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *testClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *testClock) Add(d time.Duration) {
	c.mu.Lock()
	c.now = c.now.Add(d)
	c.mu.Unlock()
}

type lifecycleFixture struct {
	registrationFixture
	clock *testClock
}

func newLifecycleFixture(t *testing.T) lifecycleFixture {
	t.Helper()
	clock := &testClock{now: time.Now()}
	f := newRegistrationFixture(t, OAuthFlowOptions{Now: clock.Now}, MCPConnectionInput{})
	return lifecycleFixture{f, clock}
}

// another is a second process on the same database and sealed store.
func (f lifecycleFixture) another(t *testing.T) *OAuthFlowService {
	t.Helper()
	other := NewOAuthFlowService(f.db, f.secrets, OAuthFlowOptions{Now: f.clock.Now, Connections: f.conns})
	t.Cleanup(func() { other.Close() })
	return other
}

// connect authorizes through the app path and confirms.
func (f lifecycleFixture) connect(t *testing.T) string {
	t.Helper()
	begun, q := f.begin(t, OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	status, err := f.flows.Deliver(context.Background(), begun.ID, "operator", begun.ReturnKey, oauthAppResponse(q))
	if err != nil || status.Status != "awaiting_confirmation" {
		t.Fatalf("deliver: %+v %v", status, err)
	}
	if err := f.flows.Confirm(context.Background(), begun.ID, "operator"); err != nil {
		t.Fatal(err)
	}
	return begun.ID
}

func (f lifecycleFixture) set(change func(p *fakeOAuth)) {
	f.provider.mu.Lock()
	change(f.provider)
	f.provider.mu.Unlock()
}

func (f lifecycleFixture) refreshes() int {
	f.provider.mu.Lock()
	defer f.provider.mu.Unlock()
	return f.provider.refreshes
}

// abandonForTest stops this service as a crash would: its receivers stop
// and its leases lapse, and nothing is recorded.
func (s *OAuthFlowService) abandonForTest() {
	s.mu.Lock()
	receivers := s.receivers
	s.receivers = map[string]oauthLocalReceiver{}
	s.closed = true
	s.mu.Unlock()
	s.stopLeases()
	for _, local := range receivers {
		local.cancel()
		_ = local.receiver.Close()
	}
}

func (f lifecycleFixture) sealed() string {
	f.secrets.mu.Lock()
	defer f.secrets.mu.Unlock()
	var all []string
	for k, v := range f.secrets.values {
		all = append(all, k+"="+v)
	}
	return strings.Join(all, "\n")
}

func TestAFreshTokenIsUsedWithoutRefreshing(t *testing.T) {
	f := newLifecycleFixture(t)
	f.connect(t)
	token, err := f.flows.AccessToken(context.Background(), "burnlite")
	if err != nil || token != "access-1" {
		t.Fatalf("token %q %v", token, err)
	}
	if f.refreshes() != 0 {
		t.Fatal("refreshed a fresh token")
	}
}

// The rotated refresh token is sealed before the new access token is handed
// out, so a crash after use cannot leave only the dead refresh token.
func TestAnExpiredTokenIsRefreshedAndTheRotationSealedFirst(t *testing.T) {
	f := newLifecycleFixture(t)
	f.connect(t)
	f.clock.Add(2 * time.Hour)
	token, err := f.flows.AccessToken(context.Background(), "burnlite")
	if err != nil || token != "access-2" {
		t.Fatalf("token %q %v", token, err)
	}
	if !strings.Contains(f.sealed(), "refresh-2") || strings.Contains(f.sealed(), "refresh-1") {
		t.Fatal("the rotated refresh token was not sealed in place of the old one")
	}
	if again, _ := f.another(t).AccessToken(context.Background(), "burnlite"); again != "access-2" || f.refreshes() != 1 {
		t.Fatalf("second process: %q, %d refreshes", again, f.refreshes())
	}
}

func TestConcurrentRefreshesAcrossProcessesRefreshOnce(t *testing.T) {
	f := newLifecycleFixture(t)
	f.connect(t)
	f.clock.Add(2 * time.Hour)
	gate := make(chan struct{})
	f.set(func(p *fakeOAuth) { p.tokenGate = gate })
	other := f.another(t)
	var wg sync.WaitGroup
	tokens := make([]string, 2)
	errs := make([]error, 2)
	for i, svc := range []*OAuthFlowService{f.flows, other} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			tokens[i], errs[i] = svc.AccessToken(context.Background(), "burnlite")
		}()
	}
	time.Sleep(200 * time.Millisecond)
	close(gate)
	wg.Wait()
	f.provider.mu.Lock()
	refreshes := f.provider.refreshes
	f.provider.mu.Unlock()
	if refreshes != 1 || errs[0] != nil || errs[1] != nil || tokens[0] != "access-2" || tokens[1] != "access-2" {
		t.Fatalf("refreshes %d, tokens %v, errors %v", refreshes, tokens, errs)
	}
}

func TestADeadRefreshTokenNeedsTheOperatorAgain(t *testing.T) {
	f := newLifecycleFixture(t)
	f.connect(t)
	f.clock.Add(2 * time.Hour)
	f.set(func(p *fakeOAuth) { p.refuseAll = true })
	if _, err := f.flows.AccessToken(context.Background(), "burnlite"); !errors.Is(err, ErrOAuthReauthRequired) {
		t.Fatalf("dead refresh: %v", err)
	}
	got, _ := f.conns.Get(context.Background(), "burnlite")
	if got.Authorization != "reauth_required" || got.Problem == nil || got.Problem.Code != "reauth_required" {
		t.Fatalf("state %+v", got)
	}
	if strings.Contains(f.sealed(), "access-") {
		t.Fatal("dead tokens stayed sealed")
	}
	// An agent's call never starts consent: it is refused until the operator connects again.
	f.set(func(p *fakeOAuth) { p.refuseAll = false })
	if _, err := f.flows.AccessToken(context.Background(), "burnlite"); !errors.Is(err, ErrOAuthReauthRequired) {
		t.Fatalf("after the server recovered: %v", err)
	}
	f.connect(t)
	if token, err := f.flows.AccessToken(context.Background(), "burnlite"); err != nil || token == "" {
		t.Fatalf("after re-authorization: %v", err)
	}
}

func TestATokenWithoutRefreshExpiresIntoReauthorization(t *testing.T) {
	f := newLifecycleFixture(t)
	f.set(func(p *fakeOAuth) { p.noRefresh = true })
	f.connect(t)
	if _, err := f.flows.AccessToken(context.Background(), "burnlite"); err != nil {
		t.Fatal(err)
	}
	f.clock.Add(2 * time.Hour)
	if _, err := f.flows.AccessToken(context.Background(), "burnlite"); !errors.Is(err, ErrOAuthReauthRequired) {
		t.Fatalf("expired without refresh: %v", err)
	}
	if f.refreshes() != 0 {
		t.Fatal("tried to refresh without a refresh token")
	}
}

func TestAnUnreachableServerKeepsTheAuthorization(t *testing.T) {
	f := newLifecycleFixture(t)
	f.connect(t)
	f.clock.Add(2 * time.Hour)
	f.set(func(p *fakeOAuth) { p.drop = true })
	if _, err := f.flows.AccessToken(context.Background(), "burnlite"); !errors.Is(err, ErrOAuthRefreshUnavailable) {
		t.Fatalf("network failure: %v", err)
	}
	got, _ := f.conns.Get(context.Background(), "burnlite")
	if got.Authorization != "authorized" {
		t.Fatalf("a network failure discarded the authorization: %+v", got)
	}
	f.set(func(p *fakeOAuth) { p.drop = false })
	if token, err := f.flows.AccessToken(context.Background(), "burnlite"); err != nil || token != "access-2" {
		t.Fatalf("after recovery: %q %v", token, err)
	}
}

func TestDisconnectRevokesUpstreamWhereSupported(t *testing.T) {
	f := newLifecycleFixture(t)
	f.set(func(p *fakeOAuth) { p.revocation = true })
	f.connect(t)
	result, err := f.flows.Disconnect(context.Background(), "burnlite")
	if err != nil || result.Upstream != "revoked" {
		t.Fatalf("disconnect: %+v %v", result, err)
	}
	f.provider.mu.Lock()
	revoked := f.provider.revoked
	f.provider.mu.Unlock()
	if len(revoked) != 1 || revoked[0].Get("token") != "refresh-1" || revoked[0].Get("token_type_hint") != "refresh_token" {
		t.Fatalf("revoked %v", revoked)
	}
	if _, err := f.flows.AccessToken(context.Background(), "burnlite"); err == nil {
		t.Fatal("a token was used after disconnect")
	}
	if f.sealed() != "" {
		t.Fatalf("sealed records survived disconnect:\n%s", f.sealed())
	}
}

func TestDisconnectSaysWhenTheServerCannotRevoke(t *testing.T) {
	f := newLifecycleFixture(t)
	f.connect(t)
	result, err := f.flows.Disconnect(context.Background(), "burnlite")
	if err != nil || result.Upstream != "not_supported" {
		t.Fatalf("disconnect: %+v %v", result, err)
	}
}

// A refresh that finishes after a disconnect cannot bring the connection back.
func TestARefreshRacingDisconnectLeavesNothingBehind(t *testing.T) {
	f := newLifecycleFixture(t)
	f.connect(t)
	f.clock.Add(2 * time.Hour)
	gate := make(chan struct{})
	f.set(func(p *fakeOAuth) { p.tokenGate = gate })
	done := make(chan error, 1)
	go func() {
		_, err := f.flows.AccessToken(context.Background(), "burnlite")
		done <- err
	}()
	time.Sleep(200 * time.Millisecond)
	if _, err := f.another(t).Disconnect(context.Background(), "burnlite"); err != nil {
		t.Fatal(err)
	}
	close(gate)
	if err := <-done; err == nil {
		t.Fatal("the racing refresh handed out a token after disconnect")
	}
	if f.sealed() != "" {
		t.Fatalf("the racing refresh left sealed records:\n%s", f.sealed())
	}
	got, _ := f.conns.Get(context.Background(), "burnlite")
	if got.Authorization == "authorized" {
		t.Fatal("disconnected connection is authorized again")
	}
}

func TestConfirmationShowsWhatWasAuthorizedWithoutSecrets(t *testing.T) {
	f := newLifecycleFixture(t)
	begun, q := f.begin(t, OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback", Scope: "read:tasks"})
	status, err := f.flows.Deliver(context.Background(), begun.ID, "operator", begun.ReturnKey, oauthAppResponse(q))
	if err != nil {
		t.Fatal(err)
	}
	ev := status.Evidence
	if ev == nil || ev.Issuer != f.provider.srv.URL || ev.Resource != f.provider.srv.URL+"/api/llm/mcp" || ev.GrantedScope != "read:tasks" || !ev.Refreshable || ev.Account != "" {
		t.Fatalf("evidence %+v", ev)
	}
	encoded, _ := json.Marshal(status)
	for _, secret := range []string{"access-1", "refresh-1", "code-1", q.Get("state")} {
		if strings.Contains(string(encoded), secret) {
			t.Fatalf("status exposed %q", secret)
		}
	}
}

func TestTheSweepReconcilesAbandonedAttempts(t *testing.T) {
	f := newLifecycleFixture(t)
	ctx := context.Background()
	app := OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"}
	expiring, _ := f.begin(t, app)
	// A web attempt whose process died: its receiver lease is not renewed.
	crashed := NewOAuthFlowService(f.db, f.secrets, OAuthFlowOptions{Now: f.clock.Now, Connections: f.conns})
	lost, err := crashed.Begin(ctx, OAuthFlowRequest{Connection: "burnlite", Owner: "operator", Mode: "web"})
	if err != nil {
		t.Fatal(err)
	}
	crashed.abandonForTest()
	// An exchange that a crash left half done.
	stuck, q := f.begin(t, app)
	if err := f.db.ClaimOAuthAttempt(ctx, stuck.ID, f.clock.Now().Unix()); err != nil {
		t.Fatal(err)
	}
	_ = q
	f.clock.Add(3 * time.Minute)
	if err := f.flows.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]string{lost.ID: "interrupted", stuck.ID: "failed", expiring.ID: "pending"} {
		st, err := f.flows.Status(ctx, id, "operator")
		if err != nil || st.Status != want {
			t.Errorf("%s: %+v %v, want %s", id, st, err, want)
		}
	}
	f.clock.Add(15 * time.Minute)
	if err := f.flows.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if st, _ := f.flows.Status(ctx, expiring.ID, "operator"); st.Status != "expired" {
		t.Fatalf("expired attempt: %+v", st)
	}
	if strings.Contains(f.sealed(), "oauth-attempt-") {
		t.Fatalf("abandoned attempts kept sealed records:\n%s", f.sealed())
	}
	// Terminal attempts are dropped after the retention.
	f.clock.Add(8 * 24 * time.Hour)
	if err := f.flows.Sweep(ctx); err != nil {
		t.Fatal(err)
	}
	if attempts, _ := f.db.ListOAuthAttempts(ctx, "burnlite"); len(attempts) != 0 {
		t.Fatalf("%d terminal attempts kept", len(attempts))
	}
}

// A callback that arrives after its attempt was swept cannot settle it.
func TestAStaleCallbackAfterASweepIsRefused(t *testing.T) {
	f := newLifecycleFixture(t)
	begun, q := f.begin(t, OAuthFlowRequest{Mode: "app", RedirectURI: "http://127.0.0.1:34567/callback"})
	f.clock.Add(20 * time.Minute)
	if err := f.flows.Sweep(context.Background()); err != nil {
		t.Fatal(err)
	}
	status, err := f.flows.Deliver(context.Background(), begun.ID, "operator", begun.ReturnKey, oauthAppResponse(q))
	if err == nil && status.Status != "expired" {
		t.Fatalf("stale callback: %+v", status)
	}
	f.provider.mu.Lock()
	forms := len(f.provider.tokenForms)
	f.provider.mu.Unlock()
	if forms != 0 {
		t.Fatal("a stale callback was exchanged")
	}
}
