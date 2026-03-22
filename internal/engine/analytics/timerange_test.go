package analytics

import (
	"testing"
	"time"
)

func TestToday(t *testing.T) {
	tr := Today()
	now := time.Now().Local()

	if tr.Period != PeriodDay {
		t.Errorf("expected PeriodDay, got %v", tr.Period)
	}
	if tr.Label != "Today" {
		t.Errorf("expected label 'Today', got %q", tr.Label)
	}
	if tr.Start.Hour() != 0 || tr.Start.Minute() != 0 || tr.Start.Second() != 0 {
		t.Errorf("expected midnight start, got %v", tr.Start)
	}
	if tr.End.Before(tr.Start) {
		t.Error("End before Start")
	}
	if !sameDay(tr.Start, now) {
		t.Errorf("start not today: %v", tr.Start)
	}
}

func TestThisWeek(t *testing.T) {
	tr := ThisWeek()

	if tr.Period != PeriodWeek {
		t.Errorf("expected PeriodWeek, got %v", tr.Period)
	}
	if tr.Start.Weekday() != time.Monday {
		t.Errorf("expected Monday start, got %v", tr.Start.Weekday())
	}
	if tr.Label != "This Week" {
		t.Errorf("expected label 'This Week', got %q", tr.Label)
	}
}

func TestThisMonth(t *testing.T) {
	tr := ThisMonth()

	if tr.Period != PeriodMonth {
		t.Errorf("expected PeriodMonth, got %v", tr.Period)
	}
	if tr.Start.Day() != 1 {
		t.Errorf("expected day 1 start, got %d", tr.Start.Day())
	}
}

func TestWeekStartMonday(t *testing.T) {
	// Wednesday 2026-03-18
	wed := time.Date(2026, 3, 18, 14, 30, 0, 0, time.Local)
	start := weekStart(wed)
	if start.Weekday() != time.Monday {
		t.Errorf("expected Monday, got %v", start.Weekday())
	}
	if start.Day() != 16 {
		t.Errorf("expected March 16, got March %d", start.Day())
	}

	// Sunday 2026-03-22
	sun := time.Date(2026, 3, 22, 10, 0, 0, 0, time.Local)
	start = weekStart(sun)
	if start.Weekday() != time.Monday {
		t.Errorf("expected Monday, got %v", start.Weekday())
	}
	if start.Day() != 16 {
		t.Errorf("expected March 16, got March %d", start.Day())
	}

	// Monday itself
	mon := time.Date(2026, 3, 16, 8, 0, 0, 0, time.Local)
	start = weekStart(mon)
	if start.Day() != 16 {
		t.Errorf("expected March 16, got March %d", start.Day())
	}
}

func TestPreviousDay(t *testing.T) {
	today := Today()
	prev := Previous(today)

	if prev.Period != PeriodDay {
		t.Errorf("expected PeriodDay, got %v", prev.Period)
	}
	expected := today.Start.AddDate(0, 0, -1)
	if !sameDay(prev.Start, expected) {
		t.Errorf("expected %v, got %v", expected, prev.Start)
	}
}

func TestPreviousWeek(t *testing.T) {
	thisWeek := ThisWeek()
	prev := Previous(thisWeek)

	if prev.Period != PeriodWeek {
		t.Errorf("expected PeriodWeek, got %v", prev.Period)
	}
	expectedStart := thisWeek.Start.AddDate(0, 0, -7)
	if !sameDay(prev.Start, expectedStart) {
		t.Errorf("expected start %v, got %v", expectedStart, prev.Start)
	}
	if prev.Start.Weekday() != time.Monday {
		t.Errorf("expected Monday, got %v", prev.Start.Weekday())
	}
}

func TestPreviousMonth(t *testing.T) {
	thisMonth := ThisMonth()
	prev := Previous(thisMonth)

	if prev.Period != PeriodMonth {
		t.Errorf("expected PeriodMonth, got %v", prev.Period)
	}
	if prev.Start.Day() != 1 {
		t.Errorf("expected day 1, got %d", prev.Start.Day())
	}
	expectedMonth := thisMonth.Start.Month() - 1
	if expectedMonth == 0 {
		expectedMonth = 12
	}
	if prev.Start.Month() != expectedMonth {
		t.Errorf("expected month %v, got %v", expectedMonth, prev.Start.Month())
	}
}

func TestNextDayAtCurrentReturnsSame(t *testing.T) {
	today := Today()
	next := Next(today)

	if !sameDay(next.Start, today.Start) {
		t.Errorf("Next from today should return today, got %v", next.Start)
	}
}

func TestNextDayFromPast(t *testing.T) {
	today := Today()
	twoDaysAgo := Previous(Previous(today))
	next := Next(twoDaysAgo)

	expectedStart := twoDaysAgo.Start.AddDate(0, 0, 1)
	if !sameDay(next.Start, expectedStart) {
		t.Errorf("expected %v, got %v", expectedStart, next.Start)
	}
}

func TestNextWeekAtCurrentReturnsSame(t *testing.T) {
	thisWeek := ThisWeek()
	next := Next(thisWeek)

	if !sameDay(next.Start, thisWeek.Start) {
		t.Errorf("Next from this week should return this week, got %v", next.Start)
	}
}

func TestNextMonthAtCurrentReturnsSame(t *testing.T) {
	thisMonth := ThisMonth()
	next := Next(thisMonth)

	if !sameDay(next.Start, thisMonth.Start) {
		t.Errorf("Next from this month should return this month, got %v", next.Start)
	}
}

func TestPreviousThenNext(t *testing.T) {
	today := Today()
	yesterday := Previous(today)
	backToToday := Next(yesterday)

	if backToToday.Label != "Today" {
		t.Errorf("expected 'Today' label, got %q", backToToday.Label)
	}
}

func TestUTCConversion(t *testing.T) {
	tr := Today()
	utcStart := tr.StartUTC()
	utcEnd := tr.EndUTC()

	if utcStart.Location() != time.UTC {
		t.Error("StartUTC not in UTC")
	}
	if utcEnd.Location() != time.UTC {
		t.Error("EndUTC not in UTC")
	}
}

func TestFormatDayLabel(t *testing.T) {
	now := time.Now().Local()
	if label := formatDayLabel(now); label != "Today" {
		t.Errorf("expected 'Today', got %q", label)
	}
	yesterday := now.AddDate(0, 0, -1)
	if label := formatDayLabel(yesterday); label != "Yesterday" {
		t.Errorf("expected 'Yesterday', got %q", label)
	}
	older := now.AddDate(0, 0, -5)
	label := formatDayLabel(older)
	if label == "Today" || label == "Yesterday" {
		t.Errorf("expected formatted date, got %q", label)
	}
}
