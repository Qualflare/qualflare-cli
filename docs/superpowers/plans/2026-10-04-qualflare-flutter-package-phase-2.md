# qualflare_flutter — Phase 2 Implementation Plan (screenshots, qf 0.3.0, devices, release)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Screenshots and the failure wrapper in `qualflare_flutter`, the `qf collect` parser addition that turns its markers into labels, links, tags, priority, steps and attachments (CLI 0.3.0), a device CI that proves both together, then pub.dev 0.1.0, dogfooding and content.

**Architecture:** Unchanged from the spec: markers in the `flutter test` results file → `qf collect` → Qualflare. The spike settled the open points: 48 KiB chunks stand, `captureImage(rootElement)` works on host and devices and sees a failing screen from a `catch`, native capture works on devices, and host widget tests draw text with the Ahem font — solved by an **opt-in** `qualflare.loadFonts()` (the user's choice).

**Tech Stack:** Dart/Flutter 3.47 (package), Go 1.26 (qualflare-cli), GitHub Actions (`flutter-action` and `android-emulator-runner` pinned), pub.dev automated publishing (OIDC).

**Spec:** `docs/superpowers/specs/2026-10-04-qualflare-flutter-package-design.md`, with `docs/superpowers/specs/2026-10-04-qualflare-flutter-package-findings.md` (qualflare-cli). Phase 1: `qualflare-flutter` main (API, 24 tests).

## Global Constraints

- Marker protocol exactly as the spec's table (`##qualflare[v1] ` + compact JSON with `k`); chunk size 48 KiB; caps 5 MiB per attachment, 20 MiB per test, inclusive.
- `qf` reads markers only from print events of non-hidden tests; marker lines never reach `system-out` or attempt stdout; results without markers parse exactly as in v0.2.0.
- `domain.Attempt` has no attachments or steps: **steps come from the final attempt; attachments from every attempt go on the case, an earlier attempt's named `attempt <n>: <name>`** (n is 1-based) so a flaky test's first-try failure screenshot is kept.
- Commits: public repos as `Qualflare Root <root@qualflare.com>` via `github-qualflare`; no Claude/Anthropic attribution. Actions pinned by SHA.
- Gates (stop and ask): the `qf` v0.3.0 tag, the first `dart pub publish`, enabling automated publishing, the dogfood upload to prod, content promotions.
- User-only steps: verified `qualflare.com` publisher on pub.dev; the `qualflare-flutter` project in the app and its token as repo secret `QUALFLARE_TOKEN`.

## Review Focus

- **A screenshot taken while a dialog/route animation is mid-flight** must still produce a PNG (not hang waiting to settle). Pinned by Task 1's `screenshot does not pump or settle`.
- **`qualflareTestWidgets` when the failure is a framework-caught exception** (e.g. a build error reported by Flutter after the body returns): no screenshot is promised, but the test must fail exactly as plain `testWidgets` would. Pinned by Task 1's `wrapper does not change how tests fail`.
- **A results file where a marker's chunks are interleaved with another attachment's chunks, or arrive out of order**, reassembles by `id`/`i`. Pinned by Task 3's `TestMarkers_ChunksInterleavedAndOutOfOrder`.
- **A marker printed after its test's `testDone`** (an unawaited step) is ignored, not attached to a later test. Pinned by Task 3's `TestMarkers_AfterTestDoneIgnored`.
- **`loadFonts()` called twice, or on a device**, is harmless (idempotent; no-op on device). Pinned by Task 1's `loadFonts is idempotent`.

---

### Task 1: Package — `screenshot`, `qualflareTestWidgets`, `loadFonts`, Phase 1 cleanups

**Repo:** qualflare-flutter, branch `feat/screenshots`.

**Files:** create `lib/src/screenshot.dart`, `lib/src/fonts.dart`, `lib/src/test_widgets.dart`; modify `lib/src/qualflare.dart`, `lib/qualflare_flutter.dart` (export `qualflareTestWidgets`), `pubspec.yaml` (add `integration_test: sdk: flutter`), README, CHANGELOG; tests in `test/screenshot_test.dart`, `test/fonts_test.dart`, `test/test_widgets_test.dart`, `test/qualflare_test.dart`.

**Interfaces (produces):**
- `Future<void> Qualflare.screenshot(WidgetTester tester, String name, {bool native = false})` — PNG attached as `<name>.png`, `image/png`, through `attachment` (so caps apply). Default: root layer via `captureImage(tester.binding.rootElement!)` inside `tester.runAsync`. `native: true` on an `IntegrationTestWidgetsFlutterBinding`: on Android call `convertFlutterSurfaceToImage()` once per process then `takeScreenshot(name)`; elsewhere ignored (falls back to root layer). Never pumps or settles. A capture error becomes a `warn` marker, never a test failure.
- `Future<void> Qualflare.loadFonts()` — host only (no-op when the binding is an `IntegrationTestWidgetsFlutterBinding`): loads every family in `FontManifest.json` via `FontLoader`, plus `Roboto` from `$FLUTTER_ROOT/bin/cache/artifacts/material_fonts/Roboto-*.ttf` (skip silently if absent); idempotent.
- `void qualflareTestWidgets(String description, WidgetTesterCallback callback, {bool? skip, Timeout? timeout, bool semanticsEnabled = true, TestVariant<Object?> variant = const DefaultTestVariant(), dynamic tags, int? retry})` — delegates to `testWidgets` with identical parameters, running `callback` through `Future<void> runWithFailureScreenshot(WidgetTester tester, WidgetTesterCallback callback)` (in `lib/src/test_widgets.dart`, not exported): on any throw it awaits `qualflare.screenshot(tester, 'failure')`, then `rethrow`s.
- Cleanups from Phase 1's deferred list: names/values/URLs/step names truncated to 8192 chars (surrogate-safe); empty step name → `step`; error truncation surrogate-safe; U+2028/U+2029/U+0085 escaped as ` `/` `/`\u0085` in `encodeMarker`; private zone key `final _stepZoneKey = Object()`.

- [ ] **Step 1: Failing tests** — `screenshot attaches a decodable PNG` (pump a coloured `MaterialApp`, screenshot, reassemble the captured `att` chunks, `instantiateImageCodec` decodes, size > 0); `screenshot does not pump or settle` (start an infinite `AnimationController.repeat()`, screenshot completes within the test); `screenshot outside a test does nothing`; `wrapper screenshots on failure and rethrows` (an inner `testWidgets` via the wrapper inside a test group cannot be asserted directly — instead call the wrapper's extracted `Future<void> runWithFailureScreenshot(WidgetTester, WidgetTesterCallback)` and assert one `failure.png` attachment and the original `TestFailure` rethrown); `wrapper does not change how tests fail` (a passing callback → no attachment, no warn); `loadFonts is idempotent` (twice, no error; a `Text` laid out after loading has different width than under Ahem); the cleanup tests (`caps long names`, `empty step name becomes step`, `does not split surrogate pairs`, `escapes line separators`, `step rethrows a StateError unchanged`, `step started from a Timer nests`).
- [ ] **Step 2:** `flutter test` → FAIL (undefined members). **Step 3:** implement. **Step 4:** `flutter test && flutter analyze --fatal-infos && dart format --set-exit-if-changed . && dart pub publish --dry-run` → pass, 0 warnings. **Step 5:** commit `feat: screenshots, qualflareTestWidgets and opt-in loadFonts`; PR.

---

### Task 2: Package — example suite and device CI that produce the golden captures

**Files:** create `example/` (a minimal app: `lib/main.dart` with a checkout screen; `integration_test/checkout_test.dart`; `test/checkout_widget_test.dart`) using every API call: `label`, `link` (issue), `tags`, `priority`, nested `step`s, `attachment` (JSON), `screenshot`, a `qualflareTestWidgets` test that fails on purpose (`expect` on missing text), and a `retry: 1` test that fails once with a screenshot and then passes. Modify `.github/workflows/ci.yml`: add jobs `example-widget` (ubuntu), `example-android` (emulator API 34, x86_64), `example-ios` (macos-15, booted iPhone simulator, 90-min limit) — each runs `flutter test … --file-reporter json:results-<platform>.json || true`, asserts the file contains `##qualflare[v1]` markers for every kind with `jq -s -e`, and uploads it as an artifact.

- [ ] **Step 1:** write the example and jobs; **Step 2:** push, CI green on all three; **Step 3:** download the three artifacts — they are Task 3's golden fixtures; **Step 4:** commit `test: an example suite exercising the API on host and devices`; merge after review.

---

### Task 3: qf — read the markers (qualflare-cli)

**Files:** create `internal/adapters/parsers/unit/flutter/markers.go` + `markers_test.go`; copy Task 2's three results files to `internal/adapters/parsers/unit/flutter/testdata/package-{widget,android,ios}.jsonl` (unedited); modify `flutter.go` (`buildCase`, `splitAttempts` callers) and `attempts.go`.

**Interfaces:**
- `type markerSet struct { labels []domain.Label; links []domain.Link; tags []string; priority domain.Severity; steps []domain.Step; attachments []domain.Attachment; warnings []string }`
- `func extractMarkers(prints []string) (set markerSet, rest []string)` — `rest` is `prints` minus marker lines; parses `label/link/tag/priority/step+/step-/att/warn`; unknown `k`, malformed JSON or a `[v2]` prefix → one warning each kind; steps ordered by start, `ParentIndex` from `parent` (index into the same slice), duration `t(step-) − t(step+)`, unfinished → `error` "step did not finish"; attachments reassembled by `id` from chunks `0..n-1` in any order, incomplete → dropped + warning; caps re-applied.
- `buildCase`: markers per attempt (split by the existing retry marks); labels/links/tags deduplicated across attempts; priority = last value; steps from the final attempt; attachments from all attempts, earlier ones renamed `attempt <n>: <name>`; warnings appended to `system-out` as `qualflare: <warning>`.

- [ ] **Step 1: Failing tests** against the golden fixtures: `TestPackageCapture_Widget/Android/IOS` (exact labels, links, tags, priority, step names/nesting/statuses, attachment names and that `failure.png` decodes as PNG on the failing test; `system-out` contains no `##qualflare`); `TestRetriedTest_AttachmentsFromBothAttempts` (`attempt 1: failure.png` + final attempt's); plus synthetic (named) `TestMarkers_ChunksInterleavedAndOutOfOrder`, `TestMarkers_IncompleteAttachmentDropped`, `TestMarkers_UnknownVersionWarned`, `TestMarkers_MalformedSkipped`, `TestMarkers_AfterTestDoneIgnored`, `TestMarkers_NoneParsesAsBefore` (the four Phase 1 parser captures produce byte-identical suites to before).
- [ ] **Step 2–4:** RED, implement, `go test ./... && gofmt -l . && go vet ./... && golangci-lint run ./internal/...` → green. **Step 5:** commit, PR; CI green including the existing `flutter-parser` job.
- [ ] **Step 6 (gate):** after merge, tag `v0.3.0` (annotated, Qualflare Root) with the user's go-ahead; release workflow green; `qf version` from the release shows 0.3.0.

---

### Task 4: End to end, dogfood and pub.dev

- [ ] **Step 1:** `qf <id> collect results-android.json --dry-run -o json` with the v0.3.0 binary shows the example's labels, steps and a `failure.png` attachment on the failing case.
- [ ] **Step 2 (user + gate):** with the `qualflare-flutter` project and `QUALFLARE_TOKEN` secret in place, add a `dogfood` job to `ci.yml` on `main` only: install `qf` 0.3.0 from the release, `qf qualflare-flutter collect results-*.json --platform android` (and `ios`), README badge from the project's public badge URL. Verify the launch in the app: labels, nested steps, the screenshot rendered.
- [ ] **Step 3 (user + gate):** user creates the verified `qualflare.com` publisher; version `0.1.0` in `pubspec.yaml` + CHANGELOG; `dart pub publish` (first publish manual, by the user or with their go-ahead); transfer to the publisher; enable automated publishing from GitHub Actions on tags `v*`; add `.github/workflows/publish.yml` (official `dart-lang/setup-dart` reusable publish workflow, pinned).

---

### Task 5: Content and the Claude Code plugin

Updates through each repo's normal PR/MR and promotion (gated): landing-fe `/flutter-test-reporting/` (new — the Flutter page Phase 2 of the parser plan scheduled, now covering parser + package), framework counts 26 → 27, footer/integrations/test-reporting/glossary links, `llms.txt`/`llms-full.txt`; qf-docs format list and a Flutter section; blog: one sentence in the two Maestro posts; app-ui framework-count constants; Claude Code plugin 0.21.0 (`/qf-init` detects Flutter and suggests `qualflare_flutter`; `/qf-run` runs `flutter test --file-reporter json:`); the parser plan's Tasks 6–7 are folded in here. Verification: new text present and old text absent on every live page.
