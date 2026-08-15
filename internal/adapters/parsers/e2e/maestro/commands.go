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

// mCommand is one entry in Maestro's commands.json, per Maestro's own docs
// (docs.maestro.dev/maestro-flows/workspace-management/test-reports-and-artifacts):
// "A JSON array with one entry per executed step, in order. Each carries
// the command, its status, duration, sequenceNumber, and any error, plus
// an artifacts list naming the files that step produced."
//
// UNVERIFIED: the exact JSON shape of the "command" field itself (a plain
// string vs. Maestro's internal tagged-union object per command type) and
// the exact status vocabulary were not confirmed against real output — this
// environment had no connectable iOS/Android/web device to run a real
// Maestro flow against (a Maestro CLI is installed here and its own
// --help text independently confirms commands.json and the JUnit XML/manifest
// live in the same --test-output-dir, which is what the sibling-file lookup
// below relies on). commandStepName and the status mapping are written to
// tolerate more than one plausible shape rather than assume one.
type mCommand struct {
	Command json.RawMessage `json:"command"`
	Status  string          `json:"status"`
	// Duration's unit is unverified — assumed milliseconds, matching every
	// other "duration"-named integer field already parsed elsewhere in this
	// codebase (e.g. Playwright's).
	Duration       int64  `json:"duration"`
	SequenceNumber int    `json:"sequenceNumber"`
	Error          string `json:"error,omitempty"`
}

func parseCommandsJSON(data []byte) ([]mCommand, error) {
	var cmds []mCommand
	if err := json.Unmarshal(data, &cmds); err != nil {
		return nil, err
	}
	return cmds, nil
}

// commandStepName derives a readable step name from a command entry's raw
// "command" JSON: a plain string is used as-is; a single-key object (the
// common "tagged union per command type" JSON pattern, e.g.
// {"tapOnCommand": {...}}) uses that key with a trailing "Command" suffix
// stripped; anything else falls back to a positional label so a step is
// never silently dropped just because its shape wasn't recognized.
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

// commandStatus maps Maestro's status text to domain.Status, tolerating
// more than one plausible vocabulary (unverified — see mCommand) rather
// than assuming an exact set of strings: the presence of an error message
// is the more reliable failure signal.
func commandStatus(c mCommand) domain.Status {
	switch strings.ToUpper(c.Status) {
	case "FAILED", "ERROR":
		return domain.StatusFailed
	case "SKIPPED", "PENDING", "NOT_EXECUTED":
		return domain.StatusSkipped
	}
	if c.Error != "" {
		return domain.StatusFailed
	}
	return domain.StatusPassed
}

func stepsFromCommands(cmds []mCommand) []domain.Step {
	steps := make([]domain.Step, len(cmds))
	for i, c := range cmds {
		steps[i] = domain.Step{
			Name:     commandStepName(c.Command, i+1),
			Status:   commandStatus(c),
			Error:    c.Error,
			Duration: time.Duration(c.Duration) * time.Millisecond,
		}
	}
	return steps
}

// enrichSuiteWithCommands attaches commands.json's steps to the suite's
// case, but only when the suite has exactly one case. commands.json (per
// its documented shape) carries no per-flow correlation key, so a
// multi-flow/multi-case suite has no reliable way to attribute which
// commands belong to which case — attaching them all to every case, or
// guessing, risks attributing one flow's steps to a different flow's case,
// which is worse than reporting no steps at all.
func enrichSuiteWithCommands(suite *domain.Suite, cmds []mCommand) {
	if len(suite.Cases) != 1 {
		return
	}
	suite.Cases[0].Steps = stepsFromCommands(cmds)
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
