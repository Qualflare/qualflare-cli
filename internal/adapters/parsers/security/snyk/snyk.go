package snyk

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Snyk JSON test output
type Parser struct{}

// Snyk JSON structures
type Report struct {
	OK              bool            `json:"ok"`
	DependencyCount int             `json:"dependencyCount"`
	Org             string          `json:"org"`
	Policy          string          `json:"policy"`
	IsPrivate       bool            `json:"isPrivate"`
	PackageManager  string          `json:"packageManager"`
	ProjectName     string          `json:"projectName"`
	Summary         string          `json:"summary"`
	FilePath        string          `json:"filePath"`
	UniqueCount     int             `json:"uniqueCount"`
	Vulnerabilities []Vulnerability `json:"vulnerabilities"`
	Remediation     *Remediation    `json:"remediation,omitempty"`
}

type Vulnerability struct {
	ID               string        `json:"id"`
	Title            string        `json:"title"`
	Severity         string        `json:"severity"`
	Description      string        `json:"description"`
	CVSSv3           string        `json:"CVSSv3"`
	CVSSScore        float64       `json:"cvssScore"`
	PackageName      string        `json:"packageName"`
	Version          string        `json:"version"`
	From             []string      `json:"from"`
	UpgradePath      []interface{} `json:"upgradePath"`
	IsPatchable      bool          `json:"isPatchable"`
	IsUpgradable     bool          `json:"isUpgradable"`
	Language         string        `json:"language"`
	PackageManager   string        `json:"packageManager"`
	PublicationTime  string        `json:"publicationTime"`
	ModificationTime string        `json:"modificationTime"`
	Identifiers      Identifiers   `json:"identifiers"`
	Semver           Semver        `json:"semver"`
	Exploit          string        `json:"exploit"`
	FixedIn          []string      `json:"fixedIn"`
	References       []Reference   `json:"references"`
}

type Identifiers struct {
	CVE []string `json:"CVE"`
	CWE []string `json:"CWE"`
}

type Semver struct {
	Vulnerable []string `json:"vulnerable"`
}

type Reference struct {
	URL   string `json:"url"`
	Title string `json:"title"`
}

type Remediation struct {
	Unresolved []Unresolved       `json:"unresolved"`
	Upgrade    map[string]Upgrade `json:"upgrade"`
	Patch      map[string]Patch   `json:"patch"`
	Ignore     map[string]Ignore  `json:"ignore"`
	Pin        map[string]Pin     `json:"pin"`
}

type Unresolved struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

type Upgrade struct {
	UpgradeTo string   `json:"upgradeTo"`
	Upgrades  []string `json:"upgrades"`
	Vulns     []string `json:"vulns"`
}

type Patch struct {
	Paths []string `json:"paths"`
}

type Ignore struct {
	Reason     string `json:"reason"`
	Expires    string `json:"expires"`
	ReasonType string `json:"reasonType"`
}

type Pin struct {
	Vulns []string `json:"vulns"`
}

// New creates a new Snyk parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Snyk JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      base.CoalesceString(report.ProjectName, "Snyk Security Scan"),
		Category:  domain.FrameworkSnyk.GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Process vulnerabilities
	for _, vuln := range report.Vulnerabilities {
		testCase := p.convertVulnerability(vuln)
		suite.Cases = append(suite.Cases, testCase)

		// Count by severity
		switch vuln.Severity {
		case "critical", "high":
			suite.Failed++
		case "medium":
			suite.Failed++
		default:
			suite.Failed++
		}
	}

	suite.TotalTests = len(suite.Cases)

	// Add metadata as properties
	suite.Properties = map[string]string{
		"projectName":     report.ProjectName,
		"packageManager":  report.PackageManager,
		"dependencyCount": fmt.Sprintf("%d", report.DependencyCount),
		"uniqueCount":     fmt.Sprintf("%d", report.UniqueCount),
		"summary":         report.Summary,
	}

	return suite, nil
}

// convertVulnerability converts a Snyk vulnerability to domain.Case
func (p *Parser) convertVulnerability(vuln Vulnerability) domain.Case {
	testCase := domain.Case{
		ID:        vuln.ID,
		Name:      fmt.Sprintf("[%s] %s in %s@%s", vuln.Severity, vuln.Title, vuln.PackageName, vuln.Version),
		ClassName: vuln.PackageName,
	}

	// Map severity to status and domain.Severity
	switch vuln.Severity {
	case "critical":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityCritical
	case "high":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityHigh
	case "medium":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityMedium
	case "low":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityLow
	default:
		testCase.Status = domain.StatusPassed
		testCase.Priority = domain.SeverityUnknown
	}

	testCase.Error = domain.FormatError(vuln.Title, vuln.Description, "")

	// Add tags
	testCase.Tags = []string{
		"vulnerability",
		"severity:" + vuln.Severity,
		vuln.Language,
	}
	for _, cve := range vuln.Identifiers.CVE {
		testCase.Tags = append(testCase.Tags, cve)
	}
	for _, cwe := range vuln.Identifiers.CWE {
		testCase.Tags = append(testCase.Tags, cwe)
	}

	// Add properties
	testCase.Properties = map[string]string{
		"package":        vuln.PackageName,
		"version":        vuln.Version,
		"severity":       vuln.Severity,
		"cvssScore":      fmt.Sprintf("%.1f", vuln.CVSSScore),
		"isPatchable":    fmt.Sprintf("%t", vuln.IsPatchable),
		"isUpgradable":   fmt.Sprintf("%t", vuln.IsUpgradable),
		"language":       vuln.Language,
		"packageManager": vuln.PackageManager,
	}

	if len(vuln.FixedIn) > 0 {
		testCase.Properties["fixedIn"] = vuln.FixedIn[0]
	}

	if len(vuln.From) > 0 {
		testCase.Properties["dependencyPath"] = fmt.Sprintf("%v", vuln.From)
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkSnyk
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
