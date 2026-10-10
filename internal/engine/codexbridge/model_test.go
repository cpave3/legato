package codexbridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"
)

// withModels has the app server offer two models before the bridge connects.
func newHarnessWithModels(t *testing.T) *harness {
	t.Helper()
	codex := newAppServer(t)
	efforts := func(levels ...string) []any {
		out := []any{}
		for _, level := range levels {
			out = append(out, map[string]any{"reasoningEffort": level, "description": level})
		}
		return out
	}
	codex.on("model/list", func(map[string]any) (any, *rpcError) {
		return map[string]any{"data": []any{
			map[string]any{"id": "gpt-6-astra", "isDefault": true, "hidden": false, "supportedReasoningEfforts": efforts("low", "medium", "high")},
			map[string]any{"id": "gpt-6-sol", "isDefault": false, "hidden": false, "supportedReasoningEfforts": efforts("low", "medium", "high", "max")},
			map[string]any{"id": "gpt-internal", "isDefault": false, "hidden": true, "supportedReasoningEfforts": efforts("low")},
		}, "nextCursor": nil}, nil
	})
	bridge := New(Options{
		TaskID: "task-1", AppServerURL: codex.url(), AppServerToken: appServerToken,
		Now: func() time.Time { return time.UnixMilli(1_700_000_000_000) }, RejoinEvery: 5 * time.Millisecond,
	})
	if err := bridge.Connect(context.Background()); err != nil {
		t.Fatalf("connecting to the app server: %v", err)
	}
	ctx, stop := context.WithCancel(context.Background())
	go func() { _ = bridge.Run(ctx) }()
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		stop()
		bridge.Close()
		server.Close()
	})
	h := &harness{t: t, codex: codex, bridge: bridge, server: server}
	h.codex.called("model/list")
	return h
}

type modelEntry struct {
	Ref       string `json:"ref"`
	IsDefault bool   `json:"isDefault"`
}

func (h *harness) models() []modelEntry {
	h.t.Helper()
	// The list is fetched as the bridge connects; give it a moment to land.
	deadline := time.After(2 * time.Second)
	for {
		var entries []modelEntry
		if code := h.getJSON("/v1/sessions/task-1/models", &entries); code != http.StatusOK {
			h.t.Fatalf("GET models = %d", code)
		}
		if len(entries) > 1 {
			return entries
		}
		select {
		case <-deadline:
			return entries
		case <-time.After(2 * time.Millisecond):
		}
	}
}

func TestThePanelOffersTheModelsCodexLists(t *testing.T) {
	h := newHarnessWithModels(t)
	h.attach()

	// The thread's own model leads and is the one that clears a choice.
	want := []modelEntry{{Ref: "openai/gpt-6-astra", IsDefault: true}, {Ref: "openai/gpt-6-sol"}}
	if got := h.models(); !reflect.DeepEqual(got, want) {
		t.Fatalf("models = %+v, want %+v", got, want)
	}
}

