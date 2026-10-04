package flutter

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

func parseFixture(t *testing.T, name string) *domain.Suite {
	t.Helper()
	f, err := os.Open("testdata/" + name)
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	suite, err := New().Parse(f)
	if err != nil {
		t.Fatalf("parse %s: %v", name, err)
	}
	return suite
}

func parseString(t *testing.T, input string) *domain.Suite {
	t.Helper()
	suite, err := New().Parse(strings.NewReader(input))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return suite
}

func readFixture(t *testing.T, name string) string {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return string(b)
}

func byID(suite *domain.Suite) map[string]domain.Case {
	m := make(map[string]domain.Case, len(suite.Cases))
	for _, c := range suite.Cases {
		m[c.ID] = c
	}
	return m
}

func mustCase(t *testing.T, cases map[string]domain.Case, id string) domain.Case {
	t.Helper()
	c, ok := cases[id]
	if !ok {
		ids := make([]string, 0, len(cases))
		for k := range cases {
			ids = append(ids, k)
		}
		t.Fatalf("case %q missing; have %q", id, ids)
	}
	return c
}

const (
	idSetUpAll  = "test/setup_all_failure_test.dart#with a broken setUpAll (setUpAll)"
	idBroken    = "test/broken_test.dart#loading test/broken_test.dart"
	idTitle     = "test/widget_cases_test.dart#login screen shows a title"
	idFails     = "test/widget_cases_test.dart#login screen fails an expectation"
	idThrows    = "test/widget_cases_test.dart#login screen throws an error"
	idSkipped   = "test/widget_cases_test.dart#login screen is skipped"
	idPrints    = "test/widget_cases_test.dart#login screen prints output"
	idThirdTry  = "test/widget_cases_test.dart#login screen passes on the third attempt"
	wantVisible = 8
)

func assertWidgetCases(t *testing.T, suite *domain.Suite, wantMachineDuration bool) {
	t.Helper()
	cases := byID(suite)

	c := mustCase(t, cases, idSetUpAll)
	if c.Status != domain.StatusError || !strings.Contains(c.Error, "Bad state: setUpAll exploded") {
		t.Errorf("setUpAll case: status=%s error=%q", c.Status, c.Error)
	}

	c = mustCase(t, cases, idBroken)
	if c.Status != domain.StatusError || !strings.Contains(c.Error, "Compilation failed") {
		t.Errorf("loading case: status=%s error=%q", c.Status, c.Error)
	}
	if c.ClassName != "test/broken_test.dart" || c.Name != "loading test/broken_test.dart" {
		t.Errorf("loading case naming: name=%q class=%q", c.Name, c.ClassName)
	}

	c = mustCase(t, cases, idTitle)
	// 380ms is the machine capture's; the file reporter's run took a different time.
	if c.Status != domain.StatusPassed || c.Duration <= 0 || (wantMachineDuration && c.Duration != 380*time.Millisecond) {
		t.Errorf("title case: status=%s duration=%s", c.Status, c.Duration)
	}
	if c.ClassName != "test/widget_cases_test.dart" || c.Properties["file"] != "test/widget_cases_test.dart" {
		t.Errorf("title case file: class=%q file=%q", c.ClassName, c.Properties["file"])
	}
	if c.Properties["line"] != "8" {
		t.Errorf("title case line = %q, want 8 (root_line, not the flutter_test line)", c.Properties["line"])
	}

	mustCase(t, cases, idFails) // its status is refined with exception-block parsing

	if c = mustCase(t, cases, idThrows); c.Status != domain.StatusError {
		t.Errorf("throws case status = %s, want error", c.Status)
	}
	if c = mustCase(t, cases, idSkipped); c.Status != domain.StatusSkipped {
		t.Errorf("skipped case status = %s, want skipped", c.Status)
	}

	c = mustCase(t, cases, idPrints)
	if c.Status != domain.StatusPassed || c.Properties["system-out"] != "hello from a widget test" {
		t.Errorf("prints case: status=%s system-out=%q", c.Status, c.Properties["system-out"])
	}

	if c = mustCase(t, cases, idThirdTry); c.Status != domain.StatusPassed {
		t.Errorf("third-attempt case status = %s, want passed", c.Status)
	}

	if len(suite.Cases) != wantVisible {
		t.Errorf("got %d cases, want %d", len(suite.Cases), wantVisible)
	}
	for _, c := range suite.Cases {
		if strings.HasPrefix(c.Name, "loading") && c.ID != idBroken {
			t.Errorf("hidden loading case leaked: %q", c.ID)
		}
		if strings.HasSuffix(c.Name, "(tearDownAll)") {
			t.Errorf("hidden tearDownAll case leaked: %q", c.ID)
		}
	}
	if suite.Properties["platform"] != "vm" {
		t.Errorf("platform = %q, want vm", suite.Properties["platform"])
	}
}

