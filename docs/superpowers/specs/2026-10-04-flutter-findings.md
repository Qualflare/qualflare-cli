# Flutter — spike findings

**Date:** 2026-10-04
**Spec:** `2026-10-04-flutter-design.md` (this directory)
**Captured with:** Flutter 3.47.6 (stable, 2026-09-30), JSON reporter `protocolVersion` 0.1.1, on GitHub
Actions: widget tests on `ubuntu-latest`, `integration_test` on an Android API 34 emulator and on an
iPhone simulator (`macos-15`). The raw captures are in `flutter-captures/`, unedited.

**Bottom line:** the architecture holds: everything the parser needs is in the stream. But three of
the spec's mapping assumptions are wrong for `testWidgets`, which is the scope we chose, and they
change the parser's rules. They are marked **Spec correction** below. Phase 2's plan argues from this
file, not from the spec's mapping table where the two differ.

## Q1. Retries: per-attempt history is available

A `retry: 2` test that passes on its third attempt has **one** `testStart` and **one** final
`testDone` (`result: success`). Between them, each failed attempt emits:

- an `error` event, `isFailure: true`, with that attempt's message and stack
  (`Expected: <3>  Actual: <1>`, then `Actual: <2>`)
- a `print` event `Retry: <full test name>`

```json
{"testID":17,"isFailure":true,"error":"Expected: <3>\n  Actual: <1>\n","type":"error"}
{"testID":17,"messageType":"print","message":"Retry: login screen passes on the third attempt","type":"print"}
{"testID":17,"isFailure":true,"error":"Expected: <3>\n  Actual: <2>\n","type":"error"}
{"testID":17,"messageType":"print","message":"Retry: login screen passes on the third attempt","type":"print"}
{"testID":17,"result":"success","skipped":false,"hidden":false,"type":"testDone"}
```

**For the parser:** each `Retry: <name>` print closes one failed attempt (with the errors since the
previous boundary), and `testDone` is the final attempt. That gives per-attempt history and flaky
detection, as in our other parsers. The `Retry:` prints are a boundary marker, not test output, and
are not copied into stdout.

## Q2. On-device `integration_test`: the same protocol, but no device

- **Same protocol**, same event types, on Android and iOS.
- **The stream never names the device.** `suite.platform` is `"vm"` on the Android emulator too, the
  same value as a host-side widget run. Nothing else in the stream identifies the device or OS.
  **Consequence:** platform comes only from `qf collect --platform android|ios`, as the spec's fallback
  said. The `suite.platform` property is still recorded, but it does not mean "host".
- **Non-JSON lines are real** in `--machine` stdout: blank lines, the build line
  (`✓ Built build/app/outputs/flutter-apk/app-debug.apk`), and a JSON *array* per test process,
  `[{"event":"test.startedProcess","params":{"vmServiceUri":…}}]`, which is valid JSON but not an
  event. **For the parser:** skip any line that is not a JSON *object* with a `type`. Detection must
  also tolerate these lines before or between events.
- **integration_test runs a hidden `(tearDownAll)`** from `package:integration_test` after the
  user's tests (name `"(tearDownAll)"`, `hidden: true`, `success`). Dropped like other hidden passes.

## Q3. Hidden and lifecycle tests

| Pseudo-test | Name in the stream | `hidden` | When it fails |
|---|---|---|---|
| loading a file | `loading <absolute path>` | `true` when it passes | **`hidden: false`**, `result: error`, error `Failed to load "<path>": Compilation failed …`, no stack |
| `setUpAll` | `<group> (setUpAll)` | `true` when it passes | **`hidden: false`**, `result: error`, with message and stack |
| `tearDownAll` | `<group> (tearDownAll)` | `true` | (not exercised failing; same mechanism) |

So the spec's rule ("hidden tests are dropped, except a failing one, which becomes an error case")
already holds without any special-casing: **Dart un-hides them itself when they fail.** The parser
drops `hidden: true` and keeps everything else. Name the compile-error case after the file (made
relative), since its raw name carries an absolute path.

