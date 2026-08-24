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
		{"timeout", domain.StatusFailed},
		{"error", domain.StatusError},
		{"aborted", domain.StatusError},
		{"skipped", domain.StatusSkipped},
		// Deliberately StatusSkipped, not StatusPending — see mapStatus's
		// doc comment (mirrors cypress.go's BUG-08 fix: StatusPending
		// out-ranks passed at the suite level server-side).
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
