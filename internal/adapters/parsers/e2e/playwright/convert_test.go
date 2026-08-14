package playwright

import (
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

func testWithResult(r Result) Test { return Test{Results: []Result{r}} }

// CLI-H4 is the contract here: every Playwright result status maps to a domain status,
// and anything unrecognised must land fail-visible rather than rolling up green.
func TestConvertTest_ResultStatusArms(t *testing.T) {
	p := &Parser{}
	tests := []struct {
		name   string
		status string
		want   domain.Status
	}{
		{"passed", "passed", domain.StatusPassed},
		{"failed", "failed", domain.StatusFailed},
		{"timedOut is a failure", "timedOut", domain.StatusFailed},
		{"skipped", "skipped", domain.StatusSkipped},
		// Emitted on --max-failures / SIGINT: an aborted test, not a pass.
		{"interrupted is an error", "interrupted", domain.StatusError},
		{"unknown status is an error", "somethingNew", domain.StatusError},
		{"empty status is an error", "", domain.StatusError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.convertTest(Spec{Title: "spec"}, testWithResult(Result{Status: tt.status}), "a.spec.ts", "")
			if got.Status != tt.want {
				t.Errorf("status %q -> %q, want %q", tt.status, got.Status, tt.want)
			}
		})
	}
}

// With no results at all, the test-level status is used — same fail-visible rule.
func TestConvertTest_TestLevelStatusArms(t *testing.T) {
	p := &Parser{}
	tests := []struct {
		status string
		want   domain.Status
	}{
		{"expected", domain.StatusPassed},
		{"unexpected", domain.StatusFailed},
		{"skipped", domain.StatusSkipped},
		{"flaky", domain.StatusError},
		{"", domain.StatusError},
	}
	for _, tt := range tests {
		t.Run(tt.status, func(t *testing.T) {
			got := p.convertTest(Spec{Title: "spec"}, Test{Status: tt.status}, "a.spec.ts", "")
			if got.Status != tt.want {
				t.Errorf("test-level status %q -> %q, want %q", tt.status, got.Status, tt.want)
			}
			// No results means no retry information at all.
			if got.RetryCount != nil {
				t.Errorf("RetryCount = %d, want nil when there are no results", *got.RetryCount)
			}
		})
	}
}

// BUG-09: test.fail() marks an expected failure. When the observed result matches
// ExpectedStatus the test behaved as designed, so it records as passed — but only then.
func TestConvertTest_ExpectedFailure(t *testing.T) {
	p := &Parser{}

	t.Run("matching expected status passes", func(t *testing.T) {
		test := testWithResult(Result{Status: "failed", Error: &TestError{Message: "boom"}})
		test.ExpectedStatus = "failed"
		got := p.convertTest(Spec{Title: "spec"}, test, "a.spec.ts", "")
		if got.Status != domain.StatusPassed {
			t.Errorf("status = %q, want passed for a matched expected failure", got.Status)
		}
		// The captured message is preserved even though the case now reads as passed.
		if !strings.Contains(got.Error, "boom") {
			t.Errorf("Error = %q, want the failure message preserved", got.Error)
		}
	})

	t.Run("mismatched expected status stays failed", func(t *testing.T) {
		test := testWithResult(Result{Status: "failed"})
		test.ExpectedStatus = "timedOut"
		got := p.convertTest(Spec{Title: "spec"}, test, "a.spec.ts", "")
		if got.Status != domain.StatusFailed {
			t.Errorf("status = %q, want failed when the result does not match ExpectedStatus", got.Status)
		}
	})

	t.Run("does not rescue a non-failed status", func(t *testing.T) {
		// interrupted maps to error, and the BUG-09 override only applies to failures.
		test := testWithResult(Result{Status: "interrupted"})
		test.ExpectedStatus = "interrupted"
		got := p.convertTest(Spec{Title: "spec"}, test, "a.spec.ts", "")
		if got.Status != domain.StatusError {
			t.Errorf("status = %q, want error — the override must not rescue an abort", got.Status)
		}
	})

	t.Run("empty ExpectedStatus never overrides", func(t *testing.T) {
		got := p.convertTest(Spec{Title: "spec"}, testWithResult(Result{Status: "failed"}), "a.spec.ts", "")
		if got.Status != domain.StatusFailed {
			t.Errorf("status = %q, want failed", got.Status)
		}
	})
}

