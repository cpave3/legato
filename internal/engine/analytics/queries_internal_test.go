package analytics

import (
	"testing"
	"time"
)

func TestUnionDuration(t *testing.T) {
	base := time.Date(2026, 3, 20, 10, 0, 0, 0, time.UTC)

	cases := []struct {
		name     string
		ivs      []interval
		expected time.Duration
	}{
		{"empty", nil, 0},
		{"single", []interval{{start: base, end: base.Add(10 * time.Minute)}}, 10 * time.Minute},
		{"disjoint", []interval{
			{start: base, end: base.Add(10 * time.Minute)},
			{start: base.Add(20 * time.Minute), end: base.Add(30 * time.Minute)},
		}, 20 * time.Minute},
		{"overlapping", []interval{
			{start: base, end: base.Add(15 * time.Minute)},
			{start: base.Add(10 * time.Minute), end: base.Add(20 * time.Minute)},
		}, 20 * time.Minute},
		{"nested", []interval{
			{start: base, end: base.Add(20 * time.Minute)},
			{start: base.Add(5 * time.Minute), end: base.Add(10 * time.Minute)},
		}, 20 * time.Minute},
		{"adjacent", []interval{
			{start: base, end: base.Add(10 * time.Minute)},
			{start: base.Add(10 * time.Minute), end: base.Add(20 * time.Minute)},
		}, 20 * time.Minute},
		{"multiple_merged", []interval{
			{start: base, end: base.Add(5 * time.Minute)},
			{start: base.Add(3 * time.Minute), end: base.Add(7 * time.Minute)},
			{start: base.Add(15 * time.Minute), end: base.Add(20 * time.Minute)},
		}, 12 * time.Minute},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := unionDuration(tc.ivs)
			if got != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, got)
			}
		})
	}
}
