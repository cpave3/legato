package store

import (
	"testing"
	"time"
)

func TestUnionDuration(t *testing.T) {
	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)
	cases := []struct {
		name      string
		intervals []interval
		want      time.Duration
	}{
		{name: "empty"},
		{name: "single", intervals: []interval{{start: base, end: base.Add(10 * time.Minute)}}, want: 10 * time.Minute},
		{name: "disjoint", intervals: []interval{{start: base, end: base.Add(10 * time.Minute)}, {start: base.Add(20 * time.Minute), end: base.Add(30 * time.Minute)}}, want: 20 * time.Minute},
		{name: "overlapping", intervals: []interval{{start: base, end: base.Add(15 * time.Minute)}, {start: base.Add(10 * time.Minute), end: base.Add(20 * time.Minute)}}, want: 20 * time.Minute},
		{name: "nested", intervals: []interval{{start: base, end: base.Add(20 * time.Minute)}, {start: base.Add(5 * time.Minute), end: base.Add(10 * time.Minute)}}, want: 20 * time.Minute},
		{name: "adjacent", intervals: []interval{{start: base, end: base.Add(10 * time.Minute)}, {start: base.Add(10 * time.Minute), end: base.Add(20 * time.Minute)}}, want: 20 * time.Minute},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if got := unionDuration(test.intervals); got != test.want {
				t.Fatalf("union duration = %v, want %v", got, test.want)
			}
		})
	}
}