func TestWidgetCapture_Cases(t *testing.T) {
	assertWidgetCases(t, parseFixture(t, "widget-machine.jsonl"), true)
}

func TestFileReporterCapture_SameCasesAsMachine(t *testing.T) {
	assertWidgetCases(t, parseFixture(t, "widget-file-reporter.jsonl"), false)

	machine := parseFixture(t, "widget-machine.jsonl")
	file := parseFixture(t, "widget-file-reporter.jsonl")
	if len(machine.Cases) != len(file.Cases) {
		t.Fatalf("case counts differ: machine %d, file reporter %d", len(machine.Cases), len(file.Cases))
	}
	fileCases := byID(file)
	for _, m := range machine.Cases {
		f := mustCase(t, fileCases, m.ID)
		if f.Status != m.Status {
			t.Errorf("%s: machine %s, file reporter %s", m.ID, m.Status, f.Status)
		}
	}
}

func TestDeviceCaptures_AndroidAndIOS(t *testing.T) {
	// device-ios.jsonl carries ~11,400 lines of -v noise around the same events.
	for _, name := range []string{"device-android.jsonl", "device-ios.jsonl"} {
		t.Run(name, func(t *testing.T) {
			suite := parseFixture(t, name)
			if len(suite.Cases) != 2 {
				t.Fatalf("got %d cases, want 2", len(suite.Cases))
			}
			cases := byID(suite)
			if c := mustCase(t, cases, "integration_test/app_test.dart#counter app starts at zero"); c.Status != domain.StatusPassed {
				t.Errorf("starts at zero status = %s", c.Status)
			}
			if c := mustCase(t, cases, "integration_test/app_test.dart#counter app fails on device"); c.Status == domain.StatusPassed {
				t.Errorf("fails on device should not pass")
			}
			for _, c := range suite.Cases {
				if c.ClassName != "integration_test/app_test.dart" {
					t.Errorf("%s ClassName = %q", c.ID, c.ClassName)
				}
			}
			if suite.Properties["platform"] != "vm" {
				t.Errorf("platform = %q, want vm", suite.Properties["platform"])
			}
		})
	}
}

// Synthetic: no capture comes from a Windows runner. A native suite.path and a
// file:///C:/ root_url must resolve to the same relative path.
func TestRelativePaths_Windows(t *testing.T) {
	root := projectRoot([]string{`C:\x\app\test\a_test.dart`})
	if root != "C:/x/app" {
		t.Fatalf("projectRoot = %q", root)
	}
	for _, p := range []string{`C:\x\app\test\a_test.dart`, "file:///C:/x/app/test/a_test.dart"} {
		if got := relativePath(p, root); got != "test/a_test.dart" {
			t.Errorf("relativePath(%q) = %q, want test/a_test.dart", p, got)
		}
	}
}

func TestRelativePaths_LinuxAndMacRunners(t *testing.T) {
	paths := []string{
		"/home/runner/work/x/x/app/test/a_test.dart",
		"/Users/runner/work/x/x/app/integration_test/b_test.dart",
	}
	// Each runner's path on its own, then one runner's test and integration_test mix.
	for path, want := range map[string]string{
		paths[0]: "test/a_test.dart",
		paths[1]: "integration_test/b_test.dart",
	} {
		root := projectRoot([]string{path})
		if got := relativePath(path, root); got != want {
			t.Errorf("relativePath(%q) = %q, want %q", path, got, want)
		}
	}

	mixed := []string{
		"/home/runner/work/x/x/app/test/a_test.dart",
		"/home/runner/work/x/x/app/integration_test/b_test.dart",
	}
	root := projectRoot(mixed)
	if root != "/home/runner/work/x/x/app" {
		t.Errorf("projectRoot(mixed) = %q", root)
	}
	if got := relativePath("file://"+mixed[1], root); got != "integration_test/b_test.dart" {
		t.Errorf("file:// path relative = %q", got)
	}
	if got := relativePath("/elsewhere/other.dart", root); got != "other.dart" {
		t.Errorf("path outside root = %q, want base name", got)
	}
	if got := relativePath("/no/segment/here.dart", ""); got != "here.dart" {
		t.Errorf("no root = %q, want base name", got)
	}
}

