package version

import (
	"runtime/debug"
	"testing"
)

func TestFromSettings(t *testing.T) {
	settings := []debug.BuildSetting{
		{Key: "vcs.revision", Value: "07b9a39284df3b7bef6a2344b329c870a73e6889"},
		{Key: "vcs.time", Value: "2026-07-21T03:44:05Z"},
	}
	if got, want := fromSettings(settings), "07b9a39_202607210344"; got != want {
		t.Fatalf("fromSettings() = %q, want %q", got, want)
	}
}

func TestFromSettingsFallsBackToDev(t *testing.T) {
	tests := [][]debug.BuildSetting{
		nil,
		{{Key: "vcs.revision", Value: "07b9a39284df"}},
		{{Key: "vcs.revision", Value: "short"}, {Key: "vcs.time", Value: "2026-07-21T03:44:05Z"}},
		{{Key: "vcs.revision", Value: "07b9a39284df"}, {Key: "vcs.time", Value: "invalid"}},
	}
	for _, settings := range tests {
		if got := fromSettings(settings); got != "dev" {
			t.Fatalf("fromSettings(%v) = %q, want dev", settings, got)
		}
	}
}
