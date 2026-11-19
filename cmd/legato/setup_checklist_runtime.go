package main

import (
	"context"

	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/server"
	"github.com/cpave3/legato/internal/service"
)

// wireSetupChecklist gives a web server the setup checklist: the doctor's
// last report, the settings and presets as they are now, the devices, and
// the phones that pair by phrase from now on.
func wireSetupChecklist(srv *server.Server, db *store.Store, reports *service.DoctorReports, presets *service.RepositoryPresetService, pairing *service.PairingService) {
	settings := settingsFor(db)
	text := func(ctx context.Context, key string) string {
		value, err := settings.Get(ctx, key)
		if err != nil {
			return ""
		}
		s, _ := value.Value.(string)
		return s
	}
	list := service.NewSetupChecklist(service.SetupChecklistOptions{
		Checks: reports.Known,
		DefaultTool: func(ctx context.Context) string {
			return firstNonEmpty(text(ctx, "agents.default_tool"), text(ctx, "swarm.worker.agent"))
		},
		Repositories: func(ctx context.Context) (int, error) {
			if presets == nil {
				return 0, nil
			}
			list, err := presets.List(ctx)
			return len(list), err
		},
		Devices: func(ctx context.Context) (int, error) {
			devices, err := db.ListApprovalDevices(ctx, store.DeviceActive)
			return len(devices), err
		},
		Tracker: func(ctx context.Context) bool {
			if text(ctx, "jira.base_url") != "" {
				return true
			}
			_, err := db.GetIntegration(ctx, service.BurnliteIntegration)
			return err == nil
		},
		Values: db,
	})
	if pairing != nil {
		pairing.OnChange(func(st service.PairingStatus) { list.RecordPairing(context.Background(), st) })
	}
	srv.SetSetupChecklist(list)
}
