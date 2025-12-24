package parsers

import (
	"bufio"
	"encoding/json"
	"io"
	"qualflare-cli/internal/core/domain"
	"strings"
	"time"
)

type GolangParser struct{}

type GolangTestEvent struct {
	Time    time.Time `json:"Time"`
	Action  string    `json:"Action"`
	Package string    `json:"Package"`
	Test    string    `json:"Test,omitempty"`
	Elapsed float64   `json:"Elapsed,omitempty"`
	Output  string    `json:"Output,omitempty"`
}

func NewGolangParser() *GolangParser {
	return &GolangParser{}
}

func (p *GolangParser) Parse(reader io.Reader) (*domain.Suite, error) {
	scanner := bufio.NewScanner(reader)
	tests := make(map[string]*domain.Case)
	suite := &domain.Suite{
		Name:      "Go Cases",
		Timestamp: time.Now(),
		Cases:     make([]domain.Case, 0),
	}

	var totalDuration time.Duration

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "{") {
			continue
		}

		var event GolangTestEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			continue
		}

		if event.Test == "" {
			if event.Action == "pass" || event.Action == "fail" {
				totalDuration = time.Duration(event.Elapsed * float64(time.Second))
			}
			continue
		}

		testKey := event.Package + "." + event.Test

		switch event.Action {
		case "run":
			tests[testKey] = &domain.Case{
				ID:   testKey,
				Name: event.Test,
			}
		case "pass":
			if test, exists := tests[testKey]; exists {
				test.Status = domain.TestStatusPassed
				test.Duration = time.Duration(event.Elapsed * float64(time.Second))
				suite.Passed++
			}
		case "fail":
			if test, exists := tests[testKey]; exists {
				test.Status = domain.TestStatusFailed
				test.Duration = time.Duration(event.Elapsed * float64(time.Second))
				suite.Failed++
			}
		case "skip":
			if test, exists := tests[testKey]; exists {
				test.Status = domain.TestStatusSkipped
				test.Duration = time.Duration(event.Elapsed * float64(time.Second))
				suite.Skipped++
			}
		case "output":
			if test, exists := tests[testKey]; exists && event.Output != "" {
				if strings.Contains(event.Output, "FAIL") || strings.Contains(event.Output, "panic") {
					if test.ErrorMessage == "" {
						test.ErrorMessage = event.Output
					} else {
						test.StackTrace += event.Output
					}
				}
			}
		}
	}

	// Convert map to slice
	for _, test := range tests {
		suite.Cases = append(suite.Cases, *test)
	}

	suite.TotalTests = len(suite.Cases)
	suite.Duration = totalDuration

	return suite, scanner.Err()
}

func (p *GolangParser) GetFramework() domain.Framework {
	return domain.FrameworkGolang
}

func (p *GolangParser) SupportedFileExtensions() []string {
	return []string{".json", ".out"}
}
