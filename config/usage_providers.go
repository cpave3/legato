package config

import "slices"

// The subscription usage providers Legato knows: their stable identity, the
// name the operator knows them by, and the meters they report. This table is
// the one place that declares them: the service's provider and meter labels,
// the TUI's short labels, the web's catalog (served from /api/usage) and the
// meters usage rules and pins may name all derive from it. The engine reader
// of each provider maps its own endpoint's names onto these meters.

// UsageMeterInfo is one meter a provider can report: a window of its limits,
// with the labels every surface shows it under.
type UsageMeterInfo struct {
	// Window is the meter's name in a reading: "five_hour".
	Window string
	// Short is the label a brief line shows: "5h".
	Short string
	// Long is the label a full line shows: "5-hour".
	Long string
}

// UsageProviderInfo is one provider Legato can read the usage of.
type UsageProviderInfo struct {
	// ID is the provider's stable identity, in readings and qualified
	// meters: "claude".
	ID string
	// Name is the provider's name as the operator knows it: "Claude".
	Name string
	// Windows are the meters this provider reports, in display order;
	// each is a UsageMeterInfo.
	Windows []string
}

// usageMeters are the meters any provider may report, once each: the meter
// names are shared, so a window's label is one label.
var usageMeters = []UsageMeterInfo{
	{Window: "five_hour", Short: "5h", Long: "5-hour"},
	{Window: "weekly", Short: "wk", Long: "weekly"},
	{Window: "weekly_opus", Short: "Opus", Long: "weekly Opus"},
	{Window: "weekly_sonnet", Short: "Sonnet", Long: "weekly Sonnet"},
	{Window: "monthly", Short: "mo", Long: "monthly"},
}

var usageProviders = []UsageProviderInfo{
	{ID: "claude", Name: "Claude", Windows: []string{"five_hour", "weekly", "weekly_opus", "weekly_sonnet"}},
	{ID: "codex", Name: "Codex", Windows: []string{"five_hour", "weekly"}},
	{ID: "opencode", Name: "OpenCode Go", Windows: []string{"five_hour", "weekly", "monthly"}},
}

// UsageProviders are the providers Legato knows, in the order they are read
// and shown.
func UsageProviders() []UsageProviderInfo { return slices.Clone(usageProviders) }

// UsageMeterInfoOf is the meter a window names, with its labels.
func UsageMeterInfoOf(window string) (UsageMeterInfo, bool) {
	for _, m := range usageMeters {
		if m.Window == window {
			return m, true
		}
	}
	return UsageMeterInfo{}, false
}

// UsageQualifiedMeters are the meters usage rules and pins may name, one per
// provider window, in provider then display order: "claude.five_hour", …
func UsageQualifiedMeters() []string {
	var out []string
	for _, p := range usageProviders {
		for _, window := range p.Windows {
			out = append(out, p.ID+"."+window)
		}
	}
	return out
}
