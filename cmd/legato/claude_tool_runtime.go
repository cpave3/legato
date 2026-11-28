package main

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/claudecode"
	"github.com/cpave3/legato/internal/engine/store"
	"github.com/cpave3/legato/internal/service"
)

// claudeChannelKey is the setting of the release channel Legato's own
// Claude Code follows.
const claudeChannelKey = "updates.claude.channel"

// claudeToolFolder is Legato's own folder of Claude Code versions.
func claudeToolFolder(cfg *config.Config) claudecode.Folder {
	return claudecode.Folder{Dir: filepath.Join(config.ResolveDataDir(cfg), service.ClaudeToolFolder)}
}

// claudeLaunch is the claude agents launch, asked at each launch: the
// operator's own (updates.claude.command), else Legato's own copy, else
// claude from PATH until Legato has adopted one.
func claudeLaunch(cfg *config.Config) func() (string, bool) {
	folder, override := claudeToolFolder(cfg), cfg.Updates.Claude.Command
	return func() (string, bool) { return service.ClaudeLaunchCommand(folder, override) }
}

// newClaudeTool is Legato's own Claude Code as a managed tool. db may be
// nil, which follows the stable channel.
func newClaudeTool(cfg *config.Config, db *store.Store) *service.ClaudeCodeTool {
	opts := service.ClaudeCodeToolOptions{
		Folder: claudeToolFolder(cfg),
		// A build is about 400 MB.
		Releases: claudecode.Releases{HTTP: &http.Client{Timeout: 15 * time.Minute}},
		Override: cfg.Updates.Claude.Command,
	}
	if db != nil {
		settings := service.StringSetting{Settings: settingsFor(db)}
		opts.Channel = func(ctx context.Context) string {
			ch, _ := settings.GetInstanceValue(ctx, claudeChannelKey)
			return ch
		}
	} else {
		opts.Channel = func(context.Context) string { return cfg.Updates.Claude.Channel }
	}
	return service.NewClaudeCodeTool(opts)
}

// claudeDoctorCommand has the doctor check the claude agents launch, with a
// note saying whose it is.
func claudeDoctorCommand(cfg *config.Config) func(id string) (string, string) {
	launch, folder := claudeLaunch(cfg), claudeToolFolder(cfg)
	return func(id string) (string, string) {
		if id != "claude-code" {
			return "", ""
		}
		command, managed := launch()
		switch {
		case cfg.Updates.Claude.Command != "":
			return command, "your own, from updates.claude.command"
		case !managed:
			return "", "" // claude on PATH, until Legato takes a copy
		}
		note := "Legato's own"
		if lg, _ := folder.LastGood(); lg != "" {
			note = fmt.Sprintf("Legato's own; last known good %s", lg)
		}
		return command, note
	}
}
