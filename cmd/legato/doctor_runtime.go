package main

import (
	"context"
	"sync"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

var (
	doctorOnce sync.Once
	doctorInst *service.Doctor
)

// processDoctor is this process's doctor: one, so a spawn reads the tool
// checks the start-up run and earlier spawns left.
func processDoctor() *service.Doctor {
	doctorOnce.Do(func() { doctorInst = service.NewDoctor(service.DoctorOptions{}) })
	return doctorInst
}

// agentsOffReason says why this process has no agent service, with the fix:
// the doctor's tmux check, or what else stopped tmux being used.
func agentsOffReason(ctx context.Context) string {
	check, _ := processDoctor().CheckTool(ctx, "tmux")
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
	reports := service.NewDoctorReports(processDoctor(), func() service.DoctorConfig {
		global, project := workflowDirs()
		return doctorConfig(cfg, workflow.DiscoverPaths(legacyWorkflowDir(), global, project).Definitions)
	})
	go reports.Recheck(context.Background())
	return reports
}
