# qualflare_flutter — Phase 1 Implementation Plan (spike, repo, metadata API)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Answer the spec's three device questions with a CI spike, create the `qualflare-flutter` repo, and ship the package's marker writer and metadata API (`label`, `link`, `tag`, `priority`, `step`, `attachment`), tested in Dart.

**Architecture:** The package prints one `##qualflare[v1] {json}` line per API call from inside the running test's zone; Dart's JSON reporter records it as that test's `print` event, and `qf collect` (a later phase) lifts it into the case. Phase 1 builds everything whose shape the spike cannot change. **Phase 2** (separate plan, from the spike's findings): `screenshot` and `qualflareTestWidgets`, the `qf` parser addition (CLI 0.3.0) with golden fixtures, the device CI and dogfood upload, pub.dev 0.1.0, and content.

**Tech Stack:** Dart ≥ 3.5 / Flutter stable, `package:test_api` hooks (`TestHandle`), `flutter_test`; GitHub Actions with `subosito/flutter-action` (pinned by SHA) and `reactivecircus/android-emulator-runner`.

**Spec:** `docs/superpowers/specs/2026-10-04-qualflare-flutter-package-design.md` (qualflare-cli)

## Global Constraints

- Package name `qualflare_flutter`; repo `Qualflare/qualflare-flutter` (public); license Apache-2.0 (as the other Qualflare packages).
- Marker line: exactly `##qualflare[v1] ` (trailing space) followed by compact JSON with `"k"`; one line per call. Kinds and fields exactly as the spec's protocol table: `label{name,value}`, `link{type,url,name?}`, `tag{tags}`, `priority{value}`, `step+{id,parent?,name,t}`, `step-{id,status,t,error?}`, `att{id,name,type,n,i,data}`, `warn{msg}`.
- Link `type` ∈ `issue`/`tms`/`custom` (default `custom`); priority ∈ `low`/`medium`/`high`/`critical`; names and values trimmed, empty ones ignored (no line printed).
- Attachment chunks ≤ 48 KiB of base64 each (Task 0 may lower it — then Task 3 uses the measured value); caps 5 MiB per attachment and 20 MiB per test (decoded bytes); over a cap → a `warn` line instead of data.
- Outside a running test (`TestHandle.current` throws `OutsideTestException`), every API call does nothing and prints nothing.
- Commits on public repos as `Qualflare Root <root@qualflare.com>` via the `github-qualflare` SSH alias; no Claude/Anthropic attribution. Actions pinned by SHA (flutter-action v2.23.0 = `1a449444c387b1966244ae4d4f8c696479add0b2`).

## Review Focus

- **Async gaps inside a step** (`await` in a step body, timers): the step's start/end lines and any nested step must still carry the right `parent`, because zone values follow async continuations. Pinned by Task 3's `step nests across awaits`.
- **Two tests running `step` concurrently** is impossible within one isolate, but **ids must stay unique within a test across many steps** and never collide with a retried attempt's ids. Pinned by Task 3's `step ids are unique and increasing`.
- **A body that throws a non-`Exception`** (an `Error`, a `String`) must still end the step `error` and rethrow the same object with its stack. Pinned by Task 3's `step rethrows any thrown object unchanged`.
- **Unicode and control characters in names/values** must not break the one-line rule (a newline in a label value would split the marker). Pinned by Task 2's `encodes newlines and unicode on one line`.
- **An attachment exactly at a cap boundary** (5 MiB, and the per-test running total reaching 20 MiB) — at the cap is allowed, one byte over is a `warn`. Pinned by Task 3's `caps are inclusive`.

---

### Task 0: CI spike — device print limits, PNG size, screenshot method

**Repo:** qualflare-cli, throwaway branch `spike/flutter-package` (deleted at the end, as the Phase 1 parser spike was). The repo for the package does not exist yet.

