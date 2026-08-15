package zap

import (
	"strings"
	"testing"
	"time"

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

// BUG-34: ZAP emits a non-zero-padded day (Java "d"), e.g. "Wed, 7 Jul 2021".
// The old layout used "02" (zero-padded) so single-digit days failed to parse and
// the scan timestamp silently fell back to upload time (time.Now()).
func TestZAPParser_SingleDigitDayTimestamp(t *testing.T) {
	jsonReport := `{
    "@version": "2.14.0",
    "@generated": "Wed, 7 Jul 2021 10:30:00",
    "site": [
        {
            "@name": "https://example.com",
            "@host": "example.com",
            "@port": "443",
            "@ssl": "true",
            "alerts": []
        }
    ]
}`

	parser := New()
	suite, err := parser.Parse(strings.NewReader(jsonReport))
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}

	want := time.Date(2021, 7, 7, 10, 30, 0, 0, time.UTC)
	if !suite.Timestamp.Equal(want) {
		t.Errorf("expected timestamp %v parsed from single-digit day, got %v (fell back to now)", want, suite.Timestamp)
	}
}

// BUG-11: SupportedFileExtensions advertised ".xml" but Parse only decodes JSON,
// so every ZAP XML upload failed. Assert we no longer claim an extension Parse
// cannot handle.
func TestZAPParser_SupportedExtensionsAreParseable(t *testing.T) {
	parser := New()
	for _, ext := range parser.SupportedFileExtensions() {
		if ext == ".xml" {
			t.Errorf("SupportedFileExtensions claims %q but Parse only decodes JSON", ext)
		}
	}
}

// A riskcode the parser doesn't recognize (or that fails to parse) must fail
// closed for a security finding — never roll up as passed/green.
func TestZAPParser_UnknownRiskCodeFailsClosed(t *testing.T) {
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
                    "pluginid": "99999",
                    "name": "Novel Finding With Unexpected Risk",
                    "riskcode": "7",
                    "desc": "some finding",
                    "solution": "fix it"
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

	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}
	if suite.Cases[0].Status == domain.StatusPassed {
		t.Errorf("unknown riskcode must not be passed (false green); got %s", suite.Cases[0].Status)
	}
	if suite.Passed != 0 {
		t.Errorf("expected 0 passed for an unknown-risk finding, got %d", suite.Passed)
	}
	if suite.Failed != 1 {
		t.Errorf("expected 1 failed (fail closed), got %d", suite.Failed)
	}
}

// An alert with multiple instances (the same vulnerability found on several
// URLs) previously only captured Instances[0] — every other occurrence's
// URL/method was silently dropped, a real data-loss bug, not just an
// unused field.
func TestZAPParser_AllInstancesCaptured(t *testing.T) {
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
                    "name": "Web Browser XSS Protection Not Enabled",
                    "riskcode": "3",
                    "desc": "XSS protection header missing",
                    "instances": [
                        {"uri": "https://example.com/", "method": "GET"},
                        {"uri": "https://example.com/login", "method": "POST"},
                        {"uri": "https://example.com/admin", "method": "GET"}
                    ],
                    "count": "3",
                    "solution": "Set X-XSS-Protection header"
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
	if len(suite.Cases) != 1 {
		t.Fatalf("expected 1 case, got %d", len(suite.Cases))
	}

	props := suite.Cases[0].Properties
	for _, want := range []string{"https://example.com/", "https://example.com/login", "https://example.com/admin"} {
		if !strings.Contains(props["affectedURL"], want) {
			t.Errorf("affectedURL = %q, missing instance URL %q", props["affectedURL"], want)
		}
	}
	for _, want := range []string{"GET", "POST"} {
		if !strings.Contains(props["method"], want) {
			t.Errorf("method = %q, missing instance method %q", props["method"], want)
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
