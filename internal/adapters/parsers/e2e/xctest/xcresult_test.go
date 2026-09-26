package xctest

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"qualflare-cli/internal/core/domain"
)

// testsFixture is real output (trimmed) captured from
// `xcresulttool get test-results tests --path Spike.xcresult --compact`
// against an actual .xcresult bundle built for this feature (one passing,
// one failing XCTest test) — not a guessed shape.
const testsFixture = `{
	"testNodes": [{
		"name": "QfSpike-Package",
		"nodeType": "Test Plan",
		"result": "Failed",
		"children": [{
			"name": "QfSpikeTests",
			"nodeType": "Unit test bundle",
			"result": "Failed",
			"children": [{
				"name": "QfSpikeTests",
				"nodeType": "Test Suite",
				"result": "Failed",
				"children": [
					{
						"name": "testAddFails()",
						"nodeIdentifier": "QfSpikeTests/testAddFails()",
						"nodeType": "Test Case",
						"result": "Failed",
						"durationInSeconds": 0.5228029489517212,
						"children": [{
							"name": "QfSpikeTests.swift:11: XCTAssertEqual failed: (\"4\") is not equal to (\"5\") - intentional failure for the spike",
							"nodeType": "Failure Message"
						}]
					},
					{
						"name": "testAddPasses()",
						"nodeIdentifier": "QfSpikeTests/testAddPasses()",
						"nodeType": "Test Case",
						"result": "Passed",
						"durationInSeconds": 0.000889897346496582
					}
				]
			}]
		}]
	}]
}`

// activitiesFixture (passing test) is real output captured from
// `xcresulttool get test-results activities --path Spike.xcresult
// --test-id "QfSpikeTests/testAddPasses()" --compact` against the same
// bundle — the test wrapped its assertion in
// `XCTContext.runActivity(named: "Adding two numbers")`.
const activitiesFixturePassing = `{
	"testIdentifier": "QfSpikeTests/testAddPasses()",
	"testName": "testAddPasses()",
	"testRuns": [{
		"activities": [{
			"title": "Adding two numbers",
			"startTime": 1786804419.93,
			"isAssociatedWithFailure": false
		}]
	}]
}`

// activitiesFixtureFailing is the same capture for testAddFails().
const activitiesFixtureFailing = `{
	"testIdentifier": "QfSpikeTests/testAddFails()",
	"testName": "testAddFails()",
	"testRuns": [{
		"activities": [{
			"title": "XCTAssertEqual failed: (\"4\") is not equal to (\"5\") - intentional failure for the spike",
			"startTime": 1786804419.408,
			"isAssociatedWithFailure": true,
			"childActivities": [{
				"title": "QfSpikeTests.testAddFails()",
				"isAssociatedWithFailure": false
			}]
		}]
	}]
}`

