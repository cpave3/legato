package workflow

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	// Schedules name zones; the binary carries their rules, so a host
	// without a zoneinfo database still knows them.
	_ "time/tzdata"
)

// TriggerSchedule is the kind of a schedule trigger.
const TriggerSchedule = "schedule"

// ScheduleTrigger starts a workflow at due times: every day, every weekday
// or once a week at a time, or as a cron expression says, in a time zone
// (the host's by default).
type ScheduleTrigger struct {
	// Every is daily, weekdays or weekly; with At (HH:MM) and, for weekly,
	// On (a weekday). Cron is the alternative to the three.
	Every string `yaml:"every,omitempty" json:"every,omitempty"`
	At    string `yaml:"at,omitempty" json:"at,omitempty"`
	On    string `yaml:"on,omitempty" json:"on,omitempty"`
	Cron  string `yaml:"cron,omitempty" json:"cron,omitempty"`
	// Timezone is an IANA zone; empty for the host's.
	Timezone string `yaml:"timezone,omitempty" json:"timezone,omitempty"`
	// CatchUp says what a start does about due times missed while no
	// Legato ran: once (one late run for all of them) or skip.
	CatchUp       string         `yaml:"catch_up,omitempty" json:"catch_up,omitempty"`
	Enabled       *bool          `yaml:"enabled,omitempty" json:"enabled,omitempty"`
	Confirm       bool           `yaml:"confirm,omitempty" json:"confirm,omitempty"`
	MaxRunsPerDay int            `yaml:"max_runs_per_day,omitempty" json:"max_runs_per_day,omitempty"`
	Inputs        map[string]any `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// Catch-up policies.
const (
	CatchUpOnce = "once"
	CatchUpSkip = "skip"
)

// CatchUpPolicy is the trigger's catch_up, once by default.
func (s *ScheduleTrigger) CatchUpPolicy() string {
	if s.CatchUp == "" {
		return CatchUpOnce
	}
	return s.CatchUp
}

var weekdayNames = map[string]int{"sunday": 0, "monday": 1, "tuesday": 2, "wednesday": 3, "thursday": 4, "friday": 5, "saturday": 6,
	"sun": 0, "mon": 1, "tue": 2, "wed": 3, "thu": 4, "fri": 5, "sat": 6}

var monthNames = map[string]int{"jan": 1, "feb": 2, "mar": 3, "apr": 4, "may": 5, "jun": 6, "jul": 7, "aug": 8, "sep": 9, "oct": 10, "nov": 11, "dec": 12}

// Validate checks the schedule: exactly one of every/at/on and cron, a
// time of day, a known zone and catch-up policy, and declared inputs.
func (s *ScheduleTrigger) Validate(inputs map[string]Input) error {
	if _, err := s.spec(); err != nil {
		field := "every"
		if s.Cron != "" {
			field = "cron"
		}
		return within(field, fmt.Errorf("schedule: %w", err))
	}
	if s.Timezone != "" {
		if _, err := time.LoadLocation(s.Timezone); err != nil {
			return within("timezone", fmt.Errorf("schedule: unknown timezone %q", s.Timezone))
		}
	}
	switch s.CatchUp {
	case "", CatchUpOnce, CatchUpSkip:
	default:
		return within("catch_up", fmt.Errorf("schedule: catch_up must be once or skip, got %q", s.CatchUp))
	}
	if err := validateMaxRunsPerDay(s.MaxRunsPerDay); err != nil {
		return within("max_runs_per_day", fmt.Errorf("schedule %w", err))
	}
	if err := validateTriggerInputs(s.Inputs, inputs); err != nil {
		return err
	}
	return nil
}

// Location is the zone the schedule's times are in: its timezone, else
// host.
func (s *ScheduleTrigger) Location(host *time.Location) *time.Location {
	if s.Timezone != "" {
		if loc, err := time.LoadLocation(s.Timezone); err == nil {
			return loc
		}
	}
	if host == nil {
		return time.Local
	}
	return host
}

// Describe says the schedule in words, for the Workflows page.
func (s *ScheduleTrigger) Describe() string {
	zone := ""
	if s.Timezone != "" {
		zone = " (" + s.Timezone + ")"
	}
	switch s.Every {
	case "daily":
		return "Every day at " + s.At + zone
	case "weekdays":
		return "Every weekday at " + s.At + zone
	case "weekly":
		return "Every " + strings.ToUpper(s.On[:1]) + strings.ToLower(s.On[1:]) + " at " + s.At + zone
	}
	return "On cron " + s.Cron + zone
}

// cronSpec is a parsed five-field cron expression: the minutes, hours,
// days of the month, months and weekdays it allows.
type cronSpec struct {
	minute, hour, dom, month, dow [64]bool
	domAny, dowAny                bool
}

// spec turns every/at/on, or cron, into one cron expression's sets.
func (s *ScheduleTrigger) spec() (*cronSpec, error) {
	hasEvery := s.Every != "" || s.At != "" || s.On != ""
	switch {
	case hasEvery && s.Cron != "":
		return nil, fmt.Errorf("give every/at/on or cron, not both")
	case !hasEvery && s.Cron == "":
		return nil, fmt.Errorf("give every and at, or cron")
	case s.Cron != "":
		return parseCron(s.Cron)
	}
	hour, minute, err := parseClock(s.At)
	if err != nil {
		return nil, err
	}
	dow := "*"
	switch s.Every {
	case "daily":
	case "weekdays":
		dow = "1-5"
	case "weekly":
		day, ok := weekdayNames[strings.ToLower(s.On)]
		if !ok {
			return nil, fmt.Errorf("weekly needs on: a weekday, got %q", s.On)
		}
		dow = strconv.Itoa(day)
	default:
		return nil, fmt.Errorf("every must be daily, weekdays or weekly, got %q", s.Every)
	}
	if s.On != "" && s.Every != "weekly" {
		return nil, fmt.Errorf("on is for every: weekly only")
	}
	return parseCron(fmt.Sprintf("%d %d * * %s", minute, hour, dow))
}

func parseClock(at string) (int, int, error) {
	parts := strings.Split(strings.TrimSpace(at), ":")
	if at == "" {
		return 0, 0, fmt.Errorf("at is required: a time such as \"09:00\"")
	}
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("at must be HH:MM, got %q", at)
	}
	hour, err1 := strconv.Atoi(parts[0])
	minute, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hour < 0 || hour > 23 || minute < 0 || minute > 59 || len(parts[1]) != 2 {
		return 0, 0, fmt.Errorf("at must be HH:MM, got %q", at)
	}
	return hour, minute, nil
}

// parseCron reads minute hour day-of-month month day-of-week, each a list
// of *, n, a-b, with an optional /step; months and weekdays may be names,
// and weekday 7 is Sunday.
func parseCron(expr string) (*cronSpec, error) {
	fields := strings.Fields(expr)
	if len(fields) != 5 {
		return nil, fmt.Errorf("cron %q: want 5 fields (minute hour day month weekday)", expr)
	}
	spec := &cronSpec{domAny: fields[2] == "*", dowAny: fields[4] == "*"}
	parts := []struct {
		set      *[64]bool
		min, max int
		names    map[string]int
	}{
		{&spec.minute, 0, 59, nil}, {&spec.hour, 0, 23, nil}, {&spec.dom, 1, 31, nil}, {&spec.month, 1, 12, monthNames}, {&spec.dow, 0, 7, weekdayNames},
	}
	for i, field := range fields {
		if err := parseCronField(field, parts[i].set, parts[i].min, parts[i].max, parts[i].names); err != nil {
			return nil, fmt.Errorf("cron %q: %w", expr, err)
		}
	}
	if spec.dow[7] {
		spec.dow[0] = true
	}
	return spec, nil
}

func parseCronField(field string, set *[64]bool, min, max int, names map[string]int) error {
	value := func(text string) (int, error) {
		if n, ok := names[strings.ToLower(text)]; ok {
			return n, nil
		}
		n, err := strconv.Atoi(text)
		if err != nil || n < min || n > max {
			return 0, fmt.Errorf("%q is not %d-%d", text, min, max)
		}
		return n, nil
	}
	for _, item := range strings.Split(field, ",") {
		rangeText, stepText, stepped := strings.Cut(item, "/")
		step := 1
		if stepped {
			n, err := strconv.Atoi(stepText)
			if err != nil || n < 1 {
				return fmt.Errorf("bad step in %q", item)
			}
			step = n
		}
		lo, hi := min, max
		switch {
		case rangeText == "*":
		case strings.Contains(rangeText, "-"):
			a, b, _ := strings.Cut(rangeText, "-")
			var err error
			if lo, err = value(a); err != nil {
				return err
			}
			if hi, err = value(b); err != nil {
				return err
			}
			if lo > hi {
				return fmt.Errorf("range %q runs backwards", rangeText)
			}
		default:
			n, err := value(rangeText)
			if err != nil {
				return err
			}
			lo, hi = n, n
			if stepped {
				hi = max
			}
		}
		for n := lo; n <= hi; n += step {
			set[n] = true
		}
	}
	return nil
}

func (c *cronSpec) dayMatches(date time.Time) bool {
	if !c.month[int(date.Month())] {
		return false
	}
	dom, dow := c.dom[date.Day()], c.dow[int(date.Weekday())]
	switch {
	case c.domAny && c.dowAny:
		return true
	case c.domAny:
		return dow
	case c.dowAny:
		return dom
	}
	return dom || dow
}

// wallInstant is the instant a wall-clock minute of a date names in loc. A
// minute that does not exist that day (clocks went forward) is the next
// minute that does; a minute that comes twice (clocks went back) is its
// first coming.
func wallInstant(year int, month time.Month, day, hour, minute int, loc *time.Location) time.Time {
	want := time.Date(year, month, day, hour, minute, 0, 0, time.UTC)
	for k := 0; k <= 24*60; k++ {
		wall := want.Add(time.Duration(k) * time.Minute)
		t := time.Date(wall.Year(), wall.Month(), wall.Day(), wall.Hour(), wall.Minute(), 0, 0, loc)
		local := t.In(loc)
		if local.Hour() != wall.Hour() || local.Minute() != wall.Minute() || local.Day() != wall.Day() {
			continue
		}
		// The same wall time an hour (or a zone's shift) earlier is its
		// first coming.
		for _, back := range []time.Duration{30 * time.Minute, time.Hour, 2 * time.Hour} {
			if earlier := t.Add(-back).In(loc); earlier.Hour() == wall.Hour() && earlier.Minute() == wall.Minute() && earlier.Day() == wall.Day() {
				t = t.Add(-back)
			}
		}
		return t
	}
	return time.Date(year, month, day, hour, minute, 0, 0, loc)
}

// searchDays bounds how far Next and Prev look: a cron for 29 February
// may wait four years.
const searchDays = 366*4 + 7

// Next is the schedule's first due time after after, in loc (the
// schedule's own zone wins); ok is false for a schedule that never comes.
func (s *ScheduleTrigger) Next(after time.Time, loc *time.Location) (time.Time, bool) {
	spec, err := s.spec()
	if err != nil {
		return time.Time{}, false
	}
	loc = s.Location(loc)
	start := after.In(loc)
	date := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, -1)
	for d := 0; d < searchDays; d++ {
		day := date.AddDate(0, 0, d)
		if !spec.dayMatches(day) {
			continue
		}
		for hour := 0; hour < 24; hour++ {
			if !spec.hour[hour] {
				continue
			}
			for minute := 0; minute < 60; minute++ {
				if !spec.minute[minute] {
					continue
				}
				if t := wallInstant(day.Year(), day.Month(), day.Day(), hour, minute, loc); t.After(after) {
					return t, true
				}
			}
		}
	}
	return time.Time{}, false
}

// Prev is the schedule's last due time before before; ok is false when
// there is none in the search window.
func (s *ScheduleTrigger) Prev(before time.Time, loc *time.Location) (time.Time, bool) {
	spec, err := s.spec()
	if err != nil {
		return time.Time{}, false
	}
	loc = s.Location(loc)
	start := before.In(loc)
	date := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, time.UTC).AddDate(0, 0, 1)
	for d := 0; d < searchDays; d++ {
		day := date.AddDate(0, 0, -d)
		if !spec.dayMatches(day) {
			continue
		}
		for hour := 23; hour >= 0; hour-- {
			if !spec.hour[hour] {
				continue
			}
			for minute := 59; minute >= 0; minute-- {
				if !spec.minute[minute] {
					continue
				}
				if t := wallInstant(day.Year(), day.Month(), day.Day(), hour, minute, loc); t.Before(before) {
					return t, true
				}
			}
		}
	}
	return time.Time{}, false
}

// DueBetween lists the due times in (from, to], at most limit of them.
func (s *ScheduleTrigger) DueBetween(from, to time.Time, loc *time.Location, limit int) []time.Time {
	var out []time.Time
	for t, ok := s.Next(from, loc); ok && !t.After(to) && len(out) < limit; t, ok = s.Next(t, loc) {
		out = append(out, t)
	}
	return out
}
