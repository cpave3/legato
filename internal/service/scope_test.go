package service_test

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

type scopeMessage struct {
	TaskID, Slot, Text string
}

type fakeScopeMessenger struct {
	mu   sync.Mutex
	sent []scopeMessage
	fail error
}

func (f *fakeScopeMessenger) QueueMessage(ctx context.Context, taskID, message string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail != nil {
		return f.fail
	}
	f.sent = append(f.sent, scopeMessage{TaskID: taskID, Slot: service.AgentSlotFrom(ctx), Text: message})
	return nil
}

func (f *fakeScopeMessenger) messages() []scopeMessage {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]scopeMessage(nil), f.sent...)
}

type scopeFixture struct {
	store     *store.Store
	svc       *service.ScopeService
	messenger *fakeScopeMessenger
	bus       *events.Bus
	dir       string
	sessions  int
}

const scopeOrigin = "http://localhost:5173"

func newScopeFixture(t *testing.T) *scopeFixture {
	t.Helper()
	s := newTestStoreForReport(t)
	f := &scopeFixture{store: s, messenger: &fakeScopeMessenger{}, bus: events.New(), dir: filepath.Join(t.TempDir(), "scope")}
	f.svc = service.NewScopeService(service.ScopeServiceOptions{
		Store: s, Dir: f.dir, Messenger: f.messenger, Bus: f.bus,
		Now: func() time.Time { return time.Date(2026, 11, 19, 10, 0, 0, 0, time.UTC) },
	})
	return f
}

func (f *scopeFixture) task(t *testing.T, id string) {
	t.Helper()
	if err := f.store.CreateTask(context.Background(), store.Task{ID: id, Title: id, Status: "Doing", CreatedAt: "2026-01-01T00:00:00Z", UpdatedAt: "2026-01-01T00:00:00Z"}); err != nil {
		t.Fatal(err)
	}
}

// agent gives the task's slot an agent session in status.
func (f *scopeFixture) agent(t *testing.T, taskID, slot, status string) {
	t.Helper()
	f.sessions++
	if err := f.store.InsertAgentSession(context.Background(), store.AgentSession{TaskID: taskID, Slot: slot, TmuxSession: "legato-" + taskID + "-" + string(rune('a'+f.sessions)), Status: status}); err != nil {
		t.Fatal(err)
	}
}

func (f *scopeFixture) setSlotStatus(t *testing.T, taskID, slot, status string) {
	t.Helper()
	if err := f.store.UpdateAgentSessionStatusInSlot(context.Background(), taskID, slot, status); err != nil {
		t.Fatal(err)
	}
}

func scopePin(path, comment string) service.ScopePinInput {
	return service.ScopePinInput{
		Comment: comment,
		Page:    service.ScopePage{URL: scopeOrigin + path, Title: "App", Viewport: service.ScopeSize{Width: 1280, Height: 800}, DPR: 2},
		Target:  service.ScopeTarget{Selectors: []string{`[data-testid="save"]`, "#save"}, Text: "Save", Box: service.ScopeBox{X: 10, Y: 20, Width: 80, Height: 30}},
	}
}

func pngBytes(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	img.Set(1, 1, color.RGBA{R: 255, A: 255})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestABatchFromASubscribedOriginIsOneScopeMessage(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	withSource := scopePin("/settings?tab=2", "Save button\noverlaps the header")
	withSource.Source = &service.ScopeSource{Name: "SaveButton", File: "src/Settings.tsx", Line: 42, Column: 7, Confidence: "exact"}

	result, err := f.svc.SendBatch(ctx, scopeOrigin+"/", []service.ScopePinInput{withSource, scopePin("/", "Logo is blurry")})
	if err != nil {
		t.Fatal(err)
	}
	if !result.Subscribed || !result.Delivered || len(result.PinIDs) != 2 || !strings.HasPrefix(result.BatchID, "sb-") || !strings.HasPrefix(result.PinIDs[0], "sp-") {
		t.Fatalf("result = %+v", result)
	}
	messages := f.messenger.messages()
	if len(messages) != 1 {
		t.Fatalf("messages = %+v, want one", messages)
	}
	text := messages[0].Text
	for _, want := range []string{"[legato scope]", result.PinIDs[0], result.PinIDs[1], "/settings?tab=2", `"Save button overlaps the header"`, `[data-testid="save"]`, "src/Settings.tsx:42", "page_pin_get", "page_pin_resolve"} {
		if !strings.Contains(text, want) {
			t.Errorf("message lacks %q:\n%s", want, text)
		}
	}
	if !strings.HasPrefix(text, "[legato scope]") || messages[0].TaskID != "task1" || messages[0].Slot != store.SlotMain {
		t.Fatalf("message = %+v", messages[0])
	}
	pins, err := f.svc.ListOrigin(ctx, scopeOrigin)
	if err != nil || len(pins) != 2 || pins[0].State != store.ScopeDelivered || pins[0].Path != "/settings?tab=2" || pins[0].Selector != `[data-testid="save"]` {
		t.Fatalf("pins = %+v, %v", pins, err)
	}
}

func TestABatchGoesToTheSubscribingSlot(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "reviewer", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "reviewer", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "x")}); err != nil {
		t.Fatal(err)
	}
	if m := f.messenger.messages(); len(m) != 1 || m[0].Slot != "reviewer" {
		t.Fatalf("messages = %+v", m)
	}
}