func TestConvertTest_RetriesAndFlaky(t *testing.T) {
	p := &Parser{}
	tests := []struct {
		name       string
		results    []Result
		wantRetry  int
		wantFlaky  bool
		wantStatus domain.Status
	}{
		{"single pass", []Result{{Status: "passed"}}, 0, false, domain.StatusPassed},
		{
			"passed after one retry is flaky",
			[]Result{{Status: "failed"}, {Status: "passed"}},
			1, true, domain.StatusPassed,
		},
		{
			"still failing after retries is not flaky",
			[]Result{{Status: "failed"}, {Status: "failed"}},
			1, false, domain.StatusFailed,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.convertTest(Spec{Title: "spec"}, Test{Results: tt.results}, "a.spec.ts", "")
			if got.RetryCount == nil || *got.RetryCount != tt.wantRetry {
				t.Errorf("RetryCount = %v, want %d", got.RetryCount, tt.wantRetry)
			}
			if got.IsFlaky == nil || *got.IsFlaky != tt.wantFlaky {
				t.Errorf("IsFlaky = %v, want %v", got.IsFlaky, tt.wantFlaky)
			}
			if got.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q", got.Status, tt.wantStatus)
			}
		})
	}
}

// The last result is the authoritative one — duration comes from it, not the first try.
func TestConvertTest_UsesLastResult(t *testing.T) {
	p := &Parser{}
	got := p.convertTest(Spec{Title: "spec"}, Test{Results: []Result{
		{Status: "failed", Duration: 100, WorkerIndex: domain.IntPtr(0)},
		{Status: "passed", Duration: 250, WorkerIndex: domain.IntPtr(3)},
	}}, "a.spec.ts", "")

	if got.Duration != 250*time.Millisecond {
		t.Errorf("Duration = %v, want 250ms from the last result", got.Duration)
	}
	if got.Status != domain.StatusPassed {
		t.Errorf("Status = %q, want the last result's status", got.Status)
	}
	// WorkerIndex, like Duration/Status, comes from the last (post-retry) result.
	if got.ShardIndex == nil || *got.ShardIndex != 3 {
		t.Errorf("ShardIndex = %v, want 3 from the last result's WorkerIndex", got.ShardIndex)
	}
}

// Mechanism A: workerIndex becomes ShardIndex — but ONLY when the report actually
// carried the key. ShardIndex == nil is the project-wide encoding of "this report has no
// shard data", so a report that never mentioned workerIndex must not come out claiming
// worker 0: that would be indistinguishable from a genuine shard 0, and it would attach
// a spurious shardIndex to every case of every non-sharded Playwright report.
func TestConvertTest_ShardIndexFromWorkerIndex(t *testing.T) {
	p := &Parser{}
	tests := []struct {
		name string
		in   *int
		want *int
	}{
		{"absent workerIndex leaves ShardIndex nil", nil, nil},
		{"explicit worker 0 is a real shard 0", domain.IntPtr(0), domain.IntPtr(0)},
		{"non-zero worker index", domain.IntPtr(4), domain.IntPtr(4)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := p.convertTest(Spec{Title: "spec"},
				testWithResult(Result{Status: "passed", WorkerIndex: tt.in}), "a.spec.ts", "")
			switch {
			case tt.want == nil && got.ShardIndex != nil:
				t.Errorf("ShardIndex = %d, want nil", *got.ShardIndex)
			case tt.want != nil && got.ShardIndex == nil:
				t.Errorf("ShardIndex = nil, want %d", *tt.want)
			case tt.want != nil && *got.ShardIndex != *tt.want:
				t.Errorf("ShardIndex = %d, want %d", *got.ShardIndex, *tt.want)
			}
		})
	}
}