// This input is synthetic: widget-machine.jsonl cut right after the testStart
// of test id 16, so the run "ended" mid-test.
func TestTruncatedStream_UnfinishedTestIsError(t *testing.T) {
	lines := strings.Split(readFixture(t, "widget-machine.jsonl"), "\n")
	cut := -1
	for i, l := range lines {
		if strings.Contains(l, `"type":"testStart"`) && strings.Contains(l, `"id":16,`) {
			cut = i
			break
		}
	}
	if cut < 0 {
		t.Fatal("testStart id 16 not found in capture")
	}
	suite := parseString(t, strings.Join(lines[:cut+1], "\n"))

	c := mustCase(t, byID(suite), idPrints)
	if c.Status != domain.StatusError {
		t.Errorf("unfinished test status = %s, want error", c.Status)
	}
	want := "test did not finish (the run ended before this test reported a result)"
	if !strings.Contains(c.Error, want) {
		t.Errorf("unfinished test error = %q, want it to contain %q", c.Error, want)
	}
}

// This input is synthetic: the file-reporter capture appended to itself, as two
// invocations writing to one file would.
func TestTwoRunsInOneFile_BothCounted(t *testing.T) {
	one := readFixture(t, "widget-file-reporter.jsonl")
	single := parseString(t, one)
	double := parseString(t, one+"\n"+one)
	if len(double.Cases) != 2*len(single.Cases) {
		t.Fatalf("got %d cases, want %d", len(double.Cases), 2*len(single.Cases))
	}
	if double.Passed != 2*single.Passed {
		t.Errorf("passed = %d, want %d", double.Passed, 2*single.Passed)
	}
}

// This input is synthetic: 50,000 print events for one test.
func TestPrintOutputIsClamped(t *testing.T) {
	var b strings.Builder
	b.WriteString(`{"protocolVersion":"0.1.1","runnerVersion":null,"pid":1,"type":"start","time":0}` + "\n")
	b.WriteString(`{"suite":{"id":0,"platform":"vm","path":"/p/app/test/noisy_test.dart"},"type":"suite","time":0}` + "\n")
	b.WriteString(`{"test":{"id":1,"name":"noisy","suiteID":0,"groupIDs":[],"metadata":{"skip":false,"skipReason":null},"line":3,"column":1,"url":"file:///p/app/test/noisy_test.dart"},"type":"testStart","time":1}` + "\n")
	for i := 0; i < 50000; i++ {
		fmt.Fprintf(&b, `{"testID":1,"messageType":"print","message":"line number %d of a very chatty test","type":"print","time":2}`+"\n", i)
	}
	b.WriteString(`{"testID":1,"result":"success","skipped":false,"hidden":false,"type":"testDone","time":3}` + "\n")
	b.WriteString(`{"success":true,"type":"done","time":4}` + "\n")

	suite := parseString(t, b.String())
	out := mustCase(t, byID(suite), "test/noisy_test.dart#noisy").Properties["system-out"]
	if out == "" {
		t.Fatal("system-out is empty")
	}
	if n := len(strings.Split(out, "\n")); n > base.MaxOutputLines {
		t.Errorf("system-out has %d lines, limit %d", n, base.MaxOutputLines)
	}
	if n := len([]rune(out)); n > base.MaxOutputRunes+base.MaxOutputLines {
		t.Errorf("system-out has %d runes, limit %d (plus separators)", n, base.MaxOutputRunes)
	}
}

func TestSkipReasonAndNonEvents(t *testing.T) {
	// Synthetic: a skipped test with a reason, surrounded by lines that are not events.
	input := strings.Join([]string{
		``,
		`Running Gradle task...`,
		`[{"event":"test.startedProcess","params":{}}]`,
		`{"no":"type"}`,
		`{"type":7}`,
		`{"protocolVersion":"0.1.1","type":"start","time":0}`,
		`{"suite":{"id":0,"platform":"vm","path":"/p/app/test/s_test.dart"},"type":"suite","time":0}`,
		`{"test":{"id":1,"name":"later","suiteID":0,"groupIDs":[],"metadata":{"skip":true,"skipReason":"flaky on CI"},"line":9,"column":1,"url":"file:///p/app/test/s_test.dart"},"type":"testStart","time":5}`,
		`{"testID":1,"result":"success","skipped":true,"hidden":false,"type":"testDone","time":5}`,
	}, "\n")
	c := mustCase(t, byID(parseString(t, input)), "test/s_test.dart#later")
	if c.Status != domain.StatusSkipped || c.Error != "flaky on CI" {
		t.Errorf("skip case: status=%s error=%q", c.Status, c.Error)
	}
}

