package sonarqube

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses SonarQube issues export
type Parser struct{}

// SonarQube JSON structures
type Report struct {
	Total       int         `json:"total"`
	P           int         `json:"p"`  // Page number
	PS          int         `json:"ps"` // Page size
	Paging      Paging      `json:"paging"`
	EffortTotal int         `json:"effortTotal"`
	Issues      []Issue     `json:"issues"`
	Components  []Component `json:"components"`
	Rules       []Rule      `json:"rules"`
	Users       []User      `json:"users"`
}

type Paging struct {
	PageIndex int `json:"pageIndex"`
	PageSize  int `json:"pageSize"`
	Total     int `json:"total"`
}

type Issue struct {
	Key               string     `json:"key"`
	Rule              string     `json:"rule"`
	Severity          string     `json:"severity"`
	Component         string     `json:"component"`
	Project           string     `json:"project"`
	Line              int        `json:"line"`
	TextRange         *TextRange `json:"textRange,omitempty"`
	Flows             []Flow     `json:"flows"`
	Status            string     `json:"status"`
	Message           string     `json:"message"`
	Effort            string     `json:"effort"`
	Debt              string     `json:"debt"`
	Assignee          string     `json:"assignee"`
	Author            string     `json:"author"`
	Tags              []string   `json:"tags"`
	CreationDate      string     `json:"creationDate"`
	UpdateDate        string     `json:"updateDate"`
	Type              string     `json:"type"`
	Scope             string     `json:"scope"`
	QuickFixAvailable bool       `json:"quickFixAvailable"`
}

type TextRange struct {
	StartLine   int `json:"startLine"`
	EndLine     int `json:"endLine"`
	StartOffset int `json:"startOffset"`
	EndOffset   int `json:"endOffset"`
}

type Flow struct {
	Locations []Location `json:"locations"`
}

type Location struct {
	Component string     `json:"component"`
	TextRange *TextRange `json:"textRange,omitempty"`
	Msg       string     `json:"msg"`
}

type Component struct {
	Key       string `json:"key"`
	Enabled   bool   `json:"enabled"`
	Qualifier string `json:"qualifier"`
	Name      string `json:"name"`
	LongName  string `json:"longName"`
	Path      string `json:"path"`
}

type Rule struct {
	Key      string `json:"key"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Lang     string `json:"lang"`
	LangName string `json:"langName"`
}

type User struct {
	Login  string `json:"login"`
	Name   string `json:"name"`
	Active bool   `json:"active"`
}

// New creates a new SonarQube parser
func New() *Parser {
	return &Parser{}
}

// Parse parses SonarQube JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      "SonarQube Analysis",
		Category:  domain.FrameworkSonarQube.GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Build component map for quick lookup
	componentMap := make(map[string]Component)
	for _, comp := range report.Components {
		componentMap[comp.Key] = comp
	}

	// Build rule map for quick lookup
	ruleMap := make(map[string]Rule)
	for _, rule := range report.Rules {
		ruleMap[rule.Key] = rule
	}

	// Process issues
	for _, issue := range report.Issues {
		testCase := p.convertIssue(issue, componentMap, ruleMap)
		suite.Cases = append(suite.Cases, testCase)

		// Count by severity
		switch issue.Severity {
		case "BLOCKER", "CRITICAL":
			suite.Failed++
		case "MAJOR":
			suite.Failed++
		case "MINOR":
			suite.Failed++
		case "INFO":
			suite.Passed++
		default:
			suite.Passed++
		}
	}

	suite.TotalTests = len(suite.Cases)

	// Add paging info
	suite.Properties = map[string]string{
		"total":       fmt.Sprintf("%d", report.Paging.Total),
		"effortTotal": fmt.Sprintf("%d", report.EffortTotal),
	}

	return suite, nil
}

// convertIssue converts a SonarQube issue to domain.Case
func (p *Parser) convertIssue(issue Issue, components map[string]Component, rules map[string]Rule) domain.Case {
	// Get component path
	componentPath := issue.Component
	if comp, ok := components[issue.Component]; ok {
		componentPath = base.CoalesceString(comp.Path, comp.LongName, comp.Name)
	}

	// Get rule name
	ruleName := issue.Rule
	if rule, ok := rules[issue.Rule]; ok {
		ruleName = rule.Name
	}

	testCase := domain.Case{
		ID:        issue.Key,
		Name:      fmt.Sprintf("[%s] %s", issue.Severity, issue.Message),
		ClassName: componentPath,
	}

	// Map severity to status and domain.Severity
	switch issue.Severity {
	case "BLOCKER":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityCritical
	case "CRITICAL":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityCritical
	case "MAJOR":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityMedium
	case "MINOR":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityLow
	case "INFO":
		testCase.Status = domain.StatusPassed
		testCase.Priority = domain.SeverityInfo
	default:
		testCase.Status = domain.StatusPassed
		testCase.Priority = domain.SeverityUnknown
	}

	// Build error with line info as stack trace
	stackTrace := ""
	if issue.TextRange != nil {
		stackTrace = fmt.Sprintf("Line %d-%d", issue.TextRange.StartLine, issue.TextRange.EndLine)
	} else if issue.Line > 0 {
		stackTrace = fmt.Sprintf("Line %d", issue.Line)
	}
	testCase.Error = domain.FormatError(issue.Message, stackTrace, "")

	// Add tags
	testCase.Tags = append([]string{
		"sonarqube",
		"severity:" + issue.Severity,
		"type:" + issue.Type,
		"status:" + issue.Status,
	}, issue.Tags...)

	// Add properties
	testCase.Properties = map[string]string{
		"rule":      issue.Rule,
		"ruleName":  ruleName,
		"severity":  issue.Severity,
		"type":      issue.Type,
		"status":    issue.Status,
		"effort":    issue.Effort,
		"component": issue.Component,
		"project":   issue.Project,
	}

	if issue.Line > 0 {
		testCase.Properties["line"] = fmt.Sprintf("%d", issue.Line)
	}

	if issue.Assignee != "" {
		testCase.Properties["assignee"] = issue.Assignee
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkSonarQube
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
