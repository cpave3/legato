package service_test

import (
	"context"
	"errors"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/events"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func newAgentUIService(t *testing.T) (*service.AgentUIService, *events.Bus) {
	t.Helper()
	bus := events.New()
	return service.NewAgentUIService(newTestStoreForReport(t), bus, service.AgentUIOptions{}), bus
}

func TestAgentUIShowStoresAnArtifactAndAnnouncesIt(t *testing.T) {
	ui, bus := newAgentUIService(t)
	changed := bus.Subscribe(events.EventAgentUIChanged)
	ctx := context.Background()

	shown, err := ui.Show(ctx, service.AgentUIShowInput{
		TaskID: "task-a", ExecutionID: "ex-1", Placement: service.AgentUIPanel,
		Format: service.AgentUIFormatHTML, Title: "Sync design", Content: "<h1>Design</h1>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !regexp.MustCompile(`^ui-[0-9a-f]{10}$`).MatchString(shown.ID) {
		t.Fatalf("id = %q, want ui-<10 hex>", shown.ID)
	}
	if shown.Revision != 1 || shown.Placement != service.AgentUIPanel {
		t.Fatalf("shown = %#v", shown)
	}

	got, err := ui.Get(ctx, shown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if got.Content != "<h1>Design</h1>" || got.Title != "Sync design" || got.Format != service.AgentUIFormatHTML || got.TaskID != "task-a" {
		t.Fatalf("stored item = %#v", got)
	}

	select {
	case event := <-changed:
		payload, ok := event.Payload.(events.AgentUIChangedPayload)
		if !ok {
			t.Fatalf("payload = %#v", event.Payload)
		}
		if payload.TaskID != "task-a" || payload.ItemID != shown.ID || payload.Placement != service.AgentUIPanel || payload.Revision != 1 || payload.Kind != "shown" {
			t.Fatalf("payload = %#v", payload)
		}
	case <-time.After(time.Second):
		t.Fatal("no change event was published")
	}
}

func TestAgentUIShowRejectsInvalidRequestsWithoutStoringThem(t *testing.T) {
	valid := service.AgentUIShowInput{
		TaskID: "task-a", Placement: service.AgentUIInline, Format: service.AgentUIFormatHTML,
		Content: "<b>27 C</b>", FallbackText: "27 C and sunny",
	}
	cases := map[string]func(*service.AgentUIShowInput){
		"missing task":             func(in *service.AgentUIShowInput) { in.TaskID = " " },
		"unknown placement":        func(in *service.AgentUIShowInput) { in.Placement = "popup" },
		"unknown format":           func(in *service.AgentUIShowInput) { in.Format = "pdf" },
		"markdown card":            func(in *service.AgentUIShowInput) { in.Format = service.AgentUIFormatMarkdown },
		"blank content":            func(in *service.AgentUIShowInput) { in.Content = " \n\t" },
		"content over the limit":   func(in *service.AgentUIShowInput) { in.Content = strings.Repeat("x", service.AgentUIMaxContentBytes+1) },
		"content not UTF-8":        func(in *service.AgentUIShowInput) { in.Content = "ok \xff\xfe" },
		"card without fallback":    func(in *service.AgentUIShowInput) { in.FallbackText = "  " },
		"fallback over the limit":  func(in *service.AgentUIShowInput) { in.FallbackText = strings.Repeat("x", 4001) },
		"title over the limit":     func(in *service.AgentUIShowInput) { in.Title = strings.Repeat("x", 201) },
		"key with a space":         func(in *service.AgentUIShowInput) { in.Key = "my key" },
		"key over the limit":       func(in *service.AgentUIShowInput) { in.Key = strings.Repeat("k", 65) },
		"artifact without a title": func(in *service.AgentUIShowInput) { in.Placement = service.AgentUIPanel; in.Title = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			ui, bus := newAgentUIService(t)
			changed := bus.Subscribe(events.EventAgentUIChanged)
			in := valid
			mutate(&in)
			_, err := ui.Show(context.Background(), in)
			if !errors.Is(err, service.ErrAgentUIInvalid) {
				t.Fatalf("error = %v, want ErrAgentUIInvalid", err)
			}
			items, listErr := ui.List(context.Background(), "task-a")
			if listErr != nil || len(items) != 0 {
				t.Fatalf("stored after rejection: %#v, %v", items, listErr)
			}
			select {
			case event := <-changed:
				t.Fatalf("event published for a rejected request: %#v", event)
			default:
			}
		})
	}

	t.Run("the valid request is accepted", func(t *testing.T) {
		ui, _ := newAgentUIService(t)
		if _, err := ui.Show(context.Background(), valid); err != nil {
			t.Fatal(err)
		}
	})
}

func TestAgentUIShowReplacesAKeyedItemAndAnnouncesTheNewRevision(t *testing.T) {
	ui, bus := newAgentUIService(t)
	ctx := context.Background()
	first, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown, Title: "Notes", Key: "notes", Content: "# One"})
	if err != nil {
		t.Fatal(err)
	}
	changed := bus.Subscribe(events.EventAgentUIChanged)
	second, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown, Title: "Notes", Key: "notes", Content: "# Two"})
	if err != nil {
		t.Fatal(err)
	}
	if second.ID != first.ID || second.Revision != 2 || second.OldestRevision != 1 {
		t.Fatalf("second = %#v, first = %#v", second, first)
	}
	if payload := (<-changed).Payload.(events.AgentUIChangedPayload); payload.ItemID != first.ID || payload.Revision != 2 {
		t.Fatalf("payload = %#v", payload)
	}
	if old, err := ui.Get(ctx, first.ID, 1); err != nil || old.Content != "# One" {
		t.Fatalf("revision 1 = %#v, %v", old, err)
	}
}

