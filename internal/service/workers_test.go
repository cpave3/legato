package service

import (
	"testing"

	"github.com/cpave3/legato/internal/engine/store"
)

func TestAWorkerOnAnOlderReleaseIsMarkedOlder(t *testing.T) {
	was := thisVersion
	t.Cleanup(func() { thisVersion = was })
	thisVersion = func() string { return "v0.22.0" }
	advert := func(v string) *string { s := `{"version":"` + v + `"}`; return &s }
	for _, tc := range []struct {
		theirs string
		older  bool
	}{{"v0.21.0", true}, {"v0.22.0", false}, {"v0.23.0", false}, {"dev", false}} {
		w := workerFromRow(store.Worker{InstanceID: "srv-1", Sandbox: true, Advert: advert(tc.theirs)})
		if w.Older != tc.older {
			t.Errorf("worker on %s: older = %v, want %v", tc.theirs, w.Older, tc.older)
		}
	}
	thisVersion = func() string { return "dev" }
	if w := workerFromRow(store.Worker{Advert: advert("v0.1.0")}); w.Older {
		t.Error("a dev main marks a release older")
	}
}
