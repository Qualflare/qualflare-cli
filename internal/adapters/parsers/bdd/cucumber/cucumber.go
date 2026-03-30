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
		Category:  domain.FrameworkCucumber.GetCategory(),
		Timestamp: time.Now().UTC(),
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

// collectTags merges feature-level and scenario-level tags, stripping the "@" prefix.
func collectTags(featureTags, scenarioTags []Tag) []string {
	tags := make([]string, 0, len(featureTags)+len(scenarioTags))
	for _, tag := range featureTags {
		tags = append(tags, strings.TrimPrefix(tag.Name, "@"))
	}
	for _, tag := range scenarioTags {
		tags = append(tags, strings.TrimPrefix(tag.Name, "@"))
	}
	return tags
}

// processHooks collects error messages from failed hooks.
func processHooks(hooks []Hook) []string {
	var errors []string
	for _, hook := range hooks {
		if hook.Result.Status == "failed" && hook.Result.ErrorMessage != "" {
			errors = append(errors, hook.Result.ErrorMessage)
		}
	}
	return errors
}

// mapStepStatus maps a Cucumber step status string to a domain.Status.
func mapStepStatus(status string) domain.Status {
	switch status {
	case "passed":
		return domain.StatusPassed
	case "failed":
		return domain.StatusFailed
	case "pending", "undefined":
		return domain.StatusPending
	case "skipped":
		return domain.StatusSkipped
	default:
		return domain.StatusSkipped
	}
}

// processSteps converts Cucumber steps to domain steps and aggregates duration, status, errors, and stack traces.
func processSteps(steps []Step) ([]domain.Step, time.Duration, domain.Status, []string, []string) {
	domainSteps := make([]domain.Step, 0, len(steps))
	var totalDuration time.Duration
	scenarioStatus := domain.StatusPassed
	var errorMessages []string
	var stackTraces []string

	for _, step := range steps {
		stepDuration := base.ParseDurationNs(step.Result.Duration)
		totalDuration += stepDuration

		domainStep := domain.Step{
			Name:     step.Keyword + step.Name,
			Keyword:  strings.TrimSpace(step.Keyword),
			Duration: stepDuration,
			Location: step.Match.Location,
		}

		domainStep.Status = mapStepStatus(step.Result.Status)

		switch domainStep.Status {
		case domain.StatusFailed:
			domainStep.Error = step.Result.ErrorMessage
			scenarioStatus = domain.StatusFailed
			if step.Result.ErrorMessage != "" {
				errorMessages = append(errorMessages, step.Result.ErrorMessage)
				stackTraces = append(stackTraces, step.Match.Location)
			}
		case domain.StatusPending:
			if scenarioStatus == domain.StatusPassed {
				scenarioStatus = domain.StatusPending
			}
		case domain.StatusSkipped:
			if scenarioStatus == domain.StatusPassed {
				scenarioStatus = domain.StatusSkipped
			}
		}

		domainSteps = append(domainSteps, domainStep)
	}

	return domainSteps, totalDuration, scenarioStatus, errorMessages, stackTraces
}

// convertScenario converts a Cucumber scenario to domain.Case
func (p *Parser) convertScenario(feature Feature, scenario Scenario) domain.Case {
	testCase := domain.Case{
		ID:   feature.ID + "_" + scenario.ID,
		Name: feature.Name + " - " + scenario.Name,
		Tags: collectTags(feature.Tags, scenario.Tags),
	}

	// Process steps
	steps, duration, scenarioStatus, errorMessages, stackTraces := processSteps(scenario.Steps)
	testCase.Steps = steps
	testCase.Duration = duration

	// Process before hooks — failed hooks override status
	for _, errMsg := range processHooks(scenario.Before) {
		scenarioStatus = domain.StatusFailed
		errorMessages = append(errorMessages, errMsg)
	}

	// Process after hooks — failed hooks override status
	for _, errMsg := range processHooks(scenario.After) {
		scenarioStatus = domain.StatusFailed
		errorMessages = append(errorMessages, errMsg)
	}

	testCase.Status = scenarioStatus

	if len(errorMessages) > 0 {
		testCase.Error = domain.FormatError(strings.Join(errorMessages, "; "), strings.Join(stackTraces, "\n"), "")
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
