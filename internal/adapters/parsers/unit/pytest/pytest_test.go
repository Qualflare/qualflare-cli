package pytest

import (
	"strings"
	"testing"
	"testing/iotest"
	"time"

	"qualflare-cli/internal/core/domain"
)

func TestPytestParserDefaultRetryCount(t *testing.T) {
	xmlReport := `
    <testsuite name="pytest" tests="1">
        <testcase name="test_example" classname="test_module">
        </testcase>
    </testsuite>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) == 0 {
		t.Fatal("expected at least one case")
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected default RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected default IsFlaky nil or false, got true")
	}
}

// BUG-38: the parser read the nonexistent attribute `skips` instead of pytest's
// real `skipped`, so a skipped test was counted as passed (Skipped=0, Passed
// inflated). A skipped case must roll up as skipped, never passed.
func TestPytestParser_SkippedNotCountedAsPassed(t *testing.T) {
	xmlReport := `
    <testsuite name="pytest" tests="2" failures="0" errors="0" skipped="1">
        <testcase name="test_ok" classname="test_module"></testcase>
        <testcase name="test_skip" classname="test_module">
            <skipped message="not applicable"/>
        </testcase>
    </testsuite>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.Skipped != 1 {
		t.Errorf("expected Skipped == 1, got %d", suite.Skipped)
	}
	if suite.Passed != 1 {
		t.Errorf("expected Passed == 1 (not inflated by skipped), got %d", suite.Passed)
	}
	if suite.TotalTests != 2 {
		t.Errorf("expected TotalTests == 2, got %d", suite.TotalTests)
	}
}

func TestPytestParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestPytestParser_MalformedXML(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("<not valid xml"))
	if err == nil {
		t.Error("expected error for malformed XML")
	}
}

// Mechanism C's fallback: a <property name="shard" value="..."/> (as written by a
// pytest-xdist conftest calling record_property("shard", ...)) sets ShardIndex. Mirrors
// junitxml's TestConvertTestCase_ShardPropertyFallback, since this parser is fully
// independent of junitxml and needs its own coverage of the same edge cases.
func TestPytestConvertTestCase_ShardPropertyFallback(t *testing.T) {
	tests := []struct {
		name      string
		props     []Property
		wantShard *int
	}{
		{"valid integer", []Property{{Name: "shard", Value: "2"}}, domain.IntPtr(2)},
		{"non-numeric value", []Property{{Name: "shard", Value: "not-a-number"}}, nil},
		{"empty value", []Property{{Name: "shard", Value: ""}}, nil},
		{"whitespace-padded value", []Property{{Name: "shard", Value: " 5 "}}, domain.IntPtr(5)},
		{"no shard property", []Property{{Name: "browser", Value: "chrome"}}, nil},
		{"no properties at all", nil, nil},
		// Same 32-bit bound as junitxml: an unstorable value is skipped, never emitted.
		{"zero", []Property{{Name: "shard", Value: "0"}}, domain.IntPtr(0)},
		{"negative value", []Property{{Name: "shard", Value: "-5"}}, nil},
		{"int32 max is still accepted", []Property{{Name: "shard", Value: "2147483647"}}, domain.IntPtr(2147483647)},
		{"one past int32 max", []Property{{Name: "shard", Value: "2147483648"}}, nil},
		{"int64 max", []Property{{Name: "shard", Value: "9223372036854775807"}}, nil},
		{"overflows any int", []Property{{Name: "shard", Value: "999999999999999999999999"}}, nil},
	}

	parser := New()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parser.convertTestCase(TestCase{Name: "t", Properties: tt.props})
			switch {
			case tt.wantShard == nil && got.ShardIndex != nil:
				t.Errorf("ShardIndex = %d, want nil", *got.ShardIndex)
			case tt.wantShard != nil && got.ShardIndex == nil:
				t.Errorf("ShardIndex = nil, want %d", *tt.wantShard)
			case tt.wantShard != nil && *got.ShardIndex != *tt.wantShard:
				t.Errorf("ShardIndex = %d, want %d", *got.ShardIndex, *tt.wantShard)
			}
		})
	}
}

