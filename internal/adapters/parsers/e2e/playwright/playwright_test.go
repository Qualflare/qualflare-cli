package playwright

import (
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

// TestPlaywrightParser_FractionalStatsDurationIsAccepted uses the exact
// stats.duration value from a real `playwright test --reporter=json` run
// (2921.147): Playwright's own reporter emits a sub-millisecond float here
// (likely performance.now()-derived), unlike per-test/per-step durations,
// which are always whole milliseconds in real output.
func TestPlaywrightParser_FractionalStatsDurationIsAccepted(t *testing.T) {
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [],
        "stats": {"duration": 2921.147}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Duration != 2921*time.Millisecond {
		t.Errorf("suite.Duration = %v, want 2921ms", suite.Duration)
	}
}

func TestPlaywrightParserExtractsRetryCount(t *testing.T) {
	// Playwright JSON with retries (test fails twice, passes on 3rd try)
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "line": 1,
            "specs": [{
                "title": "flaky test",
                "tests": [{
                    "results": [
                        {"status": "failed", "duration": 1000, "retry": 0},
                        {"status": "failed", "duration": 1000, "retry": 1},
                        {"status": "passed", "duration": 1000, "retry": 2}
                    ],
                    "status": "passed"
                }]
            }]
        }],
        "stats": {"flaky": 1}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount == nil || *testCase.RetryCount != 2 {
		t.Errorf("expected RetryCount 2, got %v", testCase.RetryCount)
	}
	if testCase.IsFlaky == nil || !*testCase.IsFlaky {
		t.Errorf("expected IsFlaky true, got false")
	}
}

