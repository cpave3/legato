package main

import (
	"context"
	"encoding/base64"
	"encoding/pem"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/cpave3/legato/internal/engine/ipc"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

func runDeviceContract(verb string, args []string) int {
	command := "device." + verb
	parsed, parseErr := parseCommandArgs(args, commandFlagSpecs("device", verb))
	jsonMode := parsed.Present["json"]
	if parseErr != nil {
		return renderCommandError(command, parseErr, jsonMode)
	}
	cfg, db, code := openCommandStoreWithConfig(command, jsonMode)
	if db == nil {
		return code
	}
	defer db.Close()
	ctx := context.Background()
	approvals := newApprovalService(cfg, db, nil, nil)
	devices := service.NewDeviceService(db, approvals, service.DeviceServiceOptions{AndroidDigests: cfg.ApprovalDevices.AndroidAppDigestBytes()})

	switch verb {
	case "list":
		list, err := devices.Devices(ctx)
		if err != nil {
			return renderCommandError(command, &commandError{Code: "store_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
		}
		var b strings.Builder
		if len(list) == 0 {
			b.WriteString("No device signs approval decisions. Enroll your phone in the Legato app (Settings › Devices on the web says how).\n")
		}
		w := tabwriter.NewWriter(&b, 0, 4, 2, ' ', 0)
		for _, d := range list {
			used := "never used"
			if d.LastUsedAt != nil {
				used = "last used " + *d.LastUsedAt
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", d.ID, d.Name, d.Kind, d.SecurityLevel, d.Status, used)
		}
		_ = w.Flush()
		return writeCommandSuccess(command, map[string]any{"devices": list}, b.String(), jsonMode)
	case "reset":
		n, err := devices.Reset(ctx)
		if err != nil {
			return renderCommandError(command, &commandError{Code: "store_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
		}
		detail := fmt.Sprintf("legato device reset: every approval device was removed (%d)", n)
		if _, err := db.AppendCredentialAudit(ctx, store.CredentialAuditEntry{Event: "revoke", DecidedBy: "host", Detail: detail}); err != nil {
			return renderCommandError(command, &commandError{Code: "audit_failed", Message: err.Error(), Exit: exitInternal}, jsonMode)
		}
		// The running Legato shows the reset in Attention.
		ipc.Broadcast(ipc.Message{Type: "approval_changed", Kind: "devices_reset"})
		if cfg.Notifications.Ntfy.Topic != "" {
			notifier := service.NewNtfyNotifier(cfg.Notifications.Ntfy.URL, cfg.Notifications.Ntfy.Topic, cfg.Notifications.Ntfy.Token)
			_ = notifier.Notify("Legato: devices reset", fmt.Sprintf("Every device that signs approvals was removed at the host (%d). If you did not do this, look at the host.", n))
		}
		return writeCommandSuccess(command, map[string]any{"removed": n},
			fmt.Sprintf("Removed %d device(s). The next phone to enroll is enrolled with the web token alone.\n", n), jsonMode)
	case "attestation":
		if len(parsed.Positionals) != 1 {
			return renderCommandError(command, usageError("missing_device", "name the device: legato device attestation <device-id>"), jsonMode)
		}
		chain, err := devices.AttestationChain(ctx, parsed.Positionals[0])
		if err != nil {
			return renderCommandError(command, &commandError{Code: "not_found", Message: err.Error(), Exit: exitNotFound}, jsonMode)
		}
		var b strings.Builder
		encoded := make([]string, 0, len(chain))
		for _, der := range chain {
			_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: der})
			encoded = append(encoded, base64.StdEncoding.EncodeToString(der))
		}
		return writeCommandSuccess(command, map[string]any{"chain": encoded}, b.String(), jsonMode)
	}
	return renderCommandError(command, usageError("unknown_verb", "device "+verb), jsonMode)
}
