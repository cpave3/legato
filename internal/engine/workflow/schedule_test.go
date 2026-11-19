package workflow_test

import (
	"strings"
	"testing"
	"time"

	"github.com/cpave3/legato/internal/engine/workflow"
)

func zone(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func TestScheduleDueTimes(t *testing.T) {
	sydney := zone(t, "Australia/Sydney")
	london := zone(t, "Europe/London")
	cases := []struct {
		name     string
		schedule workflow.ScheduleTrigger
		loc      *time.Location
		after    time.Time
		want     []string
	}{
		{"daily", workflow.ScheduleTrigger{Every: "daily", At: "09:00"}, time.UTC,
			time.Date(2026, 11, 19, 9, 0, 0, 0, time.UTC),
			[]string{"2026-11-20T09:00:00Z", "2026-11-21T09:00:00Z"}},
		{"weekdays skip the weekend", workflow.ScheduleTrigger{Every: "weekdays", At: "09:30"}, time.UTC,
			time.Date(2026, 11, 20, 10, 0, 0, 0, time.UTC), // a Friday
			[]string{"2026-11-23T09:30:00Z", "2026-11-24T09:30:00Z"}},
		{"weekly on a day", workflow.ScheduleTrigger{Every: "weekly", At: "08:00", On: "monday"}, time.UTC,
			time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC),
			[]string{"2026-11-23T08:00:00Z", "2026-11-30T08:00:00Z"}},
		{"cron", workflow.ScheduleTrigger{Cron: "*/30 9-10 * * 1-5"}, time.UTC,
			time.Date(2026, 11, 19, 9, 45, 0, 0, time.UTC),
			[]string{"2026-11-19T10:00:00Z", "2026-11-19T10:30:00Z", "2026-11-20T09:00:00Z"}},
		{"a named zone", workflow.ScheduleTrigger{Every: "daily", At: "09:00"}, sydney,
			time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC),
			[]string{"2026-11-19T22:00:00Z", "2026-11-20T22:00:00Z"}},
		// London springs forward at 01:00 on 29 March 2026: 01:30 does not
		// exist, and fires at the next valid minute, 02:00 BST.
		{"a time that does not exist", workflow.ScheduleTrigger{Every: "daily", At: "01:30"}, london,
			time.Date(2026, 3, 28, 12, 0, 0, 0, time.UTC),
			[]string{"2026-03-29T01:00:00Z", "2026-03-30T00:30:00Z"}},
		// London falls back at 02:00 on 25 October 2026: 01:30 comes twice,
		// and fires once.
		{"a time that repeats", workflow.ScheduleTrigger{Every: "daily", At: "01:30"}, london,
			time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC),
			[]string{"2026-10-25T00:30:00Z", "2026-10-26T01:30:00Z"}},
		{"an hourly cron through the repeat", workflow.ScheduleTrigger{Cron: "30 * * * *"}, london,
			time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC),
			[]string{"2026-10-25T00:30:00Z", "2026-10-25T02:30:00Z", "2026-10-25T03:30:00Z"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			after := c.after
			for _, want := range c.want {
				got, ok := c.schedule.Next(after, c.loc)
				if !ok || got.UTC().Format(time.RFC3339) != want {
					t.Fatalf("Next(%s) = %s, want %s", after.UTC().Format(time.RFC3339), got.UTC().Format(time.RFC3339), want)
				}
				after = got
			}
		})
	}
}

func TestSchedulePreviousDueTime(t *testing.T) {
	s := workflow.ScheduleTrigger{Every: "weekdays", At: "09:00"}
	got, ok := s.Prev(time.Date(2026, 11, 23, 9, 0, 0, 0, time.UTC), time.UTC) // Monday 09:00
	if !ok || got.Format(time.RFC3339) != "2026-11-20T09:00:00Z" {
		t.Fatalf("Prev = %s", got)
	}
}

const scheduled = `
schema_version: 1
name: standup
title: Standup
inputs:
  since: {type: string}
on:
  schedule:
SCHEDULE
    inputs:
      since: ${{ schedule.previous_time }}
steps:
  - id: note
    title: Note
    type: task.note
    with:
      task: ${{ workflow.task }}
      body: hi
`

func TestScheduleDefinitions(t *testing.T) {
	ok := map[string]string{
		"every":    "    every: weekdays\n    at: \"09:00\"\n",
		"weekly":   "    every: weekly\n    at: \"09:00\"\n    on: friday\n",
		"cron":     "    cron: \"0 9 * * 1-5\"\n    timezone: Australia/Sydney\n    catch_up: skip\n    enabled: false\n    max_runs_per_day: 2\n",
		"defaults": "    every: daily\n    at: \"23:59\"\n",
	}
	for name, body := range ok {
		definition, err := workflow.Parse("s.yaml", []byte(strings.Replace(scheduled, "SCHEDULE\n", body, 1)))
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if list := definition.On.List(); len(list) != 1 || list[0].Kind != workflow.TriggerSchedule {
			t.Fatalf("%s: triggers %+v", name, list)
		}
	}
	bad := map[string]string{
		"both every and cron": "    every: daily\n    at: \"09:00\"\n    cron: \"0 9 * * *\"\n",
		"neither":             "    timezone: UTC\n",
		"a bad at":            "    every: daily\n    at: \"25:00\"\n",
		"no at":               "    every: daily\n",
		"on without weekly":   "    every: daily\n    at: \"09:00\"\n    on: monday\n",
		"weekly without on":   "    every: weekly\n    at: \"09:00\"\n",
		"an unknown every":    "    every: hourly\n    at: \"09:00\"\n",
		"a bad cron":          "    cron: \"61 * * * *\"\n",
		"an unknown zone":     "    every: daily\n    at: \"09:00\"\n    timezone: Mars/Olympus\n",
		"a bad catch_up":      "    every: daily\n    at: \"09:00\"\n    catch_up: twice\n",
		"an unknown input":    "    every: daily\n    at: \"09:00\"\n    inputs: {nope: x}\n",
	}
	for name, body := range bad {
		text := strings.Replace(scheduled, "SCHEDULE\n", body, 1)
		if name == "an unknown input" {
			text = strings.Replace(text, "    inputs:\n      since: ${{ schedule.previous_time }}\n", "", 1)
		}
		if _, err := workflow.Parse("s.yaml", []byte(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}
