// Package version holds build-time identity injected via ldflags.
package version

// Values are set by GoReleaser / -ldflags. Defaults suit local builds.
var (
	Version = "dev"
	Commit  = "none"
	Date    = "unknown"
)

// String returns a human-readable version line.
func String() string {
	return "docker-tui " + Version + " (" + Commit + " " + Date + ")"
}
