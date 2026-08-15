// Package pytestbdd extracts test cases and Given/When/Then steps from
// pytest-bdd's --gherkin-terminal-reporter console output — a flat
// boundary-line convention (Scenario:/step keyword lines/PASSED|FAILED),
// unlike RSpec's and Mocha's nested-describe indentation trees.
package pytestbdd

import (
	"bufio"
	"bytes"
	"regexp"
	"strings"

	"qualflare-cli/internal/adapters/logextract/shared/ansi"
	"qualflare-cli/internal/core/domain"
)

type Extractor struct{}

func New() *Extractor {
	return &Extractor{}
}

var (
	scenarioLine = regexp.MustCompile(`^Scenario(?: Outline)?:\s*(.+?)\s*$`)
	stepLine     = regexp.MustCompile(`^(Given|When|Then|And|But)\s+(.+?)\s*$`)
	resultLine   = regexp.MustCompile(`^(PASSED|FAILED|ERROR|SKIPPED)\b`)
)

// ExtractCases scans captured pytest-bdd output line by line: a "Scenario:"
// line opens a case, Given/When/Then/And/But lines accumulate steps, and a
// PASSED/FAILED/ERROR/SKIPPED line (pytest-bdd's own result marker, with any
// trailing "[ NN%]" progress suffix) closes it. Non-matching lines (blank
// lines, "Feature:" lines, pytest banners) are ignored.
func (e *Extractor) ExtractCases(output []byte) ([]domain.Case, error) {
	var cases []domain.Case
	var current *domain.Case

	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		line := strings.TrimRight(ansi.Strip(scanner.Text()), " \t\r")
		trimmed := strings.TrimSpace(line)

		if m := scenarioLine.FindStringSubmatch(trimmed); m != nil {
			if current != nil {
				// A new Scenario: line before the previous one closed — flush
				// it with whatever status it has (zero value if none matched),
				// rather than silently dropping it.
				cases = append(cases, *current)
			}
			current = &domain.Case{Name: m[1]}
			continue
		}

		if current == nil {
			continue
		}

		if m := stepLine.FindStringSubmatch(trimmed); m != nil {
			current.Steps = append(current.Steps, domain.Step{
				Keyword: m[1],
				Name:    m[2],
			})
			continue
		}

		if m := resultLine.FindStringSubmatch(trimmed); m != nil {
			current.Status = statusFromMarker(m[1])
			for i := range current.Steps {
				current.Steps[i].Status = current.Status
			}
			cases = append(cases, *current)
			current = nil
		}
	}
	if current != nil {
		cases = append(cases, *current)
	}
	return cases, scanner.Err()
}

func statusFromMarker(marker string) domain.Status {
	switch marker {
	case "PASSED":
		return domain.StatusPassed
	case "FAILED":
		return domain.StatusFailed
	case "SKIPPED":
		return domain.StatusSkipped
	default:
		return domain.StatusError
	}
}

// Detect reports whether output looks like pytest-bdd's
// --gherkin-terminal-reporter output: a Scenario: line plus at least one
// Given/When line.
func (e *Extractor) Detect(output []byte) bool {
	if !bytes.Contains(output, []byte("Scenario:")) {
		return false
	}
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		if stepLine.MatchString(strings.TrimSpace(scanner.Text())) {
			return true
		}
	}
	return false
}

func (e *Extractor) GetFramework() domain.Framework {
	return domain.FrameworkPython
}
