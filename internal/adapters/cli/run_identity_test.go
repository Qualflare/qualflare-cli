package cli

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeReport drops a minimal qualflare-json report carrying the given runId
// ("" writes no metadata.runId at all, standing in for a report from a reporter
// released before runId existed).
func writeReport(t *testing.T, dir, name, runID string) string {
	t.Helper()
	meta := `"metadata":{"version":"0.1.0","timestamp":"2026-09-02T00:00:00Z","cliName":"qualflare-vitest"`
	if runID != "" {
		meta += `,"runId":"` + runID + `"`
	}
	meta += "}"
	body := `{"framework":"vitest",` + meta + `,"suites":[]}`
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func TestVerifySingleRun_AllowsOneRun(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "a.json", "run-1"),
		writeReport(t, dir, "b.json", "run-1"),
		writeReport(t, dir, "c.json", "run-1"),
	}
	// Three shards of one run is the normal, supported case — the whole reason
	// collect accepts a directory.
	if err := verifySingleRun(files, false); err != nil {
		t.Fatalf("three shards of one run must collect cleanly, got: %v", err)
	}
}

func TestVerifySingleRun_RejectsTwoRuns(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "a.json", "run-1"),
		writeReport(t, dir, "b.json", "run-1"),
		writeReport(t, dir, "stale.json", "run-0"),
	}
	err := verifySingleRun(files, false)
	if err == nil {
		t.Fatal("a leftover file from an earlier run must not be merged silently")
	}
	msg := err.Error()
	for _, want := range []string{"run-1", "run-0", "--allow-mixed-runs"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error should name %q so the user can act on it; got:\n%s", want, msg)
		}
	}
}

func TestVerifySingleRun_AllowMixedDowngradesToWarning(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "a.json", "run-1"),
		writeReport(t, dir, "b.json", "run-0"),
	}
	if err := verifySingleRun(files, true); err != nil {
		t.Fatalf("--allow-mixed-runs must proceed, got: %v", err)
	}
}

// The backwards-compatibility case, and the reason files without a runId are
// bucketed together rather than treated as distinct runs: every report written
// by an already-released reporter has no runId, and those must keep collecting
// exactly as before.
func TestVerifySingleRun_ReportsWithoutRunIDNeverFail(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "a.json", ""),
		writeReport(t, dir, "b.json", ""),
		writeReport(t, dir, "c.json", ""),
	}
	if err := verifySingleRun(files, false); err != nil {
		t.Fatalf("pre-runId reports must not be treated as different runs, got: %v", err)
	}
}

func TestVerifySingleRun_MixOfKnownAndUnknownIsAllowed(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "new.json", "run-1"),
		writeReport(t, dir, "old.json", ""),
	}
	// One known run plus an unidentifiable file is the shape of a mid-upgrade
	// directory. Failing here would break the very upgrade path this feature
	// needs users to take.
	if err := verifySingleRun(files, false); err != nil {
		t.Fatalf("one known run alongside pre-runId reports must pass, got: %v", err)
	}
}

func TestReadRunID_UnreadableOrMalformedYieldsEmpty(t *testing.T) {
	dir := t.TempDir()

	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A malformed report is the parser's problem to report, with a message about
	// parsing. Surfacing it here as a run-identity failure would be misleading.
	if got := readRunID(bad); got != "" {
		t.Errorf("malformed report should yield no runId, got %q", got)
	}
	if got := readRunID(filepath.Join(dir, "does-not-exist.json")); got != "" {
		t.Errorf("missing file should yield no runId, got %q", got)
	}
}

func TestVerifySingleRun_ErrorNamesTheStaleFile(t *testing.T) {
	dir := t.TempDir()
	stale := writeReport(t, dir, "stale.json", "run-0")
	files := []string{writeReport(t, dir, "a.json", "run-1"), stale}

	err := verifySingleRun(files, false)
	if err == nil {
		t.Fatal("expected an error")
	}
	// With few files the message lists them, so the user can delete the right
	// one instead of clearing a directory they may care about.
	if !strings.Contains(err.Error(), "stale.json") {
		t.Errorf("error should name the offending file; got:\n%s", err.Error())
	}
}
