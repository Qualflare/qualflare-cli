package maestro

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

// commandsFixture matches Maestro's own documented shape for commands.json
// (docs.maestro.dev/maestro-flows/workspace-management/test-reports-and-artifacts):
// a JSON array with one entry per executed step, in order, each carrying
// the command, its status, duration, sequenceNumber, and any error. The
// exact shape of the "command" field itself (string vs. a tagged-union
// object per command type) was not verifiable against real output in this
// environment (no connectable iOS/Android/web device), so commandStepName
// tolerates either.
const commandsFixture = `[
	{"command": {"tapOnCommand": {"selector": {"text": "Login"}}}, "status": "COMPLETED", "duration": 120, "sequenceNumber": 1},
	{"command": {"inputTextCommand": {"text": "hello"}}, "status": "COMPLETED", "duration": 45, "sequenceNumber": 2},
	{"command": {"assertVisibleCommand": {"selector": {"text": "Welcome"}}}, "status": "FAILED", "duration": 5000, "sequenceNumber": 3, "error": "element not found: Welcome"}
]`

func TestParseCommandsJSON(t *testing.T) {
	cmds, err := parseCommandsJSON([]byte(commandsFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(cmds) != 3 {
		t.Fatalf("commands = %d, want 3", len(cmds))
	}
	if cmds[2].Status != "FAILED" || cmds[2].Error != "element not found: Welcome" {
		t.Errorf("cmds[2] = %+v, want the failed assertVisible entry", cmds[2])
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
		{"single-key tagged object strips the Command suffix", `{"tapOnCommand":{"selector":{"text":"Login"}}}`, "tapOn"},
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

func TestStepsFromCommands(t *testing.T) {
	cmds, err := parseCommandsJSON([]byte(commandsFixture))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	steps := stepsFromCommands(cmds)
	if len(steps) != 3 {
		t.Fatalf("steps = %d, want 3", len(steps))
	}
	if steps[0].Name != "tapOn" || steps[0].Status != domain.StatusPassed {
		t.Errorf("steps[0] = %+v, want a passed tapOn", steps[0])
	}
	if steps[0].Duration != 120*time.Millisecond {
		t.Errorf("steps[0].Duration = %v, want 120ms", steps[0].Duration)
	}
	if steps[2].Name != "assertVisible" || steps[2].Status != domain.StatusFailed {
		t.Errorf("steps[2] = %+v, want a failed assertVisible", steps[2])
	}
	if steps[2].Error != "element not found: Welcome" {
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
