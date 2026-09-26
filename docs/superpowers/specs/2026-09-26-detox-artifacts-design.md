# Detox artifacts — design

**Status:** spike complete, spec revised from its findings
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

### 1. `qf collect --artifacts-dir <dir>` (the work)

Scans a Detox artifacts root, matches each per-test directory to a case in the report being
uploaded, and attaches its files.

**The flag is framework-generic, the layout is not.** `--artifacts-dir` names a directory of
artifacts; the layout inside it is inferred from the format of the report being uploaded. Detox is
the first implementation, and the flag exists in this shape so Maestro's debug output and anything
else that drops files in a directory do not each arrive as their own near-identical flag.

Two consequences to honour, or the generality is a trap rather than a feature:

- A format with no artifact-directory support must **fail loudly** when the flag is passed, naming
  the format and the formats that do support it. Silently ignoring the flag would look identical to
  a matching bug.
- The inference is from the report, never from the directory's own shape. Sniffing a directory to
  decide what wrote it is guesswork that fails in exactly the confusing cases.

**No auto-detection.** With the flag absent, nothing is scanned — `./artifacts` is never guessed,
even though it is Detox's default `rootDir`. Attaching files from a directory the user did not name
is how a stale run's video ends up on today's launch, and that mistake is invisible from the
dashboard. Given the flag, a Detox *root* (containing `<configuration>.<timestamp>` subdirectories)
resolves to the newest such subdirectory; a specific `<configuration>.<timestamp>` directory is
used as given.

**Matching — computed forward, never inverted.** The spike settled the naming rule from Detox's
source (`ArtifactPathBuilder.js`, `constructSafeFilename.js`) rather than its documentation, and
the documentation was wrong in a way that would have shipped a bug: **there is no test number in
the directory name.** The rule is

```
dirname = sanitize(prefix + fullName.slice(-(255 - prefix.length - suffix.length)) + suffix,
                   {replacement: '_'}).replace(/\$/g, '_')

prefix = '✓ ' when passed | '✗ ' when failed | '' otherwise
suffix = ' (N)' when invocations > 1 | ''
sanitize = the `sanitize-filename` npm package
```

So matching does **not** strip a prefix and compare. It computes the directory name each case
*would* have — for each status prefix and each plausible invocation — and looks that name up. The
transformation is lossy (`a/b` and `a_b` both become `a_b`), so inverting it is guesswork, while
computing it forward is exact.

The join key is confirmed on both sides: Detox builds the name from `testSummary.fullName`, and
`@qualflare/jest` records `assertion.fullName` as the case name. They are the same string.

Executed against the real algorithm, these are the outputs the corpus must contain:

| Case | Directory |
|---|---|
| passed | `✓ Login should sign in` |
| failed | `✗ Login should sign in` |
| invocation 2 | `✓ Login should sign in (2)` |
| name with `/` | `✗ Login should handle a_b paths` |
| name with `"` and `:` | `✗ Login _quoted_ and_ colons` |
| name with `$` | `✓ Money costs _5` |
| 300-char name | 253 chars, **truncated from the start** |

That last row is the subtle one: `slice(-N)` keeps the *tail*, so a long name loses its beginning.
Any matching that assumed a common prefix would fail on exactly the tests whose names are most
descriptive.

The glyph is non-ASCII (`✓`/`✗`), and this repo has been bitten by a non-ASCII path assumption
before — `qualflare-maestro`'s `go install` broke on a `screenshot-❌-….png`, because the Go module
zip rejects non-ASCII paths. Here it is benign as long as the comparison is byte-exact on UTF-8 and
the corpus contains the glyphs as real bytes rather than escapes.

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

**A real Detox run, downgraded from blocking to confirming.** The spec originally called for a
React Native app, a native build and a simulator, because the naming rule was the unknown the whole
design rested on. Reading Detox's source removed that unknown for a fraction of the cost, and the
corpus above is generated by the same algorithm rather than guessed.

What a real run would still add is narrower: that the files *inside* each directory are named as
expected, and that nothing in Detox's Jest integration changes `fullName` before the reporter sees
it. Worth doing once, before release — but it no longer blocks the plan, and it should not be
scheduled as though it does.

**End to end.** `qf collect --artifacts-dir` against that fixture's output, with `--dry-run`
showing each case carrying the artifacts its directory held, and a second run against a
non-Detox report asserting the flag fails loudly rather than being ignored.

## Out of scope

- Live or streaming reporting during a Detox run.
- Reading Detox's own config to locate artifacts automatically. The flag is explicit for now;
  auto-location can follow once the matching itself is proven.
- Any change to the server, the wire format, or `domain.Attachment`.

## Open questions for review

1. **Answered by the spike: yes.** A retried test writes one directory per invocation, the later
   ones suffixed ` (2)`, ` (3)`. Artifacts are therefore attributable per attempt — but
   `domain.Attempt` has no attachment field, and adding one means a server change, which this spec
   puts out of scope. **Decision: attach every invocation's artifacts to the case**, with the
   invocation in the attachment name (`screenshot.png (attempt 2)`), so nothing is lost and no
   schema moves. Per-attempt attachments become a later change if anyone asks for them.
2. **Decided: `--artifacts-dir`, with the layout inferred from the report's format.** Revisit only
   if the second framework to use it needs a materially different shape of input, which would mean
   the generality was wrong rather than early.
