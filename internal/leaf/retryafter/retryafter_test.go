package retryafter

import (
	"net/http"
	"testing"
	"time"
)

func TestParse(t *testing.T) {
	now := time.Date(2026, 11, 27, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name  string
		value string
		want  time.Duration
	}{
		{"empty", "", 0},
		{"blank", "   ", 0},
		{"delta seconds", "120", 2 * time.Minute},
		{"delta seconds with spaces", " 90 ", 90 * time.Second},
		{"zero seconds", "0", 0},
		{"negative seconds", "-5", 0},
		{"http date in the future", now.Add(3 * time.Minute).Format(http.TimeFormat), 3 * time.Minute},
		{"http date in the past", now.Add(-time.Minute).Format(http.TimeFormat), 0},
		{"http date now", now.Format(http.TimeFormat), 0},
		{"fractional seconds are not a delta", "1.5", 0},
		{"garbage", "soon", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := Parse(tc.value, now); got != tc.want {
				t.Fatalf("Parse(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}
