package version

import "testing"

func TestABuildFromSourceIsDev(t *testing.T) {
	// go test builds from source, with nothing stamped in.
	if got := String(); got != "dev" {
		t.Errorf("String() = %q, want dev", got)
	}
	if IsRelease() {
		t.Error("a build from source counts as a release")
	}
}

func TestAStampedReleaseReportsItsVersion(t *testing.T) {
	stamp(t, "v1.4.2")

	if got := String(); got != "v1.4.2" {
		t.Errorf("String() = %q, want v1.4.2", got)
	}
	if !IsRelease() {
		t.Error("v1.4.2 does not count as a release")
	}
}

func TestOnlyAStableVersionCountsAsARelease(t *testing.T) {
	for _, v := range []string{"1.4.2", "v1.4", "v1.4.2-rc.1", "v1.4.2+dirty", "dev", "vX.Y.Z"} {
		stamp(t, v)
		if IsRelease() {
			t.Errorf("%q counts as a release", v)
		}
	}
}

func stamp(t *testing.T, v string) {
	t.Helper()
	old := stamped
	stamped = v
	t.Cleanup(func() { stamped = old })
}

func TestNewer(t *testing.T) {
	cases := []struct {
		candidate, current string
		want               bool
	}{
		{"v1.2.4", "v1.2.3", true},
		{"v1.3.0", "v1.2.9", true},
		{"v2.0.0", "v1.99.99", true},
		{"v1.10.0", "v1.9.0", true},
		{"v1.2.3", "v1.2.3", false},
		{"v1.2.2", "v1.2.3", false},
		{"v1.2.4-rc.1", "v1.2.3", false},
		{"nonsense", "v1.2.3", false},
		{"v1.2.4", "dev", false},
	}
	for _, c := range cases {
		if got := Newer(c.candidate, c.current); got != c.want {
			t.Errorf("Newer(%q, %q) = %v, want %v", c.candidate, c.current, got, c.want)
		}
	}
}
