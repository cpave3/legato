package service

import (
	"context"
	"errors"
	"testing"
)

// updateItems reconciles the updates producer and lists its items.
func updateItems(t *testing.T, f *updaterFixture) (*AttentionService, []AttentionItem) {
	t.Helper()
	ctx := context.Background()
	attention := NewAttentionService(f.db, nil)
	if err := attention.RegisterProducer(NewUpdateAttentionAdapter(f.svc)); err != nil {
		t.Fatal(err)
	}
	if err := attention.RegisterHandler("updates", NewUpdateAttentionAdapter(f.svc)); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, "updates"); err != nil {
		t.Fatal(err)
	}
	items, err := attention.List(ctx, AttentionFilter{})
	if err != nil {
		t.Fatal(err)
	}
	return attention, items
}

func TestAnAvailableUpdateAsksThePersonInAttention(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	ctx := context.Background()
	if _, err := f.svc.Check(ctx); err != nil {
		t.Fatal(err)
	}

	attention, items := updateItems(t, f)

	if len(items) != 1 {
		t.Fatalf("items = %+v", items)
	}
	item := items[0]
	if item.SignalKind != AttentionSignalUpdateAvailable || item.Title != "Legato v1.3.0 is available" || item.SourceRevision != "v1.3.0" {
		t.Errorf("item = %+v", item)
	}
	if item.Navigation.Kind != "settings" || item.Navigation.Target != "updates" {
		t.Errorf("navigation = %+v, want Settings › Updates", item.Navigation)
	}

	if err := attention.Execute(ctx, item.ID, AttentionActionRequest{ActionID: "update", ExpectedRevision: item.SourceRevision}); err != nil {
		t.Fatal(err)
	}
	if got := f.releases.installs(); len(got) != 1 || got[0] != "v1.3.0" {
		t.Errorf("installed %v, want v1.3.0", got)
	}
	if items, _ := attention.List(ctx, AttentionFilter{}); len(items) != 0 {
		t.Errorf("item still shown once updating: %+v", items)
	}
}

func TestAFailedUpdateOffersToTryAgain(t *testing.T) {
	f := newUpdater(t, "v1.2.0")
	f.releases.installErr = errors.New("checksum does not match")
	_ = f.svc.Apply(context.Background())

	_, items := updateItems(t, f)

	if len(items) != 1 || items[0].Title != "Updating Legato to v1.3.0 failed" || items[0].Detail != "checksum does not match" {
		t.Fatalf("items = %+v", items)
	}
	if items[0].Actions[0].ID != "update" || items[0].Actions[0].Label != "Try again" {
		t.Errorf("actions = %+v", items[0].Actions)
	}
}

func TestNoUpdateItemWhenThereIsNothingToAsk(t *testing.T) {
	cases := map[string]func(t *testing.T, f *updaterFixture){
		"up to date": func(t *testing.T, f *updaterFixture) {
			f.releases.latest = release("v1.2.0")
			_, _ = f.svc.Check(context.Background())
		},
		"auto mode installs it without asking": func(t *testing.T, f *updaterFixture) {
			_ = f.svc.SetMode(context.Background(), UpdateModeAuto)
			_, _ = f.svc.Check(context.Background())
		},
		"updates off": func(t *testing.T, f *updaterFixture) {
			_ = f.svc.SetMode(context.Background(), UpdateModeOff)
			_, _ = f.svc.Check(context.Background())
		},
		"never checked": func(*testing.T, *updaterFixture) {},
	}
	for name, setup := range cases {
		t.Run(name, func(t *testing.T) {
			f := newUpdater(t, "v1.2.0")
			setup(t, f)
			if _, items := updateItems(t, f); len(items) != 0 {
				t.Errorf("items = %+v", items)
			}
		})
	}
}

func TestAnSTReleaseAsksThePersonInAttentionInAskMode(t *testing.T) {
	newFakeSTTool(t, "v0.1.0", true, "v0.2.0")
	st := NewSTUpdater(ToolUpdaterOptions{})
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) { o.Tools = []*ToolUpdater{st} })
	ctx := context.Background()
	if _, err := f.svc.Check(ctx); err != nil {
		t.Fatal(err)
	}

	attention, items := updateItems(t, f)
	if len(items) != 1 || items[0].Title != "st v0.2.0 is available" || items[0].SourceRevision != "st:v0.2.0" {
		t.Fatalf("items = %+v", items)
	}
	if err := attention.Execute(ctx, items[0].ID, AttentionActionRequest{ActionID: "update", ExpectedRevision: items[0].SourceRevision}); err != nil {
		t.Fatal(err)
	}
	if got := st.Status(); got.Version != "v0.2.0" {
		t.Fatalf("st after the action = %+v", got)
	}
	if err := f.svc.SetMode(ctx, UpdateModeAuto); err != nil {
		t.Fatal(err)
	}
	if items, _ := attention.List(ctx, AttentionFilter{}); len(items) != 0 {
		t.Errorf("items once st is current = %+v", items)
	}
}

func TestAChimeraReleaseAsksTheOperatorInAttentionInAskMode(t *testing.T) {
	newFakeChimeraTool(t, "v1.1.0", true)
	chimera := NewChimeraUpdater(ToolUpdaterOptions{})
	f := newUpdater(t, "v1.3.0", func(o *UpdateOptions) { o.Tools = []*ToolUpdater{chimera} })
	ctx := context.Background()
	if _, err := f.svc.Check(ctx); err != nil {
		t.Fatal(err)
	}

	attention, items := updateItems(t, f)
	if len(items) != 1 || items[0].Title != "chimera v1.2.0 is available" || items[0].SourceRevision != "chimera:v1.2.0" || items[0].SourceKey != "tool-update:chimera" {
		t.Fatalf("items = %+v", items)
	}
	if err := attention.Execute(ctx, items[0].ID, AttentionActionRequest{ActionID: "update", ExpectedRevision: items[0].SourceRevision}); err != nil {
		t.Fatal(err)
	}
	if got := chimera.Status(); got.Version != "v1.2.0" {
		t.Fatalf("chimera after the action = %+v", got)
	}
	if err := f.svc.SetMode(ctx, UpdateModeAuto); err != nil {
		t.Fatal(err)
	}
	if items, _ := attention.List(ctx, AttentionFilter{}); len(items) != 0 {
		t.Errorf("items once chimera is current = %+v", items)
	}
}
