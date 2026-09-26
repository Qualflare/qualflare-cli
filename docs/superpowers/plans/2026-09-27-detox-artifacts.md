# Detox Artifacts Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `qf collect --artifacts-dir <dir>` attaches Detox's screenshots, videos and logs to the right test cases.

**Architecture:** A Detox artifacts directory holds one subdirectory per test, whose name is a pure function of the test's full name, its status and its invocation number. We compute that name **forward** for every case in the report and look it up, rather than parsing directory names back into test names — the transformation is lossy, so inverting it is guesswork. Matched files become `domain.Attachment`s with `LocalPath` + `ArtifactKind`, which the existing `resolveArtifactAttachments` pass in `report_service.go` already uploads.

**Tech Stack:** Go 1.25, the existing `domain.Attachment`/`ArtifactKind` model, `internal/adapters/artifacts/` (new package). No new dependencies.

**Spec:** `docs/superpowers/specs/2026-09-26-detox-artifacts-design.md`

## Global Constraints

- **No server, wire-format or `domain` schema change.** Attachments use the existing `LocalPath`/`ArtifactKind` fields.
- **No auto-detection.** With `--artifacts-dir` absent, nothing is scanned. `./artifacts` is never guessed even though it is Detox's default `rootDir`.
- **The flag is framework-generic, the layout is not.** `--artifacts-dir` names a directory; the layout inside is inferred from the report's format. A format with no artifact support must **fail loudly** when the flag is passed, naming the format and the formats that do support it.
- **Layout inference comes from the report, never from sniffing the directory.**
- **Artifact kinds:** `.png` → `ArtifactKindImage` (uploads by default); `.mp4` → `ArtifactKindVideo` (opt-in); `.log`, `.dtxrec`, `.uihierarchy` → `ArtifactKindTrace` (opt-in). Device logs stay opt-in because they are the artifact most likely to carry customer data.
- **Retries attach to the case, not the attempt.** A retried test writes one directory per invocation (` (2)`, ` (3)`); those artifacts attach to the case with the invocation in the attachment name. `domain.Attempt` has no attachment field and adding one is out of scope.
- **Unmatched directories are reported on stderr**, never dropped silently: a scan that matched nothing looks identical to a matching bug from the dashboard.
- **Read nothing that is localised.** (Carried from the `.xcresult` work in the same package family: `xcresulttool` proved this matters. Detox's directory names are built from the test's own name, so they are safe, but any future field is suspect.)

---

## File Structure

| File | Responsibility |
|---|---|
| `internal/adapters/artifacts/detox/safename.go` | Faithful Go port of the npm `sanitize-filename` package (pure) |
| `internal/adapters/artifacts/detox/safename_test.go` | Its tests, derived from the package's documented rules |
| `internal/adapters/artifacts/detox/dirname.go` | Detox's `ArtifactPathBuilder` naming: glyph prefix, invocation suffix, trim (pure) |
| `internal/adapters/artifacts/detox/dirname_test.go` | Its tests, asserting the seven measured outputs from the spec |
| `internal/adapters/artifacts/detox/scan.go` | Root resolution + directory walk + forward matching → attachments |
| `internal/adapters/artifacts/detox/scan_test.go` | Scanner tests against a synthesised tree |
| `internal/adapters/artifacts/attach.go` | Format → layout dispatch; fails loudly for unsupported formats |
| `internal/adapters/artifacts/attach_test.go` | Dispatch tests, both directions |
| `internal/core/services/report_service.go` | Call the dispatch after parsing, before the dry-run return |
| `internal/adapters/cli/command.go` | The `--artifacts-dir` flag |
| `internal/config/config.go` | `ArtifactsDir` field + accessor |

Tasks 1–4 are pure and independently testable. Task 5 is the only wiring task. Task 6 is docs. Task 7 is a separate repo and could become its own plan.

---

### Task 1: Port `sanitize-filename`

Detox sanitises directory names with the npm `sanitize-filename` package. Matching computes names forward, so this port must agree with it exactly or artifacts go unmatched.

**Files:**
- Create: `internal/adapters/artifacts/detox/safename.go`
- Test: `internal/adapters/artifacts/detox/safename_test.go`

**Interfaces:**
- Consumes: nothing.
- Produces: `func sanitizeFilename(input, replacement string) string`

- [ ] **Step 1: Write the failing test**

```go
package detox

import "testing"

// The rules are the npm package's own (parshap/node-sanitize-filename):
// illegal chars / ? < > \ : * | " ; C0 and C1 control codes; a name of only
// dots; Windows reserved device names; trailing dots and spaces; then a
// truncation to 255 BYTES.
//
// The subtlety worth keeping: when a replacement is given the package runs its
// whole sanitise pass TWICE — once with the replacement, then again with the
// empty string. A port that runs it once agrees on ordinary names and diverges
// on the awkward ones, which is the worst possible place to differ.
func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name, in, replacement, want string
	}{
		{"illegal characters", `a/b?c<d>e\f:g*h|i"j`, "_", "a_b_c_d_e_f_g_h_i_j"},
		{"control codes", "a\x00b\x1fc\x80d", "_", "a_b_c_d"},
		{"only dots is reserved", "..", "_", "_"},
		{"windows device name", "CON", "_", "_"},
		{"windows device name with extension", "con.txt", "_", "_"},
		{"trailing dots and spaces", "name.. ", "_", "name_"},
		{"ordinary name untouched", "Login should sign in", "_", "Login should sign in"},
		{"empty replacement drops the character", "a/b", "", "ab"},
		// Truncation is by BYTES, not runes: three-byte characters cap out at 85.
		{"truncates to 255 bytes", str("ü", 200), "_", str("ü", 127)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilename(tt.in, tt.replacement); got != tt.want {
				t.Errorf("sanitizeFilename(%q, %q) = %q, want %q", tt.in, tt.replacement, got, tt.want)
			}
		})
	}
}

