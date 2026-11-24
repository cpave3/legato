package main

import (
	"context"
	"errors"
	"log"
	"regexp"
	"strings"

	"github.com/cpave3/legato/config"
	"github.com/cpave3/legato/internal/engine/store"
	workflowengine "github.com/cpave3/legato/internal/engine/workflow"
	"github.com/cpave3/legato/internal/service"
)

// learningMigratedKey marks, in instance_values, that this Legato has
// turned the deprecated learning.enabled and learning.transition_columns
// into learn workflows (or found nothing to turn).
const learningMigratedKey = "learning.template_migrated"

// learningTriggerSwitches is the part of the trigger coordinator the
// migration uses: this Legato's switch of a workflow's trigger.
type learningTriggerSwitches interface {
	TriggerSwitch(ctx context.Context, workflowName, kind string) (enabled, ok bool, err error)
	SetTriggerEnabled(ctx context.Context, workflowName, kind string, enabled bool) error
}

// learningMigration is what migrateLearningTriggers works with.
type learningMigration struct {
	Config   *config.Config
	Store    *store.Store
	Triggers learningTriggerSwitches
	// Discover is every workflow this Legato reads, the new file included
	// once it is written.
	Discover func() workflowengine.DiscoveryResult
	// Dir is the global workflows folder the learn workflows are written to.
	Dir  string
	Logf func(format string, args ...any)
}

var nonWorkflowName = regexp.MustCompile(`[^a-z0-9]+`)

// migrateLearningTriggers runs once for a Legato, at its first start with
// the learn template. Automatic Learn used to be a definition built in Go
// for each learning.transition_columns column while learning.enabled was
// on. With learning.enabled on, it writes a learn workflow for each column
// the board has (learn, then learn-<column>) to the global workflows folder
// and switches its trigger on for this Legato only, unless the operator
// has already switched it. With learning.enabled off it writes nothing.
// Either way the two settings do nothing afterwards, which it logs while
// they are still set. The claim is atomic, so of a TUI and a serve starting
// together only one writes; a failed write lets the claim go, so the next
// start tries again.
func migrateLearningTriggers(ctx context.Context, m learningMigration) {
	if m.Logf == nil {
		m.Logf = log.Printf
	}
	logf := m.Logf
	learning := m.Config.Learning
	claimed, err := m.Store.ClaimInstanceValue(ctx, learningMigratedKey, "started")
	if err != nil {
		logf("learning: migrating learning.enabled: %v", err)
		return
	}
	if !claimed || !learning.Enabled || len(learning.TransitionColumns) == 0 {
		if claimed {
			_ = m.Store.SetInstanceValue(ctx, learningMigratedKey, "nothing")
		}
		warnDeprecatedLearningSettings(m.Config, logf)
		return
	}
	board := map[string]bool{}
	for _, column := range m.Config.Board.Columns {
		board[column.Name] = true
	}
	options := workflowengine.LearnTemplateOptions{
		Agent: firstNonEmpty(learning.CuratorAdapter, m.Config.DefaultAgentTool()), Tier: learning.CuratorTier,
		WorkingDir: learning.CuratorWorkingDir, MaxItems: learning.PacketMaxItems, MaxBytes: learning.PacketMaxBytes,
	}
	failed, named := false, 0
	for _, column := range learning.TransitionColumns {
		column = strings.TrimSpace(column)
		if !board[column] {
			logf("learning: no learn workflow for %q: the board has no such column", column)
			continue
		}
		name := "learn"
		if named > 0 {
			name += "-" + strings.Trim(nonWorkflowName.ReplaceAllString(strings.ToLower(column), "-"), "-")
		}
		named++
		if err := installLearnWorkflow(ctx, m, name, column, options); err != nil {
			logf("learning: installing workflow %q (on %s): %v; the next start tries again", name, column, err)
			failed = true
			continue
		}
	}
	if failed {
		_ = m.Store.DeleteInstanceValue(ctx, learningMigratedKey)
		return
	}
	_ = m.Store.SetInstanceValue(ctx, learningMigratedKey, "installed")
	warnDeprecatedLearningSettings(m.Config, logf)
}

func installLearnWorkflow(ctx context.Context, m learningMigration, name, column string, options workflowengine.LearnTemplateOptions) error {
	for _, definition := range m.Discover().Definitions {
		if definition.Name != name {
			continue
		}
		trigger := definition.On.TaskEnteredColumn
		if !definition.UsesLearning() || trigger == nil || trigger.Column != column {
			m.Logf("learning: a workflow named %q is already there; automatic Learn on %s was not installed. Add it with legato workflow add learn", name, column)
			return nil
		}
		return switchLearnOn(ctx, m, name, column)
	}
	data, err := workflowengine.LearnTemplateFor(name, column, options)
	if err != nil {
		return err
	}
	if _, err := workflowengine.CreateFile(m.Dir, name, data); err != nil && !errors.Is(err, workflowengine.ErrWorkflowNameTaken) {
		return err
	}
	return switchLearnOn(ctx, m, name, column)
}

// switchLearnOn switches a learn workflow's trigger on for this Legato,
// unless this Legato has a switch for it already.
func switchLearnOn(ctx context.Context, m learningMigration, name, column string) error {
	kind := workflowengine.TriggerTaskEnteredColumn
	if _, ok, err := m.Triggers.TriggerSwitch(ctx, name, kind); err != nil || ok {
		return err
	}
	if err := m.Triggers.SetTriggerEnabled(ctx, name, kind, true); err != nil {
		return err
	}
	m.Logf("learning: installed workflow %q, on when a card enters %s, from learning.enabled", name, column)
	return nil
}

// warnDeprecatedLearningSettings says, once a start, that the deprecated
// learning settings still set do nothing.
func warnDeprecatedLearningSettings(cfg *config.Config, logf func(string, ...any)) {
	var set []string
	if cfg.Learning.Enabled {
		set = append(set, "learning.enabled")
	}
	if len(cfg.Learning.TransitionColumns) > 0 {
		set = append(set, "learning.transition_columns")
	}
	if len(set) == 0 {
		return
	}
	logf("learning: %s no longer does anything: automatic Learn is the learn workflow, switched on and off on the Workflows tab (legato workflow add learn)", strings.Join(set, " and "))
}

// startLearningMigration runs the migration for a long-lived process, once
// its trigger coordinator is built and before it captures any transition.
func startLearningMigration(cfg *config.Config, db *store.Store, coordinator *service.WorkflowTriggerCoordinator) {
	global, _ := workflowDirs()
	migrateLearningTriggers(context.Background(), learningMigration{
		Config: cfg, Store: db, Triggers: coordinator, Dir: global,
		Discover: workflowDiscoveryProvider(cfg, db), Logf: log.Printf,
	})
}
