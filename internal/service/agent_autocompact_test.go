package service

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

// autoCompactWorld is the agents an auto-compactor sees and what it asks.
type autoCompactWorld struct {
	percent   int
	agents    []AutoCompactAgent
	compacted []string
	refuse    error
}

func (w *autoCompactWorld) compactor() *AutoCompactor {
	return NewAutoCompactor(AutoCompactorOptions{
		Percent: func(context.Context) int { return w.percent },
		Agents:  func(context.Context) ([]AutoCompactAgent, error) { return w.agents, nil },
		Compact: func(_ context.Context, taskID string) error {
			w.compacted = append(w.compacted, taskID)
			return w.refuse
		},
	})
}

func TestAnIdleAgentOverTheThresholdIsCompacted(t *testing.T) {
	w := &autoCompactWorld{percent: 60, agents: []AutoCompactAgent{
		{TaskID: "FULL", Idle: true, Used: 130_000, Window: 200_000},
		{TaskID: "ROOMY", Idle: true, Used: 110_000, Window: 200_000},
		{TaskID: "BUSY", Idle: false, Used: 190_000, Window: 200_000},
		{TaskID: "UNKNOWN", Idle: true, Used: 0, Window: 0},
	}}

	w.compactor().Check(context.Background())

	if !reflect.DeepEqual(w.compacted, []string{"FULL"}) {
		t.Fatalf("compacted = %v, want only the idle agent over 60%%", w.compacted)
	}
}

func TestNothingIsCompactedWhileTheSettingIsOff(t *testing.T) {
	w := &autoCompactWorld{percent: 0, agents: []AutoCompactAgent{{TaskID: "FULL", Idle: true, Used: 199_000, Window: 200_000}}}

	w.compactor().Check(context.Background())

	if len(w.compacted) != 0 {
		t.Fatalf("compacted = %v, want none", w.compacted)
	}
}

// A compaction that failed, or left the context over the threshold, is not
// tried again on every check: only once the context has grown since.
func TestAnAgentIsNotCompactedAgainUntilItsContextGrows(t *testing.T) {
	w := &autoCompactWorld{percent: 60, refuse: errors.New("not enough messages"), agents: []AutoCompactAgent{
		{TaskID: "FULL", Idle: true, Used: 130_000, Window: 200_000},
	}}
	c := w.compactor()

	c.Check(context.Background())
	c.Check(context.Background())
	if len(w.compacted) != 1 {
		t.Fatalf("compacted = %v, want one try", w.compacted)
	}

	w.agents[0].Used = 140_000
	c.Check(context.Background())
	if len(w.compacted) != 2 {
		t.Fatalf("compacted = %v, want a second try once the context grew", w.compacted)
	}
}

// Compacted below the threshold, the agent is compacted again the next time
// it fills past it, however full that is.
func TestAnAgentCompactedBelowTheThresholdIsCompactedWhenItFillsAgain(t *testing.T) {
	w := &autoCompactWorld{percent: 60, agents: []AutoCompactAgent{
		{TaskID: "FULL", Idle: true, Used: 150_000, Window: 200_000},
	}}
	c := w.compactor()
	c.Check(context.Background())

	w.agents[0].Used = 20_000
	c.Check(context.Background())
	w.agents[0].Used = 125_000
	c.Check(context.Background())

	if len(w.compacted) != 2 {
		t.Fatalf("compacted = %v, want two compactions", w.compacted)
	}
}
