package parsers

import (
	"encoding/json"
	"io"
	"qualflare-cli/internal/core/domain"
	"strings"
	"time"
)

type CucumberParser struct{}

type CucumberFeature struct {
	URI         string             `json:"uri"`
	ID          string             `json:"id"`
	Keyword     string             `json:"keyword"`
	Name        string             `json:"name"`
	Description string             `json:"description"`
	Line        int                `json:"line"`
	Elements    []CucumberScenario `json:"elements"`
	Tags        []CucumberTag      `json:"tags"`
}

type CucumberScenario struct {
	ID          string         `json:"id"`
	Keyword     string         `json:"keyword"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Line        int            `json:"line"`
	Type        string         `json:"type"`
	Steps       []CucumberStep `json:"steps"`
	Tags        []CucumberTag  `json:"tags"`
}

type CucumberStep struct {
	Keyword string         `json:"keyword"`
	Name    string         `json:"name"`
	Line    int            `json:"line"`
	Match   CucumberMatch  `json:"match"`
	Result  CucumberResult `json:"result"`
}

type CucumberMatch struct {
	Location string `json:"location"`
}

type CucumberResult struct {
	Status   string `json:"status"`
	Duration int64  `json:"duration"`
	Error    string `json:"error_message,omitempty"`
}

type CucumberTag struct {
	Name string `json:"name"`
	Line int    `json:"line"`
}

func NewCucumberParser() *CucumberParser {
	return &CucumberParser{}
}

func (p *CucumberParser) Parse(reader io.Reader) (*domain.Suite, error) {
	var features []CucumberFeature
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
			if scenario.Type != "scenario" {
				continue
			}

			test := domain.Case{
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
			test.Tags = tags

			// Process steps to determine scenario status
			var scenarioDuration time.Duration
			scenarioStatus := domain.TestStatusPassed
			var errorMessages []string
			var stackTraces []string

			for _, step := range scenario.Steps {
				stepDuration := time.Duration(step.Result.Duration) * time.Nanosecond
				scenarioDuration += stepDuration

				switch step.Result.Status {
				case "failed":
					scenarioStatus = domain.TestStatusFailed
					if step.Result.Error != "" {
						errorMessages = append(errorMessages, step.Result.Error)
						stackTraces = append(stackTraces, step.Match.Location)
					}
				case "pending", "undefined":
					if scenarioStatus == domain.TestStatusPassed {
						scenarioStatus = domain.TestStatusSkipped
					}
				case "skipped":
					if scenarioStatus == domain.TestStatusPassed {
						scenarioStatus = domain.TestStatusSkipped
					}
				}
			}

			test.Status = scenarioStatus
			test.Duration = scenarioDuration
			totalDuration += scenarioDuration

			if len(errorMessages) > 0 {
				test.ErrorMessage = strings.Join(errorMessages, "; ")
			}
			if len(stackTraces) > 0 {
				test.StackTrace = strings.Join(stackTraces, "\n")
			}

			// Add properties
			test.Properties = map[string]string{
				"feature": feature.Name,
				"uri":     feature.URI,
			}

			suite.Cases = append(suite.Cases, test)

			// Update counters
			switch scenarioStatus {
			case domain.TestStatusPassed:
				suite.Passed++
			case domain.TestStatusFailed:
				suite.Failed++
			case domain.TestStatusSkipped:
				suite.Skipped++
			}
		}
	}

	suite.TotalTests = len(suite.Cases)
	suite.Duration = totalDuration

	return suite, nil
}

func (p *CucumberParser) GetFramework() domain.Framework {
	return domain.FrameworkCucumber
}

func (p *CucumberParser) SupportedFileExtensions() []string {
	return []string{".json"}
}
