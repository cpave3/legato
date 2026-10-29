package store

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestAChallengeIsSpentOnce(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := time.Date(2026, 10, 29, 12, 0, 0, 0, time.UTC)
	if err := s.CreateApprovalChallenge(ctx, ApprovalChallenge{Hash: "h1", Purpose: "decide", ApprovalID: "ap-1", DeviceID: "dev-1", ExpiresAt: now.Add(5 * time.Minute)}); err != nil {
		t.Fatal(err)
	}
	got, err := s.SpendApprovalChallenge(ctx, "h1", now)
	if err != nil || got.ApprovalID != "ap-1" || got.DeviceID != "dev-1" || got.Purpose != "decide" {
		t.Fatalf("spend = %+v, %v", got, err)
	}
	if _, err := s.SpendApprovalChallenge(ctx, "h1", now); !errors.Is(err, ErrNotFound) {
		t.Fatalf("second spend err = %v", err)
	}
	_ = s.CreateApprovalChallenge(ctx, ApprovalChallenge{Hash: "h2", Purpose: "enroll", ExpiresAt: now.Add(time.Minute)})
	if _, err := s.SpendApprovalChallenge(ctx, "h2", now.Add(2*time.Minute)); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expired spend err = %v", err)
	}
}

func TestDevicesAreKeptAndRemoved(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	d := ApprovalDevice{ID: "dev-1", Kind: "android", Name: "Pixel 9 Pro", PublicKey: []byte{1, 2}, SecurityLevel: "strongbox", Status: DeviceActive}
	if err := s.InsertApprovalDevice(ctx, d); err != nil {
		t.Fatal(err)
	}
	_ = s.InsertApprovalDevice(ctx, ApprovalDevice{ID: "dev-2", Kind: "android", Name: "Old", PublicKey: []byte{3}, Status: DevicePending})
	active, err := s.ListApprovalDevices(ctx, DeviceActive)
	if err != nil || len(active) != 1 || active[0].Name != "Pixel 9 Pro" {
		t.Fatalf("active = %+v, %v", active, err)
	}
	if err := s.SetApprovalDeviceStatus(ctx, "dev-2", DeviceActive); err != nil {
		t.Fatal(err)
	}
	if err := s.TouchApprovalDevice(ctx, "dev-1"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetApprovalDevice(ctx, "dev-1")
	if got.LastUsedAt == nil {
		t.Fatal("last use not recorded")
	}
	if err := s.SetApprovalDeviceStatus(ctx, "dev-1", DeviceRemoved); err != nil {
		t.Fatal(err)
	}
	active, _ = s.ListApprovalDevices(ctx, DeviceActive)
	if len(active) != 1 || active[0].ID != "dev-2" {
		t.Fatalf("active = %+v", active)
	}
	n, err := s.RemoveAllApprovalDevices(ctx)
	if err != nil || n != 1 {
		t.Fatalf("reset removed %d, %v", n, err)
	}
}

func TestTheServerIDIsMadeOnceAndKept(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legato.db")
	s, err := New(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	first, err := s.InstanceValue(ctx, "server_id", func() string { return "srv-first" })
	if err != nil || first != "srv-first" {
		t.Fatalf("first = %q, %v", first, err)
	}
	s.Close()
	s, _ = New(path)
	defer s.Close()
	again, _ := s.InstanceValue(ctx, "server_id", func() string { return "srv-second" })
	if again != "srv-first" {
		t.Fatalf("after a restart the server ID is %q", again)
	}
}

func TestAnApprovalCanNeedASignatureAndRecordsItsDevice(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	a, _, err := s.InsertApproval(ctx, Approval{ID: "ap-1", Source: "credential", SourceKey: "k", Title: "t", Signed: true})
	if err != nil || !a.Signed {
		t.Fatalf("approval = %+v, %v", a, err)
	}
	closed, err := s.CloseApproval(ctx, "ap-1", ApprovalClose{Status: ApprovalApproved, DecidedBy: "phone", DecidedDevice: "dev-1"})
	if err != nil || closed.DecidedDevice != "dev-1" {
		t.Fatalf("closed = %+v, %v", closed, err)
	}
}
