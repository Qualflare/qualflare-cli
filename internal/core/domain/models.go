// Package domain defines the core data models shared across all adapters.
package domain

import (
	"encoding/json"
	"strings"
	"time"
)

// Framework represents supported test frameworks
type Framework string

const (
	// Generic (JUnit-compatible) — any tool that emits standard JUnit XML
	FrameworkJUnit Framework = "junit"

	// FrameworkCTRF ingests the Common Test Report Format (https://ctrf.io), a
	// tool-agnostic JSON interchange format. Generic for the same reason JUnit
	// is: roughly fifteen producers and no single owning tool.
	//
	// It is mostly about reach. CTRF has first-party reporters for wdio,
	// jasmine, nightwatch, codeceptjs and the .NET runners (MSTest, NUnit,
	// xUnit) — none of which Qualflare supports directly, and .NET in
	// particular can otherwise only arrive as generic JUnit XML.
	// See internal/adapters/parsers/generic/ctrf.
	FrameworkCTRF Framework = "ctrf"

	// Unit Testing Frameworks
	FrameworkPython  Framework = "python"
	FrameworkGolang  Framework = "golang"
	FrameworkJest    Framework = "jest"
	FrameworkVitest  Framework = "vitest"
	FrameworkMocha   Framework = "mocha"
	FrameworkRSpec   Framework = "rspec"
	FrameworkPHPUnit Framework = "phpunit"
	FrameworkTestNG  Framework = "testng"

	// BDD Frameworks
	FrameworkCucumber Framework = "cucumber"
	FrameworkKarate   Framework = "karate"

	// UI/E2E / Mobile Testing Frameworks
	FrameworkPlaywright Framework = "playwright"
	FrameworkCypress    Framework = "cypress"
	FrameworkSelenium   Framework = "selenium"
	FrameworkTestCafe   Framework = "testcafe"
	FrameworkMaestro    Framework = "maestro"
	FrameworkXCTest     Framework = "xctest"
	FrameworkEspresso   Framework = "espresso"

	// API Testing Frameworks
	FrameworkNewman Framework = "newman"
	FrameworkK6     Framework = "k6"

	// Security Testing Tools
	FrameworkZAP       Framework = "zap"
	FrameworkTrivy     Framework = "trivy"
	FrameworkSnyk      Framework = "snyk"
	FrameworkSonarQube Framework = "sonarqube"

	// FrameworkQualflareJSON ingests @qualflare/cypress's and
	// @qualflare/cucumberjs's own Collect JSON output directly (their
	// `outputFile` config option) — the sharded-CI merge workflow. See
	// internal/adapters/parsers/native/qualflare.
	FrameworkQualflareJSON Framework = "qualflare-json"
)

// AllFrameworks returns all supported frameworks
func AllFrameworks() []Framework {
	return []Framework{
		FrameworkJUnit,
		FrameworkCTRF,
		FrameworkPython,
		FrameworkGolang,
		FrameworkJest,
		FrameworkVitest,
		FrameworkMocha,
		FrameworkRSpec,
		FrameworkPHPUnit,
		FrameworkTestNG,
		FrameworkCucumber,
		FrameworkKarate,
		FrameworkPlaywright,
		FrameworkCypress,
		FrameworkSelenium,
		FrameworkTestCafe,
		FrameworkMaestro,
		FrameworkXCTest,
		FrameworkEspresso,
		FrameworkNewman,
		FrameworkK6,
		FrameworkZAP,
		FrameworkTrivy,
		FrameworkSnyk,
		FrameworkSonarQube,
		FrameworkQualflareJSON,
	}
}

// FrameworkCategory represents the category of a testing framework
type FrameworkCategory string

const (
	CategoryGeneric  FrameworkCategory = "generic"
	CategoryUnitTest FrameworkCategory = "unit"
	CategoryBDD      FrameworkCategory = "bdd"
	CategoryE2E      FrameworkCategory = "e2e"
	CategoryAPI      FrameworkCategory = "api"
	CategorySecurity FrameworkCategory = "security"
)

