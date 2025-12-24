package version

import (
	"fmt"
	"runtime"
)

// Build-time variables (set via ldflags)
var (
	// Version is the semantic version
	Version = "dev"
	// Commit is the git commit hash
	Commit = "unknown"
	// BuildDate is the build timestamp
	BuildDate = "unknown"
)

// Info contains version information
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuildDate string `json:"buildDate"`
	GoVersion string `json:"goVersion"`
	OS        string `json:"os"`
	Arch      string `json:"arch"`
}

// Get returns the current version info
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuildDate: BuildDate,
		GoVersion: runtime.Version(),
		OS:        runtime.GOOS,
		Arch:      runtime.GOARCH,
	}
}

// String returns a human-readable version string
func (i Info) String() string {
	return fmt.Sprintf("qf %s (commit: %s, built: %s, %s/%s, %s)",
		i.Version, shortCommit(i.Commit), i.BuildDate, i.OS, i.Arch, i.GoVersion)
}

// Short returns a short version string
func (i Info) Short() string {
	return fmt.Sprintf("qf %s", i.Version)
}

// shortCommit returns the first 7 characters of a commit hash
func shortCommit(commit string) string {
	if len(commit) > 7 {
		return commit[:7]
	}
	return commit
}

// UserAgent returns the User-Agent string for HTTP requests
func UserAgent() string {
	return fmt.Sprintf("qf-cli/%s (%s/%s)", Version, runtime.GOOS, runtime.GOARCH)
}
