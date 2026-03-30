package factory

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"

	// API parsers
	"qualflare-cli/internal/adapters/parsers/api/k6"
	"qualflare-cli/internal/adapters/parsers/api/newman"

	// BDD parsers
	"qualflare-cli/internal/adapters/parsers/bdd/cucumber"
	"qualflare-cli/internal/adapters/parsers/bdd/karate"

	// E2E parsers
	"qualflare-cli/internal/adapters/parsers/e2e/cypress"
	"qualflare-cli/internal/adapters/parsers/e2e/playwright"
	"qualflare-cli/internal/adapters/parsers/e2e/selenium"
	"qualflare-cli/internal/adapters/parsers/e2e/testcafe"

	// Security parsers
	"qualflare-cli/internal/adapters/parsers/security/snyk"
	"qualflare-cli/internal/adapters/parsers/security/sonarqube"
	"qualflare-cli/internal/adapters/parsers/security/trivy"
	"qualflare-cli/internal/adapters/parsers/security/zap"

	// Unit test parsers
	"qualflare-cli/internal/adapters/parsers/unit/golang"
	"qualflare-cli/internal/adapters/parsers/unit/jest"
	"qualflare-cli/internal/adapters/parsers/unit/junit"
	"qualflare-cli/internal/adapters/parsers/unit/mocha"
	"qualflare-cli/internal/adapters/parsers/unit/phpunit"
	"qualflare-cli/internal/adapters/parsers/unit/pytest"
	"qualflare-cli/internal/adapters/parsers/unit/rspec"
)

// ParserFactory manages parser registration and detection
type ParserFactory struct {
	parsers map[domain.Framework]ports.Parser
}

// NewParserFactory creates a new parser factory with all registered parsers
func NewParserFactory() *ParserFactory {
	f := &ParserFactory{
		parsers: make(map[domain.Framework]ports.Parser),
	}

	// Unit Testing Parsers
	f.RegisterParser(junit.New())
	f.RegisterParser(pytest.New())
	f.RegisterParser(golang.New())
	f.RegisterParser(jest.New())
	f.RegisterParser(mocha.New())
	f.RegisterParser(rspec.New())
	f.RegisterParser(phpunit.New())

	// BDD Parsers
	f.RegisterParser(cucumber.New())
	f.RegisterParser(karate.New())

	// UI/E2E Testing Parsers
	f.RegisterParser(playwright.New())
	f.RegisterParser(cypress.New())
	f.RegisterParser(selenium.New())
	f.RegisterParser(testcafe.New())

	// API Testing Parsers
	f.RegisterParser(newman.New())
	f.RegisterParser(k6.New())

	// Security Testing Parsers
	f.RegisterParser(zap.New())
	f.RegisterParser(trivy.New())
	f.RegisterParser(snyk.New())
	f.RegisterParser(sonarqube.New())

	return f
}

// RegisterParser registers a parser for a framework
func (f *ParserFactory) RegisterParser(parser ports.Parser) {
	f.parsers[parser.GetFramework()] = parser
}

// GetParser returns a parser for the specified framework
func (f *ParserFactory) GetParser(framework domain.Framework) (ports.Parser, error) {
	parser, exists := f.parsers[framework]
	if !exists {
		return nil, fmt.Errorf("unsupported framework: %s", framework)
	}
	return parser, nil
}

// GetSupportedFrameworks returns all supported frameworks
func (f *ParserFactory) GetSupportedFrameworks() []domain.Framework {
	frameworks := make([]domain.Framework, 0, len(f.parsers))
	for framework := range f.parsers {
		frameworks = append(frameworks, framework)
	}
	return frameworks
}

// DetectFramework attempts to detect the framework from a filename
func (f *ParserFactory) DetectFramework(filename string) (domain.Framework, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	base := strings.ToLower(filepath.Base(filename))

	// Try to detect based on filename patterns
	switch {
	// Security tools
	case strings.Contains(base, "trivy"):
		return domain.FrameworkTrivy, nil
	case strings.Contains(base, "snyk"):
		return domain.FrameworkSnyk, nil
	case strings.Contains(base, "zap") || strings.Contains(base, "owasp"):
		return domain.FrameworkZAP, nil
	case strings.Contains(base, "sonar"):
		return domain.FrameworkSonarQube, nil

	// UI/E2E tools
	case strings.Contains(base, "playwright"):
		return domain.FrameworkPlaywright, nil
	case strings.Contains(base, "cypress") || strings.Contains(base, "mochawesome"):
		return domain.FrameworkCypress, nil
	case strings.Contains(base, "testcafe"):
		return domain.FrameworkTestCafe, nil
	case strings.Contains(base, "selenium") || strings.Contains(base, "webdriver"):
		return domain.FrameworkSelenium, nil

	// API tools
	case strings.Contains(base, "newman") || strings.Contains(base, "postman"):
		return domain.FrameworkNewman, nil
	case strings.Contains(base, "k6"):
		return domain.FrameworkK6, nil
	case strings.Contains(base, "karate"):
		return domain.FrameworkKarate, nil

	// BDD
	case strings.Contains(base, "cucumber") || strings.Contains(base, "feature"):
		return domain.FrameworkCucumber, nil

	// Unit testing
	case strings.Contains(base, "jest"):
		return domain.FrameworkJest, nil
	case strings.Contains(base, "mocha"):
		return domain.FrameworkMocha, nil
	case strings.Contains(base, "rspec"):
		return domain.FrameworkRSpec, nil
	case strings.Contains(base, "phpunit"):
		return domain.FrameworkPHPUnit, nil
	case strings.Contains(base, "pytest") || strings.Contains(base, "python"):
		return domain.FrameworkPython, nil
	case strings.Contains(base, "go-test") || (strings.Contains(base, "go") && (ext == ".json" || ext == ".out")):
		return domain.FrameworkGolang, nil

	// Default based on extension
	case ext == ".xml":
		return domain.FrameworkJUnit, nil
	case ext == ".json":
		return "", fmt.Errorf("unable to detect framework for JSON file: %s. Please specify the framework with --format", filename)
	}

	return "", fmt.Errorf("unable to detect framework for file: %s", filename)
}