// Multi-browser projects were joined straight from ranging a map[string]bool
// with no sorting — Go's randomized map iteration order meant the exact same
// input could produce "chromium, firefox" on one parse and "firefox,
// chromium" on the next. That silent nondeterminism defeats
// promoteConsistentSuiteProperties's exact-string equality check in
// report_service.go: two suites from an identical multi-browser run could
// spuriously "disagree", dropping Launch.Properties["browser"] at random.
func TestPlaywrightParser_MultiBrowserPropertyIsDeterministicallySorted(t *testing.T) {
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [
                {"title": "t1", "tests": [{"projectName": "webkit", "results": [{"status": "passed"}]}]},
                {"title": "t2", "tests": [{"projectName": "chromium", "results": [{"status": "passed"}]}]},
                {"title": "t3", "tests": [{"projectName": "firefox", "results": [{"status": "passed"}]}]}
            ]
        }]
    }
    `

	want := "chromium, firefox, webkit"
	for i := 0; i < 50; i++ {
		parser := New()
		suite, err := parser.Parse(strings.NewReader(jsonReport))
		if err != nil {
			t.Fatalf("parse error: %v", err)
		}
		if got := suite.Properties["browser"]; got != want {
			t.Fatalf("run %d: Properties[browser] = %q, want %q (sorted, deterministic)", i, got, want)
		}
	}
}

func TestPlaywrightParserNoRetries(t *testing.T) {
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [{
                "title": "stable test",
                "tests": [{
                    "results": [
                        {"status": "passed", "duration": 1000, "retry": 0}
                    ],
                    "status": "passed"
                }]
            }]
        }],
        "stats": {"flaky": 0}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount != nil && *testCase.RetryCount != 0 {
		t.Errorf("expected RetryCount nil or 0, got %d", *testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected IsFlaky nil or false, got true")
	}
}

func TestPlaywrightParserFailedAfterRetries(t *testing.T) {
	// Test that fails even after retries - should not be marked as flaky
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [{
                "title": "failing test",
                "tests": [{
                    "results": [
                        {"status": "failed", "duration": 1000, "retry": 0},
                        {"status": "failed", "duration": 1000, "retry": 1}
                    ],
                    "status": "failed"
                }]
            }]
        }],
        "stats": {"flaky": 0}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	testCase := suite.Cases[0]
	if testCase.RetryCount == nil || *testCase.RetryCount != 1 {
		t.Errorf("expected RetryCount 1, got %v", testCase.RetryCount)
	}
	if testCase.IsFlaky != nil && *testCase.IsFlaky {
		t.Errorf("expected IsFlaky nil or false for failed test, got true")
	}
}

func TestPlaywrightParserInterruptedStatusIsAborted(t *testing.T) {
	// CLI-H4: Playwright emits "interrupted" on --max-failures / SIGINT. It fell
	// through the status switch's `default` and was uploaded as PASSED — an
	// interrupted (aborted) test must surface as an error, never a green pass.
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [{
                "title": "interrupted test",
                "tests": [{
                    "results": [
                        {"status": "interrupted", "duration": 500, "retry": 0}
                    ],
                    "status": "unexpected"
                }]
            }]
        }],
        "stats": {}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	if got := suite.Cases[0].Status; got != domain.StatusAborted {
		t.Errorf("expected StatusAborted for interrupted result, got %q", got)
	}
	if suite.Errors != 1 {
		t.Errorf("expected suite.Errors 1, got %d", suite.Errors)
	}
	if suite.Passed != 0 {
		t.Errorf("expected suite.Passed 0, got %d", suite.Passed)
	}
}

func TestPlaywrightParserExpectedFailureIsPassed(t *testing.T) {
	// BUG-09: A test.fail() test has expectedStatus "failed"; when it actually
	// fails (result status == expectedStatus) that is the designed outcome and
	// must be recorded as PASSED (preserving the message), not a real failure.
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "specs": [{
                "title": "known-broken feature",
                "ok": true,
                "tests": [{
                    "expectedStatus": "failed",
                    "results": [
                        {"status": "failed", "duration": 800, "retry": 0,
                         "error": {"message": "boom", "stack": "at x"}}
                    ],
                    "status": "expected"
                }]
            }]
        }],
        "stats": {}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	testCase := suite.Cases[0]
	if testCase.Status != domain.StatusPassed {
		t.Errorf("expected StatusPassed for expected-failure, got %q", testCase.Status)
	}
	if testCase.Error == "" {
		t.Error("expected the failure message to be preserved, got empty Error")
	}
	if suite.Passed != 1 {
		t.Errorf("expected suite.Passed 1, got %d", suite.Passed)
	}
	if suite.Failed != 0 {
		t.Errorf("expected suite.Failed 0, got %d", suite.Failed)
	}
}

func TestPlaywrightParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestPlaywrightParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid json"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}

func TestPlaywrightParserExtractsSteps(t *testing.T) {
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "line": 1,
            "specs": [{
                "title": "logs in",
                "tests": [{
                    "results": [{
                        "status": "passed",
                        "duration": 500,
                        "steps": [
                            {"title": "navigate to login", "duration": 100},
                            {"title": "submit credentials", "duration": 200,
                             "steps": [
                                {"title": "fill username", "duration": 50}
                             ]}
                        ]
                    }],
                    "status": "passed"
                }]
            }]
        }],
        "stats": {}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}

	steps := suite.Cases[0].Steps
	// Matches real `playwright test --reporter=json` shape: no "category"
	// field, and the nested "fill username" becomes a sibling entry.
	if len(steps) != 3 {
		t.Fatalf("expected 3 step entries, got %d: %+v", len(steps), steps)
	}
	if steps[0].Name != "navigate to login" || steps[0].Status != domain.StatusPassed {
		t.Errorf("unexpected first step: %+v", steps[0])
	}
	if steps[1].Name != "submit credentials" {
		t.Errorf("expected second step 'submit credentials', got %+v", steps[1])
	}
	if steps[2].Name != "fill username" {
		t.Errorf("expected nested step 'fill username' flattened as a sibling, got %+v", steps[2])
	}
}

func TestPlaywrightParserStepFailureCarriesError(t *testing.T) {
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "line": 1,
            "specs": [{
                "title": "logs in",
                "tests": [{
                    "results": [{
                        "status": "failed",
                        "duration": 500,
                        "steps": [
                            {"title": "submit credentials", "duration": 200,
                             "error": {"message": "locator not found", "stack": "at foo.ts:1:1"}}
                        ]
                    }],
                    "status": "unexpected"
                }]
            }]
        }],
        "stats": {}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	steps := suite.Cases[0].Steps
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Status != domain.StatusFailed {
		t.Errorf("expected failed step status, got %v", steps[0].Status)
	}
	if !strings.Contains(steps[0].Error, "locator not found") {
		t.Errorf("expected step error to carry the failure message, got %q", steps[0].Error)
	}
}

func TestPlaywrightParserNoStepsProducesEmptySteps(t *testing.T) {
	jsonReport := `
    {
        "config": {"configFile": "playwright.config.ts"},
        "suites": [{
            "title": "Example Suite",
            "file": "example.spec.ts",
            "line": 1,
            "specs": [{
                "title": "no step() calls",
                "tests": [{
                    "results": [{"status": "passed", "duration": 100}],
                    "status": "passed"
                }]
            }]
        }],
        "stats": {}
    }
    `

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases[0].Steps) != 0 {
		t.Errorf("a test with no test.step() calls must produce no steps, got %+v", suite.Cases[0].Steps)
	}
}
