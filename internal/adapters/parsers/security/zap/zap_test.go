package zap

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestZAPParser_ParseAlerts(t *testing.T) {
	jsonReport := `{
    "@version": "2.14.0",
    "@generated": "",
    "site": [
        {
            "@name": "https://example.com",
            "@host": "example.com",
            "@port": "443",
            "@ssl": "true",
            "alerts": [
                {
                    "pluginid": "10016",
                    "alertRef": "10016",
                    "alert": "Web Browser XSS Protection Not Enabled",
                    "name": "Web Browser XSS Protection Not Enabled",
                    "riskcode": "3",
                    "confidence": "2",
                    "riskdesc": "High (Medium)",
                    "desc": "XSS protection header missing",
                    "instances": [{"uri": "https://example.com/", "method": "GET"}],
                    "count": "1",
                    "solution": "Set X-XSS-Protection header",
                    "cweid": "933",
                    "wascid": "14"
                },
                {
                    "pluginid": "10036",
                    "alertRef": "10036",
                    "alert": "Server Leaks Version Information",
                    "name": "Server Leaks Version Information",
                    "riskcode": "1",
                    "confidence": "3",
                    "riskdesc": "Low (High)",
                    "desc": "Server header leaks version",
                    "instances": [{"uri": "https://example.com/", "method": "GET"}],
                    "count": "1",
                    "solution": "Remove server header",
                    "cweid": "200",
                    "wascid": "13"
                }
            ]
        }
    ]
}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	if suite.TotalTests != 2 {
		t.Errorf("expected 2 total tests, got %d", suite.TotalTests)
	}
	if suite.Failed != 2 {
		t.Errorf("expected 2 failed (high + low risk), got %d", suite.Failed)
	}
	if suite.Passed != 0 {
		t.Errorf("expected 0 passed, got %d", suite.Passed)
	}

	for _, c := range suite.Cases {
		if c.ID == "10016" {
			if c.Priority != domain.SeverityHigh {
				t.Errorf("expected high severity for riskcode 3, got %s", c.Priority)
			}
			if c.Status != domain.StatusFailed {
				t.Errorf("expected failed status for high risk, got %s", c.Status)
			}
		}
		if c.ID == "10036" {
			if c.Priority != domain.SeverityLow {
				t.Errorf("expected low severity for riskcode 1, got %s", c.Priority)
			}
			if c.Status != domain.StatusFailed {
				t.Errorf("expected failed status for low risk, got %s", c.Status)
			}
		}
	}
}

func TestZAPParser_EmptyInput(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader(""))
	if err == nil {
		t.Error("expected error for empty input")
	}
}

func TestZAPParser_MalformedJSON(t *testing.T) {
	parser := New()
	_, err := parser.Parse(strings.NewReader("{not valid"))
	if err == nil {
		t.Error("expected error for malformed JSON")
	}
}