// GetCategory returns the category for a framework. Every real, specifically
// identified framework maps to a category NAMED AFTER ITSELF (e.g. Cypress ->
// "cypress") rather than a shared coarse bucket — one suite's category is
// then always the exact tool that produced it, which is what lets a "mixed"
// launch (see resolveLaunchFramework) still show each suite's real identity
// without any separate field. The handful of coarse buckets below
// (unit/bdd/e2e/api/security/generic) still exist as valid category values —
// for backward compatibility with data written before this change, and as
// the safe fallback for anything this can't identify: echoing back an
// unrecognized framework string as the category would fail the server's
// oneof validation and 400 the whole launch, so an unknown/invalid input
// degrades to CategoryGeneric instead of round-tripping verbatim.
func (f Framework) GetCategory() FrameworkCategory {
	switch f {
	case FrameworkQualflareJSON, FrameworkCTRF:
		// Both are PASSTHROUGH formats: the real per-suite category comes from
		// the file's own embedded producer identity — qualflare-json from its
		// `framework` field, CTRF from `results.tool.name` — not from this
		// constant.
		//
		// This case is load-bearing, not cosmetic. Without it the default arm
		// would return FrameworkCategory("ctrf"), which the SERVER's
		// Suite.Category oneof does not accept: the launch would fail
		// validation entirely. The default arm's own comment describes exactly
		// this hazard.
		return CategoryGeneric
	default:
		if f.IsValid() {
			return FrameworkCategory(f)
		}
		return CategoryGeneric
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

	// StatusTimeout and StatusAborted complete the set the SERVER accepts and
	// the @qualflare/* reporters already emit — their own CaseStatus union has
	// carried all seven for as long as it has existed.
	//
	// Before these existed the parsers folded them (timeout -> failed,
	// aborted -> error), which silently discarded a distinction the reporter
	// deliberately drew and the server can store. "The suite timed out" and
	// "the suite failed an assertion" are different findings, and only one of
	// them is usually the test's fault.
	StatusTimeout Status = "timeout"
	StatusAborted Status = "aborted"
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

// RecomputeCounts derives Passed/Failed/Skipped/Errors and TotalTests from the
// actual case statuses, so the suite counters can never disagree with the cases
// (the source of truth the server itself recomputes from). Parsers that build
// counters from a report header — or that increment them independently of case
// status (the trivy/snyk suite.Failed++ bug) — call this at the end of Parse so
// a report with real failures can never roll up green. Pending is folded into
// Skipped (matching GetStatus); Flaky/Assertions/Retries are orthogonal to
// pass/fail and left untouched.
func (s *Suite) RecomputeCounts() {
	var passed, failed, skipped, errors int
	for _, c := range s.Cases {
		switch c.Status {
		case StatusPassed:
			passed++
		// A timeout is a failure and an abort is an error FOR THIS SUMMARY only.
		// The case's own Status keeps the precise value and is what reaches the
		// server, which has counters for all seven; these four buckets exist for
		// the CLI's local rollup and for GetStatus below, neither of which needs
		// the finer distinction.
		case StatusFailed, StatusTimeout:
			failed++
		case StatusError, StatusAborted:
			errors++
		case StatusSkipped, StatusPending:
			skipped++
		default:
			// An unrecognized status is a parser bug; count it as an error so it
			// surfaces (red) rather than silently vanishing from the totals.
			errors++
		}
	}
	s.Passed, s.Failed, s.Skipped, s.Errors = passed, failed, skipped, errors
	s.TotalTests = len(s.Cases)
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

	// Retry information (pointers: nil = not applicable, 0/false = explicitly no retries)
	RetryCount *int  `json:"retryCount,omitempty"` // Number of retry attempts
	IsFlaky    *bool `json:"isFlaky,omitempty"`    // True if test passed after one or more retries

	// Attempts is the per-attempt execution history for a retried test. Unlike
	// RetryCount, which only says HOW MANY times a test ran, this carries what
	// each individual attempt did -- which is the only way a report can answer
	// "what failed on the first try?" for a test that eventually passed.
	//
	// Passed straight through to the server, which persists it into
	// case_run_attempts (see api-service launch.Attempt; the field names here
	// mirror it exactly). Nil for parsers whose format has no attempt history.
	Attempts []Attempt `json:"attempts,omitempty"`

	// Shard/parallel-worker information (pointers: nil = not applicable/unknown)
	ShardIndex *int       `json:"shardIndex,omitempty"` // Index of the worker/shard that ran this test
	StartedAt  *time.Time `json:"startedAt,omitempty"`  // When this test started executing

	// Error information (single field matching API schema)
	Error string `json:"error,omitempty"`

	// Priority for security findings (matches API schema: low, medium, high, critical)
	Priority Severity `json:"priority,omitempty"`

	// Categorization
	Tags []string `json:"tags,omitempty"`

	// Custom properties
	Properties map[string]string `json:"properties,omitempty"`

	// Attachments (screenshots, logs, etc.)
	Attachments []Attachment `json:"attachments,omitempty"`

	// Nested steps (for BDD/Cucumber)
	Steps []Step `json:"steps,omitempty"`

	// Labels are Allure-style arbitrary name/value metadata (epic, feature,
	// story, owner, severity...), written by the @qualflare/* reporters'
	// qualflare.label() API. Mirrors api-service's launch.Label; the server
	// caps these at 100 per case.
	Labels []Label `json:"labels,omitempty"`

	// Links are typed external references (a defect-tracker issue, a TMS
	// case, or an arbitrary custom URL), written by qualflare.link().
	// Mirrors api-service's launch.Link; capped at 20 per case server-side.
	Links []Link `json:"links,omitempty"`
}

// Label is one Allure-style name/value pair attached to a Case.
type Label struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Link is a typed external reference attached to a Case. Type is one of
// "issue", "tms" or "custom" -- the server rejects anything else, so the
// value is passed through verbatim rather than normalized here.
type Link struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
	URL  string `json:"url"`
}

// Attempt is one execution of a retried test.
//
// Mirrors api-service's launch.Attempt field-for-field, INCLUDING the json
// tags -- the CLI does not reshape it, so a mismatch here silently drops data
// server-side rather than failing. Number is 1-based; the server ignores
// anything lower. Duration is a time.Duration (NANOSECONDS) on the wire, which
// the server converts to milliseconds on write.
type Attempt struct {
	Number    int             `json:"attempt"`
	Status    Status          `json:"status"`
	Duration  time.Duration   `json:"duration,omitempty"`
	StartedAt *time.Time      `json:"startedAt,omitempty"`
	UID       string          `json:"attemptId,omitempty"`
	Message   string          `json:"message,omitempty"`
	Trace     string          `json:"trace,omitempty"`
	Snippet   string          `json:"snippet,omitempty"`
	Line      *int            `json:"line,omitempty"`
	Stdout    []string        `json:"stdout,omitempty"`
	Stderr    []string        `json:"stderr,omitempty"`
	Extra     json.RawMessage `json:"extra,omitempty"`
}

// Step represents a step within a test case (for BDD frameworks)
type Step struct {
	Name     string        `json:"name"`
	Keyword  string        `json:"keyword,omitempty"`
	Status   Status        `json:"status"`
	Duration time.Duration `json:"duration"`
	Error    string        `json:"error,omitempty"`
	Location string        `json:"location,omitempty"`

	// ParentIndex is a 0-based index into the SAME Case.Steps slice,
	// identifying this step's parent for Allure-style nesting. Nil means a
	// root step. The server resolves and sanity-checks these itself
	// (ResolveStepParents drops out-of-range/cyclic values rather than
	// rejecting the case), so they are passed through unvalidated here.
	ParentIndex *int `json:"parentIndex,omitempty"`

	// Parameters mirrors Allure's parameter() API. A slice, not a map:
	// duplicate names are legal (e.g. the same parameter across loop
	// iterations). Capped at 50 per step server-side.
	Parameters []Parameter `json:"parameters,omitempty"`
}

// Parameter is one name/value input recorded against a Step. Masked is a
// display hint for the UI only -- the server does not redact the value.
type Parameter struct {
	Name   string `json:"name"`
	Value  string `json:"value,omitempty"`
	Masked bool   `json:"masked,omitempty"`
}

// Attachment represents a file attachment (screenshot, log, etc.)
type Attachment struct {
	Name     string `json:"name"`
	Path     string `json:"path,omitempty"`
	MimeType string `json:"mimeType,omitempty"`
	Content  string `json:"content,omitempty"` // Base64 encoded
	// StorageKey/FileSize mirror the server's launch.Attachment fields of the
	// same name (video attachments uploaded via the presigned-URL flow) —
	// set by report_service.go's video-resolution pass, never by a parser
	// directly.
	StorageKey string `json:"storageKey,omitempty"`
	FileSize   int64  `json:"fileSize,omitempty"`
	// LocalPath is set by the qualflare-native parser only (see
	// internal/adapters/parsers/native/qualflare) when a report file
	// references a heavy artifact it hasn't uploaded itself — an absolute
	// path, resolved at parse time relative to that source file's own
	// directory. Never sent to the server: report_service.go's artifact
	// resolution pass consumes it and fills StorageKey/FileSize before
	// SendReport is called.
	LocalPath string `json:"-"`
	// ArtifactKind names what LocalPath points at, so `qf collect` can gate
	// each kind separately (--upload-artifacts). One of the ArtifactKind*
	// constants; empty whenever LocalPath is empty.
	ArtifactKind string `json:"-"`
}

// Artifact kinds carried by Attachment.ArtifactKind. These are the values
// --upload-artifacts accepts, and the reason the flag takes a list rather than
// a bool: a trace and a video are both heavy, but wanting one is not wanting
// the other.
const (
	ArtifactKindVideo = "video"
	ArtifactKindTrace = "trace"
	// ArtifactKindImage is uploaded BY DEFAULT, unlike the two above. A
	// screenshot is small, and it was always uploaded back when it had no
	// choice but to travel inline in the /collect body -- making it opt-in
	// would silently stop delivering screenshots for everyone who never passed
	// the flag. It is listed here so it can still be declined
	// (--upload-artifacts=none), not so it must be requested.
	ArtifactKindImage = "image"
)

// ArtifactKindNone is the token that declines every kind, including the ones
// that default to on. Without it "upload no images" would be inexpressible:
// an empty --upload-artifacts is indistinguishable from an absent one.
const ArtifactKindNone = "none"

// DefaultArtifactKinds is what --upload-artifacts holds when nobody sets it.
// Returned fresh each call: the result is a mutable set that callers own.
func DefaultArtifactKinds() map[string]bool {
	return map[string]bool{ArtifactKindImage: true}
}

// AllArtifactKinds is the set --upload-artifacts validates against, so a typo
// is rejected with the valid list rather than silently uploading nothing.
func AllArtifactKinds() []string {
	return []string{ArtifactKindVideo, ArtifactKindTrace, ArtifactKindImage}
}

// IntPtr returns a pointer to an int value
func IntPtr(v int) *int { return &v }

// BoolPtr returns a pointer to a bool value
func BoolPtr(v bool) *bool { return &v }

// FormatError combines error message, stack trace, and error type into a single string
func FormatError(message, stackTrace, errorType string) string {
	var parts []string
	if errorType != "" {
		parts = append(parts, errorType+": "+message)
	} else if message != "" {
		parts = append(parts, message)
	}
	if stackTrace != "" {
		parts = append(parts, stackTrace)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "\n\n")
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

// SeverityFromString maps a scanner's severity label onto a domain Severity,
// case-insensitively — scanners disagree on casing (Trivy emits "HIGH", Snyk "high").
// Anything unrecognised becomes SeverityUnknown, which ToCasePriority then coerces to
// the empty priority the API accepts.
//
// "info" is deliberately folded into SeverityUnknown rather than SeverityInfo. Neither
// scanner's own switch handled "info", so both already produced no priority for it;
// mapping it through here would silently turn that into "low". Whether an info-severity
// finding should carry a priority is a product decision, not a side effect of
// deduplicating three switches.
func SeverityFromString(s string) Severity {
	switch Severity(strings.ToLower(strings.TrimSpace(s))) {
	case SeverityCritical:
		return SeverityCritical
	case SeverityHigh:
		return SeverityHigh
	case SeverityMedium:
		return SeverityMedium
	case SeverityLow:
		return SeverityLow
	default:
		return SeverityUnknown
	}
}

// ToCasePriority maps a finding severity onto the API's case `priority` enum
// (critical/high/medium/low). Security scanners emit "info" and "unknown",
// which are not valid priorities: "info" becomes the lowest priority and
// "unknown" (or anything unrecognized) becomes "" so the field is omitted
// rather than 500'ing the upload server-side. Kept distinct from Severity
// itself because SecurityFinding.Severity legitimately uses the full set.
func (s Severity) ToCasePriority() Severity {
	switch s {
	case SeverityCritical, SeverityHigh, SeverityMedium, SeverityLow:
		return s
	case SeverityInfo:
		return SeverityLow
	default:
		return ""
	}
}

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
