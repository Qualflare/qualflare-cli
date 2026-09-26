# CTRF `environment` is read but never reaches the launch

Status: **not started**. Found 2026-09-06 while auditing CTRF conformance.

## The claim to check first

Everything below rests on one behaviour. Confirm it before writing code, because
if it is wrong the plan is wrong:

```go
// report_service.go createReport()
Branch: s.config.GetBranch(),
Commit: s.config.GetCommit(),
```

`Launch.Branch` and `Launch.Commit` come from the CLI's config — `--branch`,
`--commit`, or CI environment detection — and **never from the report file**. A
CTRF report carrying `environment.branchName` does not populate them.

Reproduce: upload a CTRF report with `results.environment.branchName` set, no
`--branch` flag, outside CI. Expect `Launch.Branch` empty.

## Two separate gaps

### 1. Eight environment fields are not decoded at all

CTRF's `environment` object has 18 properties. `types.go` decodes 10:

```
decoded      osPlatform  osRelease  osVersion  testEnvironment  branchName
             commit      buildNumber  buildUrl  shardId  reportName

NOT decoded  appName  appVersion  buildId  buildName  healthy
             repositoryName  repositoryUrl  extra
```

`buildName` and `repositoryUrl` are the ones worth having — CI reporters populate
them routinely, and a third-party CTRF upload silently loses build and repository
context that a native reporter would have carried.

### 2. The ten we DO decode only become suite properties

They are stored, prefixed, on the suite:

```go
setProp(suite.Properties, propBranch, env.BranchName)   // "ctrfBranchName"
setProp(suite.Properties, propCommit, env.Commit)       // "ctrfCommit"
```

Nothing promotes them further. `promoteConsistentSuiteProperties` lifts exactly
three keys — `browser`, `platform`, `environment` — and none of the `ctrf*` ones
are in that list.

**This is the half that matters.** `branch` is a metrics label on the dashboard
(see api-service `internal/core/domain/metrics`), so a CTRF upload without an
explicit `--branch` lands in the "unknown" branch bucket, and the branch filter
cannot see it. Field 1 is data we never read; field 2 is data we read, store, and
then do not use.

## The precedent to follow

This exact problem was already solved once, for the qualflare-json reporters'
`environment`. From `report_service.go`:

> The qualflare-json reporters write the environment they were configured with
> into their report. Until this was read, it went nowhere and the launch landed
> in the CLI's default environment instead — silently, and wherever "development"
> existed, in the wrong place rather than nowhere. **A `--environment` flag or
> `QF_ENVIRONMENT` still wins: the person running the upload outranks the file
> being uploaded.**

`SetEnvironmentFallback` is the shape to copy. The rule is not "the file wins" —
it is "the file is a FALLBACK, the operator wins".

## Plan

### Step 1 — decode the missing fields

`internal/adapters/parsers/generic/ctrf/types.go`: add `appName`, `appVersion`,
`buildId`, `buildName`, `healthy`, `repositoryName`, `repositoryUrl` to
`Environment`.

Skip `extra` — it is an arbitrary extension bag with no schema, and decoding it
into `map[string]any` invites unbounded user-authored data onto a path that
`stripUserAuthoredSuiteProperties` exists to police. If it is ever wanted, it
needs its own allowlist decision, not a `json:"extra"` tag.

`healthy` is a bool; the property channel is `map[string]string`, so decide
explicitly whether absent and `false` are distinguishable before storing it.

### Step 2 — store them as properties, like their neighbours

Same `setProp` pattern, same `ctrf` prefix. Cheap, consistent, and by itself it
already stops the data being discarded.

### Step 3 — promote the ones with first-class homes

This is the step that changes behaviour, and the one that needs care.

| CTRF field | Launch home | note |
|---|---|---|
| `branchName` | `Launch.Branch` | **the metrics label** — highest value |
| `commit` | `Launch.Commit` | |
| `buildNumber`, `buildUrl` | `Launch.Metadata` | check the existing shape first |
| `buildName`, `repositoryUrl`, `repositoryName`, `appName`, `appVersion` | undecided | may have no home; leaving them as properties is a valid answer |

Rules, all inherited from the environment precedent:

* **The operator outranks the file.** A `--branch` flag or CI detection wins; the
  report is consulted only when the config produced nothing.
* **Only when every suite agrees.** A merged shard file can carry several
  environments. `promoteConsistentSuiteProperties` already implements exactly
  this "consistent or nothing" rule — reuse it rather than writing a second one.
* **Do not invent a `mixed` branch.** Unlike `Framework`, `Branch` is matched
  against a normalized bucket by the server's metrics layer; a literal "mixed"
  would collapse to `other` and be worse than empty.

### Step 4 — decide about the undecoded qualflare-json fields

`native/qualflare/qualflare.go` has a standing comment:

> The remaining Collect fields (branch, commit, language, milestone, CI metadata,
> os) are deliberately still NOT decoded here

That is the same gap in the other format. Whatever rule step 3 settles on should
apply to both, or the two collection paths behave differently for no reason a
user could predict. Doing it in the same change is preferable; doing it with a
different rule is not.

## Tests

* A CTRF report with `environment.branchName` and no `--branch` sets
  `Launch.Branch`. **This test must FAIL before the fix** — verify that, since it
  is the entire claim.
* An explicit `--branch` beats the file's value.
* Two suites disagreeing on branch promote nothing, rather than picking one.
* The eight newly-decoded fields survive onto suite properties.
* An absent `environment` object changes nothing (it is optional in the schema).

Drive them through `ParseTestResults`, not through the promotion helper directly:
the behaviour depends on the parser storing the properties on the way through,
and testing the helper alone asserts against a fixture rather than against what
the parser produces.

## Explicitly out of scope

* **Validating CTRF conformance.** We are lenient on two fields the schema marks
  required — `reportFormat` is checked only when present, `specVersion` is decoded
  and ignored because every producer hardcodes `0.0.0`. That leniency is correct
  for a reader and should not change here. Telling a user their reporter emits
  bad CTRF is a different feature.
* **The summary counters.** Deliberately unused: `Suite.RecomputeCounts` derives
  every total from the cases, so a summary that disagrees with `tests[]` cannot
  roll a red run green. Do not "fix" this.
* **`ai`, `insights`, `baseline`.** Advisory and derived sections, not results.

## Prior art in this area

`fix/launch-framework-is-not-the-transport` (PR #58) fixed the neighbouring bug:
`Launch.Framework` carried the collection FORMAT (`ctrf` / `qualflare-json`)
rather than the producing tool, even though both parsers already resolved the
producer for the suite category. The shape of that fix — record it on the suite
during parsing, resolve it once at launch level, prefer "unknown" over a guess —
is the same shape this needs.
