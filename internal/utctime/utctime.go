package utctime

import (
	"fmt"
	"time"
)

const legacyDatabaseLayout = "2006-01-02 15:04:05"

var legacyDatabaseLayouts = []string{
	legacyDatabaseLayout,
	"2006-01-02T15:04:05",
}

// Parse accepts RFC 3339 timestamps and interprets legacy database timestamps
// without an explicit offset as UTC.
func Parse(value string) (time.Time, error) {
	if parsed, err := time.Parse(time.RFC3339Nano, value); err == nil {
		return parsed.UTC(), nil
	}
	for _, layout := range legacyDatabaseLayouts {
		if parsed, err := time.ParseInLocation(layout, value, time.UTC); err == nil {
			return parsed, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid UTC timestamp %q", value)
}

// FormatRFC3339 normalizes a timestamp to an explicit UTC offset.
func FormatRFC3339(value string) (string, error) {
	parsed, err := Parse(value)
	if err != nil {
		return "", err
	}
	return parsed.Format(time.RFC3339), nil
}

func FormatOptionalRFC3339(value *string) (*string, error) {
	if value == nil {
		return nil, nil
	}
	formatted, err := FormatRFC3339(*value)
	if err != nil {
		return nil, err
	}
	return &formatted, nil
}