func TestAgentUIDeleteRemovesAnItemAndAnnouncesIt(t *testing.T) {
	ui, bus := newAgentUIService(t)
	ctx := context.Background()
	shown, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML, Title: "Design", Content: "<p>x</p>"})
	if err != nil {
		t.Fatal(err)
	}
	changed := bus.Subscribe(events.EventAgentUIChanged)

	if err := ui.Delete(ctx, shown.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := ui.Get(ctx, shown.ID, 0); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("get after delete = %v, want ErrNotFound", err)
	}
	payload := (<-changed).Payload.(events.AgentUIChangedPayload)
	if payload.TaskID != "task-a" || payload.ItemID != shown.ID || payload.Kind != "deleted" {
		t.Fatalf("payload = %#v", payload)
	}
	if err := ui.Delete(ctx, shown.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("second delete = %v, want ErrNotFound", err)
	}
}

func TestAgentUIGetTellsTheClientWhichDomainsHTMLMayLoadFrom(t *testing.T) {
	ui := service.NewAgentUIService(newTestStoreForReport(t), nil, service.AgentUIOptions{ResourceDomains: []string{"cdn.jsdelivr.net", "fonts.gstatic.com"}})
	ctx := context.Background()
	html, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatHTML, Title: "Chart", Content: "<canvas></canvas>"})
	if err != nil {
		t.Fatal(err)
	}
	markdown, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown, Title: "Notes", Content: "# Notes"})
	if err != nil {
		t.Fatal(err)
	}

	got, err := ui.Get(ctx, html.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.ResourceDomains, ",") != "cdn.jsdelivr.net,fonts.gstatic.com" {
		t.Fatalf("html resource domains = %v", got.ResourceDomains)
	}
	if strings.Join(ui.ResourceDomains(), ",") != "cdn.jsdelivr.net,fonts.gstatic.com" {
		t.Fatalf("service resource domains = %v", ui.ResourceDomains())
	}
	plain, err := ui.Get(ctx, markdown.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(plain.ResourceDomains) != 0 {
		t.Fatalf("markdown item carries resource domains: %v", plain.ResourceDomains)
	}
}

// A client that cannot draw what an agent showed (the TUI) says how much
// there is and links to the newest of it.
func TestAgentUIShownSummarisesWhatAnAgentHasShown(t *testing.T) {
	ui, _ := newAgentUIService(t)
	ctx := context.Background()

	empty, err := ui.Shown(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if empty.Cards != 0 || empty.Artifacts != 0 || empty.LatestID != "" {
		t.Fatalf("summary of nothing = %#v", empty)
	}

	design, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown, Title: "Sync design", Key: "design", Content: "# One"})
	if err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Weather", "Build status"} {
		if _, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-a", Placement: service.AgentUIInline, Format: service.AgentUIFormatHTML, Title: title, Content: "<b>x</b>", FallbackText: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-b", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown, Title: "Other agent", Content: "# Other"}); err != nil {
		t.Fatal(err)
	}

	shown, err := ui.Shown(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if shown.Cards != 2 || shown.Artifacts != 1 {
		t.Fatalf("counts = %d cards, %d artifacts, want 2 and 1", shown.Cards, shown.Artifacts)
	}
	if shown.LatestTitle != "Build status" {
		t.Fatalf("latest = %q (%s), want the card shown last", shown.LatestTitle, shown.LatestID)
	}

	// Showing an artifact again makes it the newest thing shown, though it
	// was created first.
	if _, err := ui.Show(ctx, service.AgentUIShowInput{TaskID: "task-a", Placement: service.AgentUIPanel, Format: service.AgentUIFormatMarkdown, Title: "Sync design v2", Key: "design", Content: "# Two"}); err != nil {
		t.Fatal(err)
	}
	shown, err = ui.Shown(ctx, "task-a")
	if err != nil {
		t.Fatal(err)
	}
	if shown.LatestID != design.ID || shown.LatestTitle != "Sync design v2" || shown.Artifacts != 1 || shown.Cards != 2 {
		t.Fatalf("after a revision: %#v, want latest %s", shown, design.ID)
	}
}

func TestAgentUIWebLinkOpensTheAgentOnOneItem(t *testing.T) {
	cases := []struct{ base, task, item, want string }{
		{"http://localhost:3080", "fekz3ove", "ui-0123456789", "http://localhost:3080/agents?task=fekz3ove&artifact=ui-0123456789"},
		{"https://legato.lan:3080/", "REX-12", "ui-0123456789", "https://legato.lan:3080/agents?task=REX-12&artifact=ui-0123456789"},
		{"http://localhost:3080", "st-0a1b2c3d4e", "", "http://localhost:3080/agents?task=st-0a1b2c3d4e"},
		{"http://localhost:3080", "a task/with?odd&chars", "ui-0123456789", "http://localhost:3080/agents?task=a+task%2Fwith%3Fodd%26chars&artifact=ui-0123456789"},
		// With no web UI to link to, there is no link.
		{"", "fekz3ove", "ui-0123456789", ""},
	}
	for _, tc := range cases {
		if got := service.AgentUIWebLink(tc.base, tc.task, tc.item); got != tc.want {
			t.Errorf("AgentUIWebLink(%q, %q, %q) = %q, want %q", tc.base, tc.task, tc.item, got, tc.want)
		}
	}
}
