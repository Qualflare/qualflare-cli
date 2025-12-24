package zap

import (
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"time"

	"qualflare-cli/internal/core/domain"
)

// Parser parses OWASP ZAP JSON output
type Parser struct{}

// ZAP JSON structures
type Report struct {
	Version   string `json:"@version"`
	Generated string `json:"@generated"`
	Site      []Site `json:"site"`
}

type Site struct {
	Name   string  `json:"@name"`
	Host   string  `json:"@host"`
	Port   string  `json:"@port"`
	SSL    string  `json:"@ssl"`
	Alerts []Alert `json:"alerts"`
}

type Alert struct {
	PluginID   string            `json:"pluginid"`
	AlertRef   string            `json:"alertRef"`
	Alert      string            `json:"alert"`
	Name       string            `json:"name"`
	RiskCode   string            `json:"riskcode"`
	Confidence string            `json:"confidence"`
	RiskDesc   string            `json:"riskdesc"`
	Desc       string            `json:"desc"`
	Instances  []Instance        `json:"instances"`
	Count      string            `json:"count"`
	Solution   string            `json:"solution"`
	OtherInfo  string            `json:"otherinfo"`
	Reference  string            `json:"reference"`
	CWEId      string            `json:"cweid"`
	WAScId     string            `json:"wascid"`
	SourceID   string            `json:"sourceid"`
	Tags       map[string]string `json:"tags"`
}

type Instance struct {
	URI       string `json:"uri"`
	Method    string `json:"method"`
	Param     string `json:"param"`
	Attack    string `json:"attack"`
	Evidence  string `json:"evidence"`
	OtherInfo string `json:"otherinfo"`
}

// New creates a new ZAP parser
func New() *Parser {
	return &Parser{}
}

// Parse parses ZAP JSON content
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	var report Report
	decoder := json.NewDecoder(reader)

	if err := decoder.Decode(&report); err != nil {
		return nil, err
	}

	suite := &domain.Suite{
		Name:      "OWASP ZAP Security Scan",
		Category:  domain.FrameworkZAP.GetCategory(),
		Timestamp: time.Now(),
		Cases:     make([]domain.Case, 0),
	}

	// Parse generated time if available
	if report.Generated != "" {
		if t, err := time.Parse("Mon, 02 Jan 2006 15:04:05", report.Generated); err == nil {
			suite.Timestamp = t
		}
	}

	// Process each site
	for _, site := range report.Site {
		for _, alert := range site.Alerts {
			testCase := p.convertAlert(alert, site)
			suite.Cases = append(suite.Cases, testCase)

			// Count by risk level
			riskCode, _ := strconv.Atoi(alert.RiskCode)
			switch riskCode {
			case 3: // High
				suite.Failed++
			case 2: // Medium
				suite.Failed++
			case 1: // Low
				suite.Passed++ // Treat low risk as passed for counting
			default: // Informational
				suite.Passed++
			}
		}
	}

	suite.TotalTests = len(suite.Cases)

	// Add version as property
	suite.Properties = map[string]string{
		"zapVersion": report.Version,
	}

	return suite, nil
}

// convertAlert converts a ZAP alert to domain.Case
func (p *Parser) convertAlert(alert Alert, site Site) domain.Case {
	riskCode, _ := strconv.Atoi(alert.RiskCode)
	riskLevel := p.getRiskLevel(riskCode)

	testCase := domain.Case{
		ID:        alert.PluginID,
		Name:      fmt.Sprintf("[%s] %s", riskLevel, alert.Name),
		ClassName: site.Host,
	}

	// Map risk to status and domain.Severity
	switch riskCode {
	case 3: // High
		testCase.Status = domain.StatusFailed
		testCase.Severity = domain.SeverityHigh
	case 2: // Medium
		testCase.Status = domain.StatusFailed
		testCase.Severity = domain.SeverityMedium
	case 1: // Low
		testCase.Status = domain.StatusPassed
		testCase.Severity = domain.SeverityLow
	default: // Informational
		testCase.Status = domain.StatusPassed
		testCase.Severity = domain.SeverityInfo
	}

	testCase.ErrorMessage = alert.Desc
	testCase.StackTrace = alert.Solution

	// Add tags
	testCase.Tags = []string{
		"security",
		"risk:" + riskLevel,
	}
	if alert.CWEId != "" && alert.CWEId != "0" {
		testCase.Tags = append(testCase.Tags, "CWE-"+alert.CWEId)
	}
	if alert.WAScId != "" && alert.WAScId != "0" {
		testCase.Tags = append(testCase.Tags, "WASC-"+alert.WAScId)
	}

	// Add properties
	testCase.Properties = map[string]string{
		"host":       site.Host,
		"port":       site.Port,
		"riskCode":   alert.RiskCode,
		"riskDesc":   alert.RiskDesc,
		"confidence": alert.Confidence,
		"cweId":      alert.CWEId,
		"wascId":     alert.WAScId,
		"solution":   alert.Solution,
		"reference":  alert.Reference,
	}

	// Add instance count
	if count, err := strconv.Atoi(alert.Count); err == nil {
		testCase.Properties["instanceCount"] = fmt.Sprintf("%d", count)
	}

	// Add first instance URL if available
	if len(alert.Instances) > 0 {
		testCase.Properties["affectedURL"] = alert.Instances[0].URI
		testCase.Properties["method"] = alert.Instances[0].Method
	}

	return testCase
}

// getRiskLevel returns the risk level string
func (p *Parser) getRiskLevel(riskCode int) string {
	switch riskCode {
	case 3:
		return "High"
	case 2:
		return "Medium"
	case 1:
		return "Low"
	default:
		return "Informational"
	}
}

// GetFramework returns the framework type
func (p *Parser) GetFramework() domain.Framework {
	return domain.FrameworkZAP
}

// SupportedFileExtensions returns supported file extensions
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json", ".xml"}
}
