# qualflare_flutter package — design

**Status:** design approved in conversation; implementation waits on the Task 0 spike
**Date:** 2026-10-04
**Scope:** a new public repo `Qualflare/qualflare-flutter` (Dart package `qualflare_flutter` on pub.dev), a `qf collect` parser addition in `qualflare-cli`, the `qualflare-flutter` dogfood project, and the content that follows
**Builds on:** `2026-10-04-flutter-design.md` and `2026-10-04-flutter-findings.md` (the `flutter` parser, CLI v0.2.0), whose "Deferred: a Dart package" this designs

## Why

`qf collect` already uploads Flutter results (statuses, real widget-failure messages, retries). What the
results file cannot carry is what only the test author knows: **labels and links** (owner, severity,
feature, the issue a test covers), **named steps**, and **screenshots**, above all from on-device
`integration_test` runs. Flutter users also cannot find Qualflare where they look for test tooling: pub.dev.

**Outcome:** a Flutter team adds `qualflare_flutter` to `dev_dependencies`, calls `qualflare.*` in its
tests, runs the same two commands as today, and sees labels, links, steps and screenshots on each case —
for widget tests and for tests on a real device or emulator. The package is listed on pub.dev and its repo
dogfoods itself into the `qualflare-flutter` project with a public badge.

## The constraint that decides the architecture

The other Qualflare plugins run inside their test runner on the CI machine and write `qualflare-json`
files that `qf collect` uploads (plugin → qf → Qualflare). Flutter cannot do that literally:

1. Dart's test runner has **no pluggable reporter API**, so no package can see every test's result.
2. `integration_test` code runs **on the phone or emulator**, which cannot write files on the CI machine.

What *does* reach the CI machine from both host and device is the test's `print` output: Dart's JSON
reporter records each `print` as an event carrying the ID of the test that printed it (measured in the
Phase 1 spike, on Android and iOS too). So the chain is still **plugin → qf → Qualflare**, with the
plugin's data travelling inside the results file `qf` already reads:

```bash
flutter test --file-reporter json:flutter-results.json     # unchanged
qf <project> collect flutter-results.json                  # unchanged; needs qf >= 0.3.0 for plugin data
```

A plugin-written `qualflare-json` sidecar was considered and rejected: it works for host-side widget
tests only, so on-device screenshots — a required feature — would be impossible. `flutter drive` with a
host driver was rejected: it does not produce the JSON stream the parser reads and does not cover widget
tests.

## The package API

```dart
import 'package:qualflare_flutter/qualflare_flutter.dart';

qualflare.label('owner', 'mobile-team');              // any name/value; epic, feature, story …
qualflare.link('https://tracker/QF-1', type: 'issue', name: 'QF-1');  // type: issue | tms | custom
qualflare.tag('smoke', 'checkout');                    // case tags
qualflare.priority('high');                            // low | medium | high | critical
await qualflare.step('log in', () async { … });       // named, nestable, status + duration
await qualflare.screenshot(tester, 'checkout');        // PNG attached to the current test
qualflare.attachment('response.json', bytes, mimeType: 'application/json');

qualflareTestWidgets('pays with a card', (tester) async { … });  // testWidgets + screenshot on failure
```

- The names and arguments match the `qualflare.*` runtime API of the JS reporters
  (`@qualflare/playwright`'s `qualflare-api.ts`: `label`, `link(url, {type, name})`, `tag`, `priority`,
  `step`, `attachment`), so the API reads the same in every Qualflare plugin. `screenshot` is the
  Flutter-specific addition. The JS API's `description` and `parameter` are left out: `domain.Case` has no
  field for either today.
- Link `type` is one of `issue`, `tms`, `custom` (default `custom`; the server rejects anything else).
  Priority is one of `low`, `medium`, `high`, `critical`. Names and values are trimmed; empty ones are
  ignored.
- `step` returns its body's value. If the body throws, the step ends `failed` (a `TestFailure`) or
  `error` (anything else) and the exception is rethrown unchanged, so the test fails as it would without
  the step.
- `qualflareTestWidgets` has `testWidgets`'s signature. When its body throws, it captures a screenshot named
  `failure` before rethrowing. Tests using plain `testWidgets` use `qualflare.screenshot` explicitly.
- Calling the API outside a running test does nothing.
- Screenshots: one method for host and device, rendering Flutter's root layer to PNG; on a device, the
  `integration_test` binding's native capture is used instead when available, because it includes
  platform views (maps, web views). Task 0 confirms which works under `flutter test`, and whether the
  screen is still rendered when `qualflareTestWidgets` catches a failure.

## The marker protocol (the contract between package and parser)

Each API call prints **one line**:

```
##qualflare[v1] <compact JSON object>
```

The prefix is exactly `##qualflare[v1] ` (with the trailing space). The JSON object always has `"k"` (kind):

