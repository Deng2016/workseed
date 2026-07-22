package worktime

import (
	"testing"
	"time"
)

func TestDuration(t *testing.T) {
	location := time.FixedZone("UTC+8", 8*60*60)
	parse := func(value string) time.Time {
		t.Helper()
		parsed, err := time.ParseInLocation("2006-01-02 15:04:05", value, location)
		if err != nil {
			t.Fatal(err)
		}
		return parsed
	}

	tests := []struct {
		name      string
		started   string
		completed string
		want      time.Duration
	}{
		{name: "same workday", started: "2026-07-22 10:30:00", completed: "2026-07-22 12:00:00", want: 90 * time.Minute},
		{name: "clamp to working hours", started: "2026-07-22 08:00:00", completed: "2026-07-22 20:00:00", want: 9 * time.Hour},
		{name: "exclude overnight", started: "2026-07-22 18:00:00", completed: "2026-07-23 11:00:00", want: 2 * time.Hour},
		{name: "multiple days", started: "2026-07-22 18:00:00", completed: "2026-07-24 11:00:00", want: 11 * time.Hour},
		{name: "outside working hours", started: "2026-07-22 19:00:00", completed: "2026-07-23 10:00:00", want: 0},
		{name: "completed before started", started: "2026-07-23 11:00:00", completed: "2026-07-22 18:00:00", want: 0},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := Duration(parse(test.started), parse(test.completed), location); got != test.want {
				t.Fatalf("Duration() = %s, want %s", got, test.want)
			}
		})
	}
}

func TestDurationSecondsUsesLocalWorkingHours(t *testing.T) {
	previousLocal := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	t.Cleanup(func() { time.Local = previousLocal })

	got, err := DurationSeconds("2026-07-22 10:00:00", "2026-07-23 03:00:00")
	if err != nil {
		t.Fatal(err)
	}
	if got != 2*60*60 {
		t.Fatalf("DurationSeconds() = %d, want %d", got, 2*60*60)
	}
}