func TestANewSubscriberTakesTheOriginAndTheOldOneIsTold(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.task(t, "task2")
	f.agent(t, "task1", "", "running")
	f.agent(t, "task2", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.Subscribe(ctx, "task2", "", scopeOrigin)
	if err != nil || result.TookOverFrom != "task1" {
		t.Fatalf("takeover = %+v, %v", result, err)
	}
	messages := f.messenger.messages()
	if len(messages) != 1 || messages[0].TaskID != "task1" || !strings.HasPrefix(messages[0].Text, "[legato scope]") || !strings.Contains(messages[0].Text, scopeOrigin) || !strings.Contains(messages[0].Text, "task2") {
		t.Fatalf("notice = %+v", messages)
	}
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "x")}); err != nil {
		t.Fatal(err)
	}
	if m := f.messenger.messages(); len(m) != 2 || m[1].TaskID != "task2" {
		t.Fatalf("batch went to %+v", m)
	}
	// Subscribing again to an origin the slot holds tells nobody.
	if result, _ := f.svc.Subscribe(ctx, "task2", "", scopeOrigin); result.TookOverFrom != "" || len(f.messenger.messages()) != 2 {
		t.Fatalf("resubscribe = %+v, messages %+v", result, f.messenger.messages())
	}
}

func TestASubscriptionSurvivesARestartAndEndsWhenTheAgentIsKilled(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	// A restart or Reload stops the agent without announcing that it died
	// and starts a new execution in the same slot.
	f.setSlotStatus(t, "task1", "", "dead")
	f.agent(t, "task1", "", "running")
	f.svc.HandleAgentStarted(ctx, "task1")
	if subs, _ := f.svc.SubscriptionsOf(ctx, "task1", ""); len(subs) != 1 {
		t.Fatalf("after restart, subscriptions = %+v", subs)
	}
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "x")}); err != nil {
		t.Fatal(err)
	}
	if m := f.messenger.messages(); len(m) != 1 || m[0].TaskID != "task1" {
		t.Fatalf("messages after restart = %+v", m)
	}

	f.setSlotStatus(t, "task1", "", "dead")
	f.svc.HandleAgentDied(ctx, events.AgentDiedPayload{TaskID: "task1"})
	if subs, _ := f.svc.SubscriptionsOf(ctx, "task1", ""); len(subs) != 0 {
		t.Fatalf("after kill, subscriptions = %+v", subs)
	}
}

func TestAnInterruptedAgentKeepsItsSubscriptionUntilDismissed(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	f.setSlotStatus(t, "task1", "", store.AgentSessionInterrupted)
	f.svc.HandleAgentDied(ctx, events.AgentDiedPayload{TaskID: "task1"})
	if subs, _ := f.svc.SubscriptionsOf(ctx, "task1", ""); len(subs) != 1 {
		t.Fatalf("interrupted agent's subscriptions = %+v", subs)
	}

	// While interrupted, a batch waits.
	result, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "x")})
	if err != nil || !result.Subscribed || result.Delivered || len(f.messenger.messages()) != 0 {
		t.Fatalf("batch to an interrupted agent = %+v, %v, messages %+v", result, err, f.messenger.messages())
	}

	// Resuming tells the new agent of the waiting batch. A spawn clears
	// the slot's ended sessions, the interrupted one among them.
	if err := f.store.DeleteDeadAgentSessionsInSlot(ctx, "task1", ""); err != nil {
		t.Fatal(err)
	}
	f.agent(t, "task1", "", "running")
	f.svc.HandleAgentStarted(ctx, "task1")
	if m := f.messenger.messages(); len(m) != 1 || !strings.Contains(m[0].Text, result.PinIDs[0]) {
		t.Fatalf("on resume, messages = %+v", m)
	}

	// Dismissed: the slot has neither a running nor an interrupted agent.
	f.setSlotStatus(t, "task1", "", "dead")
	f.svc.PruneEnded(ctx)
	if subs, _ := f.svc.SubscriptionsOf(ctx, "task1", ""); len(subs) != 0 {
		t.Fatalf("after dismissal, subscriptions = %+v", subs)
	}
}

