package cypress

import (
	"testing"

	"qualflare-cli/internal/core/domain"
)

// processSuite was an entirely untested path, and it is the one that recurses — a bug
// here silently drops every test inside a nested describe() block.
func TestProcessSuite_RecursesThroughNestedDescribes(t *testing.T) {
	p := &Parser{}
	suite := Suite{
		Title:    "outer describe",
		FullFile: "cypress/e2e/login.cy.ts",
		Tests:    []Test{{UUID: "t0", Title: "top level", State: "passed"}},
		Suites: []Suite{{
			Title:    "middle describe",
			FullFile: "cypress/e2e/login.cy.ts",
			Tests:    []Test{{UUID: "t1", Title: "nested once", State: "failed"}},
			Suites: []Suite{{
				Title:    "inner describe",
				FullFile: "cypress/e2e/login.cy.ts",
				Tests: []Test{
					{UUID: "t2", Title: "nested twice", State: "passed"},
					{UUID: "t3", Title: "also nested twice", State: "pending"},
				},
			}},
		}},
	}

	dst := &domain.Suite{Cases: []domain.Case{}}
	p.processSuite(suite, dst)

	if len(dst.Cases) != 4 {
		t.Fatalf("cases = %d, want 4 across every nesting level", len(dst.Cases))
	}

	byID := make(map[string]domain.Case, len(dst.Cases))
	for _, c := range dst.Cases {
		byID[c.ID] = c
	}
	for _, id := range []string{"t0", "t1", "t2", "t3"} {
		if _, ok := byID[id]; !ok {
			t.Errorf("case %q was dropped", id)
		}
	}
	// The failure two levels down must survive, not just the count.
	if byID["t1"].Status != domain.StatusFailed {
		t.Errorf("nested failure status = %q, want failed", byID["t1"].Status)
	}
	// Each case is attributed to the file of the suite it came from.
	if byID["t2"].ClassName != "cypress/e2e/login.cy.ts" {
		t.Errorf("ClassName = %q, want the suite's fullFile", byID["t2"].ClassName)
	}
}

func TestProcessSuite_EmptyAndTestlessSuites(t *testing.T) {
	p := &Parser{}
	dst := &domain.Suite{Cases: []domain.Case{}}

	// A describe() with no tests and no children, and one that only wraps children.
	p.processSuite(Suite{Title: "empty"}, dst)
	p.processSuite(Suite{
		Title:  "wrapper only",
		Suites: []Suite{{Title: "child", Tests: []Test{{UUID: "c1", State: "passed"}}}},
	}, dst)

	if len(dst.Cases) != 1 {
		t.Fatalf("cases = %d, want 1 — only the child suite has a test", len(dst.Cases))
	}
	if dst.Cases[0].ID != "c1" {
		t.Errorf("case = %q, want the nested one", dst.Cases[0].ID)
	}
}

// Retry accounting comes from either attempts[] or the retries field, and only a test
// that eventually passed counts as flaky.
func TestConvertTest_RetriesAndFlaky(t *testing.T) {
	p := &Parser{}
	tests := []struct {
		name      string
		test      Test
		wantRetry int
		wantFlaky bool
	}{
		{"no retries", Test{UUID: "a", State: "passed"}, 0, false},
		{
			"attempts imply retries",
			Test{UUID: "a", State: "passed", Attempts: []Attempt{{}, {}, {}}},
			2, true,
		},
		{
			"retries field used when attempts absent",
			Test{UUID: "a", State: "passed", Retries: 3},
			3, true,
		},
		{
			"failed after retries is not flaky",
			Test{UUID: "a", State: "failed", Attempts: []Attempt{{}, {}}},
			1, false,
		},
		{
			"attempts take precedence over the retries field",
			Test{UUID: "a", State: "passed", Attempts: []Attempt{{}, {}}, Retries: 9},
			1, true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.convertTest(tt.test, "f.cy.ts")
			if got.RetryCount == nil || *got.RetryCount != tt.wantRetry {
				t.Errorf("RetryCount = %v, want %d", got.RetryCount, tt.wantRetry)
			}
			if got.IsFlaky == nil || *got.IsFlaky != tt.wantFlaky {
				t.Errorf("IsFlaky = %v, want %v", got.IsFlaky, tt.wantFlaky)
			}
		})
	}
}
