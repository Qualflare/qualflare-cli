# Detox artifacts — design

**Status:** approved shape, spec awaiting review
**Date:** 2026-09-26
**Scope:** `qualflare-cli` (the work), plus a thin `@qualflare/detox` npm package (positioning)

## Why this is not a new reporter

The mobile roadmap listed Detox as the third native reporter, after Espresso and XCTest. It should
not be one, for the same reason XCTest should not have been: the thing a reporter would add already
exists.

**Detox runs on Jest.** `detox test` drives Jest with its own test environment, and a Jest reporter
listed in `jest.config` runs normally. So `@qualflare/jest` already reports a Detox suite today —
statuses, durations, per-attempt retry history, steps, the `qualflare.*` runtime API, and
`attachmentFromFile`. Nothing about that is Detox-specific and nothing about it is missing.

What is missing is narrow: **Detox's own artifacts** — the screenshots, videos, device logs,
instruments recordings and UI hierarchy snapshots its artifact plugins write to disk — are never
associated with the test they belong to.

## The finding that decides the architecture

The obvious place to scan those artifacts is the Jest reporter, in `onRunComplete`. That cannot
work, and the reason is structural rather than a timing quirk:

- Detox finalises video (and flushes logs) in its artifacts plugin's `onBeforeCleanup`, which runs
  inside `detox.cleanup()`.
- Detox's Jest integration calls `detox.cleanup()` from Jest's `globalTeardown`.
- **Jest runs `globalTeardown` after every reporter's `onRunComplete`.** Measured directly, with a
  throwaway Jest project containing one reporter and one teardown, both logging:

  ```
  ORDER: reporter.onRunComplete
  ORDER: globalTeardown
  ```

A reporter-side scan therefore runs before Detox has finished writing, and would systematically
miss videos — the single largest and most useful artifact. No retry or delay inside the reporter
fixes that ordering; it would only make the miss intermittent, which is worse.

**So the scan belongs in `qf collect`**, which runs as a separate command after the Detox process
has exited. The race does not need managing, because it does not exist there.

This also reuses machinery that already exists. `qf collect` gained an artifact path for
`.xcresult` in v0.1.31: a parser sets `Attachment.LocalPath` and `Attachment.ArtifactKind`, and
`report_service.go` resolves them into presigned uploads, honouring `--upload-artifacts`. Detox
artifacts are the same shape of problem — files on disk that belong to a named test — and take the
same route.

## What ships

### 1. `qf collect --detox-artifacts <dir>` (the work)

Scans a Detox artifacts root, matches each per-test directory to a case in the report being
uploaded, and attaches its files.

**Auto-detection.** With the flag absent, nothing is scanned. With the flag passed a directory that
is a Detox *root* (containing `<configuration>.<timestamp>` subdirectories), the newest such
subdirectory is used. With the flag passed a specific `<configuration>.<timestamp>` directory, that
one is used. Auto-detection never guesses `./artifacts` on its own: silently attaching files from a
directory the user did not name is how a stale run's video ends up on today's launch.

**Matching.** Detox names each per-test directory `{glyph} {test-number} {test-full-name}`, for
example `✗ Assertions should assert an element has (accessibility) id`. Matching strips the leading
status glyph and number, then compares the remainder to the case's full name.

Two hazards, both of which the fixture task exists to pin down rather than reason about:

- The prefix glyph is **non-ASCII** (`✓`/`✗`). This repo has already been bitten by a non-ASCII
  path assumption once — `qualflare-maestro`'s `go install` broke on a `screenshot-❌-….png`,
  because the Go module zip rejects non-ASCII paths. Matching must normalise, not assume ASCII, and
  the test corpus must include the glyphs as bytes.
- Detox sanitises test names for the filesystem. Exactly which characters, and how, is not
  documented at the level this needs. A test whose name contains `/`, `:` or a newline is where
  string matching fails, and guessing the rule is how this ships broken.

Unmatched directories are reported on stderr — a count, plus the first few names — and a scan that
matched *nothing* while finding directories is a warning in its own right. Dropping them silently
is the failure to avoid: a run where nothing matched is far more likely to be a matching bug than a
run with no artifacts, and the two look identical from the dashboard.

**Artifact kinds.** Each file maps onto an existing kind, which decides both the upload route and
whether it uploads by default:

| Detox artifact | Extension | Kind | Default |
|---|---|---|---|
| Screenshot | `.png` | `image` | uploads |
| Video | `.mp4` | `video` | opt-in |
| Device log | `.log` | `trace` | opt-in |
| Instruments recording | `.dtxrec` | `trace` | opt-in |
| UI hierarchy | `.uihierarchy` | `trace` | opt-in |

Videos are opt-in deliberately and that is not a new decision: `--upload-artifacts`' existing
wording already says video "is the largest thing in a report by an order of magnitude and should be
a choice rather than a surprise on the bill". A Detox video of a failing flow is exactly that.

Device logs are the one judgement call. They are small enough to inline and genuinely useful on a
failure, but they are also the artifact most likely to contain customer data from a real app, so
they stay opt-in rather than being inlined by default.

### 2. `@qualflare/detox` (positioning, not engineering)

A thin package that re-exports `@qualflare/jest`'s reporter with Detox-sensible defaults and
Detox-shaped documentation.

Stated plainly so nobody is surprised later: **it contains almost no logic.** Its value is
discoverability — npm search, `awesome-detox`-style lists, and "detox test reporting" queries,
which is the stated point of the mobile content strategy. It is a marketing surface with a real but
small engineering core, and the README must not imply otherwise.

### 3. `@qualflare/jest` — unchanged

No change is expected. If the fixture shows Detox needs something from the reporter — most plausibly
recording the artifacts root it was configured with, so `qf collect` need not be told — that becomes
a small addition, not a redesign.

## Testing

**Unit, from a synthesised tree.** The matcher is a pure function from (directory listing, case
names) to (attachments, unmatched). It gets a corpus built in a temp directory covering: a passing
and a failing test, the `✓`/`✗` glyphs as real bytes, a name containing parentheses, a name long
enough to be truncated if Detox truncates, a directory matching no case, and a case matching no
directory.

**A real fixture, as its own task.** The corpus above encodes assumptions about Detox's naming.
Those assumptions have to come from a real run, not from documentation: a minimal React Native app,
a Detox config with all five artifact plugins on, one passing and one failing test, and one test
with an awkward name. The captured directory becomes the corpus, checked in.

This is the expensive part of the work and should be scheduled as such — an RN app, a native build,
and a simulator. It is also non-negotiable. Two spikes this month (the orchestrator's enumeration
pass, and `.xcresult`'s per-run shape) each overturned a design that read as obviously correct, and
both were caught only by running the real thing.

**End to end.** `qf collect --detox-artifacts` against that fixture's output, with `--dry-run`
showing each case carrying the artifacts its directory held.

## Out of scope

- Live or streaming reporting during a Detox run.
- Reading Detox's own config to locate artifacts automatically. The flag is explicit for now;
  auto-location can follow once the matching itself is proven.
- Any change to the server, the wire format, or `domain.Attachment`.

## Open questions for review

1. **Does a retried Detox test produce two directories?** Jest retries (`jest.retryTimes`) would
   plausibly yield `✗ name` and then `✓ name`, or a numbered pair. If so, artifacts should attach to
   the attempt rather than the case — and `domain.Attempt` has no attachment field, so the answer
   changes scope. The fixture must cover it.
2. **Is `--detox-artifacts` the right surface**, or should it be `--artifacts-dir` with the format
   inferred? A Detox-specific flag is clearer now and harder to generalise later.
