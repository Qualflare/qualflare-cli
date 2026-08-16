package maestro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

// commandsFixture is modeled directly on real commands.json captured from
// `maestro test` runs against a real Android emulator (both a passing and a
// failing flow) and cross-checked against Maestro's own source
// (mobile-dev-inc/maestro: ArtifactsGenerator.kt, TestOutputWriter.kt,
// FlowDebugOutput.kt, CommandStatus.kt, Env.kt, YamlConfig.kt). Each entry
// is a {command, metadata} wrapper, not a flat object — status/duration/
// sequenceNumber/error live under metadata, not as siblings of command. The
// first two entries (defineVariablesCommand, applyConfigurationCommand) are
// what Maestro always prepends to every flow itself; never something a test
// author wrote.
const commandsFixture = `[
	{
		"command": {"defineVariablesCommand": {"env": {"MAESTRO_FILENAME": "flow"}, "optional": false}},
		"metadata": {"status": "COMPLETED", "timestamp": 1, "duration": 2, "sequenceNumber": 0, "depth": 0}
	},
	{
		"command": {"applyConfigurationCommand": {"config": {"appId": "me.ibrahimsn.app"}, "optional": false}},
		"metadata": {"status": "COMPLETED", "timestamp": 1, "duration": 0, "sequenceNumber": 1, "depth": 0}
	},
	{
		"command": {"launchAppCommand": {"appId": "me.ibrahimsn.app", "optional": false}},
		"metadata": {"status": "COMPLETED", "timestamp": 2, "duration": 397, "sequenceNumber": 2, "depth": 0}
	},
	{
		"command": {"tapOnElement": {"selector": {"textRegex": "Leaderboard"}, "optional": false}},
		"metadata": {"status": "COMPLETED", "timestamp": 3, "duration": 2687, "sequenceNumber": 3, "depth": 0}
	},
	{
		"command": {"assertConditionCommand": {"condition": {"visible": {"textRegex": "Missing Text"}}, "optional": false}},
		"metadata": {
			"status": "FAILED",
			"timestamp": 4,
			"duration": 15696,
			"sequenceNumber": 4,
			"depth": 0,
			"error": {"message": "Assertion is false: \"Missing Text\" is visible", "debugMessage": "Check the UI hierarchy..."}
		}
	}
]`

func TestParseCommandsJSON(t *testing.T) {
	entries, err := parseCommandsJSON([]byte(commandsFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(entries) != 5 {
		t.Fatalf("entries = %d, want 5", len(entries))
	}
	last := entries[4]
	if last.Metadata.Status != "FAILED" || last.Metadata.Error == nil || last.Metadata.Error.Message != `Assertion is false: "Missing Text" is visible` {
		t.Errorf("entries[4] = %+v, want the failed assertCondition entry", last)
	}
}

func TestParseCommandsJSON_MalformedReturnsError(t *testing.T) {
	if _, err := parseCommandsJSON([]byte("not json")); err == nil {
		t.Fatal("expected an error for malformed JSON")
	}
}

func TestCommandStepName(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{"single-key tagged object with a Command suffix strips it", `{"launchAppCommand":{"appId":"x"}}`, "launchApp"},
		{"single-key tagged object without a Command suffix is used as-is", `{"tapOnElement":{"selector":{}}}`, "tapOnElement"},
		{"plain string command", `"tapOn Login"`, "tapOn Login"},
		{"unrecognized shape falls back to a positional label", `{"a":1,"b":2}`, "step 4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := commandStepName(json.RawMessage(tt.raw), 4); got != tt.want {
				t.Errorf("commandStepName(%s) = %q, want %q", tt.raw, got, tt.want)
			}
		})
	}
}

func TestIsSyntheticCommand(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want bool
	}{
		{"defineVariablesCommand is synthetic", `{"defineVariablesCommand":{"env":{}}}`, true},
		{"applyConfigurationCommand is synthetic", `{"applyConfigurationCommand":{"config":{}}}`, true},
		{"a real user step is not synthetic", `{"tapOnElement":{"selector":{}}}`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isSyntheticCommand(json.RawMessage(tt.raw)); got != tt.want {
				t.Errorf("isSyntheticCommand(%s) = %v, want %v", tt.raw, got, tt.want)
			}
		})
	}
}

func TestCommandStatus(t *testing.T) {
	tests := []struct {
		status string
		want   domain.Status
	}{
		{"COMPLETED", domain.StatusPassed},
		{"FAILED", domain.StatusFailed},
		{"SKIPPED", domain.StatusSkipped},
		{"WARNED", domain.StatusError},
		{"PENDING", domain.StatusPending},
		{"RUNNING", domain.StatusPending},
		{"SOME_FUTURE_VALUE", domain.StatusError},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			if got := commandStatus(mCommandMetadata{Status: tt.status}); got != tt.want {
				t.Errorf("commandStatus(%q) = %q, want %q", tt.status, got, tt.want)
			}
		})
	}
}

