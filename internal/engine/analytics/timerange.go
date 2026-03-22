package analytics

import (
	"fmt"
	"time"
)

// PeriodType represents the granularity of a report period.
type PeriodType int

const (
	PeriodDay PeriodType = iota
	PeriodWeek
	PeriodMonth
)

// TimeRange represents a bounded time period for analytics queries.
// Start is inclusive, End is exclusive. Both are in local time.
type TimeRange struct {
	Start  time.Time
	End    time.Time
	Label  string
	Period PeriodType
}

// StartUTC returns Start converted to UTC for SQL queries.
func (tr TimeRange) StartUTC() time.Time {
	return tr.Start.UTC()
}

// EndUTC returns End converted to UTC for SQL queries.
func (tr TimeRange) EndUTC() time.Time {
	return tr.End.UTC()
}

// Today returns a TimeRange for the current local day (midnight to now).
func Today() TimeRange {
	now := time.Now().Local()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	return TimeRange{
		Start:  start,
		End:    now,
		Label:  "Today",
		Period: PeriodDay,
	}
}

// ThisWeek returns a TimeRange from Monday 00:00 to now (local time).
func ThisWeek() TimeRange {
	now := time.Now().Local()
	start := weekStart(now)
	return TimeRange{
		Start:  start,
		End:    now,
		Label:  "This Week",
		Period: PeriodWeek,
	}
}

// ThisMonth returns a TimeRange from the 1st of the current month to now.
func ThisMonth() TimeRange {
	now := time.Now().Local()
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	return TimeRange{
		Start:  start,
		End:    now,
		Label:  formatMonthLabel(start),
		Period: PeriodMonth,
	}
}

// Last7Days returns a TimeRange for the past 7 days.
func Last7Days() TimeRange {
	now := time.Now().Local()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -6)
	return TimeRange{
		Start:  start,
		End:    now,
		Label:  "Last 7 Days",
		Period: PeriodDay,
	}
}

// Last30Days returns a TimeRange for the past 30 days.
func Last30Days() TimeRange {
	now := time.Now().Local()
	start := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()).AddDate(0, 0, -29)
	return TimeRange{
		Start:  start,
		End:    now,
		Label:  "Last 30 Days",
		Period: PeriodDay,
	}
}

// Previous returns the adjacent earlier period of the same type.
func Previous(tr TimeRange) TimeRange {
	switch tr.Period {
	case PeriodDay:
		start := tr.Start.AddDate(0, 0, -1)
		end := start.AddDate(0, 0, 1) // exclusive: midnight of next day
		return TimeRange{
			Start:  start,
			End:    end,
			Label:  formatDayLabel(start),
			Period: PeriodDay,
		}
	case PeriodWeek:
		start := tr.Start.AddDate(0, 0, -7)
		end := start.AddDate(0, 0, 7) // exclusive: midnight of next Monday
		return TimeRange{
			Start:  start,
			End:    end,
			Label:  formatWeekLabel(start),
			Period: PeriodWeek,
		}
	case PeriodMonth:
		start := tr.Start.AddDate(0, -1, 0)
		end := time.Date(start.Year(), start.Month()+1, 1, 0, 0, 0, 0, start.Location()) // exclusive: midnight of first day of next month
		return TimeRange{
			Start:  start,
			End:    end,
			Label:  formatMonthLabel(start),
			Period: PeriodMonth,
		}
	}
	return tr
}

// Next returns the adjacent later period of the same type.
// Returns the same range if already at the current period (no future data).
func Next(tr TimeRange) TimeRange {
	now := time.Now().Local()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())

	switch tr.Period {
	case PeriodDay:
		nextStart := tr.Start.AddDate(0, 0, 1)
		if nextStart.After(today) {
			return tr
		}
		if sameDay(nextStart, today) {
			return Today()
		}
		end := nextStart.AddDate(0, 0, 1) // exclusive
		return TimeRange{
			Start:  nextStart,
			End:    end,
			Label:  formatDayLabel(nextStart),
			Period: PeriodDay,
		}
	case PeriodWeek:
		nextStart := tr.Start.AddDate(0, 0, 7)
		thisWeekStart := weekStart(now)
		if nextStart.After(thisWeekStart) {
			return tr
		}
		if nextStart.Equal(thisWeekStart) {
			return ThisWeek()
		}
		end := nextStart.AddDate(0, 0, 7) // exclusive
		return TimeRange{
			Start:  nextStart,
			End:    end,
			Label:  formatWeekLabel(nextStart),
			Period: PeriodWeek,
		}
	case PeriodMonth:
		nextStart := tr.Start.AddDate(0, 1, 0)
		thisMonthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		if nextStart.After(thisMonthStart) {
			return tr
		}
		if nextStart.Equal(thisMonthStart) {
			return ThisMonth()
		}
		end := time.Date(nextStart.Year(), nextStart.Month()+1, 1, 0, 0, 0, 0, nextStart.Location()) // exclusive
		return TimeRange{
			Start:  nextStart,
			End:    end,
			Label:  formatMonthLabel(nextStart),
			Period: PeriodMonth,
		}
	}
	return tr
}

func weekStart(t time.Time) time.Time {
	day := t.Weekday()
	offset := int(day) - int(time.Monday)
	if offset < 0 {
		offset += 7
	}
	start := t.AddDate(0, 0, -offset)
	return time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, t.Location())
}

func sameDay(a, b time.Time) bool {
	return a.Year() == b.Year() && a.Month() == b.Month() && a.Day() == b.Day()
}

func formatDayLabel(t time.Time) string {
	now := time.Now().Local()
	if sameDay(t, now) {
		return "Today"
	}
	if sameDay(t, now.AddDate(0, 0, -1)) {
		return "Yesterday"
	}
	return t.Format("Mon, Jan 2")
}

func formatWeekLabel(start time.Time) string {
	end := start.AddDate(0, 0, 6)
	now := time.Now().Local()
	if sameDay(weekStart(now), start) {
		return "This Week"
	}
	return fmt.Sprintf("Week of %s – %s", start.Format("Jan 2"), end.Format("Jan 2"))
}

func formatMonthLabel(t time.Time) string {
	now := time.Now().Local()
	if t.Year() == now.Year() && t.Month() == now.Month() {
		return "This Month"
	}
	if t.Year() == now.Year() {
		return t.Format("January")
	}
	return t.Format("January 2006")
}
