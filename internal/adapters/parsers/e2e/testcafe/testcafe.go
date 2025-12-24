package testcafe

import (
	"encoding/json"
	"io"
	"time"

	"qualflare-cli/internal/core/domain"
)

// Parser parses TestCafe JSON reporter output
type Parser struct{}

// TestCafe JSON structures
type Report struct {
	StartTime  string    `json:"startTime"`
	EndTime    string    `json:"endTime"`
	UserAgents []string  `json:"userAgents"`
	Passed     int       `json:"passed"`
	Failed     int       `json:"failed"`
	Skipped    int       `json:"skipped"`
	Total      int       `json:"total"`
	Duration   int       `json:"duration"` // milliseconds
	Fixtures   []Fixture `json:"fixtures"`
	Warnings   []string  `json:"warnings"`
}

type Fixture struct {
	Name  string            `json:"name"`
	Path  string            `json:"path"`
	Meta  map[string]string `json:"meta"`
	Tests []Test            `json:"tests"`
}

type Test struct {
	Name        string            `json:"name"`
	Meta        map[string]string `json:"meta"`
	Errs        []Error           `json:"errs"`
	DurationMs  int               `json:"durationMs"`
	Skipped     bool              `json:"skipped"`
	Screenshots []Screenshot      `json:"screenshots"`
	Videos      []Video           `json:"videos"`
	TestRunInfo RunInfo           `json:"testRunInfo"`
	Unstable    bool              `json:"unstable"`
}

type Error struct {
	ErrMsg          string `json:"errMsg"`
	Stack           string `json:"stack"`
	IsTestCafeError bool   `json:"isTestCafeError"`
	Code            string `json:"code"`
}

type Screenshot struct {
	ScreenshotPath string `json:"screenshotPath"`
	ThumbnailPath  string `json:"thumbnailPath"`
	UserAgent      string `json:"userAgent"`
	TakenOnFail    bool   `json:"takenOnFail"`
}

type Video struct {
	VideoPath string `json:"videoPath"`
	UserAgent string `json:"userAgent"`
}

type RunInfo struct {
	BrowserId string `json:"browserId"`
	UserAgent string `json:"userAgent"`
	Duration  int    `json:"duration"`
}

// New creates a new TestCafe parser
func New() *Parser {
	return &Parser{}
}

// Parse parses TestCafe JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:       "TestCafe Test Results",
		TotalTests: report.Total,
		Passed:     report.Passed,
		Failed:     report.Failed,
		Skipped:    report.Skipped,
		Duration:   time.Duration(report.Duration) * time.Millisecond,
		Timestamp:  time.Now(),
		Cases:      make([]domain.Case, 0),
	}

	// Parse start time if available
	if report.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, report.StartTime); err == nil {
			suite.Timestamp = t
		}
	}

	// Process all fixtures
	for _, fixture := range report.Fixtures {
		for _, test := range fixture.Tests {
			testCase := p.convertTest(test, fixture)
			suite.Cases = append(suite.Cases, testCase)
		}
	}

	return suite, nil
}

// convertTest converts a TestCafe test to domain.Case
func (p *Parser) convertTest(test Test, fixture Fixture) domain.Case {
	testCase := domain.Case{
		ID:        fixture.Name + " > " + test.Name,
		Name:      test.Name,
		ClassName: fixture.Path,
		Duration:  time.Duration(test.DurationMs) * time.Millisecond,
	}

	// Determine status
	if test.Skipped {
		testCase.Status = domain.StatusSkipped
	} else if len(test.Errs) > 0 {
		testCase.Status = domain.StatusFailed
		// Collect error messages
		if len(test.Errs) > 0 {
			testCase.ErrorMessage = test.Errs[0].ErrMsg
			testCase.StackTrace = test.Errs[0].Stack
			testCase.ErrorType = test.Errs[0].Code
		}
	} else {
		testCase.Status = domain.StatusPassed
	}

	// Add properties
	testCase.Properties = map[string]string{
		"fixture": fixture.Name,
		"path":    fixture.Path,
	}

	// Add fixture meta
	for k, v := range fixture.Meta {
		testCase.Properties["fixture."+k] = v
	}

	// Add test meta
	for k, v := range test.Meta {
		testCase.Properties["test."+k] = v
	}

	// Add run info
	if test.TestRunInfo.UserAgent != "" {
		testCase.Properties["userAgent"] = test.TestRunInfo.UserAgent
	}

	// Convert screenshots to attachments
	if len(test.Screenshots) > 0 {
		testCase.Attachments = make([]domain.Attachment, 0, len(test.Screenshots)+len(test.Videos))
		for _, ss := range test.Screenshots {
			testCase.Attachments = append(testCase.Attachments, domain.Attachment{
				Name:     "screenshot",
				Path:     ss.ScreenshotPath,
				MimeType: "image/png",
			})
		}
	}

	// Convert videos to attachments
	for _, v := range test.Videos {
		if testCase.Attachments == nil {
			testCase.Attachments = make([]domain.Attachment, 0)
		}
		testCase.Attachments = append(testCase.Attachments, domain.Attachment{
			Name:     "video",
			Path:     v.VideoPath,
			MimeType: "video/mp4",
		})
	}

	// Mark unstable tests
	if test.Unstable {
		testCase.Tags = append(testCase.Tags, "unstable")
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkTestCafe
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
