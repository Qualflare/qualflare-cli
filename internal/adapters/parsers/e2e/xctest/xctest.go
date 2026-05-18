// Package xctest parses XCTest iOS test results (JUnit XML via xcpretty/xcbeautify).
package xctest

import (
	"io"

	"qualflare-cli/internal/adapters/parsers/shared/junitxml"
	"qualflare-cli/internal/core/domain"
)

// Parser parses XCTest output converted to JUnit XML.
type Parser struct{}

// New creates a new XCTest parser.
func New() *Parser { return &Parser{} }

// GetFramework returns the framework identifier.
func (p *Parser) GetFramework() domain.Framework { return domain.FrameworkXCTest }

// SupportedFileExtensions returns the file extensions this parser handles.
func (p *Parser) SupportedFileExtensions() []string { return []string{".xml"} }

// Parse reads XCTest JUnit XML from reader and returns a Suite.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	return junitxml.Parse(reader, domain.FrameworkXCTest)
}
