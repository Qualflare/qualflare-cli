# Flutter — design

**Status:** design approved in conversation; implementation waits on the Task 0 spike
**Date:** 2026-10-04
**Scope:** `qualflare-cli` (the work), plus the server category, app-ui mark, Claude Code plugin and content that every new framework needs

## Why

Espresso, XCTest, Maestro, Detox, WebdriverIO and Appium are shipped. Flutter is the main mobile
framework left, and today the only way a Flutter suite reaches Qualflare is by building the app and
driving it through Maestro. That loses the Flutter test suite itself: the widget tests and the
`integration_test` tests teams actually write. Several blog posts, the docs and llms files say so.

**Outcome:** a Flutter team uploads its test results to Qualflare directly. Flutter appears as its
own framework in the app, the CLI, the Claude Code plugin and the content. Failures arrive with their
errors, stack traces and output, and retries arrive as per-attempt history if Flutter's output carries
it.

## Scope

**In:** Flutter **widget tests** and **`integration_test`** tests, on device or emulator.

**Not filtered:** plain unit tests that share a `flutter test` run with widget tests are uploaded like
everything else. Widget and integration tests are what we design, test and market for, but Flutter's
output does not say which test is a "unit" and which a "widget" test, and nothing is silently dropped.

**Deferred, on purpose** (see the end): a Dart package for labels, steps and screenshots; golden-test
failure images and screenshots via `--artifacts-dir`; Flutter web.

## The finding that decides the architecture

Dart's test runner (`package:test`, which `flutter test` drives) has **no pluggable reporter API**.
The built-in reporters are fixed (`compact`, `expanded`, `json`, `github`, `failures-only`), so
there is no way to install a Qualflare reporter into a Flutter run the way `@qualflare/jest` or
`@qualflare/webdriverio` install into theirs.

