package main

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
)

func TestDeviceResetRemovesEveryDeviceAndIsRecorded(t *testing.T) {
	credentialTestConfig(t)
	cfg, _ := config.Load()
	db, err := openStore(cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	_ = db.InsertApprovalDevice(ctx, store.ApprovalDevice{ID: "dev-1", Kind: "android", Name: "Pixel 9 Pro", PublicKey: []byte{1}, SecurityLevel: "strongbox", Status: store.DeviceActive})
	db.Close()

	out := captureStdout(t, func() int { return runCLI([]string{"device", "list"}) })
	if !strings.Contains(out, "Pixel 9 Pro") || !strings.Contains(out, "strongbox") {
		t.Fatalf("list = %q", out)
	}
	out = captureStdout(t, func() int { return runCLI([]string{"device", "reset"}) })
	if !strings.Contains(out, "Removed 1 device") {
		t.Fatalf("reset = %q", out)
	}
	db, _ = openStore(cfg)
	defer db.Close()
	active, _ := db.ListApprovalDevices(ctx, store.DeviceActive)
	if len(active) != 0 {
		t.Fatalf("active = %+v", active)
	}
	rows, _ := db.ListCredentialAudit(ctx, store.CredentialAuditFilter{Event: "revoke"})
	if len(rows) != 1 || !strings.Contains(rows[0].Detail, "device reset") {
		t.Fatalf("audit = %+v", rows)
	}
}
