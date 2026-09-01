package qualflare

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
)

func TestParserFlattensMultipleSuitesIntoOneWrapperSuite(t *testing.T) {
	jsonReport := `
	{
		"framework": "cucumber",
		"suites": [
			{"name": "login.feature", "duration": 1000000000, "cases": [
				{"id": "c1", "name": "logs in", "status": "passed", "duration": 500000000}
			]},
			{"name": "logout.feature", "duration": 2000000000, "cases": [
				{"id": "c2", "name": "logs out", "status": "failed", "duration": 300000000, "error": "boom"}
			]}
		]
	}
	`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if len(suite.Cases) != 2 {
		t.Fatalf("expected 2 flattened cases, got %d", len(suite.Cases))
	}
	// Each case's real originating suite name is preserved via ClassName.
	if suite.Cases[0].ClassName != "login.feature" {
		t.Errorf("expected ClassName 'login.feature', got %q", suite.Cases[0].ClassName)
	}
	if suite.Cases[1].ClassName != "logout.feature" {
		t.Errorf("expected ClassName 'logout.feature', got %q", suite.Cases[1].ClassName)
	}
	if suite.Cases[1].Error != "boom" {
		t.Errorf("expected Error 'boom', got %q", suite.Cases[1].Error)
	}
	// Duration is the sum across every real suite.
	if suite.Duration != 3*time.Second {
		t.Errorf("expected total duration 3s, got %v", suite.Duration)
	}
	// RecomputeCounts derives counts from the flattened cases.
	if suite.Passed != 1 || suite.Failed != 1 {
		t.Errorf("expected 1 passed, 1 failed — got passed=%d failed=%d", suite.Passed, suite.Failed)
	}
}

func TestParserCategoryComesFromTheSourceFrameworkField(t *testing.T) {
	cases := []struct {
		framework string
		want      domain.FrameworkCategory
	}{
		{"cypress", domain.FrameworkCategory(domain.FrameworkCypress)},
		{"cucumber", domain.FrameworkCategory(domain.FrameworkCucumber)},
	}
	for _, c := range cases {
		jsonReport := `{"framework": "` + c.framework + `", "suites": [{"name": "s", "cases": [{"id": "1", "name": "t", "status": "passed"}]}]}`
		parser := New()
		suite, err := parser.Parse(strings.NewReader(jsonReport))
		if err != nil {
			t.Fatalf("parse error for framework %q: %v", c.framework, err)
		}
		if suite.Category != c.want {
			t.Errorf("framework %q: expected category %q, got %q", c.framework, c.want, suite.Category)
		}
	}
}