// DetectFrameworkFromContent attempts to detect the framework from file content
func (f *ParserFactory) DetectFrameworkFromContent(filename string, content []byte) (domain.Framework, error) {
	ext := strings.ToLower(filepath.Ext(filename))

	// Try content-based detection
	if ext == ".json" {
		framework, err := f.detectJSONFramework(content)
		if err == nil {
			return framework, nil
		}
	} else if ext == ".xml" {
		framework, err := f.detectXMLFramework(content)
		if err == nil {
			return framework, nil
		}
	}

	// Fall back to filename-based detection
	return f.DetectFramework(filename)
}

// detectJSONFramework detects the framework from JSON content
func (f *ParserFactory) detectJSONFramework(content []byte) (domain.Framework, error) {
	// Try to parse as JSON and look for characteristic keys
	var data interface{}
	if err := json.Unmarshal(content, &data); err != nil {
		return "", err
	}

	switch v := data.(type) {
	case []interface{}:
		// Array of objects - check first element
		if len(v) > 0 {
			if obj, ok := v[0].(map[string]interface{}); ok {
				return f.detectJSONObjectFramework(obj, true)
			}
		}
	case map[string]interface{}:
		return f.detectJSONObjectFramework(v, false)
	}

	return "", fmt.Errorf("unable to detect framework from JSON content")
}

func hasKey(obj map[string]interface{}, key string) bool {
	_, ok := obj[key]
	return ok
}

func hasKeys(obj map[string]interface{}, keys ...string) bool {
	for _, k := range keys {
		if _, ok := obj[k]; !ok {
			return false
		}
	}
	return true
}

type jsonDetector struct {
	detect    func(obj map[string]interface{}, isArray bool) bool
	framework domain.Framework
}

var jsonDetectors = []jsonDetector{
	{func(obj map[string]interface{}, _ bool) bool { return hasKey(obj, "testResults") }, domain.FrameworkJest},
	{func(obj map[string]interface{}, _ bool) bool { return hasKey(obj, "numTotalTests") }, domain.FrameworkJest},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "config", "suites") }, domain.FrameworkPlaywright},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "stats", "results") }, domain.FrameworkCypress},
	{func(obj map[string]interface{}, _ bool) bool { return hasKey(obj, "collection") }, domain.FrameworkNewman},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "run", "collection") }, domain.FrameworkNewman},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "metrics", "root_group") }, domain.FrameworkK6},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "Results", "SchemaVersion") }, domain.FrameworkTrivy},
	{func(obj map[string]interface{}, _ bool) bool { return hasKey(obj, "Vulnerabilities") }, domain.FrameworkTrivy},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "vulnerabilities", "projectName") }, domain.FrameworkSnyk},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "site", "@version") }, domain.FrameworkZAP},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "issues", "paging") }, domain.FrameworkSonarQube},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "Action", "Package") }, domain.FrameworkGolang},
	{func(obj map[string]interface{}, _ bool) bool { return hasKey(obj, "examples") }, domain.FrameworkRSpec},
	{func(obj map[string]interface{}, isArray bool) bool { return isArray && hasKeys(obj, "elements", "keyword") }, domain.FrameworkCucumber},
	{func(obj map[string]interface{}, isArray bool) bool { return isArray && hasKey(obj, "scenarioResults") }, domain.FrameworkKarate},
	{func(obj map[string]interface{}, _ bool) bool { return hasKey(obj, "fixtures") }, domain.FrameworkTestCafe},
	{func(obj map[string]interface{}, _ bool) bool { return hasKeys(obj, "stats", "tests") }, domain.FrameworkMocha},
}

// detectJSONObjectFramework detects framework from a JSON object's keys
func (f *ParserFactory) detectJSONObjectFramework(obj map[string]interface{}, isArray bool) (domain.Framework, error) {
	for _, d := range jsonDetectors {
		if d.detect(obj, isArray) {
			return d.framework, nil
		}
	}
	return "", fmt.Errorf("unable to detect framework from JSON object")
}

// detectXMLFramework detects the framework from XML content
func (f *ParserFactory) detectXMLFramework(content []byte) (domain.Framework, error) {
	// Look for root element
	content = bytes.TrimSpace(content)

	// Skip XML declaration
	if bytes.HasPrefix(content, []byte("<?xml")) {
		idx := bytes.Index(content, []byte("?>"))
		if idx > 0 {
			content = bytes.TrimSpace(content[idx+2:])
		}
	}

	// Check for common root elements
	if bytes.HasPrefix(content, []byte("<testsuites")) || bytes.HasPrefix(content, []byte("<testsuite")) {
		// Could be JUnit, pytest, or PHPUnit - default to JUnit
		// Check for pytest-specific attributes
		if bytes.Contains(content, []byte("pytest")) {
			return domain.FrameworkPython, nil
		}
		return domain.FrameworkJUnit, nil
	}

	// ZAP XML
	if bytes.HasPrefix(content, []byte("<OWASPZAPReport")) {
		return domain.FrameworkZAP, nil
	}

	return "", fmt.Errorf("unable to detect framework from XML content")
}
