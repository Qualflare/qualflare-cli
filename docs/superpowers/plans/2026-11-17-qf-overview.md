# `qf <id> overview` Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A read command that prints the project Overview (the website dashboard's six numbers) as JSON: `qf <id> overview [--days 7|28]`.

**Architecture:** One cobra command in `internal/adapters/cli/overview.go`, added to the authed subtree, calling the public API's `GET /api/v1/overview?days=N` through the existing `fetchAndPrint` (same JSON output and exit codes as every read).

**Tech Stack:** Go, cobra.

**Spec:** the owner-approved design for the public overview endpoint, `qf overview` and the Tern dashboard (kept in the private api-service repo). The endpoint is live on stage and answers: `window {from,to,days}`, `buckets[]`, `kpis[]` (`successRate`, `flakeRate`, `launchP90` in seconds, `automationRate`, `defectsOpened`, `launches`, each with `latest`, `previous`, `percentChange`, `trend`, `target`, `values[]`), `warnings[]`, `dataThrough`; `days` must be 7 or 28 (400 otherwise).

## Global Constraints

- `--days` accepts 7 (default) or 28 only; anything else is refused before any request, with an error naming `--days`.
- Output and exit codes come from `fetchAndPrint` unchanged (indented JSON; 3 not logged in, 4 forbidden/plan, 5 not found, 7 transient).
- Public repo: commits as `Qualflare Root <root@qualflare.com>`, no AI attribution; the owner opens the PR as qfroot and tags the release after the endpoint is in production.

## Review Focus

1. `--days 30` (or 0, -1): a clear error, no request sent, non-zero exit.
2. A `qf` older than this command, asked for `overview`, must not be mistaken for success by callers — out of this repo's reach; the Tern pane detects it (its own plan).
3. The endpoint answering 400 (a future server change) still maps to a failing exit, as every read does.

---

### Task 1: The command

**Files:**
- Create: `internal/adapters/cli/overview.go`
- Modify: `internal/adapters/cli/command.go` (`buildAuthedSubtree`: add `c.createOverviewCommand()` after `createMilestoneCommand()`), `README.md` ("Browse your test data" and the Commands table)
- Test: `internal/adapters/cli/resource_commands_test.go`

**Interfaces:**
- Produces: `func (c *CLI) createOverviewCommand() *cobra.Command` — `Use: "overview"`, `Short: "Show the project overview: the dashboard's six numbers"`, `--days int` (default 7), request `apiV1 + "/overview"` with param `days`.

- [ ] **Step 1: Write the failing tests** (style of `TestCasesList_BuildsSuiteScopedRequest`, using `newAPICLI` and `run`):
  - `TestOverview_DefaultsToSevenDays`: `run(t, c.createOverviewCommand())` → no error, `api.calls == 1`, `api.path == apiV1+"/overview"`, `api.params.Get("days") == "7"`.
  - `TestOverview_TwentyEightDays`: `--days 28` → `days == "28"`.
  - `TestOverview_RejectsOtherDays`: for `30`, `0`, `-1` → error containing `--days`, `api.calls == 0`.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./internal/adapters/cli/ -run TestOverview -v`
Expected: FAIL (`createOverviewCommand` undefined).

- [ ] **Step 3: Implement** `createOverviewCommand` and register it. `Long` says what the six numbers are and that `launches`/`defectsOpened` are today's counts; `Example` shows `qf <id> overview` and `qf <id> overview --days 28`.

- [ ] **Step 4: Run them to verify they pass, then the whole suite**

Run: `go test ./internal/adapters/cli/ -run TestOverview -v && go build ./... && go vet ./... && go test ./...`
Expected: PASS; build and vet clean.

- [ ] **Step 5: README** — add `qf myapp overview --days 28` to "Browse your test data", and `overview` to the read-commands row of the Commands table.

- [ ] **Step 6: Commit**

```bash
git add internal/adapters/cli/overview.go internal/adapters/cli/command.go internal/adapters/cli/resource_commands_test.go README.md
git -c user.name="Qualflare Root" -c user.email=root@qualflare.com commit -m "feat: qf <id> overview"
```

### Task 2: Stage check and hand-off

- [ ] Build this branch's `qf` against stage and run it on a throwaway stage project with uploaded runs (the api-service e2e harness's `BuildCLI` with `E2E_CLI_DIR` pointing at this worktree): `qf <id> overview` exits 0 and prints six KPIs; `--days 28` gives `window.days == 28`; `--days 30` exits non-zero with no request.
- [ ] Push `feature/overview` (owner's go-ahead) and hand the owner the PR link to open as qfroot; the release tag waits until the endpoint is in production.
