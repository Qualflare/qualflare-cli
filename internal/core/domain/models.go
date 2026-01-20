package domain

import "time"

// Framework represents supported test frameworks
type Framework string

const (
	// Unit Testing Frameworks
	FrameworkJUnit   Framework = "junit"
	FrameworkPython  Framework = "python"
	FrameworkGolang  Framework = "golang"
	FrameworkJest    Framework = "jest"
	FrameworkMocha   Framework = "mocha"
	FrameworkRSpec   Framework = "rspec"
	FrameworkPHPUnit Framework = "phpunit"

	// BDD Frameworks
	FrameworkCucumber Framework = "cucumber"
	FrameworkKarate   Framework = "karate"

	// UI/E2E Testing Frameworks
	FrameworkPlaywright Framework = "playwright"
	FrameworkCypress    Framework = "cypress"
	FrameworkSelenium   Framework = "selenium"
	FrameworkTestCafe   Framework = "testcafe"

	// API Testing Frameworks
	FrameworkNewman Framework = "newman"
	FrameworkK6     Framework = "k6"

	// Security Testing Tools
	FrameworkZAP       Framework = "zap"
	FrameworkTrivy     Framework = "trivy"
	FrameworkSnyk      Framework = "snyk"
	FrameworkSonarQube Framework = "sonarqube"
)

// AllFrameworks returns all supported frameworks
func AllFrameworks() []Framework {
	return []Framework{
		FrameworkJUnit,
		FrameworkPython,
		FrameworkGolang,
		FrameworkJest,
		FrameworkMocha,
		FrameworkRSpec,
		FrameworkPHPUnit,
		FrameworkCucumber,
		FrameworkKarate,
		FrameworkPlaywright,
		FrameworkCypress,
		FrameworkSelenium,
		FrameworkTestCafe,
		FrameworkNewman,
		FrameworkK6,
		FrameworkZAP,
		FrameworkTrivy,
		FrameworkSnyk,
		FrameworkSonarQube,
	}
}

// FrameworkCategory represents the category of a testing framework
type FrameworkCategory string

const (
	CategoryUnitTest FrameworkCategory = "unit"
	CategoryBDD      FrameworkCategory = "bdd"
	CategoryE2E      FrameworkCategory = "e2e"
	CategoryAPI      FrameworkCategory = "api"
	CategorySecurity FrameworkCategory = "security"
)

// GetCategory returns the category for a framework
func (f Framework) GetCategory() FrameworkCategory {
	switch f {
	case FrameworkJUnit, FrameworkPython, FrameworkGolang, FrameworkJest,
		FrameworkMocha, FrameworkRSpec, FrameworkPHPUnit:
		return CategoryUnitTest
	case FrameworkCucumber, FrameworkKarate:
		return CategoryBDD
	case FrameworkPlaywright, FrameworkCypress, FrameworkSelenium, FrameworkTestCafe:
		return CategoryE2E
	case FrameworkNewman, FrameworkK6:
		return CategoryAPI
	case FrameworkZAP, FrameworkTrivy, FrameworkSnyk, FrameworkSonarQube:
		return CategorySecurity
	default:
		return CategoryUnitTest
	}
}

// String returns the string representation of the framework
func (f Framework) String() string {
	return string(f)
}

// IsValid checks if the framework is valid
func (f Framework) IsValid() bool {
	for _, valid := range AllFrameworks() {
		if f == valid {
			return true
		}
	}
	return false
}

// Status represents the status of a test
type Status string

const (
	StatusPassed  Status = "passed"
	StatusFailed  Status = "failed"
	StatusSkipped Status = "skipped"
	StatusError   Status = "error"
	StatusPending Status = "pending"
)

// TestStatus is an alias for backward compatibility
type TestStatus = Status

const (
	TestStatusPassed  = StatusPassed
	TestStatusFailed  = StatusFailed
	TestStatusSkipped = StatusSkipped
	TestStatusError   = StatusError
	TestStatusPending = StatusPending
)

// Launch represents the complete test launch/run
type Launch struct {
	Framework string `json:"framework"`

	// Platform information
	Platform string `json:"platform,omitempty"`
	OS       string `json:"os,omitempty"`
	Browser  string `json:"browser,omitempty"` // Browser for E2E tests

	// Git information
	Branch string `json:"branch,omitempty"`
	Commit string `json:"commit,omitempty"`

	// Environment, language and milestone
	Environment string `json:"environment,omitempty"`
	Language    string `json:"language,omitempty"`
	Milestone   int64  `json:"milestone,omitempty"`

	// Metadata
	Metadata Metadata `json:"metadata,omitempty"`

	// Custom properties
	Properties map[string]string `json:"properties,omitempty"`

	// Test suites
	Suites []Suite `json:"suites"`
}

