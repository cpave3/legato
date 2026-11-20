package service

import (
	"strconv"
	"testing"

	"github.com/cpave3/legato/config"
)

// The registry's DefaultLabel of a setting whose zero value means a
// built-in one is the value the service uses then.
func TestTheRegistrysDefaultLabelsAreWhatTheServicesUse(t *testing.T) {
	runtime := map[string]string{
		"swarm.max_concurrent_agents":  strconv.Itoa(DefaultSwarmMaxConcurrentAgents),
		"swarm.max_subtasks_per_plan":  strconv.Itoa(DefaultSwarmMaxSubtasksPerPlan),
		"swarm.max_steps_per_plan":     strconv.Itoa(DefaultSwarmMaxStepsPerPlan),
		"swarm.brief_kickoff_delay_ms": strconv.Itoa(int(briefKickoffDelay.Milliseconds())),
		"updates.mode":                 UpdateModeAsk,
		"reeve.peer":                   PeerStart,
		"reeve.name":                   ReeveName("").Mid(),
	}
	for key, want := range runtime {
		s, ok := config.LookupSetting(key)
		if !ok {
			t.Fatalf("%s is not registered", key)
		}
		if s.DefaultLabel != want {
			t.Errorf("%s: DefaultLabel %q, the service uses %q", key, s.DefaultLabel, want)
		}
	}
}