Tests in a group whose `setUpAll` failed are **not reported at all** (the `never runs` test has no
events). The `(setUpAll)` error case is the only trace, so it must not be dropped.

## Q4. `--machine` vs `--file-reporter json:`: identical events

Ignoring `time` and `pid`, the two produce the same JSON objects, event for event. The difference is
the noise: the `--file-reporter` file has **no** non-JSON lines; `--machine` stdout has the blank lines,
build output and `startedProcess` arrays above. **Docs recommend
`flutter test --file-reporter json:<file>`**, which also keeps the normal console output readable.
The parser handles both.

## Spec corrections (from the captures, not from the docs)

1. **`testWidgets` reports a failed `expect` as `result: "error"`, not `"failure"`.** The test's
   `error` event is a generic `Test failed. See exception logs above.` with `isFailure: false` and no
   stack. The real cause is in a `print` event the test emits first:

   ```
   ══╡ EXCEPTION CAUGHT BY FLUTTER TEST FRAMEWORK ╞═════
   The following TestFailure was thrown running a test:
   Expected: exactly one matching candidate
     Actual: _TextWidgetFinder:<Found 0 widgets with text "Sign out": []>
   …
   When the exception was thrown, this was the stack:
   #4 main.<anonymous closure>.<anonymous closure> (file:///…/widget_cases_test.dart:15:7)
   ```

   A thrown exception prints the same block with `The following StateError was thrown`.
   **Parser rule:** for a test whose result is `error` and that printed an
   `EXCEPTION CAUGHT BY FLUTTER TEST FRAMEWORK` block, the status is **failed** when the block says
   `The following TestFailure was thrown`, otherwise **error**; the case's message is the block's text
   between that line and `When the exception was thrown`, and its trace is the stack that follows.
   The block is not copied into stdout. Without this rule every widget-test assertion failure would
   show up as an error with the message "See exception logs above". Plain `test()` cases keep the
   spec's mapping (`isFailure` on the error event).
2. **`testWidgets` locations point into `flutter_test`.** `test.url`/`line` is
   `package:flutter_test/src/widget_tester.dart:174` for every widget test. The user's file and line
   are in `root_url`/`root_line`. **Parser rule:** use `root_url`/`root_line` when present, otherwise
   `url`/`line`.
3. **Paths are absolute** (`suite.path`, `loading …` names, `root_url` as `file://…`). Make them
   relative to the directory holding `pubspec.yaml`, found as the longest common prefix ending in a
   directory that contains `test/` or `integration_test/`; fall back to the path from the first
   `test/` or `integration_test/` segment.

## Also seen

- **Skip:** `testDone.skipped: true` with `result: success`; the reason is in
  `testStart.test.metadata.skipReason` (`null` for `skip: true`).
- **`print`** from a test arrives as `messageType: "print"` with that test's `testID`.
- **`done.success`** is `false` when anything failed; the parser does not need it.
- **Two runs in one file** were not captured (the spike ran one invocation per file); the spec's rule
  (reset at each `start`) stands untested until Phase 2's fixtures exercise it.

## iOS

The iOS simulator run emits **the same events as the Android emulator**, event for event (only the
runner's home directory in paths differs), including `suite.platform: "vm"`, the hidden
`(tearDownAll)`, and the `TestFailure` exception block for the failing `expect`.

- **The first iOS attempt hit its 45-minute job limit** in the test step and its output was lost; the
  retry (same app, same simulator selection) finished in about 10 minutes. A first iOS build on a
  cold runner can be very slow, so CI guidance should give on-device iOS runs a generous timeout.
- **The retry ran with `-v`** (to diagnose a second hang, which did not happen), so
  `device-ios.jsonl` holds about 11,400 lines of verbose tool output around the 36 events. It is kept
  as the worst-case noise fixture: a parser that finds exactly the Android capture's events in it skips
  non-event lines correctly.
