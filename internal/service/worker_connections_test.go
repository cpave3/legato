package service

import (
	"testing"
	"time"
)

func TestReconnectsBackOffFromASecondDoublingToAMinute(t *testing.T) {
	var got []time.Duration
	for attempt := range 9 {
		got = append(got, WorkerBackoff(attempt, time.Second, time.Minute))
	}
	want := []time.Duration{1, 2, 4, 8, 16, 32, 60, 60, 60}
	for i := range want {
		if got[i] != want[i]*time.Second {
			t.Fatalf("waits %v", got)
		}
	}
	// The default jitter spreads a wait over its second half.
	jitter := NewWorkerConnections(nil, WorkerConnectionsOptions{}).opts.Jitter
	for range 100 {
		if d := jitter(time.Minute); d < 30*time.Second || d > time.Minute {
			t.Fatalf("jittered %v", d)
		}
	}
}

func TestTheMainShowsItsOwnViewOfTheWorkerFirst(t *testing.T) {
	cases := []struct {
		away AwayTask
		want string
	}{
		{AwayTask{State: "pending"}, AwayViewStarting},
		{AwayTask{State: "in_progress", Activity: HandoffActivityWorking}, AwayViewWorking},
		{AwayTask{State: "in_progress", Activity: HandoffActivityWaiting}, AwayViewNeedsYou},
		{AwayTask{State: "in_progress", Activity: HandoffActivityIdle}, AwayViewIdle},
		{AwayTask{State: "in_progress", Activity: HandoffActivityEnded}, AwayViewEnded},
		{AwayTask{State: "in_progress", Activity: HandoffActivityInterrupted}, AwayViewInterrupted},
		{AwayTask{State: "done", Activity: HandoffActivityIdle}, AwayViewDone},
		{AwayTask{State: "failed", Failure: "no clone"}, AwayViewFailed},
		{AwayTask{State: "returning"}, AwayViewComingBack},
		{AwayTask{State: "done", WorkerUnreachableSince: "t"}, AwayViewUnreachable},
		{AwayTask{State: "done", WorkerUnreachableSince: "t", WorkerRefusedAt: "t"}, AwayViewNotAccepted},
		{AwayTask{State: "done", WorkerRefusedAt: "t", LostAt: "t"}, AwayViewLost},
	}
	for _, c := range cases {
		if got := awayView(c.away); got != c.want {
			t.Errorf("%+v: %s, want %s", c.away, got, c.want)
		}
	}
}