// End-to-end coverage through Parse (not just convertTestCase directly) confirms the
// fallback is reachable via real pytest-xdist XML output, matching how record_property
// actually serializes: as a <properties><property> child of <testcase>.
func TestPytestParser_ShardPropertyFallbackViaXML(t *testing.T) {
	xmlReport := `
    <testsuite name="pytest" tests="1">
        <testcase name="test_example" classname="test_module">
            <properties>
                <property name="shard" value="3"/>
            </properties>
        </testcase>
    </testsuite>
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(xmlReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}

	testCase := suite.Cases[0]
	if testCase.ShardIndex == nil {
		t.Fatal("expected ShardIndex to be set, got nil")
	}
	if *testCase.ShardIndex != 3 {
		t.Errorf("ShardIndex = %d, want 3", *testCase.ShardIndex)
	}
	// The raw property must also still reach Properties (this parser's merge loop
	// already did this correctly before the fallback was added).
	if testCase.Properties["shard"] != "3" {
		t.Errorf("Properties[shard] = %q, want %q", testCase.Properties["shard"], "3")
	}
}

// The root element every supported pytest actually emits. Until this was fixed,
// Parse returned "expected element type <testsuite> but have <testsuites>" and
// the whole upload failed -- content detection routes any JUnit-ish XML
// mentioning pytest to this parser, so `pytest --junitxml` could not be
// collected at all. Every other fixture in this file uses a bare <testsuite>
// root, which is precisely why the gap survived.
//
// Captured verbatim from `pytest 9.1.1 --junitxml`, trimmed to two cases.
const realPytestReport = `<?xml version="1.0" encoding="utf-8"?><testsuites name="pytest tests">` +
	`<testsuite name="pytest" errors="0" failures="1" skipped="1" tests="3" time="0.021" ` +
	`timestamp="2026-09-07T08:22:23.157998+03:00" hostname="host">` +
	`<testcase classname="test_a" name="test_passes" time="0.001"/>` +
	`<testcase classname="test_a" name="test_fails" time="0.002">` +
	`<failure message="AssertionError: boom">E       AssertionError: boom</failure>` +
	`</testcase>` +
	`<testcase classname="test_a" name="test_skipped" time="0.000">` +
	`<skipped type="pytest.skip" message="deliberate">/tmp/test_a.py:9: deliberate</skipped>` +
	`</testcase>` +
	`</testsuite></testsuites>`

func TestPytestParser_ParsesTheTestsuitesWrapperRealPytestEmits(t *testing.T) {
	suite, err := New().Parse(strings.NewReader(realPytestReport))
	if err != nil {
		t.Fatalf("parse error on canonical pytest output: %v", err)
	}

	if len(suite.Cases) != 3 {
		t.Fatalf("expected 3 cases, got %d", len(suite.Cases))
	}
	if suite.Passed != 1 || suite.Failed != 1 || suite.Skipped != 1 {
		t.Errorf("expected 1/1/1 passed/failed/skipped, got %d/%d/%d",
			suite.Passed, suite.Failed, suite.Skipped)
	}
	// The inner suite's name, not the wrapper's "pytest tests": reports parsed
	// before the wrapper was understood carried this, and a rename would split
	// an existing project's suite history in two.
	if suite.Name != "pytest" {
		t.Errorf("expected suite name %q, got %q", "pytest", suite.Name)
	}
	if suite.Duration <= 0 {
		t.Errorf("expected a positive duration, got %v", suite.Duration)
	}
}

func TestPytestParser_StillParsesTheBareTestsuiteRootOfOldPytest(t *testing.T) {
	// pytest < 6.0, and every fixture written against it.
	suite, err := New().Parse(strings.NewReader(
		`<testsuite name="pytest" tests="1"><testcase name="test_x" classname="m"/></testsuite>`))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 1 || suite.Name != "pytest" {
		t.Fatalf("expected 1 case in suite %q, got %d in %q", "pytest", len(suite.Cases), suite.Name)
	}
}

func TestPytestParser_KeepsEveryTestsuiteNotOnlyTheFirst(t *testing.T) {
	// A merged report -- several runs concatenated under one wrapper. Decoding
	// only the first <testsuite> loses the rest with no error at all, which is
	// worse than the hard failure above because the launch looks complete.
	report := `<testsuites>` +
		`<testsuite name="first" time="1.0"><testcase classname="a" name="t1"/></testsuite>` +
		`<testsuite name="second" time="2.0"><testcase classname="b" name="t2"/>` +
		`<testcase classname="b" name="t3"/></testsuite>` +
		`</testsuites>`

	suite, err := New().Parse(strings.NewReader(report))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 3 {
		t.Fatalf("expected 3 cases across both suites, got %d", len(suite.Cases))
	}
	if suite.Duration != 3*time.Second {
		t.Errorf("expected durations totalled to 3s, got %v", suite.Duration)
	}
}

func TestPytestParser_KeepsNestedTestsuiteCases(t *testing.T) {
	report := `<testsuites><testsuite name="outer">` +
		`<testcase classname="a" name="t1"/>` +
		`<testsuite name="inner"><testcase classname="b" name="t2"/></testsuite>` +
		`</testsuite></testsuites>`

	suite, err := New().Parse(strings.NewReader(report))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("expected 2 cases including the nested one, got %d", len(suite.Cases))
	}
}

func TestPytestParser_EmptyReportYieldsANonNilCaseSlice(t *testing.T) {
	// A nil slice marshals to `null`, which the server rejects as a missing
	// required field -- 400-ing an entire multi-file upload over one empty run.
	suite, err := New().Parse(strings.NewReader(`<testsuites name="pytest tests"/>`))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Cases == nil {
		t.Fatal("expected a non-nil (empty) case slice")
	}
	if len(suite.Cases) != 0 {
		t.Fatalf("expected 0 cases, got %d", len(suite.Cases))
	}
}

func TestPytestParser_MalformedXMLStillErrors(t *testing.T) {
	// Falling back must not turn a broken file into an empty success.
	if _, err := New().Parse(strings.NewReader(`<testsuites><testsuite`)); err == nil {
		t.Fatal("expected an error for malformed XML, got nil")
	}
}

func TestPytestParser_NonSeekableReaderStillFallsBack(t *testing.T) {
	// The fallback re-reads from a buffer rather than seeking, so a reader that
	// cannot seek does not resume mid-document and fail for a second, wrong
	// reason. iotest.OneByteReader is deliberately not an io.Seeker.
	suite, err := New().Parse(iotest.OneByteReader(strings.NewReader(
		`<testsuite name="pytest"><testcase classname="m" name="test_x"/></testsuite>`)))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
}

// pytest-rerunfailures emits one <testcase> per ATTEMPT, all sharing a classname
// and name. pytest itself counts them as ONE test: the fixture below has nine
// <testcase> elements and declares tests="5". Captured from a real
// pytest 9.1.1 / pytest-rerunfailures 16.6.1 run.
//
// Note what is NOT in it: the failed attempts of test_recovers carry no
// <failure> and no retry property. That information does not exist in the file,
// which is why the count and the flakiness are all that can be recovered here.
const rerunReport = `<?xml version="1.0" encoding="utf-8"?><testsuites name="pytest tests">` +
	`<testsuite name="pytest" errors="0" failures="1" skipped="0" tests="5" time="0.025">` +
	`<testcase classname="test_shapes" name="test_recovers" time="0.001"/>` +
	`<testcase classname="test_shapes" name="test_recovers" time="0.001"/>` +
	`<testcase classname="test_shapes" name="test_recovers" time="0.002"/>` +
	`<testcase classname="test_shapes" name="test_never_recovers" time="0.001"/>` +
	`<testcase classname="test_shapes" name="test_never_recovers" time="0.001"/>` +
	`<testcase classname="test_shapes" name="test_never_recovers" time="0.001">` +
	`<failure message="AssertionError: always fails">E   AssertionError: always fails</failure>` +
	`</testcase>` +
	`<testcase classname="test_shapes" name="test_param[1]" time="0.000"/>` +
	`<testcase classname="test_shapes" name="test_param[2]" time="0.000"/>` +
	`<testcase classname="test_shapes" name="test_plain" time="0.000"/>` +
	`</testsuite></testsuites>`

func findCase(t *testing.T, suite *domain.Suite, name string) domain.Case {
	t.Helper()
	for _, c := range suite.Cases {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no case named %q; have %d cases", name, len(suite.Cases))
	return domain.Case{}
}

func TestPytestParser_CollapsesRerunsToTheCountPytestItselfReports(t *testing.T) {
	suite, err := New().Parse(strings.NewReader(rerunReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	// The fixture's own header says tests="5" over nine <testcase> elements.
	if len(suite.Cases) != 5 {
		t.Fatalf("expected the 5 cases pytest reports, got %d", len(suite.Cases))
	}
	if suite.Passed != 4 || suite.Failed != 1 {
		t.Errorf("expected 4 passed / 1 failed, got %d / %d", suite.Passed, suite.Failed)
	}
}

func TestPytestParser_ARecoveredRerunIsFlaky(t *testing.T) {
	suite, err := New().Parse(strings.NewReader(rerunReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	c := findCase(t, suite, "test_recovers")
	if c.Status != domain.StatusPassed {
		t.Errorf("expected the final attempt's status (passed), got %s", c.Status)
	}
	if c.RetryCount == nil || *c.RetryCount != 2 {
		t.Fatalf("expected RetryCount 2 (three attempts), got %v", c.RetryCount)
	}
	if c.IsFlaky == nil || !*c.IsFlaky {
		t.Errorf("a test that failed twice and then passed is flaky; got %v", c.IsFlaky)
	}
}

func TestPytestParser_ATestStillFailingAfterRetriesIsNotFlaky(t *testing.T) {
	// Failing after retries is failing, however many attempts it took. Mirrors
	// junitxml, which leaves IsFlaky unset unless the case ended green.
	suite, err := New().Parse(strings.NewReader(rerunReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	c := findCase(t, suite, "test_never_recovers")
	if c.Status != domain.StatusFailed {
		t.Errorf("expected failed, got %s", c.Status)
	}
	if c.RetryCount == nil || *c.RetryCount != 2 {
		t.Fatalf("expected RetryCount 2, got %v", c.RetryCount)
	}
	if c.IsFlaky != nil && *c.IsFlaky {
		t.Errorf("a still-failing test is not flaky")
	}
	// The outcome comes from the LAST attempt, which is the one carrying the
	// failure -- taking the first would report this test as passing.
	if c.Error == "" {
		t.Error("expected the final attempt's failure text to survive")
	}
}

func TestPytestParser_ATestThatRanOnceIsUntouched(t *testing.T) {
	suite, err := New().Parse(strings.NewReader(rerunReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	c := findCase(t, suite, "test_plain")
	if c.RetryCount != nil {
		t.Errorf("expected no RetryCount on a test that ran once, got %v", *c.RetryCount)
	}
	if c.IsFlaky != nil {
		t.Errorf("expected no IsFlaky on a test that ran once, got %v", *c.IsFlaky)
	}
}

func TestPytestParser_ParametrisedCasesAreNotMerged(t *testing.T) {
	// pytest gives them distinct names, so they must survive as separate cases.
	suite, err := New().Parse(strings.NewReader(rerunReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	findCase(t, suite, "test_param[1]")
	findCase(t, suite, "test_param[2]")
}

func TestPytestParser_OnlyAdjacentDuplicatesAreMerged(t *testing.T) {
	// Attempts are always adjacent, in serial and under xdist alike. Two
	// same-named cases separated by another test are not a rerun, so merging
	// them would be inventing a retry that never happened.
	report := `<testsuites><testsuite name="pytest">` +
		`<testcase classname="m" name="test_a" time="0.001"/>` +
		`<testcase classname="m" name="test_b" time="0.001"/>` +
		`<testcase classname="m" name="test_a" time="0.001"/>` +
		`</testsuite></testsuites>`

	suite, err := New().Parse(strings.NewReader(report))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 3 {
		t.Fatalf("expected 3 cases, got %d", len(suite.Cases))
	}
	for _, c := range suite.Cases {
		if c.RetryCount != nil {
			t.Errorf("case %q should carry no RetryCount, got %v", c.Name, *c.RetryCount)
		}
	}
}

func TestPytestParser_SameNameInDifferentClassesIsNotMerged(t *testing.T) {
	report := `<testsuites><testsuite name="pytest">` +
		`<testcase classname="mod_a" name="test_x" time="0.001"/>` +
		`<testcase classname="mod_b" name="test_x" time="0.001"/>` +
		`</testsuite></testsuites>`

	suite, err := New().Parse(strings.NewReader(report))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("expected 2 cases from different classes, got %d", len(suite.Cases))
	}
}

func TestPytestParser_NamelessCasesAreNeverMerged(t *testing.T) {
	// Two anonymous cases are not "the same test"; collapsing them on the
	// strength of both being nameless would lose a real result.
	report := `<testsuites><testsuite name="pytest">` +
		`<testcase classname="m" time="0.001"/>` +
		`<testcase classname="m" time="0.001"/>` +
		`</testsuite></testsuites>`

	suite, err := New().Parse(strings.NewReader(report))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 2 {
		t.Fatalf("expected 2 nameless cases to survive, got %d", len(suite.Cases))
	}
}