func str(s string, n int) string {
	out := ""
	for i := 0; i < n; i++ {
		out += s
	}
	return out
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapters/artifacts/detox/ -run TestSanitizeFilename -v`
Expected: FAIL — `undefined: sanitizeFilename`

- [ ] **Step 3: Write the implementation**

```go
package detox

import (
	"regexp"
	"strings"
)

// A faithful port of the npm `sanitize-filename` package, which is what Detox
// runs its directory names through (detox/src/utils/constructSafeFilename.js).
// Matching computes names forward, so any divergence here shows up as
// artifacts that silently fail to attach.
var (
	illegalRe        = regexp.MustCompile(`[/?<>\\:*|"]`)
	controlRe        = regexp.MustCompile("[\x00-\x1f\x80-\x9f]")
	onlyDotsRe       = regexp.MustCompile(`^\.+$`)
	windowsDeviceRe  = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\..*)?$`)
)

// maxFilenameBytes is a BYTE limit, not a rune limit: the package truncates
// with truncate-utf8-bytes.
const maxFilenameBytes = 255

func sanitizeFilename(input, replacement string) string {
	out := sanitizeOnce(input, replacement)
	if replacement == "" {
		return out
	}
	// The package runs the whole pass a second time with an empty replacement,
	// so a replacement that itself introduced something illegal cannot survive.
	return sanitizeOnce(out, "")
}

func sanitizeOnce(input, replacement string) string {
	out := illegalRe.ReplaceAllString(input, replacement)
	out = controlRe.ReplaceAllString(out, replacement)
	out = onlyDotsRe.ReplaceAllString(out, replacement)
	out = windowsDeviceRe.ReplaceAllString(out, replacement)
	out = replaceTrailingDotsAndSpaces(out, replacement)
	return truncateUTF8Bytes(out, maxFilenameBytes)
}

// replaceTrailingDotsAndSpaces mirrors the package's own loop, which avoids a
// regex deliberately (its CWE-1333 fix). Note it replaces the whole trailing
// run with ONE replacement, not one per character.
func replaceTrailingDotsAndSpaces(s, replacement string) string {
	end := len(s)
	for end > 0 && (s[end-1] == '.' || s[end-1] == ' ') {
		end--
	}
	if end == len(s) {
		return s
	}
	return s[:end] + replacement
}

// truncateUTF8Bytes cuts to at most limit bytes without splitting a rune.
func truncateUTF8Bytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
```

Add `"unicode/utf8"` to the imports and drop `strings` if unused.

- [ ] **Step 4: Run the test to verify it passes**

Run: `go test ./internal/adapters/artifacts/detox/ -run TestSanitizeFilename -v`
Expected: PASS (9 subtests)

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/artifacts/detox/safename.go internal/adapters/artifacts/detox/safename_test.go
git commit -m "feat(artifacts): port sanitize-filename, the rule Detox names directories with

Matching Detox artifacts means computing the directory name a test would
have, so this has to agree with the npm package exactly. Two details a
one-pass port gets wrong: the package runs its whole sanitise twice when a
replacement is given, and it truncates to 255 BYTES rather than characters."
```

---

### Task 2: Compute Detox's per-test directory name

**Files:**
- Create: `internal/adapters/artifacts/detox/dirname.go`
- Test: `internal/adapters/artifacts/detox/dirname_test.go`

**Interfaces:**
- Consumes: `sanitizeFilename(input, replacement string) string` (Task 1)
- Produces: `func DirectoryName(fullName, status string, invocation int) string`, where `status` is one of `"passed"`, `"failed"` or anything else (which yields no glyph), and `invocation` is 1-based.

- [ ] **Step 1: Write the failing test**

```go
package detox

import "testing"

// Every expected value here was produced by RUNNING Detox's own algorithm
// (ArtifactPathBuilder.js + constructSafeFilename.js) against the input, not
// by reading its documentation — which describes a "{test-number}
// {test-full-name}" format that does not exist. There is no test number.
func TestDirectoryName(t *testing.T) {
	tests := []struct {
		name       string
		fullName   string
		status     string
		invocation int
		want       string
	}{
		{"passed gets a check", "Login should sign in", "passed", 1, "✓ Login should sign in"},
		{"failed gets a cross", "Login should sign in", "failed", 1, "✗ Login should sign in"},
		{"an unknown status gets no glyph", "Login should sign in", "skipped", 1, "Login should sign in"},
		{"a retry is suffixed with its invocation", "Login should sign in", "passed", 2, "✓ Login should sign in (2)"},
		{"a slash is sanitised", "Login should handle a/b paths", "failed", 1, "✗ Login should handle a_b paths"},
		{"quotes and colons are sanitised", `Login "quoted" and: colons`, "failed", 1, "✗ Login _quoted_ and_ colons"},
		{"a dollar sign is sanitised by Detox itself", "Money costs $5", "passed", 1, "✓ Money costs _5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DirectoryName(tt.fullName, tt.status, tt.invocation); got != tt.want {
				t.Errorf("DirectoryName(%q, %q, %d) = %q, want %q", tt.fullName, tt.status, tt.invocation, got, tt.want)
			}
		})
	}
}