func TestStepsFromCommands(t *testing.T) {
	entries, err := parseCommandsJSON([]byte(commandsFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	steps := stepsFromCommands(entries)

	// The 2 synthetic entries (defineVariablesCommand, applyConfigurationCommand)
	// Maestro always prepends must not show up as steps.
	if len(steps) != 3 {
		t.Fatalf("steps = %d, want 3 (5 entries minus 2 synthetic)", len(steps))
	}
	if steps[0].Name != "launchApp" || steps[0].Status != domain.StatusPassed {
		t.Errorf("steps[0] = %+v, want a passed launchApp", steps[0])
	}
	if steps[0].Duration != 397*time.Millisecond {
		t.Errorf("steps[0].Duration = %v, want 397ms", steps[0].Duration)
	}
	if steps[1].Name != "tapOnElement" || steps[1].Status != domain.StatusPassed {
		t.Errorf("steps[1] = %+v, want a passed tapOnElement", steps[1])
	}
	if steps[2].Name != "assertCondition" || steps[2].Status != domain.StatusFailed {
		t.Errorf("steps[2] = %+v, want a failed assertCondition", steps[2])
	}
	if steps[2].Error != `Assertion is false: "Missing Text" is visible` {
		t.Errorf("steps[2].Error = %q", steps[2].Error)
	}
}

// enrichWithCommandSteps must only attach commands.json's steps when the
// suite has exactly one case — commands.json carries no per-flow
// correlation key, so a multi-flow suite has no reliable way to attribute
// which commands belong to which case, and guessing would risk attaching
// one flow's steps to a different flow's case (worse than no steps at all).
func TestEnrichWithCommandSteps_SingleCaseGetsSteps(t *testing.T) {
	suite := &domain.Suite{Cases: []domain.Case{{ID: "only"}}}
	cmds, _ := parseCommandsJSON([]byte(commandsFixture))
	enrichSuiteWithCommands(suite, cmds)

	if len(suite.Cases[0].Steps) != 3 {
		t.Fatalf("Steps = %d, want 3", len(suite.Cases[0].Steps))
	}
}

func TestEnrichWithCommandSteps_MultiCaseSuiteIsLeftUnenriched(t *testing.T) {
	suite := &domain.Suite{Cases: []domain.Case{{ID: "a"}, {ID: "b"}}}
	cmds, _ := parseCommandsJSON([]byte(commandsFixture))
	enrichSuiteWithCommands(suite, cmds)

	for _, c := range suite.Cases {
		if len(c.Steps) != 0 {
			t.Errorf("case %q got Steps = %+v, want none — no reliable per-flow correlation for a multi-case suite", c.ID, c.Steps)
		}
	}
}

func TestParsePath_MissingCommandsJSONFallsBackToCaseLevelOnly(t *testing.T) {
	dir := t.TempDir()
	xmlPath := writeFile(t, dir, "report.xml", `<testsuites><testsuite name="S" tests="1"><testcase name="t" classname="C"/></testsuite></testsuites>`)

	p := New()
	suite, err := p.ParsePath(xmlPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	if len(suite.Cases[0].Steps) != 0 {
		t.Errorf("expected no steps when commands.json is absent, got %+v", suite.Cases[0].Steps)
	}
}

func TestParsePath_CommandsJSONPresentEnrichesTheSingleCase(t *testing.T) {
	dir := t.TempDir()
	xmlPath := writeFile(t, dir, "report.xml", `<testsuites><testsuite name="S" tests="1"><testcase name="t" classname="C"/></testsuite></testsuites>`)
	writeFile(t, dir, "commands.json", commandsFixture)

	p := New()
	suite, err := p.ParsePath(xmlPath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(suite.Cases[0].Steps) != 3 {
		t.Fatalf("expected 3 steps merged in from the sibling commands.json, got %d", len(suite.Cases[0].Steps))
	}
}

func TestParsePath_MalformedCommandsJSONFallsBackWithoutErroring(t *testing.T) {
	dir := t.TempDir()
	xmlPath := writeFile(t, dir, "report.xml", `<testsuites><testsuite name="S" tests="1"><testcase name="t" classname="C"/></testsuite></testsuites>`)
	writeFile(t, dir, "commands.json", "not valid json")

	p := New()
	suite, err := p.ParsePath(xmlPath)
	if err != nil {
		t.Fatalf("a malformed commands.json must not fail the whole parse, got: %v", err)
	}
	if len(suite.Cases[0].Steps) != 0 {
		t.Errorf("expected no steps for a malformed sibling file, got %+v", suite.Cases[0].Steps)
	}
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
