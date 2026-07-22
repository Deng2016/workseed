package worktime

import (
	"time"

	"workseed/internal/utctime"
)

const (
	clockLayout  = "15:04"
	DefaultStart = "10:00"
	DefaultEnd   = "19:00"
)

// DurationSeconds returns the working time between two UTC timestamps stored by
// SQLite. Working hours are 10:00 through 19:00 in the application's local time.
func DurationSeconds(startedAt, completedAt string) (int64, error) {
	return DurationSecondsForSchedule(startedAt, completedAt, DefaultStart, DefaultEnd)
}

// DurationSecondsForSchedule returns working time using the supplied local
// workday boundaries in HH:MM format.
func DurationSecondsForSchedule(startedAt, completedAt, workdayStart, workdayEnd string) (int64, error) {
	started, err := utctime.Parse(startedAt)
	if err != nil {
		return 0, err
	}
	completed, err := utctime.Parse(completedAt)
	if err != nil {
		return 0, err
	}
	startClock, err := time.Parse(clockLayout, workdayStart)
	if err != nil {
		return 0, err
	}
	endClock, err := time.Parse(clockLayout, workdayEnd)
	if err != nil {
		return 0, err
	}
	startMinute := startClock.Hour()*60 + startClock.Minute()
	endMinute := endClock.Hour()*60 + endClock.Minute()
	if startMinute >= endMinute {
		return 0, &InvalidScheduleError{Start: workdayStart, End: workdayEnd}
	}
	return int64(durationForSchedule(started, completed, time.Local, startMinute, endMinute) / time.Second), nil
}

// Duration returns the overlap between [started, completed] and each local
// workday. Every calendar day is included; weekends are not excluded.
func Duration(started, completed time.Time, location *time.Location) time.Duration {
	return durationForSchedule(started, completed, location, 10*60, 19*60)
}

func durationForSchedule(started, completed time.Time, location *time.Location, startMinute, endMinute int) time.Duration {
	if location == nil {
		location = time.Local
	}
	if !completed.After(started) {
		return 0
	}

	started = started.In(location)
	completed = completed.In(location)
	day := time.Date(started.Year(), started.Month(), started.Day(), 0, 0, 0, 0, location)
	lastDay := time.Date(completed.Year(), completed.Month(), completed.Day(), 0, 0, 0, 0, location)
	var total time.Duration
	for !day.After(lastDay) {
		workdayStart := time.Date(day.Year(), day.Month(), day.Day(), startMinute/60, startMinute%60, 0, 0, location)
		workdayEnd := time.Date(day.Year(), day.Month(), day.Day(), endMinute/60, endMinute%60, 0, 0, location)
		intervalStart := maxTime(started, workdayStart)
		intervalEnd := minTime(completed, workdayEnd)
		if intervalEnd.After(intervalStart) {
			total += intervalEnd.Sub(intervalStart)
		}
		day = day.AddDate(0, 0, 1)
	}
	return total
}

// InvalidScheduleError reports a workday whose end does not follow its start.
type InvalidScheduleError struct {
	Start string
	End   string
}

func (e *InvalidScheduleError) Error() string {
	return "workday end must be later than workday start"
}

func maxTime(left, right time.Time) time.Time {
	if left.After(right) {
		return left
	}
	return right
}

func minTime(left, right time.Time) time.Time {
	if left.Before(right) {
		return left
	}
	return right
}