func TestParse_NoEvents(t *testing.T) {
	if _, err := New().Parse(strings.NewReader("just some build output\n")); err == nil {
		t.Error("expected an error when the input holds no Flutter events")
	}
}

func TestParserMetadata(t *testing.T) {
	p := New()
	if p.GetFramework() != domain.FrameworkFlutter {
		t.Errorf("GetFramework = %s", p.GetFramework())
	}
	exts := p.SupportedFileExtensions()
	if len(exts) != 2 || exts[0] != ".json" || exts[1] != ".jsonl" {
		t.Errorf("SupportedFileExtensions = %v", exts)
	}
}

func TestWidgetExpectFailure_IsFailedWithRealMessage(t *testing.T) {
	c := mustCase(t, byID(parseFixture(t, "widget-machine.jsonl")), idFails)
	if c.Status != domain.StatusFailed {
		t.Errorf("status = %s, want failed", c.Status)
	}
	if !strings.HasPrefix(c.Error, "Expected: exactly one matching candidate") {
		t.Errorf("error = %q", c.Error)
	}
	if !strings.Contains(c.Error, "widget_cases_test.dart:15:7") {
		t.Errorf("error lacks the user's stack frame: %q", c.Error)
	}
	if strings.Contains(c.Error, "See exception logs above") {
		t.Errorf("generic message leaked into error: %q", c.Error)
	}
	if strings.Contains(c.Properties["system-out"], "EXCEPTION CAUGHT") {
		t.Errorf("exception block left in system-out: %q", c.Properties["system-out"])
	}
}

func TestWidgetThrow_IsErrorWithKind(t *testing.T) {
	c := mustCase(t, byID(parseFixture(t, "widget-machine.jsonl")), idThrows)
	if c.Status != domain.StatusError {
		t.Errorf("status = %s, want error", c.Status)
	}
	if !strings.HasPrefix(c.Error, "StateError: Bad state: boom") {
		t.Errorf("error = %q", c.Error)
	}
}

func TestDeviceFailure_IsFailed(t *testing.T) {
	for _, name := range []string{"device-android.jsonl", "device-ios.jsonl"} {
		c := mustCase(t, byID(parseFixture(t, name)), "integration_test/app_test.dart#counter app fails on device")
		if c.Status != domain.StatusFailed {
			t.Errorf("%s: status = %s, want failed", name, c.Status)
		}
		if !strings.HasPrefix(c.Error, "Expected: exactly one matching candidate") {
			t.Errorf("%s: error = %q", name, c.Error)
		}
	}
}

func TestRetriedTest_PerAttemptHistory(t *testing.T) {
	c := mustCase(t, byID(parseFixture(t, "widget-machine.jsonl")), idThirdTry)
	if len(c.Attempts) != 3 {
		t.Fatalf("got %d attempts, want 3", len(c.Attempts))
	}
	for i, want := range []string{"Actual: <1>", "Actual: <2>"} {
		a := c.Attempts[i]
		if a.Number != i+1 || a.Status != domain.StatusFailed ||
			!strings.Contains(a.Message, "Expected: <3>") || !strings.Contains(a.Message, want) {
			t.Errorf("attempt %d = %+v", i, a)
		}
	}
	if a := c.Attempts[2]; a.Number != 3 || a.Status != domain.StatusPassed {
		t.Errorf("attempt 3 = %+v", a)
	}
	if c.RetryCount == nil || *c.RetryCount != 2 || c.IsFlaky == nil || !*c.IsFlaky {
		t.Errorf("retryCount=%v isFlaky=%v", c.RetryCount, c.IsFlaky)
	}
	if c.Status != domain.StatusPassed || c.Error != "" {
		t.Errorf("status=%s error=%q", c.Status, c.Error)
	}
	if strings.Contains(c.Properties["system-out"], "Retry:") {
		t.Errorf("Retry line in system-out: %q", c.Properties["system-out"])
	}
}

// captureBlock returns the exception block the widget capture printed for the
// failing-expectation test, as one message.
func captureBlock(t *testing.T) string {
	t.Helper()
	for _, l := range strings.Split(readFixture(t, "widget-machine.jsonl"), "\n") {
		var ev event
		if json.Unmarshal([]byte(l), &ev) == nil && ev.TestID == 13 && strings.HasPrefix(ev.Message, exceptionHeader) {
			return ev.Message
		}
	}
	t.Fatal("capture has no exception block")
	return ""
}

