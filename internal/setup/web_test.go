package setup

import (
	"context"
	"strings"
	"testing"

	"github.com/cpave3/legato/internal/service"
)

type fakeWeb struct {
	on, pinned bool
	enabled    int
	paired     int
	pairErr    error
}

func (f *fakeWeb) State() (bool, bool) { return f.on, f.pinned }

func (f *fakeWeb) Enable(context.Context) (string, error) {
	f.enabled++
	f.on = true
	return "web.enabled: true is in /home/op/.config/legato/config.yaml", nil
}

func (f *fakeWeb) Address() string { return "https://workstation:3080" }

func (f *fakeWeb) Pair(context.Context) error {
	f.paired++
	return f.pairErr
}

func runWebStep(t *testing.T, input string, web *fakeWeb) *setupRun {
	t.Helper()
	return runSetup(t, "s\n"+input+"s\n", nil, []service.DoctorReport{machine(nil)}, func(r *setupRun) { r.web = web })
}

func TestSetupTurnsOnTheWebUIAndSaysWhereItIs(t *testing.T) {
	web := &fakeWeb{}
	r := runWebStep(t, "\nn\n", web)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if web.enabled != 1 || !strings.Contains(r.out.String(), "https://workstation:3080") {
		t.Fatalf("enabled %d times:\n%s", web.enabled, r.out.String())
	}
	if web.paired != 0 {
		t.Fatal("paired after a no")
	}
}

func TestSetupPairsAPhoneWhenAsked(t *testing.T) {
	web := &fakeWeb{}
	r := runWebStep(t, "y\ny\n", web)
	if r.err != nil || web.paired != 1 {
		t.Fatalf("err %v, paired %d", r.err, web.paired)
	}
}

func TestSetupPairsOnceLegatoStartsWhenNoWebServerRunsYet(t *testing.T) {
	web := &fakeWeb{pairErr: ErrPairOnStart}
	r := runWebStep(t, "y\ny\n", web)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if !r.flow.PairOnStart {
		t.Fatal("pairing was not left for the start")
	}
}

func TestSetupLeavesAWebSettingInTheFileAlone(t *testing.T) {
	web := &fakeWeb{on: false, pinned: true}
	r := runWebStep(t, "", web)
	if r.err != nil {
		t.Fatal(r.err)
	}
	if web.enabled != 0 || !strings.Contains(r.out.String(), "pinned in config.yaml") {
		t.Fatalf("enabled %d:\n%s", web.enabled, r.out.String())
	}
}

func TestSetupOffersPairingWhenTheWebUIIsAlreadyOn(t *testing.T) {
	web := &fakeWeb{on: true, pinned: true}
	r := runWebStep(t, "y\n", web)
	if r.err != nil || web.enabled != 0 || web.paired != 1 {
		t.Fatalf("err %v, enabled %d, paired %d", r.err, web.enabled, web.paired)
	}
}