func TestParserStatusMapping(t *testing.T) {
	cases := []struct {
		wireStatus string
		want       domain.Status
	}{
		{"passed", domain.StatusPassed},
		{"failed", domain.StatusFailed},
		{"timeout", domain.StatusTimeout},
		{"error", domain.StatusError},
		// timeout and aborted pass straight through now that domain.Status
		// carries all seven. They used to fold into failed/error, which
		// discarded a distinction the reporters' own CaseStatus union has
		// always drawn and the server has always been able to store.
		{"aborted", domain.StatusAborted},
		{"skipped", domain.StatusSkipped},
		// STILL deliberately StatusSkipped, and for a semantic reason rather
		// than a missing constant: in Mocha, `pending` is what an it.skip
		// fires, so it MEANS skipped. StatusPending out-ranks passed at the
		// suite level server-side, so taking it literally would flip a green
		// launch pending for one skipped test (BUG-08). Contrast the CTRF
		// parser, where pending is a distinct status and is NOT folded.
		{"pending", domain.StatusSkipped},
		{"some-unrecognized-future-status", domain.StatusError},
	}
	for _, c := range cases {
		jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [{"id": "1", "name": "t", "status": "` + c.wireStatus + `"}]}]}`
		parser := New()
		suite, err := parser.Parse(strings.NewReader(jsonReport))
		if err != nil {
			t.Fatalf("parse error for status %q: %v", c.wireStatus, err)
		}
		if suite.Cases[0].Status != c.want {
			t.Errorf("wire status %q: expected domain status %q, got %q", c.wireStatus, c.want, suite.Cases[0].Status)
		}
	}
}

func TestParserRetryCountAndIsFlakyPassThrough(t *testing.T) {
	jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "flaky", "status": "passed", "retryCount": 2, "isFlaky": true}
	]}]}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	testCase := suite.Cases[0]
	if testCase.RetryCount == nil || *testCase.RetryCount != 2 {
		t.Errorf("expected RetryCount 2, got %v", testCase.RetryCount)
	}
	if testCase.IsFlaky == nil || !*testCase.IsFlaky {
		t.Errorf("expected IsFlaky true, got %v", testCase.IsFlaky)
	}
}

func TestParserResolvesLocalVideoPathToAbsolute(t *testing.T) {
	dir := t.TempDir()
	videoFile := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(videoFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	reportPath := filepath.Join(dir, "report.json")
	jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "t", "status": "passed", "attachments": [
			{"name": "screenshot", "mimeType": "image/png", "content": "aGVsbG8="},
			{"name": "video", "mimeType": "video/mp4", "localVideoPath": "clip.mp4"}
		]}
	]}]}`
	if err := os.WriteFile(reportPath, []byte(jsonReport), 0o600); err != nil {
		t.Fatal(err)
	}

	parser := New()
	suite, err := parser.ParsePath(reportPath)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	attachments := suite.Cases[0].Attachments
	if len(attachments) != 2 {
		t.Fatalf("expected 2 attachments (inline + video), got %d", len(attachments))
	}
	video := attachments[1]
	if video.LocalVideoPath != videoFile {
		t.Errorf("expected LocalVideoPath %q (resolved relative to report.json's directory), got %q", videoFile, video.LocalVideoPath)
	}
	if attachments[0].Content != "aGVsbG8=" {
		t.Errorf("expected inline screenshot to survive unchanged, got %+v", attachments[0])
	}
}

func TestParserResolvesLocalVideoPathToAbsoluteFromRelativeInputPath(t *testing.T) {
	dir := t.TempDir()
	videoFile := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(videoFile, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "t", "status": "passed", "attachments": [
			{"name": "video", "mimeType": "video/mp4", "localVideoPath": "clip.mp4"}
		]}
	]}]}`
	if err := os.WriteFile(filepath.Join(dir, "report.json"), []byte(jsonReport), 0o600); err != nil {
		t.Fatal(err)
	}

	origWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(origWD) })

	// Derive the expected path from the post-chdir cwd rather than from the
	// pre-chdir `dir` string: on macOS, t.TempDir() returns a path under
	// /var/folders/..., but /var is itself a symlink to /private/var, so
	// os.Getwd() (and therefore filepath.Abs, which ParsePath uses) reports
	// the resolved /private/var/folders/... form. Comparing against the
	// unresolved `dir` would fail on a correct implementation.
	resolvedWD, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	wantVideoFile := filepath.Join(resolvedWD, "clip.mp4")

	parser := New()
	// A bare relative filename, matching real CLI usage (qualflare collect
	// report.json) — filepath.Dir("report.json") is "." (still relative),
	// so this is the case filepath.Dir alone can't resolve to absolute.
	suite, err := parser.ParsePath("report.json")
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	video := suite.Cases[0].Attachments[0]
	if !filepath.IsAbs(video.LocalVideoPath) {
		t.Errorf("expected LocalVideoPath to be absolute, got %q", video.LocalVideoPath)
	}
	if video.LocalVideoPath != wantVideoFile {
		t.Errorf("expected LocalVideoPath %q, got %q", wantVideoFile, video.LocalVideoPath)
	}
}

func TestParserReadsShardIndexFromSource(t *testing.T) {
	jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "t", "status": "passed", "shardIndex": 2}
	]}]}`
	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Cases[0].ShardIndex == nil || *suite.Cases[0].ShardIndex != 2 {
		t.Errorf("expected ShardIndex 2, got %v", suite.Cases[0].ShardIndex)
	}
}

