package service

import (
	"context"
	"testing"
)

type memoryValues map[string]string

func (m memoryValues) GetInstanceValue(_ context.Context, key string) (string, error) { return m[key], nil }
func (m memoryValues) SetInstanceValue(_ context.Context, key, value string) error {
	m[key] = value
	return nil
}

type checklistWorld struct {
	report      DoctorReport
	known       bool
	defaultTool string
	repos       int
	devices     int
	tracker     bool
	values      memoryValues
}

func (w *checklistWorld) checklist() *SetupChecklist {
	if w.values == nil {
		w.values = memoryValues{}
	}
	return NewSetupChecklist(SetupChecklistOptions{
		Checks:       func() (DoctorReport, bool) { return w.report, w.known },
		DefaultTool:  func(context.Context) string { return w.defaultTool },
		Repositories: func(context.Context) (int, error) { return w.repos, nil },
		Devices:      func(context.Context) (int, error) { return w.devices, nil },
		Tracker:      func(context.Context) bool { return w.tracker },
		Values:       w.values,
	})
}

var failingTmux = DoctorReport{Checks: []DoctorCheck{{ID: "tmux", Required: true, Result: DoctorMissing}}}

func TestTheSetupChecklistCountsWhatIsDone(t *testing.T) {
	w := &checklistWorld{known: true, defaultTool: "codex", repos: 2}
	st := w.checklist().Status(context.Background())
	if st.Done != 3 || st.Total != 4 || !st.Show {
		t.Fatalf("status = %d/%d show %v", st.Done, st.Total, st.Show)
	}
	for _, item := range st.Items {
		if item.ID == "tracker" && !item.Optional {
			t.Error("the tracker is counted")
		}
	}
	w.devices = 1
	if st := w.checklist().Status(context.Background()); st.Done != 4 || st.Show {
		t.Fatalf("all done: %d/%d show %v", st.Done, st.Total, st.Show)
	}
}

func TestTheSetupChecklistHidesWhenDismissedAndReturnsOnAFailedCheck(t *testing.T) {
	w := &checklistWorld{known: true}
	list := w.checklist()
	if err := list.SetDismissed(context.Background(), true); err != nil {
		t.Fatal(err)
	}
	if st := list.Status(context.Background()); st.Show || !st.Dismissed {
		t.Fatalf("dismissed but shown: %+v", st)
	}
	w.report = failingTmux
	if st := w.checklist().Status(context.Background()); !st.Show {
		t.Fatal("a failed required check did not bring it back")
	}
}

func TestAPhonePairedByPhraseCountsAsADevice(t *testing.T) {
	w := &checklistWorld{known: true}
	list := w.checklist()
	list.RecordPairing(context.Background(), PairingStatus{State: PairingPaired, Purpose: PairingPhone, Device: "Pixel"})
	for _, item := range list.Status(context.Background()).Items {
		if item.ID == "device" && !item.Done {
			t.Fatal("a phone paired by phrase is not counted")
		}
	}
}
