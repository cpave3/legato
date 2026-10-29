package service

import (
	"context"
	"strings"
	"testing"
)

func TestADeviceResetIsAnAttentionItemUntilDismissed(t *testing.T) {
	f := newDevicesFixture(t)
	ctx := context.Background()
	adapter := NewDeviceAttentionAdapter(f.devices)
	if adapter.Producer() != "devices" {
		t.Fatalf("producer = %q", adapter.Producer())
	}
	if items, err := adapter.Snapshot(ctx); err != nil || len(items) != 0 {
		t.Fatalf("before any reset: %+v, %v", items, err)
	}

	f.enroll(t, newPhone(t), "Pixel")
	if _, err := f.devices.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	items, err := adapter.Snapshot(ctx)
	if err != nil || len(items) != 1 {
		t.Fatalf("after a reset: %+v, %v", items, err)
	}
	item := items[0]
	if item.SignalKind != AttentionSignalDevicesReset || item.Urgency != "high" || item.SourceKey != "devices-reset" || item.SourceRevision == "" {
		t.Fatalf("item = %+v", item)
	}
	if !strings.Contains(item.Detail, "1 device") || !strings.Contains(item.Detail, "did not") {
		t.Fatalf("detail = %q", item.Detail)
	}
	if item.Navigation.Kind != "settings" || item.Navigation.Target != "devices" {
		t.Fatalf("navigation = %+v", item.Navigation)
	}

	// Enrolling a phone afterwards does not hide it: that phone may be the
	// one the reset was for.
	f.enroll(t, newPhone(t), "Someone's phone")
	again, _ := adapter.Snapshot(ctx)
	if len(again) != 1 || again[0].SourceRevision != item.SourceRevision {
		t.Fatalf("after enrolling: %+v", again)
	}

	// A second reset is a new revision, so a dismissed one shows again.
	if _, err := f.devices.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	second, _ := adapter.Snapshot(ctx)
	if len(second) != 1 || second[0].SourceRevision == item.SourceRevision {
		t.Fatalf("second reset revision = %+v, first %q", second, item.SourceRevision)
	}
}

func TestADeviceResetIsInTheDefaultAttentionPolicy(t *testing.T) {
	found := false
	for _, s := range defaultAttentionSignals {
		found = found || s == AttentionSignalDevicesReset
	}
	if !found || !knownAttentionSignals[AttentionSignalDevicesReset] {
		t.Fatal("devices_reset is not an allowed attention signal")
	}
}

func TestADismissedResetStaysHiddenUntilTheNextReset(t *testing.T) {
	f := newDevicesFixture(t)
	ctx := context.Background()
	attention := NewAttentionService(f.db, nil)
	if err := attention.RegisterProducer(NewDeviceAttentionAdapter(f.devices)); err != nil {
		t.Fatal(err)
	}
	if _, err := f.devices.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	if err := attention.Reconcile(ctx, "devices"); err != nil {
		t.Fatal(err)
	}
	items, _ := attention.List(ctx, AttentionFilter{})
	if len(items) != 1 || items[0].SignalKind != AttentionSignalDevicesReset {
		t.Fatalf("items = %+v", items)
	}
	if err := attention.Dismiss(ctx, items[0].ID, items[0].SourceRevision); err != nil {
		t.Fatal(err)
	}
	_ = attention.Reconcile(ctx, "devices")
	if items, _ := attention.List(ctx, AttentionFilter{}); len(items) != 0 {
		t.Fatalf("a dismissed reset is shown: %+v", items)
	}
	if _, err := f.devices.Reset(ctx); err != nil {
		t.Fatal(err)
	}
	_ = attention.Reconcile(ctx, "devices")
	if items, _ := attention.List(ctx, AttentionFilter{}); len(items) != 1 {
		t.Fatalf("a new reset is hidden: %+v", items)
	}
}
