package flutter

import (
	"strings"
	"testing"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

func syntheticRun(t *testing.T, suitePaths []string, testName string, testMeta map[string]any, events ...map[string]any) *domain.Suite {
	t.Helper()
	evs := make([]map[string]any, 0, len(suitePaths)+len(events)+2)
	evs = append(evs, map[string]any{"type": "start", "time": 0})
	for i, p := range suitePaths {
		evs = append(evs, map[string]any{"type": "suite", "suite": map[string]any{"id": i, "platform": "vm", "path": p}})
	}
	test := map[string]any{"id": 1, "name": testName, "suiteID": 0, "metadata": map[string]any{}}
	for k, v := range testMeta {
		test[k] = v
	}
	evs = append(evs, map[string]any{"type": "testStart", "time": 1, "test": test})
	evs = append(evs, events...)
	return parseString(t, jsonl(t, evs...))
}

// Synthetic: the capture's block with its header and kind line renamed the way
// a layout error (RenderFlex overflow) is reported.
func TestExceptionBlock_AnyCatchingLibrary(t *testing.T) {
	block := captureBlock(t)
	block = strings.Replace(block, "FLUTTER TEST FRAMEWORK", "RENDERING LIBRARY", 1)
	block = strings.Replace(block, "The following TestFailure was thrown running a test:", "The following assertion was thrown during layout:", 1)
	suite := syntheticRun(t, []string{"test/a_test.dart"}, "overflow", nil,
		map[string]any{"type": "print", "testID": 1, "messageType": "print", "message": block},
		map[string]any{"type": "error", "testID": 1, "error": genericFailure, "stackTrace": ""},
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "error"},
	)
	c := suite.Cases[0]
	if c.Status != domain.StatusError {
		t.Errorf("status = %s, want error", c.Status)
	}
	if !strings.Contains(c.Error, "assertion") || !strings.Contains(c.Error, "exactly one matching candidate") {
		t.Errorf("error = %q", c.Error)
	}
	if strings.Contains(c.Properties["system-out"], "EXCEPTION CAUGHT") {
		t.Errorf("block left in system-out: %q", c.Properties["system-out"])
	}
}

// Synthetic suite paths: two packages, and a stray path with no test segment.
func TestRelativePath_NoSharedRoot(t *testing.T) {
	a, b := "/r/packages/a/test/widget_test.dart", "/r/packages/b/test/widget_test.dart"
	root := projectRoot([]string{a, b})
	for _, p := range []string{a, b} {
		if got := relativePath(p, root); got != "test/widget_test.dart" {
			t.Errorf("relativePath(%s) = %q", p, got)
		}
	}
	root = projectRoot([]string{a, "/x/y/other.dart"})
	if got := relativePath(a, root); got != "test/widget_test.dart" {
		t.Errorf("with stray suite: relativePath(a) = %q", got)
	}
	if got := relativePath("/x/y/other.dart", root); got != "other.dart" {
		t.Errorf("stray = %q", got)
	}
	if got := relativePath("/r/p/integration_test/x/test/y_test.dart", ""); got != "test/y_test.dart" {
		t.Errorf("last segment = %q", got)
	}
}

// Synthetic: an exception block that never closes, followed by a flood of prints.
func TestUnterminatedBlock_ErrorIsCapped(t *testing.T) {
	block := captureBlock(t)
	if i := strings.Index(block, "\n════"); i > 0 {
		block = block[:i]
	}
	evs := []map[string]any{{"type": "print", "testID": 1, "messageType": "print", "message": block}}
	for i := 0; i < 100000; i++ {
		evs = append(evs, map[string]any{"type": "print", "testID": 1, "messageType": "print", "message": "flood line of output that is long enough to matter"})
	}
	evs = append(evs, map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "error"})
	suite := syntheticRun(t, []string{"test/a_test.dart"}, "flood", nil, evs...)
	c := suite.Cases[0]
	if len(c.Error) > base.MaxAttemptMessageRunes+base.MaxAttemptTraceRunes+16 {
		t.Errorf("Error is %d bytes", len(c.Error))
	}
}

// Synthetic: one huge error event, with a retry so attempts are populated.
func TestHugeError_CappedOnCaseAndAttempts(t *testing.T) {
	huge := strings.Repeat("m", 100000)
	suite := syntheticRun(t, []string{"test/a_test.dart"}, "huge", nil,
		map[string]any{"type": "error", "testID": 1, "error": huge, "stackTrace": strings.Repeat("s", 100000), "isFailure": true},
		map[string]any{"type": "print", "testID": 1, "messageType": "print", "message": "Retry: huge"},
		map[string]any{"type": "error", "testID": 1, "error": huge, "stackTrace": strings.Repeat("s", 100000), "isFailure": true},
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "failure"},
	)
	c := suite.Cases[0]
	if len(c.Error) > base.MaxAttemptMessageRunes+base.MaxAttemptTraceRunes+16 {
		t.Errorf("Error is %d bytes", len(c.Error))
	}
	for i, a := range c.Attempts {
		if len(a.Message) > base.MaxAttemptMessageRunes || len(a.Trace) > base.MaxAttemptTraceRunes {
			t.Errorf("attempt %d: message %d trace %d", i, len(a.Message), len(a.Trace))
		}
	}
}

// Synthetic: a user test whose name merely starts with "loading ".
func TestLoadingPrefixRename_OnlyForLoadPseudoTests(t *testing.T) {
	const name = "loading data/user state"
	suite := syntheticRun(t, []string{"/p/test/a_test.dart"}, name,
		map[string]any{"url": "file:///p/test/a_test.dart", "line": 3},
		map[string]any{"type": "error", "testID": 1, "error": "boom", "stackTrace": "", "isFailure": true},
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "failure"},
	)
	c := suite.Cases[0]
	if c.Name != name || c.ID != "test/a_test.dart#"+name {
		t.Errorf("name=%q id=%q", c.Name, c.ID)
	}
}