// The same contract through the real JSON decoder, which is where the regression lived:
// a plain int field cannot tell an absent key from an explicit 0.
func TestParse_ShardIndexOnlyWhenWorkerIndexPresent(t *testing.T) {
	report := func(result string) string {
		return `{"suites":[{"title":"S","file":"a.spec.ts","specs":[{"title":"t","tests":[{"results":[` +
			result + `]}]}]}],"stats":{}}`
	}
	tests := []struct {
		name   string
		result string
		want   *int
	}{
		// Every Playwright report written before this field was wired up, plus any
		// hand-trimmed or merged blob, looks like this.
		{"no workerIndex key at all", `{"status":"passed","duration":10}`, nil},
		{"explicit null", `{"status":"passed","workerIndex":null}`, nil},
		{"explicit 0", `{"status":"passed","workerIndex":0}`, domain.IntPtr(0)},
		{"explicit 2", `{"status":"passed","workerIndex":2}`, domain.IntPtr(2)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			suite, err := New().Parse(strings.NewReader(report(tt.result)))
			if err != nil {
				t.Fatalf("Parse() = %v", err)
			}
			if len(suite.Cases) != 1 {
				t.Fatalf("cases = %d, want 1", len(suite.Cases))
			}
			got := suite.Cases[0].ShardIndex
			switch {
			case tt.want == nil && got != nil:
				t.Errorf("ShardIndex = %d, want nil — no shard data was reported", *got)
			case tt.want != nil && got == nil:
				t.Errorf("ShardIndex = nil, want %d", *tt.want)
			case tt.want != nil && *got != *tt.want:
				t.Errorf("ShardIndex = %d, want %d", *got, *tt.want)
			}
		})
	}
}

// A valid RFC3339 StartTime on the last result flows into StartedAt.
func TestConvertTest_StartedAt(t *testing.T) {
	p := &Parser{}

	t.Run("valid RFC3339 StartTime is parsed", func(t *testing.T) {
		got := p.convertTest(Spec{Title: "spec"}, testWithResult(Result{
			Status:    "passed",
			StartTime: "2026-01-15T10:30:00.000Z",
		}), "a.spec.ts", "")

		want := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
		if got.StartedAt == nil || !got.StartedAt.Equal(want) {
			t.Errorf("StartedAt = %v, want %v", got.StartedAt, want)
		}
	})

	t.Run("missing StartTime leaves StartedAt nil", func(t *testing.T) {
		got := p.convertTest(Spec{Title: "spec"}, testWithResult(Result{Status: "passed"}), "a.spec.ts", "")
		if got.StartedAt != nil {
			t.Errorf("StartedAt = %v, want nil when StartTime is missing", got.StartedAt)
		}
	})

	t.Run("malformed StartTime leaves StartedAt nil without erroring", func(t *testing.T) {
		got := p.convertTest(Spec{Title: "spec"}, testWithResult(Result{
			Status:    "passed",
			StartTime: "not-a-timestamp",
		}), "a.spec.ts", "")
		if got.StartedAt != nil {
			t.Errorf("StartedAt = %v, want nil for a malformed StartTime", got.StartedAt)
		}
		// The rest of the case must still convert normally — a bad StartTime must not
		// error the whole parse.
		if got.Status != domain.StatusPassed {
			t.Errorf("Status = %q, want passed even though StartTime was malformed", got.Status)
		}
	})
}

func TestConvertTest_NamingAndMetadata(t *testing.T) {
	p := &Parser{}
	spec := Spec{Title: "shows the dashboard", Tags: []string{"@smoke"}}
	test := Test{
		ProjectName: "chromium",
		Annotations: []Annotation{{Type: "slow"}, {Type: ""}, {Type: "fixme"}},
		Results:     []Result{{Status: "passed"}},
	}

	got := p.convertTest(spec, test, "e2e/dash.spec.ts", "Dashboard > Admin")

	if got.ID != "Dashboard > Admin > shows the dashboard" {
		t.Errorf("ID = %q, want the prefix joined with the spec title", got.ID)
	}
	if got.Name != "shows the dashboard" {
		t.Errorf("Name = %q, want the bare spec title", got.Name)
	}
	if got.ClassName != "e2e/dash.spec.ts" {
		t.Errorf("ClassName = %q, want the file", got.ClassName)
	}
	if got.Properties["file"] != "e2e/dash.spec.ts" || got.Properties["project"] != "chromium" {
		t.Errorf("Properties = %v", got.Properties)
	}
	// Spec tags survive and non-empty annotation types are appended; empty ones dropped.
	joined := strings.Join(got.Tags, ",")
	for _, want := range []string{"@smoke", "slow", "fixme"} {
		if !strings.Contains(joined, want) {
			t.Errorf("Tags = %v, missing %q", got.Tags, want)
		}
	}
	if len(got.Tags) != 3 {
		t.Errorf("Tags = %v, want the empty annotation type dropped", got.Tags)
	}

	// With no prefix the ID is just the spec title.
	bare := p.convertTest(spec, test, "f.ts", "")
	if bare.ID != "shows the dashboard" {
		t.Errorf("ID = %q, want the spec title when there is no prefix", bare.ID)
	}
}