func TestBuildCasesFromTestNodes(t *testing.T) {
	var resp testsResponse
	if err := decodeXCResultJSON([]byte(testsFixture), &resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	cases := buildCasesFromTestNodes(resp.TestNodes)
	if len(cases) != 2 {
		t.Fatalf("cases = %d, want 2 (only Test Case nodes, not the Plan/bundle/Suite ancestors)", len(cases))
	}

	byID := map[string]domain.Case{}
	for _, c := range cases {
		byID[c.ID] = c
	}

	failed, ok := byID["QfSpikeTests/testAddFails()"]
	if !ok {
		t.Fatal("missing testAddFails() case")
	}
	if failed.Status != domain.StatusFailed {
		t.Errorf("Status = %q, want failed", failed.Status)
	}
	if !strings.Contains(failed.Error, "XCTAssertEqual failed") {
		t.Errorf("Error = %q, want the Failure Message child's text", failed.Error)
	}

	passed, ok := byID["QfSpikeTests/testAddPasses()"]
	if !ok {
		t.Fatal("missing testAddPasses() case")
	}
	if passed.Status != domain.StatusPassed {
		t.Errorf("Status = %q, want passed", passed.Status)
	}
	if passed.Duration <= 0 {
		t.Errorf("Duration = %v, want a positive duration from durationInSeconds", passed.Duration)
	}
}

func TestStepsFromActivities_Passing(t *testing.T) {
	var resp activitiesResponse
	if err := decodeXCResultJSON([]byte(activitiesFixturePassing), &resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	steps := stepsFromActivities(resp.TestRuns)
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if steps[0].Name != "Adding two numbers" {
		t.Errorf("Name = %q, want the XCTContext.runActivity title", steps[0].Name)
	}
	if steps[0].Status != domain.StatusPassed {
		t.Errorf("Status = %q, want passed", steps[0].Status)
	}
}

// A failure activity's child activities are flattened as siblings (matching
// Playwright's existing convention for a nested step tree), and only the
// activity actually marked isAssociatedWithFailure reads as failed.
func TestStepsFromActivities_Failing(t *testing.T) {
	var resp activitiesResponse
	if err := decodeXCResultJSON([]byte(activitiesFixtureFailing), &resp); err != nil {
		t.Fatalf("decode error: %v", err)
	}

	steps := stepsFromActivities(resp.TestRuns)
	if len(steps) != 2 {
		t.Fatalf("steps = %d, want 2 (the failure activity + its flattened child)", len(steps))
	}
	if steps[0].Status != domain.StatusFailed {
		t.Errorf("steps[0].Status = %q, want failed", steps[0].Status)
	}
	if steps[1].Status != domain.StatusPassed {
		t.Errorf("steps[1].Status = %q, want passed (not itself marked as the failure)", steps[1].Status)
	}
}

func TestMapTestResult(t *testing.T) {
	tests := []struct {
		result string
		want   domain.Status
	}{
		{"Passed", domain.StatusPassed},
		{"Failed", domain.StatusFailed},
		{"Skipped", domain.StatusSkipped},
		// An expected failure (XCTExpectFailure) did fail as predicted — it
		// does not fail the overall test run, so it reads as passed.
		{"Expected Failure", domain.StatusPassed},
		{"unknown", domain.StatusError},
		{"something-new-a-future-xcresulttool-version-might-emit", domain.StatusError},
	}
	for _, tt := range tests {
		if got := mapTestResult(tt.result); got != tt.want {
			t.Errorf("mapTestResult(%q) = %q, want %q", tt.result, got, tt.want)
		}
	}
}

// Captured from a real bundle, generated with:
//
//	xcodebuild test -retry-tests-on-failure -test-iterations 3 -resultBundlePath Probe.xcresult
//	xcrun xcresulttool get test-results test-details --path Probe.xcresult \
//	    --test-id 'XcProbeTests/testFlakyAcrossRetries()' --compact
//
// Note what is NOT used below: `duration` ("0,75s") and `name` ("First Run",
// "Retry 1") are both LOCALIZED — that comma is a decimal separator on a
// Turkish-locale machine. Only durationInSeconds, nodeType, nodeIdentifier and
// result are safe to read.
const flakyTestDetailsJSON = `{
  "testIdentifier": "XcProbeTests/testFlakyAcrossRetries()",
  "testResult": "Passed",
  "testRuns": [
    {
      "children": [
        {
          "children": [{"name": "XcProbeTests.testFlakyAcrossRetries()", "nodeType": "Source Code Reference"}],
          "name": "failed - failing on the first attempt so a retry has something to fix",
          "nodeType": "Test Case Run",
          "result": "Failed"
        }
      ],
      "duration": "0,75s", "durationInSeconds": 0.7480369806289673,
      "name": "First Run", "nodeIdentifier": "1", "nodeType": "Repetition", "result": "Failed"
    },
    {
      "duration": "0,00061s", "durationInSeconds": 0.0006080865859985352,
      "name": "Retry 1", "nodeIdentifier": "2", "nodeType": "Repetition", "result": "Passed"
    }
  ]
}`

// The same command for a test that ran once. The shape is DIFFERENT: with no
// repetitions there are no Repetition nodes at all, and testRuns[0] is the
// device that ran it. Assuming the retried shape here would have produced one
// bogus attempt for every ordinary test in every suite.
const singleRunTestDetailsJSON = `{
  "testIdentifier": "XcProbeTests/testPasses()",
  "testResult": "Passed",
  "testRuns": [
    {
      "children": [
        {"duration": "0,00061s", "durationInSeconds": 0.0006051063537597656,
         "name": "Test Scheme Action", "nodeIdentifier": "1",
         "nodeType": "Test Plan Configuration", "result": "Passed"}
      ],
      "details": "macOS 26.5.2", "durationInSeconds": 0.0006051063537597656,
      "name": "Mac mini", "nodeIdentifier": "00006020-0016086C2133C01E", "nodeType": "Device"
    }
  ]
}`

func TestAttemptsFromTestDetails_Retried(t *testing.T) {
	var resp testDetailsResponse
	if err := decodeXCResultJSON([]byte(flakyTestDetailsJSON), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	attempts := attemptsFromTestRuns(resp.TestRuns)
	if len(attempts) != 2 {
		t.Fatalf("got %d attempts, want 2 (a first run and its retry)", len(attempts))
	}
	if attempts[0].Status != domain.StatusFailed || attempts[1].Status != domain.StatusPassed {
		t.Errorf("got %q then %q, want failed then passed", attempts[0].Status, attempts[1].Status)
	}
	if attempts[0].Number != 1 || attempts[1].Number != 2 {
		t.Errorf("attempt numbers were %d,%d — want 1,2", attempts[0].Number, attempts[1].Number)
	}
	if !strings.Contains(attempts[0].Message, "failing on the first attempt") {
		t.Errorf("the failed attempt lost its message: %q", attempts[0].Message)
	}
	// Read from durationInSeconds, never the localized "0,75s" string.
	if attempts[0].Duration < 700*time.Millisecond {
		t.Errorf("duration = %v, want ~0.748s from durationInSeconds", attempts[0].Duration)
	}
}

func TestAttemptsFromTestDetails_SingleRunHasNoAttemptHistory(t *testing.T) {
	var resp testDetailsResponse
	if err := decodeXCResultJSON([]byte(singleRunTestDetailsJSON), &resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if attempts := attemptsFromTestRuns(resp.TestRuns); len(attempts) != 0 {
		t.Errorf("a test that ran once must carry no attempt history, got %d: %+v", len(attempts), attempts)
	}
}

func TestFlakyFromAttempts(t *testing.T) {
	failed := domain.Attempt{Number: 1, Status: domain.StatusFailed}
	passed := domain.Attempt{Number: 2, Status: domain.StatusPassed}
	tests := []struct {
		name      string
		attempts  []domain.Attempt
		wantRetry int
		wantFlaky bool
	}{
		{"failed then passed is flaky", []domain.Attempt{failed, passed}, 1, true},
		{"failed twice is not flaky, it just failed", []domain.Attempt{failed, failed}, 1, false},
		{"no history at all", nil, 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			retries, flaky := retrySummary(tt.attempts)
			if retries != tt.wantRetry || flaky != tt.wantFlaky {
				t.Errorf("got retries=%d flaky=%v, want %d/%v", retries, flaky, tt.wantRetry, tt.wantFlaky)
			}
		})
	}
}

// Captured from a real bundle's manifest.json, written by:
//
//	xcrun xcresulttool export attachments --path Probe.xcresult --output-path <dir>
//
// One call per RUN yields every attachment plus this manifest, keyed by test.
const attachmentManifestJSON = `[
  {
    "attachments": [
      {"configurationName": "Test Scheme Action", "deviceName": "My Mac",
       "exportedFileName": "1CBD22E3-C605-4664-BA65-D526BDAFA1D6.png",
       "isAssociatedWithFailure": true, "repetitionNumber": 1,
       "suggestedHumanReadableName": "pixel_0_D7579D4F-66F9-4664-9204-EF82E212000F.png",
       "timestamp": 1790416720.918},
      {"configurationName": "Test Scheme Action", "deviceName": "My Mac",
       "exportedFileName": "25F1E6CA-2A68-4A12-B2E5-F20EB9AB1D7B.txt",
       "isAssociatedWithFailure": false, "repetitionNumber": 1,
       "suggestedHumanReadableName": "notes_0_E47228CE-1EF1-49EE-9ED9-10494C2D7C69.txt",
       "timestamp": 1790416720.918}
    ],
    "testIdentifier": "XcProbeTests/testWithAttachments()"
  }
]`

func TestAttachmentsFromManifest(t *testing.T) {
	dir := t.TempDir()
	png := []byte{0x89, 'P', 'N', 'G', 1, 2, 3}
	text := []byte("a text attachment from the probe")
	if err := os.WriteFile(filepath.Join(dir, "1CBD22E3-C605-4664-BA65-D526BDAFA1D6.png"), png, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "25F1E6CA-2A68-4A12-B2E5-F20EB9AB1D7B.txt"), text, 0o644); err != nil {
		t.Fatal(err)
	}

	byTest, err := attachmentsFromManifest([]byte(attachmentManifestJSON), dir)
	if err != nil {
		t.Fatalf("attachmentsFromManifest: %v", err)
	}
	got := byTest["XcProbeTests/testWithAttachments()"]
	if len(got) != 2 {
		t.Fatalf("got %d attachments, want 2", len(got))
	}

	image, notes := got[0], got[1]

	// An image takes the artifact route: images upload by default, and a
	// screenshot is the whole point of reading attachments at all.
	if image.LocalPath == "" || image.ArtifactKind != domain.ArtifactKindImage {
		t.Errorf("image should use the artifact flow, got LocalPath=%q kind=%q", image.LocalPath, image.ArtifactKind)
	}
	if !filepath.IsAbs(image.LocalPath) {
		t.Errorf("LocalPath must be absolute, got %q", image.LocalPath)
	}
	if image.MimeType != "image/png" {
		t.Errorf("mime = %q, want image/png", image.MimeType)
	}
	// The name Apple suggests is mangled with an index and a UUID. A user
	// reading the report wants the name their test used.
	if image.Name != "pixel.png" {
		t.Errorf("name = %q, want the de-mangled pixel.png", image.Name)
	}

	// A small non-image is inlined instead: marking it a trace would make it
	// opt-in, so it would silently vanish for anyone who never passed
	// --upload-artifacts.
	if notes.Content == "" {
		t.Errorf("a small text attachment should be inlined, got no content")
	}
	if notes.LocalPath != "" {
		t.Errorf("an inlined attachment should not also take the artifact route")
	}
	if notes.Name != "notes.txt" {
		t.Errorf("name = %q, want notes.txt", notes.Name)
	}
	if decoded, err := base64.StdEncoding.DecodeString(notes.Content); err != nil {
		t.Errorf("content is not valid base64: %v", err)
	} else if string(decoded) != string(text) {
		t.Errorf("content round-trip = %q, want %q", decoded, text)
	}
}

func TestCleanAttachmentName(t *testing.T) {
	tests := []struct{ in, want string }{
		{"pixel_0_D7579D4F-66F9-4664-9204-EF82E212000F.png", "pixel.png"},
		{"notes_0_E47228CE-1EF1-49EE-9ED9-10494C2D7C69.txt", "notes.txt"},
		{"Screenshot of main screen_1_AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE.jpeg", "Screenshot of main screen.jpeg"},
		// Anything that is not Apple's mangling is left exactly as it is —
		// this is cosmetic, and a wrong guess must never lose the real name.
		{"already-clean.png", "already-clean.png"},
		{"weird_name_without_uuid.txt", "weird_name_without_uuid.txt"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := cleanAttachmentName(tt.in); got != tt.want {
			t.Errorf("cleanAttachmentName(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestAttachmentsFromManifest_MissingFileIsSkipped(t *testing.T) {
	// The manifest names a file the export did not write. Dropping that one
	// attachment beats failing the parse: the rest of the run is still good.
	byTest, err := attachmentsFromManifest([]byte(attachmentManifestJSON), t.TempDir())
	if err != nil {
		t.Fatalf("a missing file must not fail the parse: %v", err)
	}
	if got := byTest["XcProbeTests/testWithAttachments()"]; len(got) != 0 {
		t.Errorf("got %d attachments for files that do not exist, want 0", len(got))
	}
}
