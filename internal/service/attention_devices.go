package service

import (
	"context"
	"fmt"
	"strconv"
)

// DeviceAttentionAdapter shows the last `legato device reset` in Attention
// until the person dismisses it. A reset lets the next phone enroll with the
// web token alone, so one the person did not run must not go unseen.
type DeviceAttentionAdapter struct {
	devices *DeviceService
}

func NewDeviceAttentionAdapter(devices *DeviceService) *DeviceAttentionAdapter {
	return &DeviceAttentionAdapter{devices: devices}
}

func (a *DeviceAttentionAdapter) Producer() string { return "devices" }

func (a *DeviceAttentionAdapter) Snapshot(ctx context.Context) ([]AttentionInput, error) {
	reset, ok, err := a.devices.db.LastDeviceReset(ctx)
	if err != nil || !ok {
		return nil, err
	}
	devices := "1 device"
	if reset.Removed != 1 {
		devices = fmt.Sprintf("%d devices", reset.Removed)
	}
	return []AttentionInput{{
		SignalKind: AttentionSignalDevicesReset,
		SourceKey:  "devices-reset", SourceRevision: strconv.Itoa(reset.Seq),
		Title:  "Approval devices were reset",
		Reason: "legato device reset ran at the host at " + reset.At,
		Detail: fmt.Sprintf("It removed %s that sign approvals, so the next phone enrolls with the web token alone. If you did not do this, check Settings › Devices and the host.", devices),
		Category: "security", Urgency: "high",
		Navigation: AttentionNavigation{Kind: "settings", Target: "devices"},
	}}, nil
}
