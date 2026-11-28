package main

import (
	"context"
	"sync"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/service"
)

var (
	doctorOnce sync.Once
	doctorInst *service.Doctor
)

// processDoctor is this process's doctor: one, so a spawn reads the tool
// checks the start-up run and earlier spawns left. It checks the claude
// agents launch (cfg's, the first time it is asked).
func processDoctor(cfg *config.Config) *service.Doctor {
	doctorOnce.Do(func() { doctorInst = newDoctor(cfg) })
	return doctorInst
}

// newDoctor is a doctor for cfg's machine.
func newDoctor(cfg *config.Config) *service.Doctor {
	return service.NewDoctor(service.DoctorOptions{ToolCommand: claudeDoctorCommand(cfg)})
}

// agentsOffReason says why this process has no agent service, with the fix:
// the doctor's tmux check, or what else stopped tmux being used.
func agentsOffReason(ctx context.Context, cfg *config.Config) string {
	check, _ := processDoctor(cfg).CheckTool(ctx, "tmux")
	switch check.Result {
	case service.DoctorMissing:
		return "tmux not found: agents are off. " + check.Fix
	case service.DoctorOK:
		return "tmux could not be used: agents are off. Run legato doctor."
	}
	return "tmux cannot run: agents are off. " + check.Detail + ". " + check.Fix
}

// startDoctorReports gives a web server the doctor's reports, read with
// this process's configuration, and runs the checks once at start.
func startDoctorReports(cfg *config.Config) *service.DoctorReports {
	reports := service.NewDoctorReports(processDoctor(cfg), func() service.DoctorConfig {
		return doctorConfigFor(cfg)
	})
	go reports.Recheck(context.Background())
	return reports
}
