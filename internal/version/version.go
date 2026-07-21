package version

import (
	"runtime/debug"
	"time"
)

const timestampLayout = "200601021504"

var current = resolve()

// Current returns a version derived from the Git revision and commit time
// recorded automatically by the Go toolchain during a VCS build.
func Current() string {
	return current
}

func resolve() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "dev"
	}
	return fromSettings(info.Settings)
}

func fromSettings(settings []debug.BuildSetting) string {
	var revision, commitTime string
	for _, setting := range settings {
		switch setting.Key {
		case "vcs.revision":
			revision = setting.Value
		case "vcs.time":
			commitTime = setting.Value
		}
	}
	if len(revision) < 7 || commitTime == "" {
		return "dev"
	}
	parsed, err := time.Parse(time.RFC3339, commitTime)
	if err != nil {
		return "dev"
	}
	return revision[:7] + "_" + parsed.UTC().Format(timestampLayout)
}
