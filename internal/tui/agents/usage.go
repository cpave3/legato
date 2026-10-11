package agents

import (
	"fmt"
	"strings"
)

// Usage is how many tokens the agents used, when there is somewhere to read
// that from.
type Usage struct {
	TokensToday int64
	// InputToday and OutputToday are the two parts of TokensToday: what went
	// in to the models and what came out. Both are zero when only the total
	// is known.
	InputToday  int64
	OutputToday int64
	// Hourly holds the tokens of each recent hour, oldest first.
	Hourly []int64
}

// SetUsage sets the usage the view shows; nil when none is known.
func (m *Model) SetUsage(usage *Usage) {
	m.usage = usage
}

// hasParts reports whether the two parts of today's total are known.
func (u Usage) hasParts() bool {
	return u.InputToday > 0 || u.OutputToday > 0
}

// formatTokens writes a count of tokens in at most four characters and a
// unit: 950, 12.3k, 1.4M, 920M.
func formatTokens(tokens int64) string {
	value, unit := float64(tokens), ""
	for _, next := range []string{"k", "M", "B", "T"} {
		if value < 999.5 {
			break
		}
		value, unit = value/1000, next
	}
	if unit == "" || value >= 99.95 {
		return fmt.Sprintf("%.0f%s", value, unit)
	}
	return strings.TrimSuffix(fmt.Sprintf("%.1f", value), ".0") + unit
}

// levelBar draws a row of values as bars, each as tall as its share of the
// greatest. A row of nothing is a flat line.
func levelBar(values []int64) string {
	var most int64 = 1
	for _, value := range values {
		most = max(most, value)
	}
	levels := []rune("▁▂▃▄▅▆▇█")
	bar := make([]rune, len(values))
	for i, value := range values {
		bar[i] = levels[value*int64(len(levels)-1)/most]
	}
	return string(bar)
}