func TestAnArchivedTasksSubscriptionsEnd(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "", store.AgentSessionInterrupted)
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	if err := f.store.ArchiveTask(ctx, "task1"); err != nil {
		t.Fatal(err)
	}
	f.svc.PruneEnded(ctx)
	if subs, _ := f.svc.SubscriptionsOf(ctx, "task1", ""); len(subs) != 0 {
		t.Fatalf("archived task's subscriptions = %+v", subs)
	}
}

func TestABatchWithNoSubscriberWaitsAndTheNextSubscribeReportsIt(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "", "running")

	result, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/a", "one"), scopePin("/b", "two")})
	if err != nil || result.Subscribed || result.Delivered {
		t.Fatalf("result = %+v, %v", result, err)
	}
	sub, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin)
	if err != nil || len(sub.Waiting) != 2 || sub.Waiting[0].ID != result.PinIDs[0] {
		t.Fatalf("subscribe = %+v, %v", sub, err)
	}
	if !strings.Contains(sub.Report(), result.PinIDs[1]) || !strings.Contains(sub.Report(), "page_pin_get") {
		t.Fatalf("report = %s", sub.Report())
	}
	pins, _ := f.svc.ListOrigin(ctx, scopeOrigin)
	if pins[0].State != store.ScopeDelivered {
		t.Fatalf("reported pins stay %s", pins[0].State)
	}
	if len(f.messenger.messages()) != 0 {
		t.Fatalf("subscribe also pushed: %+v", f.messenger.messages())
	}
}

func TestListingForASlotReportsWaitingPinsOnItsOrigins(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	f.messenger.fail = errors.New("agent is starting")
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "x")}); err != nil {
		t.Fatal(err)
	}
	f.messenger.fail = nil
	if _, err := f.svc.SendBatch(ctx, "http://other:3000", []service.ScopePinInput{{Comment: "y", Page: service.ScopePage{URL: "http://other:3000/"}}}); err != nil {
		t.Fatal(err)
	}

	pins, err := f.svc.ListForSlot(ctx, "task1", "", "", "")
	if err != nil || len(pins) != 1 || pins[0].Origin != scopeOrigin || pins[0].State != store.ScopeDelivered {
		t.Fatalf("slot's pins = %+v, %v", pins, err)
	}
	other, err := f.svc.ListForSlot(ctx, "task1", "", "http://other:3000", "")
	if err != nil || len(other) != 1 || other[0].State != store.ScopeSent {
		t.Fatalf("a named origin's pins = %+v, %v (listing another slot's origin delivers nothing)", other, err)
	}
	if fixed, _ := f.svc.ListForSlot(ctx, "task1", "", "", store.ScopeFixed); len(fixed) != 0 {
		t.Fatalf("fixed pins = %+v", fixed)
	}
}

func TestAFailedPushLeavesTheBatchUndelivered(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	f.messenger.fail = errors.New("pane gone")
	result, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "x")})
	if err != nil || !result.Subscribed || result.Delivered {
		t.Fatalf("result = %+v, %v", result, err)
	}
	batch, err := f.store.GetScopeBatch(ctx, result.BatchID)
	if err != nil || batch.DeliveredAt != nil || !strings.Contains(batch.DeliveryError, "pane gone") {
		t.Fatalf("batch = %+v, %v", batch, err)
	}
	pins, _ := f.svc.ListOrigin(ctx, scopeOrigin)
	if pins[0].State != store.ScopeSent {
		t.Fatalf("pin state = %s", pins[0].State)
	}

	// The next batch pushes both.
	f.messenger.fail = nil
	second, _ := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "y")})
	m := f.messenger.messages()
	if len(m) != 1 || !strings.Contains(m[0].Text, result.PinIDs[0]) || !strings.Contains(m[0].Text, second.PinIDs[0]) {
		t.Fatalf("messages = %+v", m)
	}
}

