package version

import (
	"runtime"
	"strings"
	"testing"
)

// The ldflags variables are the contract with .goreleaser.yml: it injects
// version.Version, version.Commit, and version.BuildDate by exact path. Renaming or
// moving them silently reverts every built binary to these defaults.
func TestBuildVariableDefaults(t *testing.T) {
	// Guard the un-injected values, which is what a plain `go build` produces.
	if Version == "" || Commit == "" || BuildDate == "" {
		t.Errorf("build vars must have non-empty defaults: %q/%q/%q", Version, Commit, BuildDate)
	}
}

func TestGet(t *testing.T) {
	info := Get()

	if info.Version != Version || info.Commit != Commit || info.BuildDate != BuildDate {
		t.Errorf("Get() = %+v, want it to mirror the build vars", info)
	}
	if info.GoVersion != runtime.Version() {
		t.Errorf("GoVersion = %q, want %q", info.GoVersion, runtime.Version())
	}
	if info.OS != runtime.GOOS || info.Arch != runtime.GOARCH {
		t.Errorf("OS/Arch = %q/%q, want %q/%q", info.OS, info.Arch, runtime.GOOS, runtime.GOARCH)
	}
}

// String is what `qf version` prints, so it must carry every field a bug report needs.
func TestInfo_String(t *testing.T) {
	info := Info{
		Version:   "1.2.3",
		Commit:    "abcdef1234567890",
		BuildDate: "2026-01-02T03:04:05Z",
		GoVersion: "go1.25.0",
		OS:        "darwin",
		Arch:      "arm64",
	}

	got := info.String()
	for _, want := range []string{"qf 1.2.3", "2026-01-02T03:04:05Z", "darwin/arm64", "go1.25.0"} {
		if !strings.Contains(got, want) {
			t.Errorf("String() = %q, missing %q", got, want)
		}
	}
	// The commit is abbreviated, so the full hash must not appear.
	if !strings.Contains(got, "abcdef1") {
		t.Errorf("String() = %q, want the short commit", got)
	}
	if strings.Contains(got, "abcdef1234567890") {
		t.Errorf("String() = %q, want the commit abbreviated", got)
	}
}

func TestInfo_Short(t *testing.T) {
	if got := (Info{Version: "1.2.3"}).Short(); got != "qf 1.2.3" {
		t.Errorf("Short() = %q, want %q", got, "qf 1.2.3")
	}
}

func TestShortCommit(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"abcdef1234567890", "abcdef1"},
		{"abcdef1", "abcdef1"}, // exactly 7 is left alone
		{"abcdef", "abcdef"},   // shorter than 7 must not panic or pad
		{"", ""},
		{"unknown", "unknown"}, // the default value is 7 chars, so it survives intact
	}
	for _, tt := range tests {
		if got := shortCommit(tt.in); got != tt.want {
			t.Errorf("shortCommit(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

// UserAgent is sent on every API request, so the server can attribute traffic and
// identify which CLI build produced an upload. It must always carry a version.
func TestUserAgent(t *testing.T) {
	got := UserAgent()

	if !strings.HasPrefix(got, "qf-cli/") {
		t.Errorf("UserAgent() = %q, want the qf-cli/ prefix", got)
	}
	if !strings.Contains(got, Version) {
		t.Errorf("UserAgent() = %q, want it to carry the version %q", got, Version)
	}
	if !strings.Contains(got, runtime.GOOS+"/"+runtime.GOARCH) {
		t.Errorf("UserAgent() = %q, want the os/arch pair", got)
	}
	// A header value cannot contain a newline, and Version comes from ldflags.
	if strings.ContainsAny(got, "\r\n") {
		t.Errorf("UserAgent() = %q, must not contain line breaks", got)
	}
}
