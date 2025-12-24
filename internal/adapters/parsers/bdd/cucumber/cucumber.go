package cucumber

import (
	"encoding/json"
	"io"
	"strings"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Cucumber JSON output
type Parser struct{}

// Cucumber JSON structures
type Feature struct {
	URI         string     `json:"uri"`
	ID          string     `json:"id"`
	Keyword     string     `json:"keyword"`
	Name        string     `json:"name"`
	Description string     `json:"description"`
	Line        int        `json:"line"`
	Elements    []Scenario `json:"elements"`
	Tags        []Tag      `json:"tags"`
}

type Scenario struct {
	ID          string `json:"id"`
	Keyword     string `json:"keyword"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Line        int    `json:"line"`
	Type        string `json:"type"`
	Steps       []Step `json:"steps"`
	Tags        []Tag  `json:"tags"`
	Before      []Hook `json:"before,omitempty"`
	After       []Hook `json:"after,omitempty"`
}

type Step struct {
	Keyword string `json:"keyword"`
	Name    string `json:"name"`
	Line    int    `json:"line"`
	Match   Match  `json:"match"`
	Result  Result `json:"result"`
}

type Hook struct {
	Match  Match  `json:"match"`
	Result Result `json:"result"`
}

type Match struct {
	Location string `json:"location"`
}

type Result struct {
	Status       string `json:"status"`
	Duration     int64  `json:"duration"` // in nanoseconds
	ErrorMessage string `json:"error_message,omitempty"`
}

type Tag struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

// New creates a new Cucumber parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Cucumber JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var features []Feature
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&features); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      "Cucumber Features",
		Timestamp: time.Now(),
		Cases:     make([]domain.Case, 0),
	}

	var totalDuration time.Duration

	for _, feature := range features {
		for _, scenario := range feature.Elements {
			if scenario.Type != "scenario" && scenario.Type != "scenario_outline" {
				continue
			}

			testCase := p.convertScenario(feature, scenario)
			suite.Cases = append(suite.Cases, testCase)
			totalDuration += testCase.Duration

			// Update counters
			switch testCase.Status {
			case domain.StatusPassed:
				suite.Passed++
			case domain.StatusFailed:
				suite.Failed++
			case domain.StatusSkipped, domain.StatusPending:
				suite.Skipped++
			}
		}
	}

	suite.TotalTests = len(suite.Cases)
	suite.Duration = totalDuration

	return suite, nil
}

// convertScenario converts a Cucumber scenario to domain.Case
func (p *Parser) convertScenario(feature Feature, scenario Scenario) domain.Case {
	testCase := domain.Case{
		ID:   feature.ID + "_" + scenario.ID,
		Name: feature.Name + " - " + scenario.Name,
	}

	// Collect tags
	tags := make([]string, 0)
	for _, tag := range feature.Tags {
		tags = append(tags, strings.TrimPrefix(tag.Name, "@"))
	}
	for _, tag := range scenario.Tags {
		tags = append(tags, strings.TrimPrefix(tag.Name, "@"))
	}
	testCase.Tags = tags

	// Process steps to build Steps slice and determine status
	testCase.Steps = make([]domain.Step, 0, len(scenario.Steps))
	var scenarioDuration time.Duration
	scenarioStatus := domain.StatusPassed
	var errorMessages []string
	var stackTraces []string

	// Process before hooks
	for _, hook := range scenario.Before {
		if hook.Result.Status == "failed" {
			scenarioStatus = domain.StatusFailed
			if hook.Result.ErrorMessage != "" {
				errorMessages = append(errorMessages, hook.Result.ErrorMessage)
			}
		}
	}

	// Process steps
	for _, step := range scenario.Steps {
		stepDuration := base.ParseDurationNs(step.Result.Duration)
		scenarioDuration += stepDuration

		domainStep := domain.Step{
			Name:     step.Keyword + step.Name,
			Keyword:  strings.TrimSpace(step.Keyword),
			Duration: stepDuration,
			Location: step.Match.Location,
		}

		switch step.Result.Status {
		case "passed":
			domainStep.Status = domain.StatusPassed
		case "failed":
			domainStep.Status = domain.StatusFailed
			domainStep.Error = step.Result.ErrorMessage
			scenarioStatus = domain.StatusFailed
			if step.Result.ErrorMessage != "" {
				errorMessages = append(errorMessages, step.Result.ErrorMessage)
				stackTraces = append(stackTraces, step.Match.Location)
			}
		case "pending":
			domainStep.Status = domain.StatusPending
			if scenarioStatus == domain.StatusPassed {
				scenarioStatus = domain.StatusPending
			}
		case "undefined":
			domainStep.Status = domain.StatusPending
			if scenarioStatus == domain.StatusPassed {
				scenarioStatus = domain.StatusPending
			}
		case "skipped":
			domainStep.Status = domain.StatusSkipped
			if scenarioStatus == domain.StatusPassed {
				scenarioStatus = domain.StatusSkipped
			}
		default:
			domainStep.Status = domain.StatusSkipped
		}

		testCase.Steps = append(testCase.Steps, domainStep)
	}

	// Process after hooks
	for _, hook := range scenario.After {
		if hook.Result.Status == "failed" {
			scenarioStatus = domain.StatusFailed
			if hook.Result.ErrorMessage != "" {
				errorMessages = append(errorMessages, hook.Result.ErrorMessage)
			}
		}
	}

	testCase.Status = scenarioStatus
	testCase.Duration = scenarioDuration

	if len(errorMessages) > 0 {
		testCase.ErrorMessage = strings.Join(errorMessages, "; ")
	}
	if len(stackTraces) > 0 {
		testCase.StackTrace = strings.Join(stackTraces, "\n")
	}

	// Add properties
	testCase.Properties = map[string]string{
		"feature": feature.Name,
		"uri":     feature.URI,
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkCucumber
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
