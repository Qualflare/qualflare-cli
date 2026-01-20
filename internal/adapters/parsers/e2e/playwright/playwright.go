package playwright

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"qualflare-cli/internal/core/domain"
)

// Parser parses Playwright JSON reporter output
type Parser struct{}

// Playwright JSON structures
type Report struct {
	Config Config  `json:"config"`
	Suites []Suite `json:"suites"`
	Errors []Error `json:"errors"`
	Stats  Stats   `json:"stats"`
}

type Config struct {
	ConfigFile string `json:"configFile"`
	RootDir    string `json:"rootDir"`
}

type Suite struct {
	Title  string  `json:"title"`
	File   string  `json:"file"`
	Line   int     `json:"line"`
	Column int     `json:"column"`
	Specs  []Spec  `json:"specs"`
	Suites []Suite `json:"suites"` // Nested suites
}

type Spec struct {
	Title  string `json:"title"`
	OK     bool   `json:"ok"`
	Tags   []string `json:"tags"`
	Tests  []Test   `json:"tests"`
	File   string `json:"file"`
	Line   int    `json:"line"`
	Column int    `json:"column"`
}

type Test struct {
	Timeout        int          `json:"timeout"`
	Annotations    []Annotation `json:"annotations"`
	ExpectedStatus string       `json:"expectedStatus"`
	ProjectID      string       `json:"projectId"`
	ProjectName    string       `json:"projectName"`
	Results        []Result     `json:"results"`
	Status         string       `json:"status"`
}

type Annotation struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type Result struct {
	WorkerIndex int          `json:"workerIndex"`
	Status      string       `json:"status"`
	Duration    int          `json:"duration"` // milliseconds
	Error       *TestError   `json:"error"`
	Attachments []Attachment `json:"attachments"`
	Retry       int          `json:"retry"`
	StartTime   string       `json:"startTime"`
}

type TestError struct {
	Message string `json:"message"`
	Stack   string `json:"stack"`
}

type Attachment struct {
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Path        string `json:"path"`
}

type Error struct {
	Message string `json:"message"`
}

type Stats struct {
	StartTime  string `json:"startTime"`
	Duration   int    `json:"duration"`
	Expected   int    `json:"expected"`
	Skipped    int    `json:"skipped"`
	Unexpected int    `json:"unexpected"`
	Flaky      int    `json:"flaky"`
}

// New creates a new Playwright parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Playwright JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      "Playwright Test Results",
		Category:  domain.FrameworkPlaywright.GetCategory(),
		Duration:  time.Duration(report.Stats.Duration) * time.Millisecond,
		Timestamp: time.Now(),
		Cases:     make([]domain.Case, 0),
	}

	// Parse start time if available
	if report.Stats.StartTime != "" {
		if t, err := time.Parse(time.RFC3339, report.Stats.StartTime); err == nil {
			suite.Timestamp = t
		}
	}

	// Set flaky count from stats
	suite.Flaky = report.Stats.Flaky

	// Process all suites and track retries
	browsers := make(map[string]bool)
	p.processSuites(report.Suites, suite, "", browsers)

	// Store browser in properties for Launch to use
	if len(browsers) > 0 {
		browserList := make([]string, 0, len(browsers))
		for b := range browsers {
			browserList = append(browserList, b)
		}
		if suite.Properties == nil {
			suite.Properties = make(map[string]string)
		}
		if len(browserList) == 1 {
			suite.Properties["browser"] = browserList[0]
		} else {
			// Multiple browsers - join them
			suite.Properties["browser"] = strings.Join(browserList, ", ")
		}
	}

	suite.TotalTests = len(suite.Cases)

	return suite, nil
}

// processSuites recursively processes Playwright suites
func (p *Parser) processSuites(suites []Suite, domainSuite *domain.Suite, prefix string, browsers map[string]bool) {
	for _, s := range suites {
		currentPrefix := s.Title
		if prefix != "" {
			currentPrefix = prefix + " > " + s.Title
		}

		// Process specs in this suite
		for _, spec := range s.Specs {
			for _, test := range spec.Tests {
				testCase := p.convertTest(spec, test, s.File, currentPrefix)
				domainSuite.Cases = append(domainSuite.Cases, testCase)

				// Collect browser from project name
				if test.ProjectName != "" {
					browsers[test.ProjectName] = true
				}

				// Track retries (each result beyond the first is a retry)
				if len(test.Results) > 1 {
					domainSuite.Retries += len(test.Results) - 1
				}

				// Update counters
				switch testCase.Status {
				case domain.StatusPassed:
					domainSuite.Passed++
				case domain.StatusFailed:
					domainSuite.Failed++
				case domain.StatusSkipped:
					domainSuite.Skipped++
				}
			}
		}

		// Process nested suites
		if len(s.Suites) > 0 {
			p.processSuites(s.Suites, domainSuite, currentPrefix, browsers)
		}
	}
}

// convertTest converts a Playwright test to domain.Case
func (p *Parser) convertTest(spec Spec, test Test, file string, prefix string) domain.Case {
	fullName := spec.Title
	if prefix != "" {
		fullName = prefix + " > " + spec.Title
	}

	testCase := domain.Case{
		ID:        fullName,
		Name:      spec.Title,
		ClassName: file,
		Tags:      spec.Tags,
	}

	// Get the last result (after retries)
	if len(test.Results) > 0 {
		lastResult := test.Results[len(test.Results)-1]
		testCase.Duration = time.Duration(lastResult.Duration) * time.Millisecond

		// Calculate retry count and flaky status
		testCase.RetryCount = len(test.Results) - 1
		testCase.IsFlaky = testCase.RetryCount > 0 &&
			lastResult.Status == "passed" &&
			len(test.Results) > 1

		// Determine status
		switch lastResult.Status {
		case "passed":
			testCase.Status = domain.StatusPassed
		case "failed", "timedOut":
			testCase.Status = domain.StatusFailed
			if lastResult.Error != nil {
				testCase.ErrorMessage = lastResult.Error.Message
				testCase.StackTrace = lastResult.Error.Stack
			}
		case "skipped":
			testCase.Status = domain.StatusSkipped
		default:
			testCase.Status = domain.StatusPassed
		}

		// Convert attachments
		if len(lastResult.Attachments) > 0 {
			testCase.Attachments = make([]domain.Attachment, 0, len(lastResult.Attachments))
			for _, att := range lastResult.Attachments {
				testCase.Attachments = append(testCase.Attachments, domain.Attachment{
					Name:     att.Name,
					Path:     att.Path,
					MimeType: att.ContentType,
				})
			}
		}
	} else {
		// No results - use test level status
		switch test.Status {
		case "expected":
			testCase.Status = domain.StatusPassed
		case "unexpected":
			testCase.Status = domain.StatusFailed
		case "skipped":
			testCase.Status = domain.StatusSkipped
		default:
			testCase.Status = domain.StatusPassed
		}
	}

	// Add properties
	testCase.Properties = map[string]string{
		"file":    file,
		"project": test.ProjectName,
	}

	// Add annotations as tags
	for _, ann := range test.Annotations {
		if ann.Type != "" {
			testCase.Tags = append(testCase.Tags, ann.Type)
		}
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkPlaywright
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
