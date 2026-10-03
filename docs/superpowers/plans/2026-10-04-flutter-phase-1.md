# Flutter support — Phase 1 Implementation Plan (spike, server, app-ui)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Capture real Flutter test output that settles the spec's four open questions, and make the server and app-ui accept and display the `flutter` framework.

**Architecture:** Flutter support is a `qf collect` parser for Dart's JSON reporter protocol (see the spec). Phase 1 is the work that does not depend on how that protocol behaves in practice: a CI spike that records it, and the server-side `flutter` category plus its app-ui mark. **Phase 2** (the parser, the Claude Code plugin, content) gets its own plan, written from Task 0's findings, because its code depends on them.

**Tech Stack:** GitHub Actions with `subosito/flutter-action` and `reactivecircus/android-emulator-runner`; Go 1.26 + PostgreSQL migrations (api-service); React 19 + TypeScript + Vitest + pnpm (app-ui).

**Spec:** `docs/superpowers/specs/2026-10-04-flutter-design.md` (qualflare-cli)

## Global Constraints

- The framework label and server category is exactly `flutter`.
- Server first: api-service must be **in prod** before app-ui is promoted and before any CLI change that emits `flutter`.
- api-service: never run `sqlc generate`; `db/sqlc/` is hand-maintained.
- A new `test_type` value touches six places: the migration, `db/sqlc/models.go`, `internal/core/domain/test_type/test_type.go` (const + `All`), the `Suite.Category` oneof in `internal/core/domain/launch/launch.go`, `internal/core/domain/ctrf/tools.go` (only if a CTRF tool name maps to it), and `MIGRATE_TARGET_VERSION` in `.gitlab/ci/deploy-prod.yml` (two places).
- Marks come from Simple Icons (CC0) only, per `frameworks.tsx`'s provenance rule.
- Commits on public GitHub repos (qualflare-cli) are authored `Qualflare Root <root@qualflare.com>` and pushed through the `github-qualflare` SSH alias; GitLab repos (api-service, app-ui) use the default identity. No Claude/Anthropic attribution in any commit, PR or MR.
- The spike is throwaway: its branch is deleted after its artifacts are downloaded.

## Review Focus

- **Non-JSON lines in captured output must be kept, not cleaned.** The parser must handle what users' files really contain, so Task 0 saves raw stdout and asserts nothing is stripped.
- **A migration number taken by someone else between planning and merge.** `0296` is next today; Task 1 re-checks the head before writing the file and runs `validate-migrate-target.sh`.
- **An emulator or simulator that never boots.** The device jobs carry timeouts and record the failure instead of hanging, so the spike still delivers the host-side findings.
- **app-ui promoted before api-service prod.** The UI would then show a mark for a value the server rejects. Task 2 starts with an explicit `git branch -r --contains` check.
- **A Flutter fixture app that fails to build for reasons unrelated to the protocol** (SDK and dependency drift). Task 0 pins the Flutter channel to `stable` and records the exact `flutter --version` in its findings.

---

### Task 0: CI spike — capture real `flutter test` output

**Repo:** qualflare-cli, on throwaway branch `spike/flutter-output` (deleted at the end).

**Files:**
- Create: `spike/flutter/tests/widget_cases_test.dart`
- Create: `spike/flutter/tests/setup_all_failure_test.dart`
- Create: `spike/flutter/tests/broken_test.dart`
- Create: `spike/flutter/tests/app_test.dart` (integration_test)
- Create: `.github/workflows/spike-flutter-output.yml`
- Create (on main, at the end): `docs/superpowers/specs/2026-10-04-flutter-findings.md`

**Interfaces:**
- Produces: artifact `flutter-output` containing `widget-machine.jsonl`, `widget-file-reporter.jsonl`, `device-android.jsonl`, `device-ios.jsonl`, `flutter-version.txt`. Phase 2 copies these into `internal/adapters/parsers/unit/flutter/testdata/` as golden fixtures.
- Produces: the findings file answering the spec's four open questions, which Phase 2's plan argues from.

- [ ] **Step 1: Create the spike branch from main**