What it does have is a stable, documented, machine-readable output: the
[JSON reporter protocol](https://github.com/dart-lang/test/blob/master/pkgs/test/doc/json_reporter.md).
`flutter test --machine` streams it to stdout, and `--file-reporter json:<path>` writes it to a file
while keeping normal console output. It carries every suite, group, test, result, error with stack
trace, and `print` call.

So Flutter support is a **parser in `qf collect`**, like the existing `go test -json` parser, which
also folds an event stream into cases. There is no new repository and no package to install:

```bash
flutter test --file-reporter json:flutter-results.json
qf <project> collect flutter-results.json
```

A Dart package (approach B) was considered and deferred: without a reporter API it would have to wrap
or hook users' tests, it needs its own pub.dev publishing pipeline, and it would still depend on this
parser for the basic results.

## Mapping: events to cases

| Event | Becomes |
|---|---|
| `suite` (`path: test/login_test.dart`) | the case's test file, relative, as `ClassName` and a `file` property |
| `group` chain + `testStart.test.name` | the case name. Dart already joins group names (`"login screen shows an error"`) |
| case id | `<file>#<full name>`, stable across runs |
| `testDone.result` | `success` → passed; `failure` (a failed `expect`) → failed; `error` (any other exception) → error |
| `testDone.skipped: true` | skipped, with the skip reason from the test's metadata or the `print` event of type `skip` |
| `error` events for a test | message → `error`, stack → `trace`. A test can emit several; all are kept, in order |
| `print` events for a test | that test's stdout, capped to the server's limits |
| `testDone.time − testStart.time` | duration (the protocol's `time` is milliseconds since the run started) |
| `suite.platform` | a suite property (`vm` for host-side runs; on-device value: Task 0) |

One parsed file is one `domain.Suite`, as today for every parser; the cases record their test file,
the same way one JUnit XML with many classes does.

## Edge cases, each of which would otherwise lose data

- **A file that fails to compile must not vanish.** Dart reports loading each file as a *hidden*
  pseudo-test (`"loading test/foo_test.dart"`). Hidden tests are dropped, **except** a hidden test
  that fails: it becomes an `error` case named after the file. Otherwise a broken test file reads as
  "fewer tests" instead of "red".
- **`setUpAll` / `tearDownAll` failures** surface the same way, as an `error` case for that group.
- **A truncated stream** (a crash or CI timeout): a test with `testStart` and no `testDone` becomes
  `error`, "did not finish", the rule `@qualflare/webdriverio` already uses.
- **Non-JSON lines are skipped, not fatal.** `flutter test` can interleave tool output (downloads, an
  on-device build) with the stream.
- **Two runs appended into one file** are handled by resetting state at each `start` event, because
  test IDs restart from 0.
- **Detection:** the first record is `{"type":"start","protocolVersion":…,"runnerVersion":…}`, which
  identifies the format by content through the existing NDJSON detector. A filename rule (`flutter`)
  is only the fallback, as for every format.

## Open questions Task 0 must answer before the parser is written

1. **Retries.** With `retry: n`, does the stream report each failed attempt (errors before a final
   `testDone`, or a `testStart` per attempt) or only the final result? The first gives per-attempt
   history and flaky detection; the second is documented as a limitation.
2. **On-device `integration_test`.** Does `flutter test integration_test/ -d <device> --machine` emit
   the same protocol, and does it name the device or platform anywhere in the stream? How much
   non-JSON tool output is mixed in?
3. **Hidden and lifecycle tests.** The exact names and `hidden` flags of loading, `setUpAll` and
   `tearDownAll` pseudo-tests, passing and failing.
4. **`--machine` vs `--file-reporter json:`.** Whether the two produce identical event streams, so the
   docs can recommend `--file-reporter` (console output stays readable) without a second code path.

The spike runs in CI (the Flutter SDK is not installed locally): widget tests covering pass, fail,
error, skip, `print`, `retry:`, a compile error and a failing `setUpAll`; plus an `integration_test`
run on an Android emulator and an iOS simulator on a macOS runner. The captured output becomes the
parser's golden fixtures.

## Platform

Qualflare's launch `platform` comes from `--platform` (or `QF_PLATFORM`); a parser cannot set it.
Widget tests run on the host, so the default is fine. For `integration_test` the docs show
`--platform android` / `--platform ios`. If Task 0 finds the device in the stream, it is recorded as a
suite property as well.

## Sequencing

```
0 spike ─┬─ 1 api-service (flutter test_type) ─▶ prod ─┬─ 2 app-ui (union + mark) ─▶ prod
         │                                             └─ 3 CLI parser ─▶ release
         └─────────────────────────────────────────────────────────────┴─▶ 4 plugin, 5 content
```

- **1 api-service:** migration adding `flutter` to `test_type`, and the five other sync points
  (sqlc constants, domain const + `All`, the `Suite.Category` oneof, CTRF `toolCategories` if a
  Flutter CTRF tool name exists, and `MIGRATE_TARGET_VERSION`).
- **2 app-ui:** the `flutter` test type and the Flutter mark from Simple Icons (CC0).
- **3 CLI:** the `flutter` parser, `FrameworkFlutter` in `AllFrameworks`, the server-enum copy in
  `models_test.go`, the filename rule, `list-formats` group and help text, README.
  **Hard rule: never add `flutter` to `AllFrameworks` before the server accepts the category**, since
  the server rejects the whole launch for an unknown category.
- **4 Claude Code plugin:** `/qf-init` detects Flutter (`pubspec.yaml` with `flutter_test` or
  `integration_test`); `/qf-run` runs `flutter test --file-reporter json:…` for the widget suite, and
  `integration_test` when a device is available.
- **5 Content:** a `/flutter-test-reporting/` landing page; framework counts 26 → 27; the "Flutter
  only through Maestro" claims in the blog, qf-docs and llms files.

## Verification

- **Golden fixtures from real runs**, never hand-written JSON: the Task 0 captures are checked in and
  every parser test runs against them.
- **Unit tests** for every mapping row and every edge case above.
- **A real Flutter job in CI**: a small Flutter fixture project in the CLI repo runs `flutter test`
  on the Flutter SDK, and the parser's output is asserted. A Flutter release that changes the
  protocol then fails our CI instead of mis-parsing users' runs.
- **End to end:** a `qf collect --dry-run` over real output shows framework `flutter` with the right
  counts; after the server is in prod and the CLI released, a real upload lands in a Qualflare project
  with a public report page (a dogfood badge, like the other frameworks).
- **Server safety:** `TestEveryFrameworkCategoryIsAcceptedByTheServer` gains `flutter` only together
  with the server's enum.

## Deferred

- **A Dart package** (`qualflare_flutter` on pub.dev) for labels, steps and `integration_test`
  screenshots. Built only if users ask for metadata the stream cannot carry.
- **Screenshots and golden-test failure images** attached via `qf collect --artifacts-dir`, as Detox
  artifacts are, rather than through a package.
- **Flutter web** (`flutter test --platform chrome`): the same stream, but outside the widget and
  integration scope.
