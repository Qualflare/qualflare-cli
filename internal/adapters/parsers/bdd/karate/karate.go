package karate

import (
	"encoding/json"
	"io"
	"strings"
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
	Name           string   `json:"name"`
	Description    string   `json:"description"`
	DurationMillis int64    `json:"durationMillis"`
	StepResults    []Step   `json:"stepResults"`
	Failed         bool     `json:"failed"`
	Skipped        bool     `json:"skipped"`
	Tags           []string `json:"tags"`
	Line           int      `json:"line"`
	ExampleIndex   int      `json:"exampleIndex"`
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
	reports, err := decodeReports(reader)
	if err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      "Karate Test Results",
		Category:  domain.FrameworkKarate.GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	var totalDuration time.Duration

	// The first named report supplies the suite name. Tracked with an explicit flag
	// rather than comparing against the default string, which would let a report
	// legitimately named "Karate Test Results" be overwritten by the next one.
	nameSet := false

	for _, report := range reports {
		if report.Name != "" && !nameSet {
			suite.Name = report.Name
			nameSet = true
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

// decodeReports reads either an array of reports or a single bare report object, which
// are both shapes Karate emits depending on how it was invoked.
func decodeReports(reader io.Reader) ([]Report, error) {
	var reports []Report
	if err := json.NewDecoder(reader).Decode(&reports); err == nil {
		return reports, nil
	}

	// Rewind and retry as one report. A non-seekable reader has already been
	// consumed, in which case the second decode fails and the error is returned.
	if seeker, ok := reader.(io.Seeker); ok {
		if _, err := seeker.Seek(0, 0); err != nil {
			return nil, err
		}
	}

	var single Report
	if err := json.NewDecoder(reader).Decode(&single); err != nil {
		return nil, err
	}
	return []Report{single}, nil
}

// convertScenario converts a Karate scenario to domain.Case
func (p *Parser) convertScenario(scenario Scenario, report Report) domain.Case {
	testCase := domain.Case{
		ID:       report.FeatureName + "_" + scenario.Name,
		Name:     scenario.Name,
		Duration: time.Duration(scenario.DurationMillis) * time.Millisecond,
		Tags:     mergeTags(report.Tags, scenario.Tags),
	}

	steps, errorMessages := convertSteps(scenario.StepResults)
	testCase.Steps = steps

	switch {
	case scenario.Skipped:
		testCase.Status = domain.StatusSkipped
	case scenario.Failed:
		testCase.Status = domain.StatusFailed
		testCase.Error = aggregateErrors(errorMessages)
	default:
		testCase.Status = domain.StatusPassed
	}

	testCase.Properties = map[string]string{
		"feature": report.FeatureName,
	}

	return testCase
}

// mergeTags combines the feature-level and scenario-level tags.
//
// BUG-07: append(report.Tags, scenario.Tags...) aliased report.Tags' backing array, so
// scenarios overwrote each other's tags across the loop. Allocating a FRESH slice per
// scenario is the fix, so this must never be "optimised" back into an append onto
// report.Tags.
func mergeTags(reportTags, scenarioTags []string) []string {
	tags := make([]string, 0, len(reportTags)+len(scenarioTags))
	tags = append(tags, reportTags...)
	tags = append(tags, scenarioTags...)
	return tags
}

// convertSteps converts the visible steps, also returning the error messages the failed
// ones carried. Hidden steps are Karate's own bookkeeping: they are neither reported as
// steps nor allowed to contribute an error message.
func convertSteps(steps []Step) ([]domain.Step, []string) {
	out := make([]domain.Step, 0, len(steps))
	var errorMessages []string

	for _, step := range steps {
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
		default:
			// "skipped", and anything unrecognised — neither may read as a pass.
			domainStep.Status = domain.StatusSkipped
		}

		out = append(out, domainStep)
	}

	return out, errorMessages
}

// aggregateErrors folds the failed steps' messages into one case-level error, keeping
// the first as the headline and the rest as the trace.
func aggregateErrors(messages []string) string {
	switch len(messages) {
	case 0:
		return ""
	case 1:
		return messages[0]
	default:
		var stackTrace strings.Builder
		for _, m := range messages[1:] {
			stackTrace.WriteString(m)
			stackTrace.WriteString("\n")
		}
		return domain.FormatError(messages[0], stackTrace.String(), "")
	}
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkKarate
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
