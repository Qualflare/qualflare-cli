# qualflare_flutter — spike findings

**Date:** 2026-10-04
**Spec:** `2026-10-04-qualflare-flutter-package-design.md` (this directory)
**Captured with:** Flutter 3.47.6 (stable), `flutter test --file-reporter json:` on GitHub Actions:
widget tests on `ubuntu-latest`, `integration_test` on an Android API 34 x86_64 emulator (320×640
screen) and on an iPhone simulator on `macos-15` (750×1334). Raw results files are in
`flutter-package-captures/` (gzipped, unedited), with the screenshots reassembled from them.

**Bottom line:** the spec's transport works as designed on all three platforms. No device truncates the
markers, the 48 KiB chunk size stands, both screenshot methods work under `flutter test`, and a failing
screen is still capturable from a `catch`. One new finding changes Phase 2: **widget-test screenshots
render text as black boxes** unless real fonts are loaded.

## Q1. Long print lines from a device: intact up to 1 MiB everywhere

Lines of `##qualflare[v1] {"k":"spike","n":N,"data":"<N chars>"}` for N = 4 KiB … 1 MiB arrived as single
`print` events of exactly the printed length on every platform:

| N | widget (host) | Android emulator | iOS simulator |
|---|---|---|---|
| 4 KiB | 4144 | 4144 | 4144 |
| 16 KiB | 16433 | 16433 | 16433 |
| 48 KiB | 49201 | 49201 | 49201 |
| 64 KiB | 65585 | 65585 | 65585 |
| 256 KiB | 262194 | 262194 | 262194 |
| 1 MiB | 1048627 | 1048627 | 1048627 |

(Each length is N plus the 51 characters of the marker around it.) `flutter test` forwards device
output through the VM service, not logcat, so Android's ~4 KB log-line limit does not apply.
**Consequence:** the spec's 48 KiB chunk size stands. Larger chunks would also work; 48 KiB keeps one
line well under any scanner limit (`qf`'s is 16 MiB).

## Q2. PNG size and time

| Method | Platform | Size | Capture | Print (chunks) |
|---|---|---|---|---|
| root layer (`captureImage(rootElement)`) | widget, 2400×1800 (800×600 @3x) | 22.8 KB | 102 ms | 3 ms (1) |
| root layer | Android, 320×640 | 5.6 KB | 358 ms first, 17 ms after | 4 ms (1) |
| root layer | iOS, 750×1334 | 15.2 KB | 117 ms | 2 ms (1) |
| native (`takeScreenshot`) | Android | 5.6 KB | 118 ms | 2 ms (1) |
| native | iOS | 65.2 KB | 60 ms | 3 ms (2) |

Simple screens compress very well; a real app screen at phone resolution will be larger (hundreds of
KB), still far under the 5 MiB cap and a few dozen chunks.

## Q3. Which method works, and is the failing screen still there?

- **Root layer** — `flutter_test`'s public `captureImage(tester.binding.rootElement!)` inside
  `tester.runAsync` — works on host, Android and iOS, with the same code.
- **Native** — `IntegrationTestWidgetsFlutterBinding.takeScreenshot`, after
  `convertFlutterSurfaceToImage()` on Android — works on both devices under `flutter test` (not only
  `flutter drive`). It includes the system status bar on Android (and platform views, which the root
  layer cannot see). It only exists on a device.
- **After a failure:** a test that pumps a red screen and fails an `expect` inside `try { … } catch`
  captured the **red failing screen** with the root-layer method on all three platforms
  (`widget-failure.png`, `android-failure.png`, `ios-failure.png`).

**Consequence for Phase 2:** `qualflare.screenshot(tester, name)` uses the native capture when the
binding is an `IntegrationTestWidgetsFlutterBinding` (on a device), and the root layer otherwise;
`qualflareTestWidgets` captures in its `catch` before rethrowing.

## New: widget-test screenshots show text as boxes

In host widget tests, `flutter_test` renders all text with the "Ahem" test font, so every glyph is a
black box (`widget-failure.png`). The layout and colours are right; the words are not readable. Device
screenshots render real fonts.

**Consequence for Phase 2:** decide between documenting it, or loading the app's real fonts (and
Material/Cupertino fonts) before capture as golden-test tooling does. Loading fonts changes text
metrics for the rest of the test, so it must be opt-in, not a side effect of `screenshot`.

## Also seen

- The marker lines, including base64 chunks, also appear in the normal console output of
  `flutter test`. Expected from the transport; the README should say so for large attachments.
- Each spike run's results file was ~1.5 MB because of the 1 MiB test line; real runs carry only what
  the tests attach.
