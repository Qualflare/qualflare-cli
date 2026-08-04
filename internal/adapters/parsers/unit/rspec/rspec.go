package rspec

import (
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"time"

	"qualflare-cli/internal/core/domain"
)

// Parser parses RSpec JSON formatter output
type Parser struct{}

// RSpec JSON structures
type Report struct {
	Version     string    `json:"version"`
	Seed        int       `json:"seed"`
	Examples    []Example `json:"examples"`
	Summary     Summary   `json:"summary"`
	SummaryLine string    `json:"summary_line"`
}

type Example struct {
	ID              string     `json:"id"`
	Description     string     `json:"description"`
	FullDescription string     `json:"full_description"`
	Status          string     `json:"status"`
	FilePath        string     `json:"file_path"`
	LineNumber      int        `json:"line_number"`
	RunTime         float64    `json:"run_time"`
	PendingMessage  string     `json:"pending_message,omitempty"`
	Exception       *Exception `json:"exception,omitempty"`
}

type Exception struct {
	Class     string   `json:"class"`
	Message   string   `json:"message"`
	Backtrace []string `json:"backtrace"`
}

type Summary struct {
	Duration                     float64 `json:"duration"`
	ExampleCount                 int     `json:"example_count"`
	FailureCount                 int     `json:"failure_count"`
	PendingCount                 int     `json:"pending_count"`
	ErrorsOutsideOfExamplesCount int     `json:"errors_outside_of_examples_count"`
}

// New creates a new RSpec parser
func New() *Parser {
	return &Parser{}
}

// Parse parses RSpec JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      "RSpec Test Results",
		Category:  domain.FrameworkRSpec.GetCategory(),
		Duration:  time.Duration(report.Summary.Duration * float64(time.Second)),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0, len(report.Examples)),
	}

	for _, example := range report.Examples {
		testCase := p.convertExample(example)
		suite.Cases = append(suite.Cases, testCase)
	}

	// BUG-17: a load-time error (spec_helper/rails_helper raising) makes RSpec
	// report example_count 0, failure_count 0, but errors_outside_of_examples_count
	// > 0. Without a synthesized case the suite would upload empty and roll up
	// green, hiding the failure. Materialize it as a failed case so the suite goes
	// red and the summary line is preserved.
	if report.Summary.ErrorsOutsideOfExamplesCount > 0 {
		message := report.SummaryLine
		if message == "" {
			message = "RSpec reported errors that occurred outside of examples (e.g. a load-time failure in spec_helper/rails_helper)"
		}
		suite.Cases = append(suite.Cases, domain.Case{
			ID:     "rspec:errors-outside-of-examples",
			Name:   "Errors occurred outside of examples",
			Status: domain.StatusFailed,
			Error:  domain.FormatError(message, "", ""),
		})
	}

	// Counters are header-derived; recompute from case statuses so they can never
	// disagree with the cases (and can never roll up green over a load-time error).
	suite.RecomputeCounts()

	return suite, nil
}

// convertExample converts an RSpec example to domain.Case
func (p *Parser) convertExample(example Example) domain.Case {
	testCase := domain.Case{
		ID:        example.ID,
		Name:      example.Description,
		ClassName: example.FilePath,
		Duration:  time.Duration(example.RunTime * float64(time.Second)),
	}

	// Determine status
	switch example.Status {
	case "passed":
		testCase.Status = domain.StatusPassed
	case "failed":
		testCase.Status = domain.StatusFailed
		if example.Exception != nil {
			stackTrace := ""
			if len(example.Exception.Backtrace) > 0 {
				stackTrace = example.Exception.Backtrace[0]
				var stackTraceSb121 strings.Builder
				for i := 1; i < len(example.Exception.Backtrace) && i < 10; i++ {
					stackTraceSb121.WriteString("\n" + example.Exception.Backtrace[i])
				}
				stackTrace += stackTraceSb121.String()
			}
			testCase.Error = domain.FormatError(example.Exception.Message, stackTrace, example.Exception.Class)
		}
	case "pending":
		testCase.Status = domain.StatusPending
		testCase.Error = domain.FormatError(example.PendingMessage, "", "")
	default:
		// BUG-17: an unrecognized RSpec status must fail visibly, never be masked as
		// skipped/passed (the cardinal sin is a non-passing test rolling up green).
		testCase.Status = domain.StatusError
	}

	// Add properties
	testCase.Properties = map[string]string{
		"file":        example.FilePath,
		"line_number": strconv.Itoa(example.LineNumber),
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkRSpec
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