func TestResolveReopenAndWithdrawFollowThePinStates(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.agent(t, "task1", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	result, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "x")})
	if err != nil {
		t.Fatal(err)
	}
	id := result.PinIDs[0]
	changed := f.bus.Subscribe(events.EventScopeChanged)

	if err := f.svc.Withdraw(ctx, id); !errors.Is(err, service.ErrScopePinState) {
		t.Fatalf("withdrawing a delivered pin = %v, want ErrScopePinState", err)
	}
	if _, err := f.svc.Resolve(ctx, id, "maybe", "note", "task1"); !errors.Is(err, service.ErrScopeInvalid) {
		t.Fatalf("an unknown outcome = %v", err)
	}
	if _, err := f.svc.Resolve(ctx, id, "fixed", "  ", "task1"); !errors.Is(err, service.ErrScopeInvalid) {
		t.Fatalf("a blank note = %v", err)
	}
	pin, err := f.svc.Resolve(ctx, id, "fixed", "Moved the button\nbelow the header", "task1")
	if err != nil || pin.State != store.ScopeFixed || pin.Note != "Moved the button below the header" {
		t.Fatalf("resolved = %+v, %v", pin, err)
	}
	select {
	case event := <-changed:
		if p, _ := event.Payload.(events.ScopeChangedPayload); p.Origin != scopeOrigin {
			t.Fatalf("payload = %#v", event.Payload)
		}
	case <-time.After(time.Second):
		t.Fatal("no EventScopeChanged after a resolve")
	}
	if _, err := f.svc.Resolve(ctx, id, "wont_fix", "no", "task1"); !errors.Is(err, service.ErrScopePinState) {
		t.Fatalf("resolving twice = %v", err)
	}

	pin, err = f.svc.Reopen(ctx, id, "Still overlaps on mobile")
	if err != nil || pin.Comment != "Still overlaps on mobile" {
		t.Fatalf("reopen = %+v, %v", pin, err)
	}
	// Reopening pushes the pin again; the push makes it delivered.
	m := f.messenger.messages()
	if len(m) != 2 || !strings.Contains(m[1].Text, "Still overlaps on mobile") || !strings.HasPrefix(m[1].Text, "[legato scope]") {
		t.Fatalf("messages = %+v", m)
	}
	if pins, _ := f.svc.ListOrigin(ctx, scopeOrigin); pins[0].State != store.ScopeDelivered {
		t.Fatalf("reopened pin state = %s", pins[0].State)
	}
	if _, err := f.svc.Reopen(ctx, id, "again"); !errors.Is(err, service.ErrScopePinState) {
		t.Fatalf("reopening a delivered pin = %v", err)
	}
	if _, err := f.svc.Reopen(ctx, "sp-missing", "x"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("reopening an unknown pin = %v", err)
	}
}

func TestWithdrawingAnUndeliveredPinRemovesItAndItsScreenshot(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	pin := scopePin("/", "x")
	pin.Screenshot = pngBytes(t)
	result, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{pin})
	if err != nil {
		t.Fatal(err)
	}
	data, mediaType, err := f.svc.Screenshot(ctx, result.PinIDs[0])
	if err != nil || mediaType != "image/png" || !bytes.Equal(data, pin.Screenshot) {
		t.Fatalf("screenshot = %d bytes %q, %v", len(data), mediaType, err)
	}
	entries, _ := os.ReadDir(f.dir)
	if len(entries) != 1 {
		t.Fatalf("screenshot files = %v", entries)
	}
	if err := f.svc.Withdraw(ctx, result.PinIDs[0]); err != nil {
		t.Fatal(err)
	}
	if pins, _ := f.svc.ListOrigin(ctx, scopeOrigin); len(pins) != 0 {
		t.Fatalf("pins after withdraw = %+v", pins)
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 {
		t.Fatalf("screenshot files after withdraw = %v", entries)
	}
}

func TestPinDetailCarriesTheCapturedContext(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	pin := scopePin("/settings", "x")
	pin.Styles = service.ScopeStyles{Computed: map[string]string{"z-index": "10"}, Classes: []string{"btn"}}
	pin.Source = &service.ScopeSource{Name: "SaveButton", File: "src/Settings.tsx", Line: 42, Confidence: "exact", Owners: []string{"SettingsPage"}}
	pin.Screenshot = pngBytes(t)
	result, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{pin})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := f.svc.Get(ctx, result.PinIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if detail.Page.Title != "App" || detail.Target.Box.Width != 80 || detail.Styles.Computed["z-index"] != "10" || detail.Source == nil || detail.Source.Line != 42 || !detail.HasScreenshot {
		t.Fatalf("detail = %+v", detail)
	}
	text := detail.Text()
	for _, want := range []string{result.PinIDs[0], "/settings", "z-index: 10", "src/Settings.tsx:42", "SettingsPage", `[data-testid="save"]`, "1280×800"} {
		if !strings.Contains(text, want) {
			t.Errorf("detail text lacks %q:\n%s", want, text)
		}
	}
	if _, err := f.svc.Get(ctx, "sp-missing"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("an unknown pin = %v", err)
	}
}

