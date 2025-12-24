package karate

import (
	"encoding/json"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Karate JSON output
type Parser struct{}

// Karate JSON structures
type Report struct {
	FeatureName     string     `json:"featureName"`
	Name            string     `json:"name"`
	Description     string     `json:"description"`
	DurationMillis  int64      `json:"durationMillis"`
	PassedCount     int        `json:"passedCount"`
	FailedCount     int        `json:"failedCount"`
	ScenarioCount   int        `json:"scenarioCount"`
	ScenarioResults []Scenario `json:"scenarioResults"`
	Tags            []string   `json:"tags"`
}

type Scenario struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	DurationMillis int64  `json:"durationMillis"`
	StepResults    []Step `json:"stepResults"`
	Failed         bool   `json:"failed"`
	Skipped        bool   `json:"skipped"`
	Tags           []string `json:"tags"`
	Line           int    `json:"line"`
	ExampleIndex   int    `json:"exampleIndex"`
}

type Step struct {
	Step          string `json:"step"`
	Line          int    `json:"line"`
	DurationNanos int64  `json:"durationNanos"`
	Result        string `json:"result"` // passed, failed, skipped
	ErrorMessage  string `json:"errorMessage,omitempty"`
	Hidden        bool   `json:"hidden"`
}

// New creates a new Karate parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Karate JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	// Karate can output either a single report or an array of reports
	var reports []Report

	decoder := json.NewDecoder(reader)

	// Try to decode as array first
	if err := decoder.Decode(&reports); err != nil {
		// Try as single report
		if seeker, ok := reader.(io.Seeker); ok {
			if _, err := seeker.Seek(0, 0); err != nil {
				return nil, err
			}
		}

		var singleReport Report
		decoder = json.NewDecoder(reader)
		if err := decoder.Decode(&singleReport); err != nil {
			return nil, err
		}
		reports = []Report{singleReport}
	}

	suite := &domain.Suite{
		Name:      "Karate Test Results",
		Timestamp: time.Now(),
		Cases:     make([]domain.Case, 0),
	}

	var totalDuration time.Duration

	for _, report := range reports {
		if report.Name != "" && suite.Name == "Karate Test Results" {
			suite.Name = report.Name
		}

		totalDuration += time.Duration(report.DurationMillis) * time.Millisecond

		for _, scenario := range report.ScenarioResults {
			testCase := p.convertScenario(scenario, report)
			suite.Cases = append(suite.Cases, testCase)

			// Update counters
			switch testCase.Status {
			case domain.StatusPassed:
				suite.Passed++
			case domain.StatusFailed:
				suite.Failed++
			case domain.StatusSkipped:
				suite.Skipped++
			}
		}
	}

	suite.TotalTests = len(suite.Cases)
	suite.Duration = totalDuration

	return suite, nil
}

// convertScenario converts a Karate scenario to domain.Case
func (p *Parser) convertScenario(scenario Scenario, report Report) domain.Case {
	testCase := domain.Case{
		ID:       report.FeatureName + "_" + scenario.Name,
		Name:     scenario.Name,
		Duration: time.Duration(scenario.DurationMillis) * time.Millisecond,
		Tags:     append(report.Tags, scenario.Tags...),
	}

	// Build steps
	testCase.Steps = make([]domain.Step, 0, len(scenario.StepResults))
	var errorMessages []string

	for _, step := range scenario.StepResults {
		if step.Hidden {
			continue
		}

		domainStep := domain.Step{
			Name:     step.Step,
			Duration: base.ParseDurationNs(step.DurationNanos),
		}

		switch step.Result {
		case "passed":
			domainStep.Status = domain.StatusPassed
		case "failed":
			domainStep.Status = domain.StatusFailed
			domainStep.Error = step.ErrorMessage
			if step.ErrorMessage != "" {
				errorMessages = append(errorMessages, step.ErrorMessage)
			}
		case "skipped":
			domainStep.Status = domain.StatusSkipped
		default:
			domainStep.Status = domain.StatusSkipped
		}

		testCase.Steps = append(testCase.Steps, domainStep)
	}

	// Determine scenario status
	if scenario.Skipped {
		testCase.Status = domain.StatusSkipped
	} else if scenario.Failed {
		testCase.Status = domain.StatusFailed
		if len(errorMessages) > 0 {
			testCase.ErrorMessage = errorMessages[0]
			if len(errorMessages) > 1 {
				for i := 1; i < len(errorMessages); i++ {
					testCase.StackTrace += errorMessages[i] + "\n"
				}
			}
		}
	} else {
		testCase.Status = domain.StatusPassed
	}

	// Add properties
	testCase.Properties = map[string]string{
		"feature": report.FeatureName,
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkKarate
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
