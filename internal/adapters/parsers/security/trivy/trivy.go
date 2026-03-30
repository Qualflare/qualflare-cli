package trivy

import (
	"encoding/json"
	"fmt"
	"io"
	"time"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Trivy JSON output
type Parser struct{}

// Trivy JSON structures
type Report struct {
	SchemaVersion int       `json:"SchemaVersion"`
	ArtifactName  string    `json:"ArtifactName"`
	ArtifactType  string    `json:"ArtifactType"`
	Metadata      Metadata  `json:"Metadata"`
	Results       []Result  `json:"Results"`
}

type Metadata struct {
	RepoTags    []string    `json:"RepoTags"`
	RepoDigests []string    `json:"RepoDigests"`
	ImageConfig ImageConfig `json:"ImageConfig"`
}

type ImageConfig struct {
	Architecture string `json:"architecture"`
	OS           string `json:"os"`
}

type Result struct {
	Target            string          `json:"Target"`
	Class             string          `json:"Class"`
	Type              string          `json:"Type"`
	Vulnerabilities   []Vulnerability `json:"Vulnerabilities"`
	MisconfSummary    *MisconfSummary `json:"MisconfSummary,omitempty"`
	Misconfigurations []Misconfig     `json:"Misconfigurations,omitempty"`
}

type Vulnerability struct {
	VulnerabilityID  string           `json:"VulnerabilityID"`
	PkgID            string           `json:"PkgID"`
	PkgName          string           `json:"PkgName"`
	InstalledVersion string           `json:"InstalledVersion"`
	FixedVersion     string           `json:"FixedVersion"`
	Layer            *Layer           `json:"Layer,omitempty"`
	SeveritySource   string           `json:"SeveritySource"`
	PrimaryURL       string           `json:"PrimaryURL"`
	Title            string           `json:"Title"`
	Description      string           `json:"Description"`
	Severity         string           `json:"Severity"`
	CweIDs           []string         `json:"CweIDs"`
	CVSS             map[string]CVSS  `json:"CVSS"`
	References       []string         `json:"References"`
	PublishedDate    string           `json:"PublishedDate"`
	LastModifiedDate string           `json:"LastModifiedDate"`
}

type Layer struct {
	DiffID string `json:"DiffID"`
	Digest string `json:"Digest"`
}

type CVSS struct {
	V2Vector string  `json:"V2Vector,omitempty"`
	V3Vector string  `json:"V3Vector,omitempty"`
	V2Score  float64 `json:"V2Score,omitempty"`
	V3Score  float64 `json:"V3Score,omitempty"`
}

type MisconfSummary struct {
	Successes  int `json:"Successes"`
	Failures   int `json:"Failures"`
	Exceptions int `json:"Exceptions"`
}

type Misconfig struct {
	Type        string `json:"Type"`
	ID          string `json:"ID"`
	Title       string `json:"Title"`
	Description string `json:"Description"`
	Message     string `json:"Message"`
	Resolution  string `json:"Resolution"`
	Severity    string `json:"Severity"`
	PrimaryURL  string `json:"PrimaryURL"`
	Status      string `json:"Status"`
}

// New creates a new Trivy parser
func New() *Parser {
	return &Parser{}
}

// Parse parses Trivy JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      base.CoalesceString(report.ArtifactName, "Trivy Security Scan"),
		Category:  domain.FrameworkTrivy.GetCategory(),
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Process each result (target)
	for _, result := range report.Results {
		// Process vulnerabilities
		for _, vuln := range result.Vulnerabilities {
			testCase := p.convertVulnerability(vuln, result.Target)
			suite.Cases = append(suite.Cases, testCase)

			suite.Failed++
		}

		// Process misconfigurations
		for _, misconf := range result.Misconfigurations {
			testCase := p.convertMisconfig(misconf, result.Target)
			suite.Cases = append(suite.Cases, testCase)

			if misconf.Status == "FAIL" {
				suite.Failed++
			} else {
				suite.Passed++
			}
		}
	}

	suite.TotalTests = len(suite.Cases)

	// Add metadata as properties
	suite.Properties = map[string]string{
		"artifactName": report.ArtifactName,
		"artifactType": report.ArtifactType,
	}

	return suite, nil
}

// convertVulnerability converts a Trivy vulnerability to domain.Case
func (p *Parser) convertVulnerability(vuln Vulnerability, target string) domain.Case {
	testCase := domain.Case{
		ID:        vuln.VulnerabilityID,
		Name:      fmt.Sprintf("[%s] %s in %s", vuln.Severity, vuln.VulnerabilityID, vuln.PkgName),
		ClassName: target,
	}

	// Map severity to status and domain.Severity
	switch vuln.Severity {
	case "CRITICAL":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityCritical
	case "HIGH":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityHigh
	case "MEDIUM":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityMedium
	case "LOW":
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityLow
	default:
		testCase.Status = domain.StatusPassed
		testCase.Priority = domain.SeverityUnknown
	}

	testCase.Error = domain.FormatError(vuln.Title, vuln.Description, "")

	// Add tags based on severity
	testCase.Tags = []string{
		"vulnerability",
		"severity:" + vuln.Severity,
	}
	testCase.Tags = append(testCase.Tags, vuln.CweIDs...)

	// Add properties
	testCase.Properties = map[string]string{
		"package":          vuln.PkgName,
		"installedVersion": vuln.InstalledVersion,
		"fixedVersion":     vuln.FixedVersion,
		"severity":         vuln.Severity,
		"url":              vuln.PrimaryURL,
	}

	// Add CVSS score if available
	for source, cvss := range vuln.CVSS {
		if cvss.V3Score > 0 {
			testCase.Properties["cvss_"+source] = fmt.Sprintf("%.1f", cvss.V3Score)
		}
	}

	return testCase
}

// convertMisconfig converts a Trivy misconfiguration to domain.Case
func (p *Parser) convertMisconfig(misconf Misconfig, target string) domain.Case {
	testCase := domain.Case{
		ID:        misconf.ID,
		Name:      fmt.Sprintf("[%s] %s", misconf.Severity, misconf.Title),
		ClassName: target,
	}

	if misconf.Status == "FAIL" {
		testCase.Status = domain.StatusFailed
		testCase.Error = domain.FormatError(misconf.Message, misconf.Resolution, "")
	} else {
		testCase.Status = domain.StatusPassed
	}

	// Map severity
	switch misconf.Severity {
	case "CRITICAL":
		testCase.Priority = domain.SeverityCritical
	case "HIGH":
		testCase.Priority = domain.SeverityHigh
	case "MEDIUM":
		testCase.Priority = domain.SeverityMedium
	case "LOW":
		testCase.Priority = domain.SeverityLow
	default:
		testCase.Priority = domain.SeverityUnknown
	}

	testCase.Tags = []string{
		"misconfiguration",
		"severity:" + misconf.Severity,
		misconf.Type,
	}

	testCase.Properties = map[string]string{
		"type":       misconf.Type,
		"severity":   misconf.Severity,
		"resolution": misconf.Resolution,
		"url":        misconf.PrimaryURL,
	}

	return testCase
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkTrivy
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