func TestParserOmitsShardIndexWhenSourceOmitsIt(t *testing.T) {
	jsonReport := `{"framework": "cypress", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "t", "status": "passed"}
	]}]}`
	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Cases[0].ShardIndex != nil {
		t.Errorf("expected nil ShardIndex, got %v", *suite.Cases[0].ShardIndex)
	}
}

func TestParserMapsSteps(t *testing.T) {
	jsonReport := `{"framework": "cucumber", "suites": [{"name": "s", "cases": [
		{"id": "1", "name": "t", "status": "passed", "steps": [
			{"name": "Given a step", "keyword": "Given", "status": "passed", "duration": 1000000}
		]}
	]}]}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	steps := suite.Cases[0].Steps
	if len(steps) != 1 {
		t.Fatalf("expected 1 step, got %d", len(steps))
	}
	if steps[0].Name != "Given a step" || steps[0].Keyword != "Given" || steps[0].Duration != time.Millisecond {
		t.Errorf("unexpected step: %+v", steps[0])
	}
}

// Fix 3 regression test: browser/platform, set only on the top-level Collect object
// by both reporters (see Collect.Platform/Collect.Browser's doc comment), must survive
// onto the synthetic wrapper Suite's Properties so report_service.go's existing
// promoteConsistentSuiteProperties can promote them to Launch.Properties.
func TestParserCapturesBrowserAndPlatformIntoSuiteProperties(t *testing.T) {
	jsonReport := `{"framework": "cypress", "platform": "web", "browser": "chrome 118", "suites": [
		{"name": "s", "cases": [{"id": "1", "name": "t", "status": "passed"}]}
	]}`
	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Properties["browser"] != "chrome 118" {
		t.Errorf("Properties[browser] = %q, want %q", suite.Properties["browser"], "chrome 118")
	}
	if suite.Properties["platform"] != "web" {
		t.Errorf("Properties[platform] = %q, want %q", suite.Properties["platform"], "web")
	}
}

func TestParserOmitsBrowserAndPlatformPropertiesWhenSourceOmitsThem(t *testing.T) {
	jsonReport := `{"framework": "cypress", "suites": [
		{"name": "s", "cases": [{"id": "1", "name": "t", "status": "passed"}]}
	]}`
	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if _, ok := suite.Properties["browser"]; ok {
		t.Errorf("expected no browser property, got %q", suite.Properties["browser"])
	}
	if _, ok := suite.Properties["platform"]; ok {
		t.Errorf("expected no platform property, got %q", suite.Properties["platform"])
	}
}

func TestParserSatisfiesPathAwareParser(t *testing.T) {
	var _ ports.PathAwareParser = New()
}

func TestParserGetFrameworkAndExtensions(t *testing.T) {
	parser := New()
	if parser.GetFramework() != domain.FrameworkQualflareJSON {
		t.Errorf("expected framework %q, got %q", domain.FrameworkQualflareJSON, parser.GetFramework())
	}
	ext := parser.SupportedFileExtensions()
	if len(ext) != 1 || ext[0] != ".json" {
		t.Errorf("expected [\".json\"], got %v", ext)
	}
}

// Labels and links are written by every @qualflare/* reporter's metadata API
// (qualflare.label()/qualflare.link()) and are accepted by /collect, but this
// parser silently dropped them until now — so a user's epic/story/owner and
// issue links never reached the server from ANY reporter. Regression test for
// the whole chain.
func TestParserPreservesLabelsAndLinks(t *testing.T) {
	jsonReport := `
	{
		"framework": "playwright",
		"metadata": {},
		"suites": [
			{"name": "checkout.spec.ts", "duration": 1000000000, "cases": [
				{"id": "c1", "name": "checks out", "status": "passed", "duration": 500000000,
				 "labels": [
					{"name": "epic", "value": "Billing"},
					{"name": "owner", "value": "payments-team"}
				 ],
				 "links": [
					{"type": "issue", "name": "QF-1", "url": "https://example.com/issue/1"},
					{"type": "tms", "url": "https://example.com/tms/9"}
				 ]}
			]}
		]
	}
	`

	suite, err := New().Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	c := suite.Cases[0]

	if len(c.Labels) != 2 {
		t.Fatalf("expected 2 labels, got %d", len(c.Labels))
	}
	if c.Labels[0].Name != "epic" || c.Labels[0].Value != "Billing" {
		t.Errorf("label[0] = %+v, want {epic Billing}", c.Labels[0])
	}
	if c.Labels[1].Name != "owner" || c.Labels[1].Value != "payments-team" {
		t.Errorf("label[1] = %+v", c.Labels[1])
	}

	if len(c.Links) != 2 {
		t.Fatalf("expected 2 links, got %d", len(c.Links))
	}
	if c.Links[0].Type != "issue" || c.Links[0].Name != "QF-1" || c.Links[0].URL != "https://example.com/issue/1" {
		t.Errorf("link[0] = %+v", c.Links[0])
	}
	// name is optional server-side; an omitted one must not become a phantom value.
	if c.Links[1].Type != "tms" || c.Links[1].Name != "" {
		t.Errorf("link[1] = %+v, want type tms with empty name", c.Links[1])
	}
}

// A case with no labels/links must not gain empty slices — the server treats
// omitempty absence and [] differently in its validators.
func TestParserOmitsLabelsAndLinksWhenSourceOmitsThem(t *testing.T) {
	jsonReport := `
	{"framework": "playwright", "metadata": {}, "suites": [
		{"name": "a.spec.ts", "duration": 1, "cases": [
			{"id": "c1", "name": "t", "status": "passed", "duration": 1}
		]}
	]}
	`
	suite, err := New().Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if suite.Cases[0].Labels != nil {
		t.Errorf("Labels = %v, want nil", suite.Cases[0].Labels)
	}
	if suite.Cases[0].Links != nil {
		t.Errorf("Links = %v, want nil", suite.Cases[0].Links)
	}
}

// Step nesting: /collect accepts Step.ParentIndex (case_run_steps.parent_id,
// migrations 0229/0230) and Step.Parameters, and the reporters emit both — but
// this parser decoded neither, flattening every step tree on the way through.
func TestParserPreservesStepParentIndexAndParameters(t *testing.T) {
	jsonReport := `
	{
		"framework": "playwright",
		"metadata": {},
		"suites": [
			{"name": "a.spec.ts", "duration": 1000000000, "cases": [
				{"id": "c1", "name": "t", "status": "passed", "duration": 1000000,
				 "steps": [
					{"name": "outer", "status": "passed", "duration": 1000000},
					{"name": "inner", "status": "passed", "duration": 500000, "parentIndex": 0,
					 "parameters": [{"name": "user", "value": "alice"}, {"name": "pw", "value": "x", "masked": true}]}
				 ]}
			]}
		]}
	`
	suite, err := New().Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	steps := suite.Cases[0].Steps
	if len(steps) != 2 {
		t.Fatalf("expected 2 steps, got %d", len(steps))
	}
	if steps[0].ParentIndex != nil {
		t.Errorf("root step ParentIndex = %v, want nil", *steps[0].ParentIndex)
	}
	if steps[1].ParentIndex == nil || *steps[1].ParentIndex != 0 {
		t.Fatalf("nested step ParentIndex = %v, want 0", steps[1].ParentIndex)
	}
	if len(steps[1].Parameters) != 2 {
		t.Fatalf("expected 2 parameters, got %d", len(steps[1].Parameters))
	}
	if steps[1].Parameters[0].Name != "user" || steps[1].Parameters[0].Value != "alice" {
		t.Errorf("parameters[0] = %+v", steps[1].Parameters[0])
	}
	if !steps[1].Parameters[1].Masked {
		t.Error("parameters[1].Masked = false, want true")
	}
}
