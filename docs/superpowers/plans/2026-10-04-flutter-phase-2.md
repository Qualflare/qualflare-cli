# Flutter support — Phase 2 Implementation Plan (parser, plugin, content)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** `qf collect` reads Flutter's JSON test output and uploads it as framework `flutter`; the Claude Code plugin sets it up and runs it; the website and docs say so.

**Architecture:** A new parser package `internal/adapters/parsers/unit/flutter` folds Dart's JSON reporter event stream into one `domain.Suite`, the way `unit/golang` folds `go test -json`. It is registered under `FrameworkFlutter`, detected by content from its `start` record, and tested against the real captures Phase 1 recorded. The plugin and content follow the patterns already used for Detox, WebdriverIO and Appium.

**Tech Stack:** Go 1.26 (qualflare-cli); Node test runner + Markdown skills (qualflare-claude-code); Astro (landing-fe), MDX (qf-docs), Markdown (blog).

**Spec:** `docs/superpowers/specs/2026-10-04-flutter-design.md`, **as corrected by** `docs/superpowers/specs/2026-10-04-flutter-findings.md`. Where the two differ, the findings win: they are measured.

## Global Constraints

- Framework slug, server category and `--format` value: exactly `flutter`. The server accepts it (api-service migration 0296 is in prod since pipeline #2066).
- Recommended command in every doc: `flutter test --file-reporter json:flutter-results.json`, then `qf <project> collect flutter-results.json`. For `integration_test` on a device, add `--platform android` or `--platform ios` to `qf collect`: the stream never names the device (findings Q2).
- One parsed file is one `domain.Suite`. Case `ClassName` is the test file **relative to the Flutter project root** (e.g. `test/widget_cases_test.dart`); the case also carries a `file` property with the same value.
- Case ID: `<relative file>#<full test name>`.
- Test output goes in the case property `system-out` (the name `ctrf.go:367` uses), clamped with the same limits as `ctrf.clampOutput`; attempt output goes in `Attempt.Stdout`, clamped the same way.
- Golden fixtures are the Phase 1 captures, copied unedited. No hand-written JSON stands in for a real capture; synthetic inputs are allowed only for cases no capture contains (truncation, two runs in one file), and each is named as synthetic in its test.
- Public repos (qualflare-cli, qualflare-claude-code) commit as `Qualflare Root <root@qualflare.com>` through the `github-qualflare` remote; private repos (landing-fe, blog) and GitLab (qf-docs, app-ui) use the default identity. No Claude/Anthropic attribution anywhere.
- Publishing gates: a CLI release tag, landing-fe `prod`, the qf-docs promotion MR, and the blog's publish each need the user's go-ahead (`! touch ~/.claude/publish-approved` for prod pushes).
- landing-fe builds with `astro build`, never `npm run build` (its postbuild submits to IndexNow).

## Review Focus

- **A `--machine` file whose first line is not an event** (the iOS capture starts with 11,000 lines of `-v` output; Android's starts with the `start` record but has a build line later). Detection must find the `start` record past leading noise, not demand `--format flutter`. Pinned by Task 3's `TestDetect_FlutterPastLeadingNoise`.
- **Paths from a different machine than the one running `qf collect`** (CI builds on Linux, collect runs elsewhere; or `/Users/runner/...` on macOS). Relative paths must come from the paths in the file, never from the current directory. Pinned by Task 1's `TestRelativePaths_*`.
- **A widget test that fails with something other than an exception block** (a timeout, `pumpAndSettle` never settling) still reports a useful error, not an empty message. Pinned by Task 2's `TestWidgetErrorWithoutBlock_KeepsGenericMessage`.
- **A retried `testWidgets` test**: the `Retry:` boundary applies to widget tests too, and each attempt's exception block belongs to that attempt. Pinned by Task 2's `TestRetriedWidgetTest_BlocksPerAttempt` (synthetic, built from the capture's own block text).
- **A huge `print` stream** (a test logging in a loop) must not blow the upload size. Pinned by Task 1's `TestPrintOutputIsClamped`.

---

### Task 1: Parser core — events to cases

**Repo:** qualflare-cli, branch `feat/flutter-parser` from `origin/main`.

**Files:**
- Create: `internal/adapters/parsers/unit/flutter/flutter.go` (parser + event types)
- Create: `internal/adapters/parsers/unit/flutter/paths.go` (project-root detection, relative paths)
- Create: `internal/adapters/parsers/unit/flutter/flutter_test.go`
- Create: `internal/adapters/parsers/unit/flutter/testdata/` ← copy of `docs/superpowers/specs/flutter-captures/*.jsonl`
- Modify: `internal/core/domain/models.go` — `FrameworkFlutter Framework = "flutter"` (const only; **not** in `AllFrameworks` yet, see Task 3)
- Modify: `internal/adapters/parsers/generic/ctrf/ctrf.go` — move `clampOutput` and its two limit constants to `internal/adapters/parsers/base` as exported `ClampOutput`, and call it from ctrf (no behaviour change)

**Interfaces:**
- Produces: `flutter.New() *Parser`; `(*Parser).Parse(io.Reader) (*domain.Suite, error)`; `GetFramework() domain.Framework` → `domain.FrameworkFlutter`; `SupportedFileExtensions() []string` → `[".json", ".jsonl"]`.
- Produces: `base.ClampOutput(lines []string) []string` (same limits as today's ctrf).
- Produces (package-internal, Task 2 extends): per-test state `testState{ id int; name, file string; line *int; start, end int64; result string; skipped, hidden bool; skipReason string; errors []testError; prints []string }`, where `testError{ message, stack string; isFailure bool }`.

Rules this task implements (findings Q2, Q3, corrections 2 and 3):

| Input | Rule |
|---|---|
| a line that is not a JSON object with a string `type` | skipped (blank lines, build output, `[{"event":"test.startedProcess"…}]` arrays, `-v` logs) |
| `start` | resets all state, so two runs appended into one file both parse (test IDs restart at 0) |
| `testDone.hidden == true` | dropped (passing loads, `setUpAll`, `tearDownAll`, integration_test's own `(tearDownAll)`) |
| a failed `loading <path>` test (Dart un-hides it) | an `error` case, `Name` `loading <relative file>`, `ClassName` the relative file |
| file and line | `test.root_url`/`root_line` when present, else `test.url`/`line`, else the suite's `path` |
| status | `skipped: true` → skipped; `success` → passed; `failure` → failed; `error` → error (Task 2 refines widget errors) |
| skip reason | `test.metadata.skipReason` when non-null, into `Error` |
| duration | `testDone.time − testStart.time`, milliseconds |
| error events | message + stack joined with `domain.FormatError`, in order, separated by a blank line |
| `print` events (`messageType: "print"`) | the test's `system-out` property, clamped |
| `testStart` with no `testDone` | `error`, message `test did not finish (the run ended before this test reported a result)` |
| suite property | `platform` = first `suite.platform` seen (`vm` in every capture) |

Relative paths (`paths.go`): the project root is the longest directory prefix shared by all `suite.path` values that ends just before a `test/` or `integration_test/` segment; a path is made relative to it. A path with neither segment falls back to its base name. `file://` prefixes are stripped first.

- [ ] **Step 1: Copy the captures into `testdata/` unedited**, and write the failing tests against `testdata/widget-machine.jsonl`:

```go
func TestWidgetCapture_Cases(t *testing.T) // asserts exactly these visible cases, by ID:
//   test/setup_all_failure_test.dart#with a broken setUpAll (setUpAll)   error  Error contains "Bad state: setUpAll exploded"
//   test/broken_test.dart#loading test/broken_test.dart                  error  Error contains "Compilation failed"
//   test/widget_cases_test.dart#login screen shows a title               passed Duration 380ms
//   test/widget_cases_test.dart#login screen throws an error             error
//   test/widget_cases_test.dart#login screen is skipped                  skipped
//   test/widget_cases_test.dart#login screen prints output               passed Properties["system-out"] == "hello from a widget test"
//   test/widget_cases_test.dart#login screen passes on the third attempt passed
//   (fails an expectation: present; its status is Task 2's)
// and no case whose name starts with "loading" except broken_test's, none ending "(tearDownAll)".
func TestFileReporterCapture_SameCasesAsMachine(t *testing.T) // widget-file-reporter.jsonl → same IDs, statuses
func TestDeviceCaptures_AndroidAndIOS(t *testing.T)          // both: exactly 2 cases, "counter app starts at zero" passed,
                                                               // "counter app fails on device" not passed; ClassName integration_test/app_test.dart;
                                                               // Suite.Properties["platform"] == "vm"
func TestRelativePaths_LinuxAndMacRunners(t *testing.T)      // /home/runner/work/x/x/app/test/a_test.dart and
                                                               // /Users/runner/work/x/x/app/integration_test/b_test.dart → test/a_test.dart, integration_test/b_test.dart
func TestTruncatedStream_UnfinishedTestIsError(t *testing.T) // synthetic: widget-machine.jsonl cut right after "testStart" id 16
func TestTwoRunsInOneFile_BothCounted(t *testing.T)          // synthetic: widget-file-reporter.jsonl concatenated twice → 2× cases, no ID collision panic
func TestPrintOutputIsClamped(t *testing.T)                  // synthetic: 50,000 print events for one test → system-out within base.ClampOutput's limits
```

- [ ] **Step 2: Run them to see them fail** — `go test ./internal/adapters/parsers/unit/flutter/...` → FAIL: package does not compile (`flutter.New` undefined).
- [ ] **Step 3: Move `clampOutput` to `base.ClampOutput`**, run `go test ./internal/adapters/parsers/generic/ctrf/...` → PASS (unchanged behaviour).
- [ ] **Step 4: Implement the parser** to the rule table above. Read line by line with a `bufio.Scanner` whose buffer allows 16 MiB lines (exception blocks and `-v` lines can be long). State is a `map[int]*testState` keyed by test ID plus suite and group maps, reset at `start`.
- [ ] **Step 5: Run the package tests** → PASS, except the `fails an expectation` status, which Task 2 fixes (its test asserts only presence here).
- [ ] **Step 6: Commit** — `feat(flutter): parse Flutter's JSON test reporter output`.

---

### Task 2: Widget exception blocks and retry attempts

**Files:**
- Create: `internal/adapters/parsers/unit/flutter/widget_errors.go`
- Create: `internal/adapters/parsers/unit/flutter/attempts.go`
- Modify: `internal/adapters/parsers/unit/flutter/flutter.go` (call both when building a case)
- Test: `internal/adapters/parsers/unit/flutter/flutter_test.go`

**Interfaces:**
- Consumes: Task 1's `testState`, `testError`.
- Produces: `parseExceptionBlock(prints []string) (block exceptionBlock, rest []string, ok bool)` where `exceptionBlock{ kind, message, stack string }`; `splitAttempts(st *testState) []attemptSlice`.

**Exception block (findings correction 1).** A block starts at a print line beginning `══╡ EXCEPTION CAUGHT BY FLUTTER TEST FRAMEWORK ╞`. Its second line is `The following <Kind> was thrown running a test:`. The message is every line after that up to the line `When the exception was thrown, this was the stack:`; the stack is the lines after it up to the closing `════…` line (or the end). For a test with result `error` and a block: status **failed** when `Kind == "TestFailure"`, else **error**; `Error = FormatError(message, stack, kind unless TestFailure)`; the generic `Test failed. See exception logs above.` error event is dropped; the block's lines are removed from `system-out`. With no block, Task 1's behaviour stands.

**Attempts (findings Q1).** A print `Retry: <the test's full name>` closes one failed attempt. Attempt *n* holds the errors (and any exception block) seen since the previous boundary. The final `testDone` is the last attempt. When at least one boundary exists: `Attempts` is 1-based, earlier attempts status `failed` (or `error` per the same rules), the last attempt has the test's final status; `RetryCount = len(Attempts) − 1`; `IsFlaky = true` when the final status is passed. `Retry:` lines never reach `system-out`; the case's `Error` describes the final attempt only (empty when it passed).

- [ ] **Step 1: Write the failing tests**

```go
func TestWidgetExpectFailure_IsFailedWithRealMessage(t *testing.T) // widget-machine.jsonl, "login screen fails an expectation":
//   Status failed; Error starts "Expected: exactly one matching candidate"; Error contains "widget_cases_test.dart:15:7";
//   Error does not contain "See exception logs above"; system-out does not contain "EXCEPTION CAUGHT"
func TestWidgetThrow_IsErrorWithKind(t *testing.T)               // "login screen throws an error": Status error; Error starts "StateError: Bad state: boom"
func TestDeviceFailure_IsFailed(t *testing.T)                     // android and ios "counter app fails on device": Status failed
func TestRetriedTest_PerAttemptHistory(t *testing.T)              // "login screen passes on the third attempt":
//   3 attempts; [0] failed "Expected: <3>"…"Actual: <1>"; [1] failed …"Actual: <2>"; [2] passed;
//   *RetryCount == 2; *IsFlaky == true; Error == ""; system-out has no "Retry:" line
func TestRetriedWidgetTest_BlocksPerAttempt(t *testing.T)         // synthetic, built from the capture's own block lines: a testWidgets test
//   printing block → "Retry: <name>" → block → testDone error ⇒ 2 attempts, each failed with its own block's message
func TestWidgetErrorWithoutBlock_KeepsGenericMessage(t *testing.T) // synthetic: result error, error event "Test failed. See exception logs above.", no block
//   ⇒ Status error, Error contains "Test failed"
```

- [ ] **Step 2: Run them to see them fail** — `go test ./internal/adapters/parsers/unit/flutter/ -run 'Widget|Retried|DeviceFailure'` → FAIL (status `error` where `failed` is expected; no attempts).
- [ ] **Step 3: Implement `parseExceptionBlock` and `splitAttempts`** and call them from case building.
- [ ] **Step 4: Run the whole package** → PASS, including every Task 1 test.
- [ ] **Step 5: Commit** — `feat(flutter): real messages for widget-test failures, and per-attempt retry history`.

---

### Task 3: Register `flutter` everywhere a framework is listed

**Files:**
- Modify: `internal/core/domain/models.go` — add `FrameworkFlutter` to `AllFrameworks()` after `FrameworkAppium`, with a comment naming api-service migration 0296
- Modify: `internal/core/domain/models_test.go:467` — add `"flutter": true` to the server-enum copy
- Modify: `internal/adapters/parsers/factory/factory.go` — `f.RegisterParser(flutter.New())`; a `jsonDetectors` entry `hasKeys(obj, "protocolVersion", "type")` with `obj["type"] == "start"` → `FrameworkFlutter`; a filename rule `strings.Contains(base, "flutter")` → `FrameworkFlutter` placed before the `.json` default
- Modify: `factory.go` `detectNDJSONFramework` — scan forward past lines that do not start with `{`, up to the first 1 MiB of content, and classify the first object line found (the go-test path keeps working: its first line is an object)
- Modify: `internal/adapters/cli/command.go` — `frameworkDisplayGroups`: `domain.FrameworkFlutter: domain.CategoryE2E`
- Modify: `README.md:210` — add Flutter to the **UI / E2E / Mobile** row, and a short Flutter section with the Global Constraints command
- Test: `internal/adapters/parsers/factory/factory_ndjson_test.go`, `factory_test.go`

**Interfaces:**
- Consumes: Task 1's `flutter.New()`, `domain.FrameworkFlutter`.

- [ ] **Step 1: Write the failing tests**

```go
func TestDetect_FlutterMachineAndFileReporter(t *testing.T) // widget-machine.jsonl, widget-file-reporter.jsonl, device-android.jsonl → FrameworkFlutter
func TestDetect_FlutterPastLeadingNoise(t *testing.T)       // device-ios.jsonl (starts with -v log lines) → FrameworkFlutter
func TestDetect_GoTestJSONUnchanged(t *testing.T)           // an existing go-test fixture → FrameworkGolang
func TestDetectByFilename_Flutter(t *testing.T)             // "flutter-results.json" → FrameworkFlutter
```

plus the existing `TestEveryFrameworkCategoryIsAcceptedByTheServer` and `TestBuildFrameworkDisplayGroups_IncludesEveryFramework`, which fail until `models_test.go` and `frameworkDisplayGroups` gain `flutter`.

- [ ] **Step 2: Run** `go test ./internal/adapters/parsers/factory/... ./internal/core/domain/... ./internal/adapters/cli/...` → FAIL on the new tests.
- [ ] **Step 3: Make the edits listed under Files.**
- [ ] **Step 4: Run the full suite** — `go test ./... && gofmt -l . && go vet ./...` → all pass, `gofmt -l` prints nothing.
- [ ] **Step 5: Smoke-test the binary** on every capture:

```bash
go build -o /tmp/qf ./cmd/... 2>/dev/null || go build -o /tmp/qf .
for f in internal/adapters/parsers/unit/flutter/testdata/*.jsonl; do /tmp/qf demo collect "$f" --dry-run 2>&1 | grep -E -i 'framework|passed|failed|error|skipped' | head -6; done
/tmp/qf list-formats | grep -i flutter
```

Expected: every file reports framework `flutter`; widget-machine shows 3 passed (1 of them flaky), 1 failed, 3 errors (the failed `setUpAll`, the failed load, the thrown `StateError`), 1 skipped; each device file 1 passed, 1 failed; `list-formats` lists flutter under E2E.
- [ ] **Step 6: Commit** — `feat(flutter): register the flutter format, with content detection past tool output`.

---

### Task 4: A real Flutter run in CI

**Files:**
- Create: `test/integration/fixtures/flutter-project/` — `pubspec.yaml` (`flutter_test` dev dependency only), `lib/main.dart` (a one-screen app), `test/widget_test.dart` with one passing, one failing `expect`, one `retry: 1` test that fails once, one skipped
- Modify: `.github/workflows/ci.yml` — job `flutter-parser` on `ubuntu-latest`: `subosito/flutter-action@v2` (`channel: stable`), `flutter test --file-reporter json:flutter-results.json || true` in the fixture, then `go run . demo collect flutter-results.json --dry-run --output json` and assert with `jq`: framework `flutter`, 4 cases, 1 failed, 1 skipped, 1 flaky with 2 attempts
- Test: the CI job itself

- [ ] **Step 1: Add the fixture and the job; push the branch** — the job runs on the PR.
- [ ] **Step 2: Watch it** — `gh run watch <id> --repo Qualflare/qualflare-cli --exit-status` → `flutter-parser` passes. If the `--output json` flag name differs, use the flag `qf collect --help` lists for machine-readable dry-run output; ledger the substitution.
- [ ] **Step 3: Commit** — `ci: run a real Flutter suite through the flutter parser`.
- [ ] **Step 4: Open the PR** (qfroot), title `feat: Flutter test reporting (qf collect --format flutter)`.

---

### Task 5: Release and dogfood

- [ ] **Step 1: After the PR merges, release** the next CLI minor through the repo's existing release workflow (tag `v0.x.0` on main). Gate: the user's go-ahead.
- [ ] **Step 2: Real upload.** Install the released `qf`, run `qf <dogfood project> collect testdata/widget-file-reporter.jsonl` against prod, open the launch: framework shows the Flutter mark; the flaky test shows 3 attempts; "fails an expectation" is failed with the `Expected: exactly one matching candidate` message.
- [ ] **Step 3: app-ui count constants** (Phase 1 deferred minor): in `src/components/icons/framework-marks.ts`, set `AUTO_DETECTED_FORMAT_COUNT` and the "N of the M formats" / "a mark for N of them" comments from `qf list-formats` of the released CLI; `pnpm exec tsc -b && pnpm exec vitest run` → pass; MR to app-ui main.

---

### Task 6: Claude Code plugin 0.21.0

**Repo:** qualflare-claude-code (local checkout `/Users/ibrahim/Astrais/qualflare-ai`), branch `feat/flutter`.

**Files:**
- Modify: `skills/qf-init/references/framework-slugs.md` — a `flutter` row after `appium`: category E2E; detected by `pubspec.yaml` with `flutter_test` or `integration_test` under `dev_dependencies`; note "widget and integration_test suites; plain `test()` cases in the same run are uploaded too". Test-file globs row: `test/**/*_test.dart`, `integration_test/**/*_test.dart`
- Modify: `skills/qf-run/SKILL.md` — a `flutter` row: run `flutter test --file-reporter json:<R>/flutter/flutter-results.json` (widget suite); for `integration_test/`, only when `flutter devices --machine` lists a connected device or a running emulator/simulator, run `flutter test integration_test/ -d <id> --file-reporter json:<R>/flutter/integration-results.json` and pass `--platform android|ios` from that device's `targetPlatform` to `qf collect`; never boot a device
- Modify: `scripts/check-skills.mjs` — `flutter: 'json'`
- Modify: `scripts/fixtures/list-formats.txt` — regenerate from the released CLI's `qf list-formats`
- Modify: `.claude-plugin/plugin.json` (and the marketplace manifest the repo versions alongside it) — `0.21.0`; `CHANGELOG` / README framework list
- Test: `npm test && npm run validate-manifests && npm run check-skills && npm run check-slugs`

- [ ] **Step 1:** Make the edits; run the four checks → `check-slugs` fails until `list-formats.txt` is regenerated from the **released** CLI (Task 5), then passes.
- [ ] **Step 2: Commit and PR** (qfroot): `feat: Flutter in /qf-init and /qf-run (0.21.0)`.

---

### Task 7: Content

**Repos:** landing-fe (private, GitHub), qf-docs (GitLab), blog (private, GitHub). Each change goes through its normal MR/PR and promotion; verification is on the live site: new text present AND old text absent.

**Files:**
- landing-fe — Create `src/pages/flutter-test-reporting.astro`, modelled on `espresso-test-reporting.astro` (a CLI-ingested mobile framework): the Global Constraints commands; what arrives (statuses, real widget-test failure messages, per-attempt retries, `print` output); the `--platform` note for devices; FAQ "Do I need Maestro to report Flutter tests?" → no; limitations (no screenshots or labels yet; device not in the stream). Link it wherever `/xctest-test-reporting/` is listed: `src/components/sections/footer.tsx`, `src/pages/integrations.astro`, `src/pages/test-reporting.astro`, `src/data/glossary.ts` (mobile entries), `src/data/page-dates.json`, `public/llms.txt`, `public/llms-full.txt`. Framework counts 26 → 27 where they appear (`src/pages/features/frameworks.astro`, `src/pages/features/claude-code.astro`, llms files). `src/pages/maestro-test-reporting.astro:444` ("Incidental reach into React Native and Flutter results"): add that Flutter's own test suites now upload directly, linking the new page.
- qf-docs — the format list in `content/docs/cli/collect.mdx` and `content/docs/cli/index.mdx`, counts in `content/docs/faq/index.mdx`, `content/docs/concepts/claude-code.mdx`, `content/docs/reference/claude-code.mdx`, `content/docs/reference/glossary.mdx`; a Flutter example in `collect.mdx`.
- blog — in `posts/maestro-mobile-testing.md` and `posts/appium-vs-espresso-vs-xcuitest-vs-maestro.md`, where Flutter is mentioned as a Maestro target, add one sentence that Flutter's own widget and integration tests report to Qualflare directly, linking the landing page. No other rewrites.

- [ ] **Step 1: Find every count and claim before editing** — `git grep -n -E '\b26\b.*(frameworks|formats)|(frameworks|formats).*\b26\b'` and `git grep -n -i flutter` in each repo; the edit list is that output, not the list above alone.
- [ ] **Step 2: Build** — landing-fe `npx astro build`; qf-docs `npm ci && npm run build`; blog's own check. All pass.
- [ ] **Step 3: PRs/MRs**, then promotion with the user's go-ahead; verify live: `curl -s https://qualflare.com/flutter-test-reporting/ | grep -c 'file-reporter json:'` ≥ 1, and the old counts absent on every changed page.

---

## Order

Tasks 1 → 2 → 3 → 4 on one CLI branch and PR; 5 after merge; 6 after the release (it needs the released `list-formats`); 7 after the release (its pages describe the released behaviour).
