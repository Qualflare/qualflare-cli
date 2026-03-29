package k6

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses k6 JSON summary output
type Parser struct{}

// k6 JSON structures
type Report struct {
	RootGroup Group              `json:"root_group"`
	Options   Options            `json:"options"`
	State     State              `json:"state"`
	Metrics   map[string]Metric  `json:"metrics"`
}

type Group struct {
	Name   string  `json:"name"`
	Path   string  `json:"path"`
	ID     string  `json:"id"`
	Groups []Group `json:"groups"`
	Checks []Check `json:"checks"`
}

type Check struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	ID     string `json:"id"`
	Passes int64  `json:"passes"`
	Fails  int64  `json:"fails"`
}

type Options struct {
	SummaryTrendStats []string `json:"summaryTrendStats"`
}

type State struct {
	IsStdOutTTY       bool    `json:"isStdOutTTY"`
	IsStdErrTTY       bool    `json:"isStdErrTTY"`
	TestRunDurationMs float64 `json:"testRunDurationMs"`
}

type Metric struct {
	Type       string               `json:"type"`
	Contains   string               `json:"contains"`
	Values     map[string]float64   `json:"values"`
	Thresholds map[string]Threshold `json:"thresholds,omitempty"`
}

type Threshold struct {
	OK bool `json:"ok"`
}

// New creates a new k6 parser
func New() *Parser {
	return &Parser{}
}

// Parse parses k6 JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      base.CoalesceString(report.RootGroup.Name, "k6 Load Test Results"),
		Category:  domain.FrameworkK6.GetCategory(),
		Duration:  time.Duration(report.State.TestRunDurationMs) * time.Millisecond,
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Process checks from the root group and track assertions
	p.processGroup(report.RootGroup, suite, "")

	// Process threshold violations as test cases
	p.processThresholds(report.Metrics, suite)

	// Calculate total assertions from all checks (passes + fails)
	for _, c := range suite.Cases {
		if passes, ok := c.Properties["passes"]; ok {
			var p int
			if _, err := fmt.Sscanf(passes, "%d", &p); err == nil {
				suite.Assertions += p
			}
		}
		if fails, ok := c.Properties["fails"]; ok {
			var f int
			if _, err := fmt.Sscanf(fails, "%d", &f); err == nil {
				suite.Assertions += f
			}
		}
	}

	// Calculate totals
	suite.TotalTests = len(suite.Cases)

	// Add performance metrics as suite properties
	suite.Properties = p.extractMetricsSummary(report.Metrics)

	return suite, nil
}

// processGroup recursively processes k6 groups and checks
func (p *Parser) processGroup(group Group, suite *domain.Suite, prefix string) {
	currentPrefix := group.Name
	if prefix != "" {
		currentPrefix = prefix + " > " + group.Name
	}

	// Process checks in this group
	for _, check := range group.Checks {
		testCase := p.convertCheck(check, currentPrefix)
		suite.Cases = append(suite.Cases, testCase)

		// Update counters
		if check.Fails == 0 {
			suite.Passed++
		} else if check.Passes == 0 {
			suite.Failed++
		} else {
			// Partial failures - count as failed
			suite.Failed++
		}
	}

	// Process nested groups
	for _, nestedGroup := range group.Groups {
		p.processGroup(nestedGroup, suite, currentPrefix)
	}
}

// convertCheck converts a k6 check to domain.Case
func (p *Parser) convertCheck(check Check, groupPath string) domain.Case {
	testCase := domain.Case{
		ID:   check.ID,
		Name: check.Name,
	}

	total := check.Passes + check.Fails
	passRate := float64(0)
	if total > 0 {
		passRate = float64(check.Passes) / float64(total) * 100
	}

	if check.Fails == 0 {
		testCase.Status = domain.StatusPassed
	} else if check.Passes == 0 {
		testCase.Status = domain.StatusFailed
		testCase.Error = fmt.Sprintf("All %d checks failed", check.Fails)
	} else {
		testCase.Status = domain.StatusFailed
		testCase.Error = fmt.Sprintf("%.1f%% pass rate (%d passed, %d failed)", passRate, check.Passes, check.Fails)
	}

	testCase.Properties = map[string]string{
		"path":     check.Path,
		"passes":   fmt.Sprintf("%d", check.Passes),
		"fails":    fmt.Sprintf("%d", check.Fails),
		"passRate": fmt.Sprintf("%.1f%%", passRate),
		"group":    groupPath,
	}

	return testCase
}

// processThresholds creates test cases for threshold violations
func (p *Parser) processThresholds(metrics map[string]Metric, suite *domain.Suite) {
	for metricName, metric := range metrics {
		if len(metric.Thresholds) == 0 {
			continue
		}

		for thresholdName, threshold := range metric.Thresholds {
			testCase := domain.Case{
				ID:   metricName + "_" + thresholdName,
				Name: fmt.Sprintf("Threshold: %s %s", metricName, thresholdName),
				Tags: []string{"threshold"},
			}

			if threshold.OK {
				testCase.Status = domain.StatusPassed
				suite.Passed++
			} else {
				testCase.Status = domain.StatusFailed
				testCase.Error = fmt.Sprintf("Threshold '%s' failed for metric '%s'", thresholdName, metricName)
				suite.Failed++
			}

			// Add metric values as properties
			testCase.Properties = make(map[string]string)
			for k, v := range metric.Values {
				testCase.Properties[k] = fmt.Sprintf("%.2f", v)
			}

			suite.Cases = append(suite.Cases, testCase)
		}
	}
}

// extractMetricsSummary extracts key performance metrics
func (p *Parser) extractMetricsSummary(metrics map[string]Metric) map[string]string {
	summary := make(map[string]string)

	// Extract key metrics
	keyMetrics := []string{
		"http_req_duration",
		"http_req_failed",
		"http_reqs",
		"iterations",
		"vus",
		"vus_max",
	}

	for _, key := range keyMetrics {
		if metric, ok := metrics[key]; ok {
			if avg, ok := metric.Values["avg"]; ok {
				summary[key+"_avg"] = fmt.Sprintf("%.2f", avg)
			}
			if p95, ok := metric.Values["p(95)"]; ok {
				summary[key+"_p95"] = fmt.Sprintf("%.2f", p95)
			}
			if count, ok := metric.Values["count"]; ok {
				summary[key+"_count"] = fmt.Sprintf("%.0f", count)
			}
			if rate, ok := metric.Values["rate"]; ok {
				summary[key+"_rate"] = fmt.Sprintf("%.2f", rate)
			}
		}
	}

	return summary
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkK6
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
