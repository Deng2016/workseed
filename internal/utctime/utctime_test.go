package utctime

import (
	"testing"
	"time"
)

func TestParseTreatsLegacyDatabaseFormatsAsUTC(t *testing.T) {
	for _, value := range []string{"2026-07-22 10:30:00", "2026-07-22T10:30:00", "2026-07-22T18:30:00+08:00"} {
		parsed, err := Parse(value)
		if err != nil {
			t.Fatalf("Parse(%q): %v", value, err)
		}
		if parsed.Location() != time.UTC || parsed.Format(time.RFC3339) != "2026-07-22T10:30:00Z" {
			t.Fatalf("Parse(%q) = %s in %s", value, parsed.Format(time.RFC3339), parsed.Location())
		}
	}
}

func TestFormatRFC3339(t *testing.T) {
	formatted, err := FormatRFC3339("2026-07-22 10:30:00")
	if err != nil {
		t.Fatal(err)
	}
	if formatted != "2026-07-22T10:30:00Z" {
		t.Fatalf("FormatRFC3339() = %q", formatted)
	}
	if _, err := FormatRFC3339("not-a-time"); err == nil {
		t.Fatal("invalid timestamp was accepted")
	}
}