func TestConvertTest_Attachments(t *testing.T) {
	p := &Parser{}
	got := p.convertTest(Spec{Title: "spec"}, testWithResult(Result{
		Status: "failed",
		Attachments: []Attachment{
			{Name: "screenshot", ContentType: "image/png", Path: "/tmp/s.png"},
			{Name: "trace", ContentType: "application/zip", Path: "/tmp/t.zip"},
		},
	}), "a.spec.ts", "")

	if len(got.Attachments) != 2 {
		t.Fatalf("Attachments = %d, want 2", len(got.Attachments))
	}
	if got.Attachments[0].Name != "screenshot" || got.Attachments[0].MimeType != "image/png" ||
		got.Attachments[0].Path != "/tmp/s.png" {
		t.Errorf("attachment[0] = %+v", got.Attachments[0])
	}
	// contentType maps to MimeType, not a passthrough field name.
	if none := p.convertTest(Spec{Title: "s"}, testWithResult(Result{Status: "passed"}), "f.ts", ""); none.Attachments != nil {
		t.Errorf("Attachments = %v, want nil when there are none", none.Attachments)
	}
}

// processSuites walks nested suites, builds the " > "-joined prefix, collects the
// browser set, and accumulates retries across every test it finds.
func TestProcessSuites_NestingAndAggregation(t *testing.T) {
	p := &Parser{}
	suites := []Suite{{
		Title: "Root",
		File:  "root.spec.ts",
		Specs: []Spec{{Title: "top", Tests: []Test{{
			ProjectName: "chromium",
			Results:     []Result{{Status: "failed"}, {Status: "passed"}}, // one retry
		}}}},
		Suites: []Suite{{
			Title: "Nested",
			File:  "nested.spec.ts",
			Specs: []Spec{{Title: "inner", Tests: []Test{{
				ProjectName: "firefox",
				Results:     []Result{{Status: "passed"}},
			}}}},
			Suites: []Suite{{
				Title: "Deep",
				File:  "deep.spec.ts",
				Specs: []Spec{{Title: "deepest", Tests: []Test{{
					ProjectName: "webkit",
					Results:     []Result{{Status: "failed"}, {Status: "failed"}, {Status: "failed"}}, // two retries
				}}}},
			}},
		}},
	}}

	dst := &domain.Suite{Cases: []domain.Case{}}
	browsers := map[string]bool{}
	p.processSuites(suites, dst, "", browsers)

	if len(dst.Cases) != 3 {
		t.Fatalf("cases = %d, want 3 across all nesting levels", len(dst.Cases))
	}
	// Retries accumulate: 1 from the top spec, 0 from the middle, 2 from the deepest.
	if dst.Retries != 3 {
		t.Errorf("Retries = %d, want 3", dst.Retries)
	}
	for _, b := range []string{"chromium", "firefox", "webkit"} {
		if !browsers[b] {
			t.Errorf("browser %q was not collected", b)
		}
	}

	ids := make(map[string]bool, len(dst.Cases))
	for _, c := range dst.Cases {
		ids[c.ID] = true
	}
	for _, want := range []string{
		"Root > top",
		"Root > Nested > inner",
		"Root > Nested > Deep > deepest",
	} {
		if !ids[want] {
			t.Errorf("missing case %q; got %v", want, ids)
		}
	}
}

func TestProcessSuites_EmptyInputs(t *testing.T) {
	p := &Parser{}
	dst := &domain.Suite{Cases: []domain.Case{}}
	browsers := map[string]bool{}

	p.processSuites(nil, dst, "", browsers)
	p.processSuites([]Suite{{Title: "empty"}}, dst, "", browsers)

	if len(dst.Cases) != 0 {
		t.Errorf("cases = %d, want 0", len(dst.Cases))
	}
	if len(browsers) != 0 {
		t.Errorf("browsers = %v, want none", browsers)
	}
}
