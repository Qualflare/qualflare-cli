// Package xctest parses XCTest iOS test results: either the legacy JUnit XML
// (via xcpretty/xcbeautify — case-level only, no steps) most CI setups
// already produce, or the native .xcresult bundle directly (via
// xcresulttool — case-level results plus per-test XCTContext.runActivity
// step data), when given one.
package xctest

import (
	"fmt"
	"io"
	"os"

	"qualflare-cli/internal/adapters/parsers/shared/junitxml"
	"qualflare-cli/internal/core/domain"
)

// Parser parses XCTest output converted to JUnit XML, or a native .xcresult
// bundle.
type Parser struct{}

// New creates a new XCTest parser.
func New() *Parser { return &Parser{} }

// GetFramework returns the framework identifier.
func (p *Parser) GetFramework() domain.Framework { return domain.FrameworkXCTest }

// SupportedFileExtensions returns the file extensions this parser handles.
func (p *Parser) SupportedFileExtensions() []string { return []string{".xml", ".xcresult"} }

// Parse reads XCTest JUnit XML from reader and returns a Suite. This is the
// legacy path (no step data) — ParsePath is preferred when a filesystem
// path is available, since it can also recognize a .xcresult bundle.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	return junitxml.Parse(reader, domain.FrameworkXCTest)
}

// ParsePath implements ports.PathAwareParser. A regular file is parsed
// exactly as Parse does today (unchanged legacy behavior); a directory (a
// .xcresult bundle) is parsed via xcresulttool for case-level results plus
// per-test step data.
func (p *Parser) ParsePath(path string) (*domain.Suite, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("failed to stat path: %w", err)
	}
	if !info.IsDir() {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("failed to open file: %w", err)
		}
		defer func() { _ = file.Close() }()
		return p.Parse(file)
	}
	return parseXCResultBundle(path)
}
