package service

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/config"
)

func TestTheRolePromptSaysASetMayRunAtOnceAndToKeepToOneStep(t *testing.T) {
	for _, want := range []string{"may run at once", "one step at a time", "read its result before"} {
		if !strings.Contains(ReeveRolePrompt, want) {
			t.Errorf("the role prompt does not say %q", want)
		}
	}
}

func TestTheSnapshotSaysWhatRunsWithoutAsking(t *testing.T) {
	_, db, _, _ := newReeveFixture(t)
	on := true
	reeve := &config.ReeveConfig{
		Levels:             config.ReeveLevels{Change: "auto"},
		HostOperationsAuto: &on,
		Rules:              []config.ReeveRule{{Pattern: "POST /api/tasks*", Level: "auto"}},
	}
	got := ReeveSnapshot(context.Background(), ReeveSnapshotSource{Store: db, Reeve: reeve})
	for _, want := range []string{"change auto", "risky default", "Host operations: on, run without asking (not as root)", "Run without asking: changes, host operations not as root, POST /api/tasks*"} {
		if !strings.Contains(got, want) {
			t.Errorf("snapshot lacks %q:\n%s", want, got)
		}
	}
}
