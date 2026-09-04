package cli

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// writeReport drops a minimal qualflare-json report carrying the given runId
// ("" writes no metadata.runId at all, standing in for a report from a reporter
// released before runId existed) and timestamp ("" writes none).
func writeReport(t *testing.T, dir, name, runID, ts string) string {
	t.Helper()
	meta := `"metadata":{"version":"0.1.0","cliName":"qualflare-vitest"`
	if runID != "" {
		meta += `,"runId":"` + runID + `"`
	}
	if ts != "" {
		meta += `,"timestamp":"` + ts + `"`
	}
	meta += "}"
	body := `{"framework":"vitest",` + meta + `,"suites":[]}`
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
	return path
}

func bases(paths []string) []string {
	out := make([]string, 0, len(paths))
	for _, p := range paths {
		out = append(out, filepath.Base(p))
	}
	sort.Strings(out)
	return out
}

func TestSelectCurrentRun_OneRunIsUploadedWhole(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "a.json", "run-1", "2026-09-04T10:00:00Z"),
		writeReport(t, dir, "b.json", "run-1", "2026-09-04T10:00:01Z"),
		writeReport(t, dir, "c.json", "run-1", "2026-09-04T10:00:02Z"),
	}
	// Three shards of one run is the normal, supported case — the whole reason
	// collect accepts a directory.
	got := selectCurrentRun(files, false, io.Discard)
	if len(got) != 3 {
		t.Fatalf("all three shards must upload, got %v", bases(got))
	}
}

// THE point of the change. A leftover report used to fail the whole upload and
// send the user off to clear the directory; now the run that just finished is
// uploaded and the stale one is left on disk.
func TestSelectCurrentRun_KeepsTheNewestRunAndDropsTheStaleOne(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "stale.json", "run-0", "2026-09-04T09:00:00Z"),
		writeReport(t, dir, "new-a.json", "run-1", "2026-09-04T10:00:00Z"),
		writeReport(t, dir, "new-b.json", "run-1", "2026-09-04T10:00:05Z"),
	}
	var warn bytes.Buffer

	got := selectCurrentRun(files, false, &warn)

	want := []string{"new-a.json", "new-b.json"}
	if strings.Join(bases(got), ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", bases(got), want)
	}
	// Silently dropping data would be its own bug: say what was left out.
	for _, frag := range []string{"ignored 1 file", "1 earlier run", "--allow-mixed-runs"} {
		if !strings.Contains(warn.String(), frag) {
			t.Errorf("warning should mention %q; got: %s", frag, warn.String())
		}
	}
}

func TestSelectCurrentRun_AllowMixedUploadsEverything(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "a.json", "run-1", "2026-09-04T10:00:00Z"),
		writeReport(t, dir, "b.json", "run-0", "2026-09-04T09:00:00Z"),
	}
	var warn bytes.Buffer

	got := selectCurrentRun(files, true, &warn)

	if len(got) != 2 {
		t.Fatalf("--allow-mixed-runs must merge both runs, got %v", bases(got))
	}
	if !strings.Contains(warn.String(), "merging 2 runs") {
		t.Errorf("merging several runs should still be announced; got: %s", warn.String())
	}
}

// The backwards-compatibility case, and the reason files without a runId are
// bucketed together rather than treated as distinct runs: every report written
// by an already-released reporter has no runId, and those must keep collecting
// exactly as before.
func TestSelectCurrentRun_ReportsWithoutRunIDAllUpload(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "a.json", "", "2026-09-04T10:00:00Z"),
		writeReport(t, dir, "b.json", "", "2026-09-04T09:00:00Z"),
		writeReport(t, dir, "c.json", "", ""),
	}
	if got := selectCurrentRun(files, false, io.Discard); len(got) != 3 {
		t.Fatalf("pre-runId reports must not be narrowed, got %v", bases(got))
	}
}

// A mixed-version directory: an updated reporter stamps a runId, an older one
// beside it does not, and both describe the SAME real run. Dropping the
// unattributable file to guard against stale data would discard live results.
func TestSelectCurrentRun_UnattributableFilesRideAlong(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "stale.json", "run-0", "2026-09-04T09:00:00Z"),
		writeReport(t, dir, "new.json", "run-1", "2026-09-04T10:00:00Z"),
		writeReport(t, dir, "old-reporter.json", "", "2026-09-04T10:00:00Z"),
	}
	got := bases(selectCurrentRun(files, false, io.Discard))
	want := []string{"new.json", "old-reporter.json"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("got %v, want %v", got, want)
	}
}

// Selection must be total. Two shards can write in the same millisecond, and a
// report may carry no parseable timestamp at all — the answer must still be the
// same on every invocation rather than depending on map iteration order.
func TestSelectCurrentRun_IsDeterministicWhenTimestampsTie(t *testing.T) {
	dir := t.TempDir()
	files := []string{
		writeReport(t, dir, "a.json", "run-a", "2026-09-04T10:00:00Z"),
		writeReport(t, dir, "b.json", "run-b", "2026-09-04T10:00:00Z"),
	}
	first := strings.Join(bases(selectCurrentRun(files, false, io.Discard)), ",")
	for i := 0; i < 20; i++ {
		if got := strings.Join(bases(selectCurrentRun(files, false, io.Discard)), ","); got != first {
			t.Fatalf("selection differed between calls: %q then %q", first, got)
		}
	}
}

// A report with no usable timestamp must not win by default, or a malformed
// leftover would outrank the run that just finished.
func TestSelectCurrentRun_FallsBackToMtimeWhenTimestampIsUnusable(t *testing.T) {
	dir := t.TempDir()
	stale := writeReport(t, dir, "stale.json", "run-0", "not-a-timestamp")
	fresh := writeReport(t, dir, "fresh.json", "run-1", "not-a-timestamp")
	old := time.Now().Add(-2 * time.Hour)
	if err := os.Chtimes(stale, old, old); err != nil {
		t.Fatal(err)
	}

	got := bases(selectCurrentRun([]string{stale, fresh}, false, io.Discard))
	if len(got) != 1 || got[0] != "fresh.json" {
		t.Errorf("with no parseable timestamps the newer FILE should win; got %v", got)
	}
}

func TestReadRunMeta_UnreadableOrMalformedYieldsZeroValues(t *testing.T) {
	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.json")
	if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	// A malformed report is the parser's problem to report, with a message about
	// parsing. Surfacing it here as a run-identity failure would be misleading.
	if m := readRunMeta(bad); m.runID != "" || !m.timestamp.IsZero() {
		t.Errorf("malformed report should yield no run identity, got %+v", m)
	}
	if m := readRunMeta(filepath.Join(dir, "nope.json")); m.runID != "" {
		t.Errorf("missing file should yield no runId, got %q", m.runID)
	}
}