func TestAModelAndEffortChosenInLegatoGoWithTheNextTurnItStarts(t *testing.T) {
	h := newHarnessWithModels(t)
	h.attach()
	h.models()
	events := h.subscribe("")

	var changed struct {
		To            string `json:"to"`
		SettingsState struct {
			Overrides    map[string]any `json:"overrides"`
			Effective    map[string]any `json:"effective"`
			Capabilities struct {
				Settings map[string]struct {
					Values []string `json:"values"`
				} `json:"settings"`
			} `json:"capabilities"`
		} `json:"settingsState"`
	}
	code, body := h.post("/v1/sessions/task-1/model", map[string]any{"model": "openai/gpt-6-sol", "settings": map[string]any{"reasoningEffort": "max"}})
	if code != http.StatusOK {
		t.Fatalf("status = %d %s", code, body)
	}
	if event := nextEvent(t, events); event["type"] != "model_changed" || event["to"] != "openai/gpt-6-sol" {
		t.Fatalf("event = %v", event)
	}
	h.getJSON("/v1/sessions/task-1/model", &changed.SettingsState)
	if changed.SettingsState.Effective["reasoningEffort"] != "max" || changed.SettingsState.Overrides["reasoningEffort"] != "max" {
		t.Fatalf("settings = %+v", changed.SettingsState)
	}
	// The levels offered are the chosen model's own.
	if got := changed.SettingsState.Capabilities.Settings["reasoningEffort"].Values; !reflect.DeepEqual(got, []string{"low", "medium", "high", "max"}) {
		t.Fatalf("effort levels = %v", got)
	}

	h.post("/v1/sessions/task-1/messages", map[string]any{"content": "carry on"})
	started := h.codex.called("turn/start")
	if started["model"] != "gpt-6-sol" || started["effort"] != "max" {
		t.Fatalf("turn/start = %v", started)
	}

	// Codex keeps them for the thread and says so; nothing is left to apply.
	h.codex.notify("thread/settings/updated", map[string]any{"threadId": threadID, "threadSettings": map[string]any{"model": "gpt-6-sol", "effort": "max", "cwd": "/work/repo"}})
	deadline := time.After(2 * time.Second)
	for pending := true; pending; {
		var state struct {
			Overrides map[string]any `json:"overrides"`
		}
		h.getJSON("/v1/sessions/task-1/model", &state)
		pending = len(state.Overrides) > 0
		select {
		case <-deadline:
			t.Fatal("the choice stayed pending after Codex took it on")
		case <-time.After(2 * time.Millisecond):
		}
	}
	h.post("/v1/sessions/task-1/messages", map[string]any{"content": "and again"})
	for h.codex.count("turn/start") < 2 {
		select {
		case <-deadline:
			t.Fatal("the second message never started a turn")
		case <-time.After(2 * time.Millisecond):
		}
	}
	if again := h.codex.called("turn/start"); again["model"] != nil || again["effort"] != nil {
		t.Fatalf("second turn/start still carries the choice: %v", again)
	}
}

func TestAnEffortTheModelDoesNotTakeIsRefused(t *testing.T) {
	h := newHarnessWithModels(t)
	h.attach()
	h.models()

	if code, _ := h.post("/v1/sessions/task-1/model", map[string]any{"settings": map[string]any{"reasoningEffort": "max"}}); code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400: gpt-6-astra has no max effort", code)
	}
	if code, _ := h.post("/v1/sessions/task-1/model", map[string]any{"settings": map[string]any{"temperature": 0.2}}); code != http.StatusBadRequest {
		t.Fatalf("unknown setting: status = %d, want 400", code)
	}
	if code, _ := h.post("/v1/sessions/task-1/model", map[string]any{"model": "openai/gpt-6-sol", "expectedModel": "openai/something-else"}); code != http.StatusConflict {
		t.Fatalf("stale settings dialog: status = %d, want 409", code)
	}
}

func TestAModelChangedInTheTerminalIsAnnounced(t *testing.T) {
	h := newHarnessWithModels(t)
	h.attach()
	events := h.subscribe("")

	h.codex.notify("thread/settings/updated", map[string]any{"threadId": threadID, "threadSettings": map[string]any{"model": "gpt-6-sol", "effort": "high", "cwd": "/work/repo"}})

	if event := nextEvent(t, events); event["type"] != "model_changed" || event["to"] != "openai/gpt-6-sol" {
		t.Fatalf("event = %v", event)
	}
	if snap := h.snapshot(); snap.Model.ModelID != "gpt-6-sol" || snap.Model.Settings["reasoningEffort"] != "high" {
		t.Fatalf("model = %+v", snap.Model)
	}
}