func jsonl(t *testing.T, events ...map[string]any) string {
	t.Helper()
	var b strings.Builder
	for _, e := range events {
		raw, err := json.Marshal(e)
		if err != nil {
			t.Fatal(err)
		}
		b.Write(raw)
		b.WriteByte('\n')
	}
	return b.String()
}

// Synthetic: no capture has a widget test that fails, retries and fails again.
// The blocks are the capture's own text, the second with a different expectation.
func TestRetriedWidgetTest_BlocksPerAttempt(t *testing.T) {
	block1 := captureBlock(t)
	block2 := strings.Replace(block1, "Sign out", "Log out", 1)
	if block1 == block2 {
		t.Fatal("capture block changed shape")
	}
	const name = "flaky widget"
	suite := parseString(t, jsonl(t,
		map[string]any{"type": "start", "time": 0},
		map[string]any{"type": "suite", "suite": map[string]any{"id": 0, "platform": "vm", "path": "test/a_test.dart"}},
		map[string]any{"type": "testStart", "time": 1, "test": map[string]any{"id": 1, "name": name, "suiteID": 0, "metadata": map[string]any{}}},
		map[string]any{"type": "print", "testID": 1, "messageType": "print", "message": block1},
		map[string]any{"type": "error", "testID": 1, "error": genericFailure, "stackTrace": ""},
		map[string]any{"type": "print", "testID": 1, "messageType": "print", "message": "Retry: " + name},
		map[string]any{"type": "print", "testID": 1, "messageType": "print", "message": block2},
		map[string]any{"type": "error", "testID": 1, "error": genericFailure, "stackTrace": ""},
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "error"},
	))
	if len(suite.Cases) != 1 {
		t.Fatalf("got %d cases", len(suite.Cases))
	}
	c := suite.Cases[0]
	if len(c.Attempts) != 2 || c.Status != domain.StatusFailed {
		t.Fatalf("attempts=%d status=%s", len(c.Attempts), c.Status)
	}
	if !strings.Contains(c.Attempts[0].Message, `"Sign out"`) || !strings.Contains(c.Attempts[1].Message, `"Log out"`) {
		t.Errorf("attempt messages: %q / %q", c.Attempts[0].Message, c.Attempts[1].Message)
	}
	for i, a := range c.Attempts {
		if a.Status != domain.StatusFailed || strings.Contains(a.Message, "See exception logs") {
			t.Errorf("attempt %d = %+v", i, a)
		}
	}
	if c.RetryCount == nil || *c.RetryCount != 1 || c.IsFlaky == nil || *c.IsFlaky {
		t.Errorf("retryCount=%v isFlaky=%v", c.RetryCount, c.IsFlaky)
	}
	if !strings.Contains(c.Error, `"Log out"`) || strings.Contains(c.Error, `"Sign out"`) {
		t.Errorf("case error should describe the final attempt only: %q", c.Error)
	}
	if out := c.Properties["system-out"]; out != "" {
		t.Errorf("system-out = %q, want empty", out)
	}
}

// Synthetic: no capture has a widget error with no exception block.
func TestWidgetErrorWithoutBlock_KeepsGenericMessage(t *testing.T) {
	suite := parseString(t, jsonl(t,
		map[string]any{"type": "start", "time": 0},
		map[string]any{"type": "suite", "suite": map[string]any{"id": 0, "platform": "vm", "path": "test/a_test.dart"}},
		map[string]any{"type": "testStart", "time": 1, "test": map[string]any{"id": 1, "name": "no block", "suiteID": 0, "metadata": map[string]any{}}},
		map[string]any{"type": "error", "testID": 1, "error": genericFailure + "\nThe test description was: no block", "stackTrace": ""},
		map[string]any{"type": "testDone", "time": 5, "testID": 1, "result": "error"},
	))
	c := suite.Cases[0]
	if c.Status != domain.StatusError || !strings.Contains(c.Error, "Test failed") {
		t.Errorf("status=%s error=%q", c.Status, c.Error)
	}
}

// Once flutter is in domain.AllFrameworks, the suite's category is the
// framework itself (which the server accepts since api-service migration 0296),
// not the "generic" fallback.
func TestParse_SuiteCategoryIsFlutter(t *testing.T) {
	suite := parseFixture(t, "widget-machine.jsonl")
	if string(suite.Category) != "flutter" {
		t.Errorf("Suite.Category = %q, want %q", suite.Category, "flutter")
	}
}
