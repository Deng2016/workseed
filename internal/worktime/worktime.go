package worktime

import "time"

const (
	storedTimeLayout = "2006-01-02 15:04:05"
	workdayStartHour = 10
	workdayEndHour   = 19
)

// DurationSeconds returns the working time between two UTC timestamps stored by
// SQLite. Working hours are 10:00 through 19:00 in the application's local time.
func DurationSeconds(startedAt, completedAt string) (int64, error) {
	started, err := time.ParseInLocation(storedTimeLayout, startedAt, time.UTC)
	if err != nil {
		return 0, err
	}
	completed, err := time.ParseInLocation(storedTimeLayout, completedAt, time.UTC)
	if err != nil {
		return 0, err
	}
	return int64(Duration(started, completed, time.Local) / time.Second), nil
}

// Duration returns the overlap between [started, completed] and each local
// workday. Every calendar day is included; weekends are not excluded.
func Duration(started, completed time.Time, location *time.Location) time.Duration {
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
		workdayStart := time.Date(day.Year(), day.Month(), day.Day(), workdayStartHour, 0, 0, 0, location)
		workdayEnd := time.Date(day.Year(), day.Month(), day.Day(), workdayEndHour, 0, 0, 0, location)
		intervalStart := maxTime(started, workdayStart)
		intervalEnd := minTime(completed, workdayEnd)
		if intervalEnd.After(intervalStart) {
			total += intervalEnd.Sub(intervalStart)
		}
		day = day.AddDate(0, 0, 1)
	}
	return total
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