// A long name loses its BEGINNING, because Detox trims with slice(-N). Any
// matching that assumed a shared prefix would fail on exactly the tests whose
// names are most descriptive.
func TestDirectoryNameTrimsFromTheStart(t *testing.T) {
	long := ""
	for i := 0; i < 300; i++ {
		long += "x"
	}
	got := DirectoryName(long, "failed", 1)
	if len([]rune(got)) != 253 {
		t.Errorf("got %d runes, want 253 (255 less the two-rune glyph prefix)", len([]rune(got)))
	}
	if got[:len("✗ ")] != "✗ " {
		t.Errorf("the prefix should survive trimming, got %q", got[:6])
	}
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapters/artifacts/detox/ -run TestDirectoryName -v`
Expected: FAIL — `undefined: DirectoryName`

- [ ] **Step 3: Write the implementation**

```go
package detox

import (
	"fmt"
	"strings"
)

// DirectoryName returns the directory Detox writes a test's artifacts into,
// computed the same way Detox computes it:
//
//	dirname = sanitize(prefix + fullName.slice(-(255 - len(prefix) - len(suffix))) + suffix,
//	                   "_").replace("$", "_")
//
// The two truncations differ in unit and that is deliberate, not an oversight:
// Detox trims the name by JavaScript string length (UTF-16 code units, which
// for the BMP is runes), while sanitize-filename truncates the result by UTF-8
// BYTES. Both are reproduced rather than unified, because agreeing with Detox
// matters more than being internally tidy.
func DirectoryName(fullName, status string, invocation int) string {
	prefix := statusGlyph(status)
	suffix := ""
	if invocation > 1 {
		suffix = fmt.Sprintf(" (%d)", invocation)
	}

	// slice(-N) on the rune sequence: keep the TAIL.
	budget := maxFilenameBytes - len([]rune(prefix)) - len([]rune(suffix))
	runes := []rune(fullName)
	if budget < 0 {
		budget = 0
	}
	if len(runes) > budget {
		runes = runes[len(runes)-budget:]
	}

	unsafe := prefix + string(runes) + suffix
	// Detox replaces "$" itself, after sanitize-filename has run.
	return strings.ReplaceAll(sanitizeFilename(unsafe, "_"), "$", "_")
}

func statusGlyph(status string) string {
	switch status {
	case "passed":
		return "✓ "
	case "failed":
		return "✗ "
	default:
		return ""
	}
}
```

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/adapters/artifacts/detox/ -v`
Expected: PASS — 7 `DirectoryName` subtests plus the trimming test

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/artifacts/detox/dirname.go internal/adapters/artifacts/detox/dirname_test.go
git commit -m "feat(artifacts): compute the directory name Detox gives a test

Taken from Detox's source rather than its docs, which describe a test number
that does not exist in the name. Two things the tests pin down: a retried
test's directory is suffixed ' (2)', and a long name is trimmed from its
START, so matching cannot assume a shared prefix."
```

---

### Task 3: Resolve the artifacts root

A Detox run writes `<rootDir>/<configuration>.<timestamp>/`. The flag may name either the root or one run inside it.

**Files:**
- Create: `internal/adapters/artifacts/detox/scan.go`
- Test: `internal/adapters/artifacts/detox/scan_test.go`

**Interfaces:**
- Consumes: nothing from earlier tasks.
- Produces: `func ResolveRunDir(dir string) (string, error)`

- [ ] **Step 1: Write the failing test**

```go
package detox

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestResolveRunDir(t *testing.T) {
	t.Run("a directory holding run directories resolves to the newest", func(t *testing.T) {
		root := t.TempDir()
		older := filepath.Join(root, "ios.sim.debug.2026-09-01 10-00-00Z")
		newer := filepath.Join(root, "ios.sim.debug.2026-09-27 10-00-00Z")
		for _, d := range []string{older, newer} {
			if err := os.MkdirAll(filepath.Join(d, "✓ a test"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		// Make the intended winner unambiguously newer, since a temp dir's
		// entries can share a timestamp to the second.
		future := time.Now().Add(time.Hour)
		if err := os.Chtimes(newer, future, future); err != nil {
			t.Fatal(err)
		}

		got, err := ResolveRunDir(root)
		if err != nil {
			t.Fatalf("ResolveRunDir: %v", err)
		}
		if got != newer {
			t.Errorf("got %q, want the newest run %q", got, newer)
		}
	})

	t.Run("a run directory is used as given", func(t *testing.T) {
		run := t.TempDir()
		if err := os.MkdirAll(filepath.Join(run, "✗ a failing test"), 0o755); err != nil {
			t.Fatal(err)
		}
		got, err := ResolveRunDir(run)
		if err != nil {
			t.Fatalf("ResolveRunDir: %v", err)
		}
		if got != run {
			t.Errorf("got %q, want the directory as given %q", got, run)
		}
	})

	t.Run("a missing directory is an error the user can act on", func(t *testing.T) {
		_, err := ResolveRunDir(filepath.Join(t.TempDir(), "nope"))
		if err == nil {
			t.Fatal("want an error for a directory that does not exist")
		}
	})

	t.Run("an empty directory is an error, not an empty success", func(t *testing.T) {
		if _, err := ResolveRunDir(t.TempDir()); err == nil {
			t.Fatal("want an error: an empty directory means the path is wrong, and silently attaching nothing hides that")
		}
	})
}
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapters/artifacts/detox/ -run TestResolveRunDir -v`
Expected: FAIL — `undefined: ResolveRunDir`

- [ ] **Step 3: Write the implementation**

```go
package detox

import (
	"fmt"
	"os"
	"path/filepath"
)

// ResolveRunDir turns whatever the user passed into the single run directory
// to scan.
//
// Detox writes <rootDir>/<configuration>.<timestamp>/, so --artifacts-dir may
// reasonably name either level. A directory whose own children are test
// directories is a run; a directory whose children are run directories is a
// root, and the newest run wins.
//
// "Newest" is by modification time rather than by parsing the timestamp out of
// the name: the name's format is Detox's to change, and the filesystem already
// knows the answer.
func ResolveRunDir(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("reading the artifacts directory: %w", err)
	}

	var subdirs []os.DirEntry
	for _, e := range entries {
		if e.IsDir() {
			subdirs = append(subdirs, e)
		}
	}
	if len(subdirs) == 0 {
		return "", fmt.Errorf("%s holds no directories: it is neither a Detox artifacts root nor a run within one", dir)
	}

	// A run directory's children are per-test directories, which carry Detox's
	// status glyph. Nothing else in the layout does, which makes it the cheapest
	// reliable way to tell the two levels apart.
	for _, e := range subdirs {
		if hasStatusGlyph(e.Name()) {
			return dir, nil
		}
	}

	newest, newestMod := "", int64(-1)
	for _, e := range subdirs {
		info, err := e.Info()
		if err != nil {
			continue
		}
		if mod := info.ModTime().UnixNano(); mod > newestMod {
			newest, newestMod = filepath.Join(dir, e.Name()), mod
		}
	}
	if newest == "" {
		return "", fmt.Errorf("could not read any run directory under %s", dir)
	}
	return newest, nil
}

func hasStatusGlyph(name string) bool {
	return strings.HasPrefix(name, "✓ ") || strings.HasPrefix(name, "✗ ")
}
```

Add `"strings"` to the imports.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/adapters/artifacts/detox/ -v`
Expected: PASS — all four `ResolveRunDir` subtests

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/artifacts/detox/scan.go internal/adapters/artifacts/detox/scan_test.go
git commit -m "feat(artifacts): resolve a Detox artifacts root to one run directory

--artifacts-dir may name the root or a single run, so both are accepted: a
directory whose children carry Detox's status glyph is a run, otherwise the
newest child is. Newest by mtime rather than by parsing the timestamp out of
the name, which is Detox's format to change.

An empty directory is an error rather than an empty success: it means the
path is wrong, and attaching nothing silently hides that."
```

---

### Task 4: Match directories to cases and build attachments

**Files:**
- Modify: `internal/adapters/artifacts/detox/scan.go`
- Modify: `internal/adapters/artifacts/detox/scan_test.go`

**Interfaces:**
- Consumes: `DirectoryName(fullName, status string, invocation int) string` (Task 2), `ResolveRunDir(dir string) (string, error)` (Task 3)
- Produces: `func Attach(launch *domain.Launch, dir string) (matched int, unmatched []string, err error)` — mutates `launch`, appending to each case's `Attachments`.

- [ ] **Step 1: Write the failing test**

```go
func TestAttach(t *testing.T) {
	run := t.TempDir()

	// Two invocations of one flaky test, plus a passing test, plus a directory
	// belonging to no case in the report.
	write := func(dir, file string, content string) {
		if err := os.MkdirAll(filepath.Join(run, dir), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(run, dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("✗ Login should sign in", "testFnFailure.png", "png-bytes")
	write("✗ Login should sign in", "device.log", "log-bytes")
	write("✓ Login should sign in (2)", "afterEach.png", "png-bytes")
	write("✓ Checkout should charge", "test.mp4", "mp4-bytes")
	write("✓ A test nobody reported", "orphan.png", "png-bytes")

	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Name: "Login should sign in", Status: domain.StatusPassed},
		{Name: "Checkout should charge", Status: domain.StatusPassed},
	}}}}

	matched, unmatched, err := Attach(launch, run)
	if err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if matched != 2 {
		t.Errorf("matched %d cases, want 2", matched)
	}
	if len(unmatched) != 1 || unmatched[0] != "✓ A test nobody reported" {
		t.Errorf("unmatched = %v, want the one orphan directory", unmatched)
	}

	login := launch.Suites[0].Cases[0]
	if len(login.Attachments) != 3 {
		t.Fatalf("the flaky test should carry all three files across both invocations, got %d", len(login.Attachments))
	}
	// A retried test's later invocations are distinguishable, since they attach
	// to the case rather than to an attempt.
	var names []string
	for _, a := range login.Attachments {
		names = append(names, a.Name)
	}
	if !slices.Contains(names, "afterEach.png (attempt 2)") {
		t.Errorf("invocation 2's file should name its attempt, got %v", names)
	}

	// Kinds decide the upload route AND whether it uploads by default.
	byName := map[string]domain.Attachment{}
	for _, a := range login.Attachments {
		byName[a.Name] = a
	}
	if got := byName["testFnFailure.png"]; got.ArtifactKind != domain.ArtifactKindImage {
		t.Errorf("a screenshot should be an image (uploads by default), got %q", got.ArtifactKind)
	}
	if got := byName["device.log"]; got.ArtifactKind != domain.ArtifactKindTrace {
		t.Errorf("a device log should be a trace (opt-in: it may carry customer data), got %q", got.ArtifactKind)
	}
	checkout := launch.Suites[0].Cases[1]
	if checkout.Attachments[0].ArtifactKind != domain.ArtifactKindVideo {
		t.Errorf("a video should be a video (opt-in: it is the largest thing in a report), got %q", checkout.Attachments[0].ArtifactKind)
	}
	if !filepath.IsAbs(checkout.Attachments[0].LocalPath) {
		t.Errorf("LocalPath must be absolute for report_service to resolve it, got %q", checkout.Attachments[0].LocalPath)
	}
}

func TestAttachMatchesRegardlessOfReportedStatus(t *testing.T) {
	// The report says passed; Detox wrote the directory when the test failed on
	// its first invocation. Matching must try every status glyph rather than
	// only the one the case ended on.
	run := t.TempDir()
	if err := os.MkdirAll(filepath.Join(run, "✗ Flaky test"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(run, "✗ Flaky test", "shot.png"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{
		{Name: "Flaky test", Status: domain.StatusPassed},
	}}}}
	if _, _, err := Attach(launch, run); err != nil {
		t.Fatalf("Attach: %v", err)
	}
	if len(launch.Suites[0].Cases[0].Attachments) != 1 {
		t.Error("a case that ended green must still pick up the directory from its failed invocation")
	}
}
```

Add `"slices"` and the `domain` import to the test file.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapters/artifacts/detox/ -run TestAttach -v`
Expected: FAIL — `undefined: Attach`

- [ ] **Step 3: Write the implementation**

```go
// maxInvocationsProbed bounds how many retries we look for per case. Detox
// suffixes invocation 2 onward, and a suite retrying a single test more than
// this is pathological; probing is cheap (a map lookup) but unbounded probing
// is still a loop with no reason to stop.
const maxInvocationsProbed = 10

// Attach matches each per-test directory under dir to a case in launch and
// appends its files as attachments.
//
// Matching computes forward: for every case, every plausible directory name is
// computed and looked up. The reverse — parsing a directory name back into a
// test name — cannot work, because the transformation is lossy: a test named
// "a/b" and one named "a_b" both produce "a_b".
//
// Every status glyph is tried per case, not just the one the case ended on: a
// test that failed and was retried green has a "✗ " directory for its first
// invocation and a "✓ … (2)" for its second.
func Attach(launch *domain.Launch, dir string) (int, []string, error) {
	runDir, err := ResolveRunDir(dir)
	if err != nil {
		return 0, nil, err
	}
	entries, err := os.ReadDir(runDir)
	if err != nil {
		return 0, nil, fmt.Errorf("reading the run directory: %w", err)
	}

	remaining := map[string]bool{}
	for _, e := range entries {
		if e.IsDir() {
			remaining[e.Name()] = true
		}
	}

	matched := 0
	for s := range launch.Suites {
		for c := range launch.Suites[s].Cases {
			cs := &launch.Suites[s].Cases[c]
			found := false
			for invocation := 1; invocation <= maxInvocationsProbed; invocation++ {
				for _, status := range []string{"passed", "failed", ""} {
					name := DirectoryName(cs.Name, status, invocation)
					if !remaining[name] {
						continue
					}
					delete(remaining, name)
					atts, err := attachmentsIn(filepath.Join(runDir, name), invocation)
					if err != nil {
						continue
					}
					cs.Attachments = append(cs.Attachments, atts...)
					found = true
				}
			}
			if found {
				matched++
			}
		}
	}

	unmatched := make([]string, 0, len(remaining))
	for name := range remaining {
		unmatched = append(unmatched, name)
	}
	sort.Strings(unmatched)
	return matched, unmatched, nil
}

// attachmentsIn reads one per-test directory. The invocation is folded into
// each name rather than into a separate field, because artifacts attach to the
// CASE: domain.Attempt has no attachment field, and adding one would be a
// server change.
func attachmentsIn(dir string, invocation int) ([]domain.Attachment, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}

	var atts []domain.Attachment
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if invocation > 1 {
			name = fmt.Sprintf("%s (attempt %d)", name, invocation)
		}
		kind, mime := kindForArtifact(e.Name())
		atts = append(atts, domain.Attachment{
			Name:         name,
			MimeType:     mime,
			LocalPath:    filepath.Join(abs, e.Name()),
			ArtifactKind: kind,
		})
	}
	return atts, nil
}

// kindForArtifact maps Detox's five artifact types onto the kinds
// --upload-artifacts gates. Images upload by default; video and traces are
// opt-in, video because it is the largest thing in a report by an order of
// magnitude and logs because a device log from a real app is the artifact most
// likely to carry customer data.
func kindForArtifact(filename string) (kind, mime string) {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".png":
		return domain.ArtifactKindImage, "image/png"
	case ".jpg", ".jpeg":
		return domain.ArtifactKindImage, "image/jpeg"
	case ".mp4":
		return domain.ArtifactKindVideo, "video/mp4"
	case ".log":
		return domain.ArtifactKindTrace, "text/plain"
	case ".dtxrec", ".uihierarchy":
		return domain.ArtifactKindTrace, "application/octet-stream"
	default:
		return domain.ArtifactKindTrace, "application/octet-stream"
	}
}
```

Add `"sort"`, `"strings"` and the `domain` import as needed.

- [ ] **Step 4: Run the tests to verify they pass**

Run: `go test ./internal/adapters/artifacts/detox/ -v`
Expected: PASS — every test in the package

- [ ] **Step 5: Commit**

```bash
git add internal/adapters/artifacts/detox/
git commit -m "feat(artifacts): match Detox directories to cases, forward not backward

For each case, compute every directory name it could have — each status
glyph, each invocation — and look it up. The reverse cannot work: the
transformation is lossy, so 'a/b' and 'a_b' both produce 'a_b'.

Every glyph is tried per case rather than only the status the case ended on,
because a test that failed and was retried green owns both a '✗ name' and a
'✓ name (2)'. Those artifacts attach to the case with the invocation in the
name, since domain.Attempt has no attachment field.

Unmatched directories come back to the caller to report: a scan that matched
nothing is far more likely to be a bug than a run without artifacts."
```

---

### Task 5: Wire the flag through to the pipeline

**Files:**
- Create: `internal/adapters/artifacts/attach.go`
- Test: `internal/adapters/artifacts/attach_test.go`
- Modify: `internal/config/config.go` (add `ArtifactsDir string` beside `UploadArtifacts`, and an `ArtifactsDirectory() string` accessor next to the other accessors)
- Modify: `internal/adapters/cli/command.go:254` area (flag registration) and the `collectOptions` struct at `:281`, and `applyCollectOptions` at `:364`
- Modify: `internal/core/services/report_service.go:59-86` (`ProcessTestResults`)

**Interfaces:**
- Consumes: `detox.Attach(launch *domain.Launch, dir string) (int, []string, error)` (Task 4)
- Produces: `func AttachFromDirectory(launch *domain.Launch, framework domain.Framework, dir string, warn io.Writer) error`

- [ ] **Step 1: Write the failing test**

```go
package artifacts

import (
	"bytes"
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestAttachFromDirectoryRejectsUnsupportedFormats(t *testing.T) {
	// Silently ignoring the flag would be indistinguishable from a matching
	// bug, so an unsupported format is a hard error that names the alternatives.
	err := AttachFromDirectory(&domain.Launch{}, domain.FrameworkJUnit, t.TempDir(), &bytes.Buffer{})
	if err == nil {
		t.Fatal("want an error for a format with no artifact-directory support")
	}
	if !strings.Contains(err.Error(), "detox") {
		t.Errorf("the error should name the formats that DO support it, got %q", err)
	}
}

// The case that would otherwise reject every real Detox upload: Detox writes a
// Jest report, so detection calls it jest unless --format detox is passed.
func TestAttachFromDirectoryAcceptsJest(t *testing.T) {
	run := t.TempDir()
	if err := os.MkdirAll(filepath.Join(run, "✓ a test"), 0o755); err != nil {
		t.Fatal(err)
	}
	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{Name: "a test"}}}}}
	if err := AttachFromDirectory(launch, domain.FrameworkJest, run, &bytes.Buffer{}); err != nil {
		t.Fatalf("a Detox report detected as jest must be accepted: %v", err)
	}
}

func TestAttachFromDirectoryWarnsAboutUnmatchedDirectories(t *testing.T) {
	run := t.TempDir()
	if err := os.MkdirAll(filepath.Join(run, "✓ nobody reported this"), 0o755); err != nil {
		t.Fatal(err)
	}
	var warn bytes.Buffer
	launch := &domain.Launch{Suites: []domain.Suite{{Cases: []domain.Case{{Name: "a different test"}}}}}

	if err := AttachFromDirectory(launch, domain.FrameworkDetox, run, &warn); err != nil {
		t.Fatalf("AttachFromDirectory: %v", err)
	}
	if !strings.Contains(warn.String(), "nobody reported this") {
		t.Errorf("an unmatched directory must be reported, got %q", warn.String())
	}
	if !strings.Contains(warn.String(), "matched no cases") {
		t.Errorf("a scan that matched nothing deserves its own warning, got %q", warn.String())
	}
}
```

Add `"os"` and `"path/filepath"` imports.

- [ ] **Step 2: Run the test to verify it fails**

Run: `go test ./internal/adapters/artifacts/ -v`
Expected: FAIL — `undefined: AttachFromDirectory`, and `domain.FrameworkDetox` may not exist

- [ ] **Step 3: Write the implementation**

`domain.FrameworkDetox` does not exist yet. Adding a `Framework` means three sites in
`internal/core/domain/models.go`, not one:

1. the const block (beside `FrameworkJest Framework = "jest"`, around line 31) — `FrameworkDetox Framework = "detox"`;
2. `AllFrameworks()` (around line 96), which is what `IsValid()` and `--format`'s validation read;
3. `GetCategory()`'s switch (around line 151) — Detox is an e2e framework, so it belongs with the
   frameworks that return their own name as the category.

Then register it in `internal/adapters/parsers/factory/factory.go` so `--format detox` resolves to
the **Jest** parser, since a Detox report is a Jest report.

**The support check must accept `jest` as well as `detox`, and this is the subtle part.** Detox
writes a Jest report, so content detection identifies a real Detox upload as `FrameworkJest` unless
the user passes `--format detox`. Keying support on `FrameworkDetox` alone would reject every actual
Detox run — the flag would error on exactly the input it was built for. Both are accepted, and the
error is reserved for formats that genuinely have no artifact layout.

```go
// Package artifacts attaches files a framework left on disk to the cases they
// belong to.
//
// It runs after parsing and before the dry-run return in ProcessTestResults,
// so `--dry-run` shows what would be attached. It cannot run in a reporter:
// Detox finalises video in detox.cleanup(), which its Jest integration calls
// from Jest's globalTeardown — and globalTeardown runs AFTER every reporter's
// onRunComplete, so a reporter-side scan would systematically miss videos.
package artifacts

import (
	"fmt"
	"io"

	"qualflare-cli/internal/adapters/artifacts/detox"
	"qualflare-cli/internal/core/domain"
)

// supported lists the formats whose artifact layout is known. The layout is
// inferred from the REPORT's format, never sniffed from the directory: guessing
// what wrote a directory fails in exactly the confusing cases.
// Both jest and detox map to the Detox layout. A Detox report IS a Jest
// report — Detox drives Jest — so unless the user passes --format detox,
// content detection identifies a real Detox upload as jest. Accepting only
// detox here would reject exactly the input this flag exists for.
var detoxLayout = map[domain.Framework]bool{
	domain.FrameworkJest:  true,
	domain.FrameworkDetox: true,
}

func AttachFromDirectory(launch *domain.Launch, framework domain.Framework, dir string, warn io.Writer) error {
	if !detoxLayout[framework] {
		return fmt.Errorf("--artifacts-dir is not supported for %s reports; it currently applies to detox (and jest, since a Detox report is a Jest report)", framework)
	}

	matched, unmatched, err := detox.Attach(launch, dir)
	if err != nil {
		return err
	}

	if len(unmatched) > 0 {
		shown := unmatched
		if len(shown) > 5 {
			shown = shown[:5]
		}
		fmt.Fprintf(warn, "warning: %d artifact directory/directories matched no case: %v\n", len(unmatched), shown)
	}
	if matched == 0 {
		fmt.Fprintf(warn, "warning: --artifacts-dir %s matched no cases at all — check that it is the run you just uploaded\n", dir)
	}
	return nil
}
```

Then wire it. In `command.go`, beside the `--upload-artifacts` registration:

```go
cmd.Flags().StringVar(&artifactsDir, "artifacts-dir", "",
	"Directory of artifacts a framework left on disk, attached to the matching test cases. "+
		"Currently applies to Detox reports, where it is the Detox artifacts root (or one "+
		"<configuration>.<timestamp> run inside it). Never guessed: without this flag nothing is "+
		"scanned, because attaching files from a directory nobody named is how a stale run's video "+
		"ends up on today's launch.")
```

Add `artifactsDir` to `collectOptions`, set `cfg.ArtifactsDir` in `applyCollectOptions`, and in `ProcessTestResults`:

```go
	report, err := s.ParseTestResults(ctx, files, framework)
	if err != nil {
		return err
	}

	// Before the dry-run return, so --dry-run shows what would be attached.
	if dir := s.config.ArtifactsDirectory(); dir != "" {
		if err := artifacts.AttachFromDirectory(report, framework, dir, s.warnWriter()); err != nil {
			return err
		}
	}

	// Check for dry run mode
	if s.config.IsDryRun() {
		return nil
	}
```

- [ ] **Step 4: Run the whole suite**

Run: `go build ./... && go test ./...`
Expected: PASS — 38+ packages, no failures

- [ ] **Step 5: Verify by hand, end to end**

```bash
mkdir -p /tmp/detox-probe/"ios.sim.debug.2026-09-27 10-00-00Z"/"✗ Login should sign in"
echo png > /tmp/detox-probe/"ios.sim.debug.2026-09-27 10-00-00Z"/"✗ Login should sign in"/testFnFailure.png
# Then a Jest report whose case fullName is "Login should sign in":
go run ./cmd <project> collect jest-report.json --artifacts-dir /tmp/detox-probe --dry-run -o json
```
Expected: the case carries `testFnFailure.png`. Then run it against a JUnit XML report and confirm the flag errors rather than being ignored.

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/artifacts/ internal/config/config.go internal/adapters/cli/command.go internal/core/services/report_service.go
git commit -m "feat(collect): --artifacts-dir attaches a framework's on-disk artifacts

Runs after parsing and before the dry-run return, so --dry-run shows what
would be attached, and the existing resolveArtifactAttachments pass uploads
it honouring --upload-artifacts.

The flag is framework-generic and the layout is inferred from the report's
format, so Maestro's debug output does not arrive later as a second
near-identical flag. A format with no support fails loudly: silently ignoring
the flag is indistinguishable from a matching bug."
```

---

### Task 6: Document it

**Files:**
- Modify: `README.md` (the format table area, around line 210)
- Modify: `../qf-docs/content/docs/cli/collect.mdx` (the native-reporters section and the per-framework examples)

- [ ] **Step 1: Add the CLI README section**

Under the collect documentation, after the format table:

```markdown
### Framework artifacts on disk

Some frameworks write screenshots, videos and logs to a directory rather than
into the results file. `--artifacts-dir` attaches those to the matching cases:

```bash
# Detox: the artifacts root, or one <configuration>.<timestamp> run inside it
qf myapp collect jest-results.json --artifacts-dir ./artifacts
```

Screenshots upload by default. Videos and device logs do not — pass
`--upload-artifacts=video,trace` for those. A video is the largest thing in a
report by an order of magnitude, and a device log from a real app is the
artifact most likely to carry customer data, so neither is a surprise by
default.

The directory is never guessed: without the flag, nothing is scanned.
```

- [ ] **Step 2: Add the docs page section**

In `collect.mdx`, beside the XCTest bundle example:

```markdown
# Detox — the Jest report, plus Detox's artifacts directory
qf myapp collect jest-results.json --artifacts-dir ./artifacts
```

- [ ] **Step 3: Commit**

```bash
git add README.md && git commit -m "docs(collect): document --artifacts-dir"
```

---

### Task 7: The `@qualflare/detox` package

**This task is a new repository and could reasonably be its own plan.** It is listed here because the spec includes it, and kept last because nothing else depends on it.

**Scope, stated plainly:** it re-exports `@qualflare/jest`'s reporter with Detox-sensible defaults. It contains almost no logic. Its value is discoverability — npm search, `awesome-detox`-style lists, "detox test reporting" queries — and its README must not imply otherwise.

- [ ] **Step 1: Create the repo from the qualflare-jest template**, matching the family: Apache-2.0, `package.json` with `@qualflare/detox`, a dependency on `@qualflare/jest`, and the same CI shape.

- [ ] **Step 2: The entire implementation**

```js
// index.js — @qualflare/detox
// Detox runs on Jest, so the Jest reporter already reports a Detox suite.
// This package exists so someone looking for a Detox reporter finds one.
module.exports = require('@qualflare/jest');
```

- [ ] **Step 3: The README leads with the truth**

It must say: Detox runs on Jest, this package is `@qualflare/jest` under a name Detox users will search for, and artifacts are attached by `qf collect --artifacts-dir` rather than by the reporter — with the reason (Jest runs `globalTeardown`, where Detox finalises video, *after* every reporter's `onRunComplete`).

- [ ] **Step 4: Publish and verify** — `npm publish`, then install it into a scratch project and confirm the reporter resolves.

---

### Task 8: Confirm against a real Detox run (pre-release)

Not a blocker, and deliberately last. The naming rule is known exactly from Detox's source, and the corpus in Tasks 2–4 is generated by that same algorithm. What a real run still confirms is narrower: that the files *inside* each directory are named as expected, and that nothing in Detox's Jest integration alters `fullName` before the reporter records it.

- [ ] **Step 1:** A minimal React Native app with Detox configured and all five artifact plugins enabled.
- [ ] **Step 2:** Three tests — one passing, one failing, one whose name contains `/`, `:` and `$`.
- [ ] **Step 3:** `jest.retryTimes(1)` on the failing one, so a ` (2)` directory really appears.
- [ ] **Step 4:** Run it, then `qf collect --artifacts-dir` against the output and confirm every file lands on the case it belongs to.
- [ ] **Step 5:** If the real names differ from the computed ones in any way, fix the computation and add the real name to the corpus as a regression test.

---

## Self-Review

**Spec coverage.** `--artifacts-dir` with report-inferred layout → Task 5. No auto-detection → Task 3 (empty directory is an error) and Task 5's flag help. Fail-loud for unsupported formats → Task 5. Forward matching → Task 4. Artifact kinds and defaults → Task 4. Retries attach to the case with the invocation in the name → Task 4. Unmatched reporting → Tasks 4 and 5. The `@qualflare/detox` wrapper → Task 7. The real-run confirmation → Task 8. No server or schema change → nothing in any task touches `domain.Attachment`'s shape.

**Placeholders.** None: every code step carries real code, and Task 8's steps name specific artifacts rather than "test it on a device".

**Type consistency.** `sanitizeFilename(input, replacement string) string` (Task 1) is used by `DirectoryName` (Task 2). `DirectoryName(fullName, status string, invocation int) string` and `ResolveRunDir(dir string) (string, error)` (Task 3) are both used by `Attach` (Task 4). `detox.Attach(launch *domain.Launch, dir string) (int, []string, error)` is used by `AttachFromDirectory` (Task 5) with the same signature. `domain.FrameworkDetox` is introduced in Task 5 and used only there.

**Two gaps found and closed while reviewing:**

1. `domain.FrameworkDetox` does not exist. Task 5 now names all three registration sites in
   `models.go` (the const block, `AllFrameworks()`, `GetCategory()`) plus the parser factory, rather
   than saying "beside the other constants".
2. **A bug that would have shipped:** the support check was keyed on `FrameworkDetox` alone. A Detox
   report is a Jest report, so content detection labels a real Detox upload `jest` — the flag would
   have errored on exactly the input it exists for. Both formats are now accepted, with a test
   (`TestAttachFromDirectoryAcceptsJest`) that fails if someone narrows it again.