```bash
cd /Users/ibrahim/Astrais/qualflare-cli
git fetch origin && git worktree add -b spike/flutter-output ../qualflare-cli-spike origin/main
cd ../qualflare-cli-spike
```

- [ ] **Step 2: Write the widget test cases (one per mapping row and edge case)**

`spike/flutter/tests/widget_cases_test.dart`:

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

var flakyAttempts = 0;

void main() {
  group('login screen', () {
    testWidgets('shows a title', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: Text('Sign in')));
      expect(find.text('Sign in'), findsOneWidget);
    });

    testWidgets('fails an expectation', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: Text('Sign in')));
      expect(find.text('Sign out'), findsOneWidget);
    });

    testWidgets('throws an error', (tester) async {
      throw StateError('boom');
    });

    testWidgets('is skipped', (tester) async {}, skip: true);

    testWidgets('prints output', (tester) async {
      // ignore: avoid_print
      print('hello from a widget test');
    });

    test('passes on the third attempt', () {
      flakyAttempts++;
      expect(flakyAttempts, 3);
    }, retry: 2);
  });
}
```

- [ ] **Step 3: Write the lifecycle and compile-error cases**

`spike/flutter/tests/setup_all_failure_test.dart`:

```dart
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('with a broken setUpAll', () {
    setUpAll(() => throw StateError('setUpAll exploded'));
    test('never runs', () {});
  });
}
```

`spike/flutter/tests/broken_test.dart` (does not compile, on purpose):

```dart
import 'package:flutter_test/flutter_test.dart';

void main() {
  test('cannot compile', () {
    final int x = 'not an int';
  });
}
```

- [ ] **Step 4: Write the integration_test case**

`spike/flutter/tests/app_test.dart`:

```dart
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:integration_test/integration_test.dart';

void main() {
  IntegrationTestWidgetsFlutterBinding.ensureInitialized();

  group('counter app', () {
    testWidgets('starts at zero', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: Text('0')));
      expect(find.text('0'), findsOneWidget);
    });

    testWidgets('fails on device', (tester) async {
      await tester.pumpWidget(const MaterialApp(home: Text('0')));
      expect(find.text('1'), findsOneWidget);
    });
  });
}
```

- [ ] **Step 5: Write the spike workflow**

`.github/workflows/spike-flutter-output.yml`:

```yaml
name: Spike — Flutter test output

# THROWAWAY. Captures the raw JSON reporter stream `flutter test` writes, so
# the qf collect parser is designed against real output. Raw stdout is kept
# verbatim, non-JSON lines included: the parser must handle what users' files
# really contain.
on:
  push:
    branches: [spike/flutter-output]