**Files:**
- Create: `spike/flutter-package/integration_test/spike_test.dart`, `spike/flutter-package/test/widget_spike_test.dart`
- Create: `.github/workflows/spike-flutter-package.yml` (jobs `widget` on ubuntu, `android` with `reactivecircus/android-emulator-runner` API 34 x86_64, `ios` on `macos-15` with a booted iPhone simulator — the Phase 1 spike workflow's job shapes; every job uploads its raw `--file-reporter json:` output and stderr with `if: always()`, timeout 90 min for iOS)
- Create on main afterwards: `docs/superpowers/specs/2026-10-04-qualflare-flutter-package-findings.md` + `docs/superpowers/specs/flutter-package-captures/*.jsonl`

**What the spike tests print and do** (both widget and `integration_test`, run with `--file-reporter json:`):
1. Print single lines of `##qualflare[v1] ` + `{"k":"spike","n":<N>,"data":"<N 'a's>"}` for N = 4 KiB, 16 KiB, 48 KiB, 64 KiB, 256 KiB, 1 MiB.
2. Capture the screen as PNG by (a) rendering the root layer (`RendererBinding.instance.renderViews.first.debugLayer as OffsetLayer` → `toImage(bounds, pixelRatio: devicePixelRatio)` → `toByteData(format: png)`, inside `tester.runAsync` in widget tests) and (b) on device, `IntegrationTestWidgetsFlutterBinding.takeScreenshot('x')` (after `convertFlutterSurfaceToImage()` on Android). Print each PNG's byte length and capture time, then print it as base64 chunks of 48 KiB.
3. A test that pumps a red screen, then fails an `expect` inside a `try/catch` wrapper that captures with (a) in the `catch` before rethrowing: record whether the PNG is non-empty and not all-transparent.

**Questions the findings must answer:** the largest line length that arrives intact per platform (compare lengths in the capture), PNG size/time per method per platform, which method works under `flutter test`, and whether the failing screen is still rendered in the `catch`.

- [ ] **Step 1:** Create the branch and files; push; `gh run watch` until all three jobs finish.
- [ ] **Step 2:** Download artifacts; for each N, check `jq` that the print event's message length equals the printed length; record PNG sizes/times and method results.
- [ ] **Step 3:** Write the findings file with the answers, the exact evidence lines, and the consequence for the chunk size and the screenshot method; commit it with the captures on a `docs/flutter-package-findings` branch and open a PR (qfroot).
- [ ] **Step 4:** Delete the spike branch (remote and local).

---

### Task 1: Create the repo and package skeleton

**Files (new repo `Qualflare/qualflare-flutter`):**
- Create: `pubspec.yaml` — `name: qualflare_flutter`, `description` (one sentence: labels, links, steps and screenshots for Flutter tests in Qualflare), `version: 0.1.0-dev.1`, `homepage: https://qualflare.com/flutter-test-reporting/`, `repository: https://github.com/Qualflare/qualflare-flutter`, `environment: sdk: ">=3.5.0 <4.0.0", flutter: ">=3.24.0"`, `dependencies: flutter (sdk), flutter_test (sdk), test_api: any` (flutter_test pins its version), `dev_dependencies: flutter_lints`
- Create: `lib/qualflare_flutter.dart` (library export file), `analysis_options.yaml` (`include: package:flutter_lints/flutter.yaml`), `LICENSE` (Apache-2.0, "Copyright 2026 Qualflare"), `README.md` (what it does, the two commands from the spec, "needs qf ≥ 0.3.0 — coming with 0.1.0", status: pre-release), `CHANGELOG.md` (`## 0.1.0-dev.1 — unreleased`), `.gitignore` (Flutter package defaults)
- Create: `.github/workflows/ci.yml` — on push to main and PRs: `flutter-action` (pinned SHA, `channel: stable`), `flutter pub get`, `dart format --output=none --set-exit-if-changed .`, `flutter analyze --fatal-infos`, `flutter test`

- [ ] **Step 1: Create the repo** — `gh repo create Qualflare/qualflare-flutter --public --description "Qualflare test reporting for Flutter: labels, links, steps and screenshots for widget and integration tests" --homepage https://qualflare.com/flutter-test-reporting/` (as qfroot), then clone it via `git@github-qualflare:Qualflare/qualflare-flutter.git` and set the local identity to Qualflare Root.
- [ ] **Step 2: Add the skeleton** above with one placeholder test (`test/smoke_test.dart`: the library imports), run `flutter pub get && flutter analyze --fatal-infos && flutter test` → pass. (Flutter is not installed locally: install it with `brew install --cask flutter` or `fvm`, or run these only in CI and say so in the report.)
- [ ] **Step 3: Commit** `chore: package skeleton`; push to `main` (new empty repo); CI → green.
- [ ] **Step 4: Topics** — `gh repo edit Qualflare/qualflare-flutter --add-topic flutter,dart,testing,test-reporting,integration-test,widget-test,qualflare,flutter-test,test-management,reporter`.

---

### Task 2: The marker writer

**Files:**
- Create: `lib/src/marker.dart`
- Test: `test/marker_test.dart`

**Interfaces:**
- Produces: `const markerPrefix = '##qualflare[v1] ';` · `String encodeMarker(Map<String, Object?> fields)` (drops null-valued keys, compact JSON, prefix) · `List<String> attachmentMarkers({required String id, required String name, required String type, required List<int> bytes, int chunkSize = 48 * 1024})` (base64 of all bytes, split into ≤ `chunkSize`-char chunks, one `att` line each with `n` and `i`).

- [ ] **Step 1: Failing tests** — `encodes a label exactly` (`encodeMarker({'k':'label','name':'owner','value':'mobile'}) == '##qualflare[v1] {"k":"label","name":"owner","value":"mobile"}'`); `drops null fields` (`{'k':'link','type':'custom','url':'u','name':null}` has no `"name"`); `encodes newlines and unicode on one line` (a value `'a\nb ✓'` → result contains no `\n` character and decodes back to `'a\nb ✓'`); `chunks an attachment` (100 KiB of bytes with `chunkSize: 48*1024` → 3 lines, `n == 3`, `i` 0..2, every `data` ≤ 49152 chars, concatenated `data` base64-decodes to the input); `empty attachment is one chunk` (`n == 1`, `data == ''`).
- [ ] **Step 2: Run** `flutter test test/marker_test.dart` → FAIL (undefined).
- [ ] **Step 3: Implement** the two functions.
- [ ] **Step 4: Run** → PASS; `flutter analyze --fatal-infos` clean.
- [ ] **Step 5: Commit** `feat: marker encoding`.

---

### Task 3: The `qualflare` API (metadata, steps, attachments)

**Files:**
- Create: `lib/src/qualflare.dart` (the `Qualflare` class and the top-level `final qualflare = Qualflare._()`), export it from `lib/qualflare_flutter.dart`
- Test: `test/qualflare_test.dart` (unit: capture output with `runZoned(..., zoneSpecification: ZoneSpecification(print: …))` inside a real `test()`), `test/qualflare_widget_test.dart` (the same calls inside `testWidgets`)

**Interfaces:**
- Consumes: Task 2's `encodeMarker`, `attachmentMarkers`, `markerPrefix`.
- Produces (public API, exact signatures):
  - `void label(String name, String value)`
  - `void link(String url, {String type = 'custom', String? name})`
  - `void tag(String tag)` and `void tags(List<String> tags)` — Dart has no varargs (JS's `tag(...tags)`); both print one `{"k":"tag","tags":[…]}` line
  - `void priority(String value)`
  - `Future<T> step<T>(String name, FutureOr<T> Function() body)`
  - `void attachment(String name, List<int> bytes, {String mimeType = 'application/octet-stream'})`
  - Phase 2 adds `Future<void> screenshot(WidgetTester tester, String name)` and `qualflareTestWidgets(...)`.

Rules (from the spec): inside a test = `TestHandle.current` (from `package:test_api/hooks.dart`) does not throw; outside, every call is a no-op. Invalid `type`/priority → a `warn` line naming the bad value, nothing else. Step ids: a process-wide increasing `int` counter (unique within any test). The current step is a zone value set by `step` around its body (`runZoned(..., zoneValues: {#qualflareStep: id})`), so nesting survives `await`. `t` is `DateTime.now().millisecondsSinceEpoch`. Step status: body returns → `passed`; throws `TestFailure` → `failed`; throws anything else → `error`; `error` field = `'$e'` truncated to 8192 chars; the original object is rethrown with its stack (`Error.throwWithStackTrace`). Attachment ids: `'a<counter>'`. Per-test byte total for the 20 MiB cap: a `Map<String, int>` keyed by `TestHandle.current.name` (the full test name; a retried test shares its total across attempts, which only makes the cap stricter).

- [ ] **Step 1: Failing tests** — `label prints one marker`; `trims and ignores empty`; `link defaults to custom`; `rejects a bad link type with a warn`; `rejects a bad priority with a warn`; `tag and tags print one marker each`; `step prints start and end with passed`; `step nests across awaits` (outer step → `await Future.delayed(Duration.zero)` → inner step: inner `step+` has `parent` = outer id); `step ids are unique and increasing` (100 sequential steps → 100 distinct increasing ids); `failed expect ends the step failed and rethrows TestFailure`; `step rethrows any thrown object unchanged` (throw `'boom'` → caught `'boom'` identical, step ended `error`); `attachment chunks through attachmentMarkers`; `caps are inclusive` (exactly 5 MiB → data lines; 5 MiB + 1 → one `warn`; four 5 MiB attachments then one more byte → the last is a `warn`); `outside a test nothing is printed` (call every method from `setUpAll`'s *outer* zone or a top-level `main` statement before `test()` — no marker lines in the captured output); and in `qualflare_widget_test.dart`, `works inside testWidgets` (label + step inside `testWidgets` print the same markers).
- [ ] **Step 2: Run** `flutter test` → FAIL.
- [ ] **Step 3: Implement** `lib/src/qualflare.dart`.
- [ ] **Step 4: Run** `flutter test && flutter analyze --fatal-infos && dart format --set-exit-if-changed .` → PASS.
- [ ] **Step 5: End-to-end check in CI** — add to `ci.yml`: `flutter test --file-reporter json:results.json test/qualflare_widget_test.dart || true`, then `jq` asserts that a `print` event with the label's marker exists and its `testID` belongs to the test named `works inside testWidgets`. Push; CI green.
- [ ] **Step 6: Commit** `feat: qualflare.label/link/tag/priority/step/attachment`; push; README gains an "API" section listing these six.

---

## Phase 2 (separate plan, written from Task 0's findings)

`screenshot` and `qualflareTestWidgets` (method and chunk size from the findings); the `qf` parser addition (markers → labels, links, tags, priority, steps, attachments; per attempt) released as CLI 0.3.0 with golden fixtures captured from this package; the device CI (Android emulator + iOS simulator) and the dogfood upload to the `qualflare-flutter` project; pub.dev publisher, first publish and automated publishing; content and the Claude Code plugin.
