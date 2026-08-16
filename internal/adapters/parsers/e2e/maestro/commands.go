package maestro

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/shared/sibling"
	"qualflare-cli/internal/core/domain"
)

const commandsFileName = "commands.json"

// mCommandEntry is one entry in Maestro's commands.json: a two-key wrapper
// around the executed command and its execution metadata — NOT a flat
// object. Verified two ways: against Maestro's own source
// (mobile-dev-inc/maestro: ArtifactsGenerator.kt's CommandDebugWrapper,
// TestOutputWriter.kt, FlowDebugOutput.kt's CommandDebugMetadata) and
// against real commands.json captured from `maestro test` runs against a
// real Android emulator, both a passing and a failing flow.
type mCommandEntry struct {
	Command  json.RawMessage  `json:"command"`
	Metadata mCommandMetadata `json:"metadata"`
}

// mCommandMetadata is the "metadata" object nested in every commands.json
// entry (maestro.orchestra.debug.CommandDebugMetadata). Duration is
// confirmed milliseconds: Maestro computes it as finishedAt - startedAt,
// both java System.currentTimeMillis(). Status's vocabulary is the
// exhaustive real enum (maestro.orchestra.debug.CommandStatus) — not a
// guess tolerating multiple plausible spellings, as it was before.
type mCommandMetadata struct {
	Status         string         `json:"status"`
	Duration       int64          `json:"duration"`
	SequenceNumber int            `json:"sequenceNumber"`
	Error          *mCommandError `json:"error,omitempty"`
}

// mCommandError is Maestro's serialized Throwable (its SlimThrowableMixin
// restricts serialization to these two fields) — an object, not a plain
// string, confirmed against a real failed assertion's commands.json entry.
// debugMessage is Maestro CLI's generic troubleshooting boilerplate, not
// failure-specific content, so only message is surfaced.
type mCommandError struct {
	Message string `json:"message"`
}

func parseCommandsJSON(data []byte) ([]mCommandEntry, error) {
	var entries []mCommandEntry
	if err := json.Unmarshal(data, &entries); err != nil {
		return nil, err
	}
	return entries, nil
}

// syntheticCommandKeys are commands Maestro always injects itself before a
// flow's first user-authored step — a defineVariablesCommand carrying its
// own MAESTRO_* env vars (maestro.orchestra.util.Env.kt), then an
// applyConfigurationCommand carrying the flow's YAML-header config
// (maestro.orchestra.yaml.YamlConfig.kt) — confirmed present at
// sequenceNumber 0 and 1 of every real commands.json captured. Never
// something the test author wrote, so never surfaced as a Step.
var syntheticCommandKeys = map[string]bool{
	"defineVariablesCommand":    true,
	"applyConfigurationCommand": true,
}

func isSyntheticCommand(raw json.RawMessage) bool {
	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObject); err != nil || len(asObject) != 1 {
		return false
	}
	for key := range asObject {
		return syntheticCommandKeys[key]
	}
	return false
}

// commandStepName derives a readable step name from a command entry's raw
// "command" JSON: a single-key object (Maestro's real tagged-union-per-
// command-type shape, e.g. {"launchAppCommand": {...}}) uses that key with
// a trailing "Command" suffix stripped when present (not every real key has
// one, e.g. "tapOnElement" — left as-is); a plain string is used as-is,
// kept as defensive tolerance for a future Maestro format change (real
// output never produces this today); anything else falls back to a
// positional label so a step is never silently dropped.
func commandStepName(raw json.RawMessage, position int) string {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		return asString
	}
	var asObject map[string]json.RawMessage
	if err := json.Unmarshal(raw, &asObject); err == nil && len(asObject) == 1 {
		for key := range asObject {
			return strings.TrimSuffix(key, "Command")
		}
	}
	return fmt.Sprintf("step %d", position)
}

// commandStatus maps Maestro's real, exhaustive CommandStatus enum
// (maestro.orchestra.debug.CommandStatus) to domain.Status. WARNED (a
// genuine non-fatal outcome, e.g. an optional assertion that didn't hold)
// has no clean Passed/Failed equivalent, so it maps to StatusError —
// matching this codebase's existing convention (xctest.go) of using Error
// for "notable, not a clean pass" rather than silently folding it into
// Passed. PENDING/RUNNING can only appear from a flow that never reached a
// terminal state (an aborted run) and map to StatusPending. Any future,
// currently-unknown value fails closed as StatusError rather than
// defaulting to Passed.
func commandStatus(m mCommandMetadata) domain.Status {
	switch m.Status {
	case "COMPLETED":
		return domain.StatusPassed
	case "FAILED":
		return domain.StatusFailed
	case "SKIPPED":
		return domain.StatusSkipped
	case "WARNED":
		return domain.StatusError
	case "PENDING", "RUNNING":
		return domain.StatusPending
	default:
		return domain.StatusError
	}
}

func errorMessage(e *mCommandError) string {
	if e == nil {
		return ""
	}
	return e.Message
}

func stepsFromCommands(entries []mCommandEntry) []domain.Step {
	steps := make([]domain.Step, 0, len(entries))
	for _, e := range entries {
		if isSyntheticCommand(e.Command) {
			continue
		}
		steps = append(steps, domain.Step{
			Name:     commandStepName(e.Command, e.Metadata.SequenceNumber),
			Status:   commandStatus(e.Metadata),
			Error:    errorMessage(e.Metadata.Error),
			Duration: time.Duration(e.Metadata.Duration) * time.Millisecond,
		})
	}
	return steps
}

// enrichSuiteWithCommands attaches commands.json's steps to the suite's
// case, but only when the suite has exactly one case. commands.json (per
// its real shape, confirmed against Maestro's own source) carries no
// per-flow correlation key, so a multi-flow/multi-case suite has no
// reliable way to attribute which commands belong to which case —
// attaching them all to every case, or guessing, risks attributing one
// flow's steps to a different flow's case, which is worse than reporting
// no steps at all.
func enrichSuiteWithCommands(suite *domain.Suite, entries []mCommandEntry) {
	if len(suite.Cases) != 1 {
		return
	}
	suite.Cases[0].Steps = stepsFromCommands(entries)
}

// enrichWithCommandSteps looks for commands.json next to path and, if
// present and well-formed, merges its step data into suite. Best-effort: a
// missing or malformed sibling leaves suite exactly as already parsed from
// the primary JUnit XML, never an error.
func enrichWithCommandSteps(suite *domain.Suite, path string) {
	data, ok, err := sibling.Read(path, commandsFileName)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: maestro: could not read %s: %v; reporting case-level results only\n", commandsFileName, err)
		return
	}
	if !ok {
		return
	}
	cmds, err := parseCommandsJSON(data)
	if err != nil {
		fmt.Fprintf(os.Stderr, "warning: maestro: could not parse %s: %v; reporting case-level results only\n", commandsFileName, err)
		return
	}
	enrichSuiteWithCommands(suite, cmds)
}