jobs:
  widget:
    runs-on: ubuntu-latest
    timeout-minutes: 20
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: subosito/flutter-action@v2
        with:
          channel: stable
      - name: Create the fixture app
        run: |
          flutter --version > flutter-version.txt
          flutter create --no-pub spike_app
          cd spike_app
          flutter pub add 'dev:integration_test:{"sdk":"flutter"}'
          rm -f test/widget_test.dart
          cp ../spike/flutter/tests/widget_cases_test.dart test/
          cp ../spike/flutter/tests/setup_all_failure_test.dart test/
          cp ../spike/flutter/tests/broken_test.dart test/
      - name: --machine (raw stdout)
        working-directory: spike_app
        run: flutter test --machine > ../widget-machine.jsonl || true
      - name: --file-reporter json
        working-directory: spike_app
        run: flutter test --file-reporter json:../widget-file-reporter.jsonl || true
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        with:
          name: flutter-output-widget
          path: |
            widget-machine.jsonl
            widget-file-reporter.jsonl
            flutter-version.txt

  android:
    runs-on: ubuntu-latest
    timeout-minutes: 40
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - name: Enable KVM
        run: |
          echo 'KERNEL=="kvm", GROUP="kvm", MODE="0666", OPTIONS+="static_node=kvm"' | sudo tee /etc/udev/rules.d/99-kvm4all.rules
          sudo udevadm control --reload-rules && sudo udevadm trigger --name-match=kvm
      - uses: subosito/flutter-action@v2
        with:
          channel: stable
      - name: Create the fixture app
        run: |
          flutter create --no-pub spike_app
          cd spike_app
          flutter pub add 'dev:integration_test:{"sdk":"flutter"}'
          mkdir -p integration_test
          cp ../spike/flutter/tests/app_test.dart integration_test/
      - uses: reactivecircus/android-emulator-runner@v2
        with:
          api-level: 34
          arch: x86_64
          working-directory: spike_app
          script: flutter test integration_test/app_test.dart -d emulator-5554 --machine > ../device-android.jsonl || true
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        with:
          name: flutter-output-android
          path: device-android.jsonl

  ios:
    runs-on: macos-15
    timeout-minutes: 45
    steps:
      - uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1 # v7.0.1
      - uses: subosito/flutter-action@v2
        with:
          channel: stable
      - name: Pick and boot an iPhone simulator
        run: |
          read -r SIM_UDID SIM_VERSION SIM_NAME < <(
            xcrun simctl list devices available -j | jq -r '
              [ .devices | to_entries[]
                | select(.key | test("SimRuntime.iOS-"))
                | (.key | capture("iOS-(?<v>[0-9-]+)$").v | gsub("-"; ".")) as $v
                | .value[] | select(.name | startswith("iPhone"))
                | { udid, name, v: $v } ]
              | sort_by(.v | split(".") | map(tonumber)) | last
              | "\(.udid) \(.v) \(.name)"')
          xcrun simctl boot "$SIM_UDID" || true
          xcrun simctl bootstatus "$SIM_UDID" -b
          echo "SIM_UDID=$SIM_UDID" >> "$GITHUB_ENV"
      - name: Create the fixture app
        run: |
          flutter create --no-pub spike_app
          cd spike_app
          flutter pub add 'dev:integration_test:{"sdk":"flutter"}'
          mkdir -p integration_test
          cp ../spike/flutter/tests/app_test.dart integration_test/
      - name: integration_test on the simulator (raw stdout)
        working-directory: spike_app
        run: flutter test integration_test/app_test.dart -d "$SIM_UDID" --machine > ../device-ios.jsonl || true
      - uses: actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a # v7.0.1
        with:
          name: flutter-output-ios
          path: device-ios.jsonl
```

- [ ] **Step 6: Push and wait for the run**

```bash
git add spike .github/workflows/spike-flutter-output.yml
git -c user.name="Qualflare Root" -c user.email=root@qualflare.com commit -m "spike: capture flutter test JSON output (throwaway)"
git push -u origin spike/flutter-output
gh run watch "$(gh run list --branch spike/flutter-output --limit 1 --json databaseId -q '.[0].databaseId')"
```

Expected: all three jobs complete. A device job that fails to boot is recorded in the findings, not retried indefinitely.

- [ ] **Step 7: Download the artifacts and check they are raw**

```bash
mkdir -p /tmp/flutter-output && cd /tmp/flutter-output
gh run download "$(gh run list --repo Qualflare/qualflare-cli --branch spike/flutter-output --limit 1 --json databaseId -q '.[0].databaseId')" --repo Qualflare/qualflare-cli
head -c 400 flutter-output-widget/widget-machine.jsonl
grep -c -v '^{' flutter-output-*/*.jsonl
```

Expected: the first record has `"type":"start"` with `protocolVersion` and `runnerVersion`. The non-JSON line count is recorded either way, since that is finding Q2.

- [ ] **Step 8: Answer the four open questions from the captures**

```bash
cd /tmp/flutter-output
# Q1 retries: every event for the retried test, in order
ID=$(jq -r 'select(.type=="testStart" and (.test.name|test("third attempt"))) | .test.id' flutter-output-widget/widget-machine.jsonl | head -1)
jq -c --argjson id "$ID" 'select(.testID==$id or .test.id?==$id)' flutter-output-widget/widget-machine.jsonl
# Q2 on-device: suite platform values and non-JSON lines
jq -r 'select(.type=="suite") | .suite.platform' flutter-output-android/device-android.jsonl flutter-output-ios/device-ios.jsonl 2>/dev/null | sort | uniq -c
# Q3 hidden and lifecycle tests: names, hidden flags, results
jq -c 'select((.type=="testStart" and (.test.name|test("loading|setUpAll|tearDownAll"))) or (.type=="testDone" and .hidden==true))' flutter-output-widget/widget-machine.jsonl
# Q4 --machine vs --file-reporter: same events, ignoring timing
diff <(jq -c 'del(.time)' flutter-output-widget/widget-machine.jsonl | grep -v '"type":"debug"') <(jq -c 'del(.time)' flutter-output-widget/widget-file-reporter.jsonl | grep -v '"type":"debug"') && echo "Q4: identical"
```

- [ ] **Step 9: Write the findings file on main**

`docs/superpowers/specs/2026-10-04-flutter-findings.md` records, for each of Q1–Q4: the answer, the exact events that show it (copied from the captures), and the consequence for the parser. It also records `flutter-version.txt` and every job that failed to produce output. Commit it on a `docs/flutter-findings` branch with the four `.jsonl` captures under `docs/superpowers/specs/flutter-captures/`, open a PR as `qfroot`, and keep the captures: Phase 2 moves them into the parser's `testdata/`.

- [ ] **Step 10: Delete the spike branch**

```bash
git push origin --delete spike/flutter-output
cd /Users/ibrahim/Astrais/qualflare-cli && git worktree remove ../qualflare-cli-spike && git branch -D spike/flutter-output
```

---

### Task 1: api-service — the `flutter` test type

**Repo:** api-service (GitLab), branch `feat/test-type-flutter` from `origin/main`.

**Files:**
- Create: `db/migrations/0296_test_type_flutter.up.sql`
- Create: `db/migrations/0296_test_type_flutter.down.sql`
- Modify: `db/sqlc/models.go` (after the `TestTypeAppium` constant, around line 1362)
- Modify: `internal/core/domain/test_type/test_type.go` (const block after `TestTypeAppium`; `All` after `TestTypeAppium`)
- Modify: `internal/core/domain/launch/launch.go:255` (the `Suite.Category` oneof)
- Modify: `.gitlab/ci/deploy-prod.yml` (`MIGRATE_TARGET_VERSION=295` → `296`, two places)

**Interfaces:**
- Produces: server acceptance of `category: "flutter"` and `test_type = 'flutter'`. Phase 2's CLI change depends on this being **in prod**.

- [ ] **Step 1: Create the worktree and confirm the migration head**

```bash
cd /Users/ibrahim/Astrais/api-service && git fetch origin
git worktree add -b feat/test-type-flutter ../api-flutter origin/main && cd ../api-flutter
git ls-tree --name-only origin/main db/migrations/ | sort | tail -1
```

Expected: `db/migrations/0295_ai_usage_events.up.sql`. If a higher number exists, use the next free number everywhere below instead of `0296`/`296`.

- [ ] **Step 2: Write the migration**

`db/migrations/0296_test_type_flutter.up.sql`:

```sql
-- Add 'flutter' to the test_type enum.
--
-- qualflare-cli parses Flutter's JSON test reporter output (`flutter test
-- --machine` / `--file-reporter json:`) and labels the suites `flutter`.
-- Dart's test runner has no pluggable reporter API, so this is a CLI parser
-- rather than a native reporter package.
--
-- A new test_type value must stay in sync with SIX places:
--   1. this enum
--   2. db/sqlc/models.go TestType constants (hand-maintained; never regenerate)
--   3. TestType's const block AND All in internal/core/domain/test_type/test_type.go
--   4. Suite.Category's oneof in internal/core/domain/launch/launch.go
--   5. toolCategories in internal/core/domain/ctrf/tools.go (only if a CTRF tool name maps to it; none does for Flutter)
--   6. MIGRATE_TARGET_VERSION in .gitlab/ci/deploy-prod.yml
-- and, only AFTER this is in prod, qualflare-cli's Framework enum and
-- AllFrameworks(). The CLI going first makes the server 400 the whole launch.
--
-- ADD VALUE only, with no use of the new value in this same migration, so it
-- is safe inside PG12+'s implicit transaction.
--
-- Irreversible: PostgreSQL has no ALTER TYPE ... DROP VALUE. The down
-- migration is a deliberate no-op, matching 0240, 0242 and 0294.
ALTER TYPE test_type ADD VALUE IF NOT EXISTS 'flutter';
```

`db/migrations/0296_test_type_flutter.down.sql`:

```sql
-- No-op: PostgreSQL enums cannot have values removed in place, and recreating
-- the enum without 'flutter' FAILS whenever any row already uses it. Leaving
-- the value in place on rollback is harmless, so this is intentionally a no-op
-- (matching migrations 0074, 0079, 0089, 0117, 0240, 0242 and 0294). A rollback
-- that must remove the value requires a forward-fix migration or a backup restore.
SELECT 1;
```

- [ ] **Step 3: Add the value in the four Go places and the deploy target**

```bash
python3 - <<'PY'
import pathlib
def edit(path, old, new):
    p = pathlib.Path(path); s = p.read_text()
    assert s.count(old) == 1, (path, old)
    p.write_text(s.replace(old, new))
edit('db/sqlc/models.go',
     '\tTestTypeAppium        TestType = "appium"\n',
     '\tTestTypeAppium        TestType = "appium"\n\tTestTypeFlutter       TestType = "flutter"\n')
edit('internal/core/domain/test_type/test_type.go',
     '\tTestTypeAppium      TestType = "appium"\n',
     '\tTestTypeAppium      TestType = "appium"\n\tTestTypeFlutter     TestType = "flutter"\n')
edit('internal/core/domain/test_type/test_type.go',
     'TestTypeWebdriverIO, TestTypeAppium,\n',
     'TestTypeWebdriverIO, TestTypeAppium, TestTypeFlutter,\n')
edit('internal/core/domain/launch/launch.go',
     ' webdriverio appium newman ',
     ' webdriverio appium flutter newman ')
p = pathlib.Path('.gitlab/ci/deploy-prod.yml'); s = p.read_text()
assert s.count('MIGRATE_TARGET_VERSION=295') == 2
p.write_text(s.replace('MIGRATE_TARGET_VERSION=295', 'MIGRATE_TARGET_VERSION=296'))
PY
gofmt -w internal db/sqlc/models.go
```

- [ ] **Step 4: Build, test and validate the deploy target**

```bash
go build ./... && go vet ./internal/core/domain/...
ln -s /Users/ibrahim/Astrais/app-ui ../app-ui   # the TypeScript drift gates read app-ui beside api-service
go test ./...
rm ../app-ui
bash deploy/scripts/validate-migrate-target.sh .gitlab/ci/deploy-prod.yml
```

Expected: build and every package pass; the validator prints `All migrate targets current (head=296).`

- [ ] **Step 5: Commit, push and open the MR**

```bash
git add -A
git commit -m "feat: add the flutter test type" -m "qualflare-cli will parse Flutter's JSON test reporter output and label suites flutter; the server must accept the value first. Migration 0296 (ADD VALUE, no-op down), sqlc constant (hand edit), domain const + All, the Suite.Category oneof, MIGRATE_TARGET_VERSION 295 -> 296. No CTRF tool name maps to Flutter."
git push -u origin feat/test-type-flutter
glab mr create --source-branch feat/test-type-flutter --target-branch main --title "feat: add the flutter test type" --yes --description "Adds flutter as a test type (migration 0296). It must be in prod before qualflare-cli emits the category, and before app-ui is promoted."
```

- [ ] **Step 6: After merge, open the prod promotion and confirm the deploy**

```bash
git fetch origin
git log --no-merges --format='%h %ae %s' origin/prod..origin/main
glab mr create --source-branch main --target-branch prod --title "Promote main to prod: flutter test type" --yes --description "Ships migration 0296 (flutter test type). List any other commits the log above shows."
```

After that MR merges: `deploy-prod:api-service` must succeed on a prod pipeline whose commit contains the change (`git branch -r --contains <sha> | grep origin/prod`).

---

### Task 2: app-ui — the `flutter` test type and mark

**Repo:** app-ui (GitLab, pnpm), branch `feat/test-type-flutter` from `origin/main`. **Do not promote to prod until Task 1 is in prod.**

**Files:**
- Modify: `src/models/test-type.tsx` (union, after `'appium'`, line 44)
- Modify: `src/components/icons/frameworks.tsx` (new `FlutterIcon` after `AppiumIcon`)
- Modify: `src/components/icons/framework-marks.ts` (import + `flutter` entry after `appium`)

**Interfaces:**
- Consumes: server acceptance of `flutter` (Task 1, in prod) before promotion.

- [ ] **Step 1: Create the worktree and install**

```bash
cd /Users/ibrahim/Astrais/app-ui && git fetch origin
git worktree add -b feat/test-type-flutter ../app-ui-flutter origin/main && cd ../app-ui-flutter
pnpm install --frozen-lockfile
```

- [ ] **Step 2: Add the union member, the icon and the mark**

```bash
python3 - <<'PY'
import pathlib
def edit(path, old, new):
    p = pathlib.Path(path); s = p.read_text()
    assert s.count(old) == 1, (path, old)
    p.write_text(s.replace(old, new))
edit('src/models/test-type.tsx', "    | 'appium'\n", "    | 'appium'\n    | 'flutter'\n")
edit('src/components/icons/frameworks.tsx', "AppiumIcon.displayName = 'AppiumIcon'\n", """AppiumIcon.displayName = 'AppiumIcon'


export const FlutterIcon = forwardRef<SVGSVGElement, LucideProps>((props, ref) => (
    <FrameworkIcon
        ref={ref}
        paths={[
            'M14.314 0L2.3 12 6 15.7 21.684.013h-7.357zm.014 11.072L7.857 17.53l6.47 6.47H21.7l-6.46-6.468 6.46-6.46h-7.37z',
        ]}
        {...props}
    />
))
FlutterIcon.displayName = 'FlutterIcon'
""")
edit('src/components/icons/framework-marks.ts', "    EspressoIcon,\n", "    EspressoIcon,\n    FlutterIcon,\n")
edit('src/components/icons/framework-marks.ts',
     "    appium: { label: 'Appium', Icon: AppiumIcon },\n",
     "    appium: { label: 'Appium', Icon: AppiumIcon },\n    // Parsed by qualflare-cli from Flutter's JSON test output; test_type since\n    // api-service migration 0296.\n    flutter: { label: 'Flutter', Icon: FlutterIcon },\n")
PY
```

The path is Simple Icons 16.33.0's `flutter.svg` (CC0), the source `frameworks.tsx` requires.

- [ ] **Step 3: Typecheck, lint and test**

```bash
pnpm exec tsc -b && pnpm run lint && pnpm exec vitest run
```

Expected: no type or lint errors; all tests pass.

- [ ] **Step 4: Commit, push and open the MR**

```bash
git add src
git commit -m "feat: show Flutter as a test type" -m "api-service migration 0296 adds flutter to test_type; qualflare-cli labels Flutter suites with it. Adds the union member and the Flutter mark (Simple Icons 16.33.0, CC0)."
git push -u origin feat/test-type-flutter
glab mr create --source-branch feat/test-type-flutter --target-branch main --title "feat: show Flutter as a test type" --yes --description "Adds flutter to the TestType union with its mark. Promote only after api-service migration 0296 is in prod."
```

- [ ] **Step 5: Before promoting, confirm the server is in prod**

```bash
cd /Users/ibrahim/Astrais/api-service && git fetch origin
git branch -r --contains "$(git log origin/main --format=%H -1 -- db/migrations/0296_test_type_flutter.up.sql)" | grep 'origin/prod$'
```

Expected: prints `origin/prod`. Only then open app-ui's `main → prod` promotion MR.

---

## Phase 2 (separate plan, written from Task 0's findings)

The `flutter` parser in qualflare-cli (golden fixtures from Task 0's captures, the edge cases in the spec, a real-Flutter CI job, `FrameworkFlutter` only after Task 1 is in prod), the Claude Code plugin's `/qf-init` and `/qf-run`, and content (landing page, counts 26 → 27, the "only through Maestro" claims). Its plan cannot be written honestly until the four open questions are answered.