func TestScopeLimitsAndOriginsAreChecked(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	many := make([]service.ScopePinInput, service.ScopeMaxPinsPerBatch+1)
	for i := range many {
		many[i] = scopePin("/", "x")
	}
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, many); !errors.Is(err, service.ErrScopeTooLarge) {
		t.Fatalf("too many pins = %v", err)
	}
	big := scopePin("/", "x")
	big.Screenshot = append(pngBytes(t), make([]byte, service.ScopeMaxScreenshotBytes)...)
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{big}); !errors.Is(err, service.ErrScopeTooLarge) {
		t.Fatalf("a large screenshot = %v", err)
	}
	notImage := scopePin("/", "x")
	notImage.Screenshot = []byte("<html></html>")
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{notImage}); !errors.Is(err, service.ErrScopeInvalid) {
		t.Fatalf("a screenshot that is not an image = %v", err)
	}
	for _, origin := range []string{"", "localhost:5173", "ftp://host", "http://", "http://host/path", "http://user@host", "http://host?q=1"} {
		if _, err := f.svc.SendBatch(ctx, origin, []service.ScopePinInput{scopePin("/", "x")}); !errors.Is(err, service.ErrScopeInvalidOrigin) {
			t.Errorf("origin %q = %v, want ErrScopeInvalidOrigin", origin, err)
		}
	}
	elsewhere := scopePin("/", "x")
	elsewhere.Page.URL = "http://localhost:3000/"
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{elsewhere}); !errors.Is(err, service.ErrScopeInvalid) {
		t.Fatalf("a pin from another origin = %v", err)
	}
	blank := scopePin("/", " ")
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{blank}); !errors.Is(err, service.ErrScopeInvalid) {
		t.Fatalf("a blank comment = %v", err)
	}
	if _, err := f.svc.SendBatch(ctx, scopeOrigin, nil); !errors.Is(err, service.ErrScopeInvalid) {
		t.Fatalf("an empty batch = %v", err)
	}
	if pins, _ := f.svc.ListOrigin(ctx, scopeOrigin); len(pins) != 0 {
		t.Fatalf("refused batches left pins: %+v", pins)
	}
	if entries, _ := os.ReadDir(f.dir); len(entries) != 0 {
		t.Fatalf("refused batches left files: %v", entries)
	}
	if got, err := service.NormalizeScopeOrigin("HTTP://LocalHost:5173/"); err != nil || got != scopeOrigin {
		t.Fatalf("normalized = %q, %v", got, err)
	}
}

func TestUnsubscribingEndsOnlyTheSlotsOwnSubscription(t *testing.T) {
	f := newScopeFixture(t)
	ctx := context.Background()
	f.task(t, "task1")
	f.task(t, "task2")
	f.agent(t, "task1", "", "running")
	f.agent(t, "task2", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	if err := f.svc.Unsubscribe(ctx, "task2", "", scopeOrigin); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("unsubscribing another slot's origin = %v", err)
	}
	if err := f.svc.Unsubscribe(ctx, "task1", "", scopeOrigin+"/"); err != nil {
		t.Fatal(err)
	}
	if result, _ := f.svc.SendBatch(ctx, scopeOrigin, []service.ScopePinInput{scopePin("/", "x")}); result.Subscribed {
		t.Fatal("still subscribed after unsubscribe")
	}
}

func TestTheScopeEventLoopEndsSubscriptionsOfDeadAgents(t *testing.T) {
	f := newScopeFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.task(t, "task1")
	f.agent(t, "task1", "", "running")
	if _, err := f.svc.Subscribe(ctx, "task1", "", scopeOrigin); err != nil {
		t.Fatal(err)
	}
	stop := f.svc.StartEventLoop(ctx)
	defer stop()
	f.setSlotStatus(t, "task1", "", "dead")
	f.bus.Publish(events.Event{Type: events.EventAgentDied, Payload: events.AgentDiedPayload{TaskID: "task1"}})
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if subs, _ := f.svc.SubscriptionsOf(ctx, "task1", ""); len(subs) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the subscription outlived its agent")
}