| `k` | Fields | Meaning |
|---|---|---|
| `label` | `name`, `value` | one label |
| `link` | `type` (`issue`/`tms`/`custom`), `url`, `name`? | one link |
| `tag` | `tags` (array of strings) | case tags |
| `priority` | `value` (`low`/`medium`/`high`/`critical`) | case priority; the last one wins |
| `step+` | `id` (int, unique within the test), `parent`? (int), `name`, `t` (ms since epoch) | a step started |
| `step-` | `id`, `status` (`passed`/`failed`/`error`), `t`, `error`? | that step ended |
| `att` | `id` (string, unique within the test), `name`, `type` (MIME), `n` (chunk count), `i` (0-based chunk index), `data` (base64 of this chunk) | one chunk of an attachment |
| `warn` | `msg` | the package dropped something (over a cap, outside a test); surfaced as test output |

- Chunks carry at most **48 KiB of base64** each (Task 0 may lower this if device output truncates long
  lines); an attachment is complete when chunks `0..n-1` are all present.
- Caps, enforced by the package and again by the parser: **5 MiB per attachment, 20 MiB per test**
  (decoded bytes). Over a cap, the package emits `warn` instead of the data.
- `[v1]` versions the protocol. A parser that sees another version skips the line and adds one warning to
  the case's output.

## The `qf` parser addition (qualflare-cli, released as 0.3.0)

The `flutter` parser reads marker lines from each test's `print` events:

- **Labels, links, tags** → `Case.Labels`, `Case.Links`, `Case.Tags`, deduplicated by (name, value) /
  (type, url) / tag across attempts; **priority** → `Case.Priority`, the last value printed.
- **Steps** → `Case.Steps` in start order, `ParentIndex` from `parent`, duration from the two `t`s,
  status from `step-`. A step with `step+` and no `step-` (the test ended or crashed inside it) is `error`
  with "step did not finish".
- **Attachments** → `Attachment{Name, MimeType, Content: base64}` on the case, or on the attempt that
  produced it when the test was retried; `qf` already uploads in-memory attachments through the
  presigned-URL flow (`client.UploadAttachment`). An attachment with missing chunks is dropped with a
  warning in the case's output.
- **Retries:** markers belong to the attempt in which they were printed (the parser's existing `Retry:`
  boundaries). Steps and attachments stay per attempt; the case shows the final attempt's.
- Marker lines are removed from `system-out` and attempt output; malformed JSON or an unknown `k` is
  skipped with one warning.
- Results without markers parse exactly as in v0.2.0.

## Order

```
0 spike ──┬─ 1 qf parser (markers) ─▶ CLI 0.3.0 ──┐
          └─ 2 qualflare_flutter package ──────────┴─▶ 3 dogfood CI + pub.dev 0.1.0 ─▶ 4 content + Claude Code plugin
```

**Task 0 spike** (throwaway, CI on Ubuntu + Android emulator + iOS simulator), answering:
1. Do long `print` lines from a device (≥ 48 KiB) reach the results file intact on Android and iOS? If
   not, the largest safe chunk size.
2. Size and time for a full-screen PNG through chunked prints, on each platform.
3. Which screenshot method works under `flutter test` (root-layer render; `integration_test` native
   capture), and whether the failing screen is still rendered when a wrapper catches the failure.

Its raw output becomes the golden fixtures for Task 1, as in Phase 1.

## Verification

- **Package:** Dart unit tests for every marker kind (exact lines), chunking, caps, nesting, outside-a-test
  no-ops; widget tests for the API and `qualflareTestWidgets`.
- **On devices:** the repo's `integration_test` suite runs in CI on an Android emulator and an iOS
  simulator; `qf collect --dry-run` on its results must show the expected labels, links, steps and a
  decodable PNG on the right cases.
- **Parser:** golden tests on real captures of the package's output (never hand-written marker lines,
  except for malformed-input cases, each named synthetic).
- **Dogfood:** the repo's CI uploads its own widget and integration results to the `qualflare-flutter`
  project, which backs a public badge in the README.

## Release and accounts (the user's steps)

- **pub.dev publisher:** a verified `qualflare.com` publisher, created by the user in pub.dev with a Google
  account that can verify the domain. The package is published from GitHub Actions with pub.dev's
  automated publishing (OIDC from a version tag; no stored secret), enabled by the user in the package's
  pub.dev admin page after the first manual publish.
- **Dogfood project:** the user creates the `qualflare-flutter` project in the app and stores its token as
  the repo secret `QUALFLARE_TOKEN`.
- Repo commits as `Qualflare Root <root@qualflare.com>` through the `github-qualflare` remote; no
  Claude/Anthropic attribution.

## Deferred

- Automatic screenshots for failures in plain `testWidgets` (needs a failure hook Dart does not offer).
- Video recording of device runs.
- Flutter web (`--platform chrome`).