func TestProtocolRoutesTheNativePanelReads(t *testing.T) {
	h := newHarness(t)
	h.attach()

	var instance struct {
		PID     int    `json:"pid"`
		CWD     string `json:"cwd"`
		Version string `json:"version"`
	}
	if code := h.getJSON("/v1/instance", &instance); code != http.StatusOK || instance.PID == 0 || instance.CWD != "/work/repo" || instance.Version != "legato-codex-bridge/1" {
		t.Fatalf("instance = %d %+v", code, instance)
	}
	// Codex has no counterpart for these; an empty list hides the control.
	for _, list := range []string{"skills", "modes", "commands", "subagents"} {
		var entries []any
		if code := h.getJSON("/v1/sessions/task-1/"+list, &entries); code != http.StatusOK || entries == nil || len(entries) != 0 {
			t.Fatalf("GET %s = %d %v", list, code, entries)
		}
	}
	if code := h.getJSON("/v1/sessions/task-1/context", nil); code != http.StatusOK {
		t.Fatalf("GET context = %d", code)
	}
	if code, _ := h.post("/v1/sessions/task-1/mode", map[string]any{"mode": "plan"}); code != http.StatusConflict {
		t.Fatalf("POST mode = %d, want 409", code)
	}
	if code, _ := h.post("/v1/sessions/task-1/paths", map[string]any{"path": "/work/repo/a.go"}); code != http.StatusNoContent {
		t.Fatalf("POST paths = %d, want 204", code)
	}
}

func TestRequestsWithoutTheSecretPathAreNotServed(t *testing.T) {
	codex := newAppServer(t)
	bridge := New(Options{TaskID: "task-1", Token: "s3cret", AppServerURL: codex.url(), AppServerToken: appServerToken})
	if err := bridge.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(bridge.Close)
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(server.Close)

	for path, want := range map[string]int{
		"/v1/sessions/task-1":         http.StatusNotFound,
		"/s3cretx/v1/sessions/task-1": http.StatusNotFound,
		"/s3cret/v1/sessions/task-1":  http.StatusOK,
	} {
		resp, err := http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Fatalf("GET %s = %d, want %d", path, resp.StatusCode, want)
		}
	}
}

func TestTheAppServerRefusesABridgeWithoutItsToken(t *testing.T) {
	codex := newAppServer(t)
	bridge := New(Options{TaskID: "task-1", AppServerURL: codex.url(), AppServerToken: "wrong"})

	if err := bridge.Connect(context.Background()); err == nil {
		bridge.Close()
		t.Fatal("connected without the app server's token")
	}
}

func TestTheBridgeEndsWhenTheAppServerGoesAway(t *testing.T) {
	h := newHarness(t)
	h.attach()
	ended := make(chan error, 1)
	go func() { ended <- h.bridge.Run(context.Background()) }()

	h.codex.hangUp()

	select {
	case <-ended:
	case <-time.After(2 * time.Second):
		t.Fatal("the bridge kept running without its app server")
	}
}

func TestTheSessionHasAWorkingDirectoryBeforeItsTerminalHasAThread(t *testing.T) {
	codex := newAppServer(t)
	bridge := New(Options{TaskID: "task-1", AppServerURL: codex.url(), AppServerToken: appServerToken, CWD: "/work/launched-in"})
	if err := bridge.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	server := httptest.NewServer(bridge.Handler())
	t.Cleanup(func() {
		bridge.Close()
		server.Close()
	})
	h := &harness{t: t, codex: codex, bridge: bridge, server: server}

	// Legato resolves @path attachments against the session's directory, so
	// a message sent before the terminal is ready needs one to be accepted.
	if snap := h.snapshot(); snap.CWD != "/work/launched-in" {
		t.Fatalf("cwd before the terminal attached = %q", snap.CWD)
	}

	// Once the terminal has a thread, the thread's directory is the session's.
	h.attach()
	if snap := h.snapshot(); snap.CWD != "/work/repo" {
		t.Fatalf("cwd = %q, want the thread's", snap.CWD)
	}
}