// Metadata contains version and timestamp information
type Metadata struct {
	Version   string `json:"version"`
	Timestamp string `json:"timestamp"`
	CLIName   string `json:"cliName"`
}

// Suite represents a collection of test cases
type Suite struct {
	// Identification
	Name     string            `json:"name"`
	Category FrameworkCategory `json:"category,omitempty"`

	// Test counts
	TotalTests int `json:"total"`
	Passed     int `json:"passed"`
	Failed     int `json:"failed"`
	Skipped    int `json:"skipped"`
	Errors     int `json:"errors,omitempty"`
	Flaky      int `json:"flaky,omitempty"`      // Tests that passed after retry
	Assertions int `json:"assertions,omitempty"` // Total assertions executed (API tests)
	Retries    int `json:"retries,omitempty"`    // Total retry attempts

	// Timing
	Duration  time.Duration `json:"duration"`
	Timestamp time.Time     `json:"timestamp,omitempty"`

	// Custom properties
	Properties map[string]string `json:"properties,omitempty"`

	// Test cases
	Cases []Case `json:"cases"`
}

// GetStatus returns the overall status of the suite
func (s *Suite) GetStatus() Status {
	if s.Failed > 0 || s.Errors > 0 {
		return StatusFailed
	}
	if s.Passed == 0 && s.Skipped > 0 {
		return StatusSkipped
	}
	return StatusPassed
}

// Case represents a single test case
type Case struct {
	// Identification
	ID        string `json:"id"`
	Name      string `json:"name"`
	ClassName string `json:"className,omitempty"`

	// Status and timing
	Status   Status        `json:"status"`
	Duration time.Duration `json:"duration"`

	// Retry information
	RetryCount int  `json:"retryCount"` // Number of retry attempts
	IsFlaky    bool `json:"isFlaky"`    // True if test passed after one or more retries

	// Error information
	ErrorMessage string `json:"errorMessage,omitempty"`
	StackTrace   string `json:"stackTrace,omitempty"`
	ErrorType    string `json:"errorType,omitempty"`

	// Severity (for security findings)
	Severity Severity `json:"severity,omitempty"`

	// Categorization
	Tags []string `json:"tags,omitempty"`

	// Custom properties
	Properties map[string]string `json:"properties,omitempty"`

	// Attachments (screenshots, logs, etc.)
	Attachments []Attachment `json:"attachments,omitempty"`

	// Nested steps (for BDD/Cucumber)
	Steps []Step `json:"steps,omitempty"`
}

// Step represents a step within a test case (for BDD frameworks)
type Step struct {
	Name     string        `json:"name"`
	Keyword  string        `json:"keyword,omitempty"`
	Status   Status        `json:"status"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
	Location string        `json:"location,omitempty"`
}

// Attachment represents a file attachment (screenshot, log, etc.)
type Attachment struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Content  string `json:"content,omitempty"` // Base64 encoded
}

// SecurityFinding represents a security vulnerability finding
type SecurityFinding struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Severity    Severity `json:"severity"`
	CVE         string   `json:"cve,omitempty"`
	CWE         string   `json:"cwe,omitempty"`
	CVSS        float64  `json:"cvss,omitempty"`
	Package     string   `json:"package,omitempty"`
	Version     string   `json:"version,omitempty"`
	FixedIn     string   `json:"fixedIn,omitempty"`
	URL         string   `json:"url,omitempty"`
	Location    string   `json:"location,omitempty"`
}

// Severity represents the severity level of a security finding
type Severity string

const (
	SeverityCritical Severity = "critical"
	SeverityHigh     Severity = "high"
	SeverityMedium   Severity = "medium"
	SeverityLow      Severity = "low"
	SeverityInfo     Severity = "info"
	SeverityUnknown  Severity = "unknown"
)

// SecuritySuite represents a security scan result as a suite
type SecuritySuite struct {
	Suite
	Findings []SecurityFinding `json:"findings,omitempty"`
	Summary  SecuritySummary   `json:"summary,omitempty"`
}

// SecuritySummary provides a summary of security findings by severity
type SecuritySummary struct {
	Critical int `json:"critical"`
	High     int `json:"high"`
	Medium   int `json:"medium"`
	Low      int `json:"low"`
	Info     int `json:"info"`
}
