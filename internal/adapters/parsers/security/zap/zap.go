package zap

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
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
		Timestamp: time.Now().UTC(),
		Cases:     make([]domain.Case, 0),
	}

	// Parse generated time if available.
	// BUG-34: ZAP emits a non-zero-padded day (Java "d"), e.g. "Wed, 7 Jul 2021".
	// A single "02" (zero-padded) layout failed to parse single-digit days, silently
	// falling back to the upload time. Try both single- and zero-padded day layouts,
	// and warn (never silently mask) when none match.
	if report.Generated != "" {
		layouts := []string{
			"Mon, 2 Jan 2006 15:04:05",  // single-digit day
			"Mon, 02 Jan 2006 15:04:05", // zero-padded day
		}
		parsed := false
		for _, layout := range layouts {
			if t, err := time.Parse(layout, report.Generated); err == nil {
				suite.Timestamp = t
				parsed = true
				break
			}
		}
		if !parsed {
			fmt.Fprintf(os.Stderr, "warning: zap: could not parse @generated timestamp %q; using upload time\n", report.Generated)
		}
	}

	// Process each site
	for _, site := range report.Site {
		for _, alert := range site.Alerts {
			testCase := p.convertAlert(alert, site)
			suite.Cases = append(suite.Cases, testCase)
		}
	}

	// Derive counters from the case statuses (never from independent increments)
	// so a real finding can never disagree with the cases or roll up green.
	suite.RecomputeCounts()

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
		ID: alert.PluginID,
		// Include the host: the same alert name across multiple scanned sites would
		// otherwise collapse into one row (the server dedupes by Name) — SYNC-02.
		Name:      fmt.Sprintf("[%s] %s (%s)", riskLevel, alert.Name, site.Host),
		ClassName: site.Host,
	}

	// Map risk to status and domain.Severity
	switch riskCode {
	case 3: // High
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityHigh
	case 2: // Medium
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityMedium
	case 1: // Low
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityLow
	default: // Informational or an unrecognized/unparseable risk code
		// A security finding is never a pass — fail closed so an unknown risk
		// code can't roll up green (the cardinal sin for a QA/security parser).
		testCase.Status = domain.StatusFailed
		testCase.Priority = domain.SeverityInfo
	}

	testCase.Error = domain.FormatError(alert.Desc, alert.Solution, "")

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
		testCase.Properties["instanceCount"] = strconv.Itoa(count)
	}

	// Capture every instance's URL/method, not just the first — an alert
	// commonly recurs across several URLs, and Instances[0]-only silently
	// dropped every other occurrence's evidence.
	if len(alert.Instances) > 0 {
		urls := make([]string, len(alert.Instances))
		methods := make([]string, len(alert.Instances))
		for i, inst := range alert.Instances {
			urls[i] = inst.URI
			methods[i] = inst.Method
		}
		testCase.Properties["affectedURL"] = strings.Join(urls, ", ")
		testCase.Properties["method"] = strings.Join(methods, ", ")
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

// SupportedFileExtensions returns supported file extensions.
// BUG-11: ".xml" was advertised but Parse only decodes JSON, so every ZAP XML
// upload failed. Dropped ".xml" (simplest correct fix) rather than adding an XML
// decode path, since Parse is JSON-only.
func (p *Parser) SupportedFileExtensions() []string {
	return []string{".json"}
}
