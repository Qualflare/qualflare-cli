// Package junit parses JUnit XML test results (the generic JUnit format).
package junit

import (
	"io"

	"qualflare-cli/internal/adapters/parsers/shared/junitxml"
	"qualflare-cli/internal/core/domain"
)

// Parser parses standard JUnit XML output.
type Parser struct{}

// New creates a new JUnit parser.
func New() *Parser { return &Parser{} }

// GetFramework returns the framework identifier.
func (p *Parser) GetFramework() domain.Framework { return domain.FrameworkJUnit }

// SupportedFileExtensions returns the file extensions this parser handles.
func (p *Parser) SupportedFileExtensions() []string { return []string{".xml"} }

// Parse reads JUnit XML from reader and returns a Suite.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	return junitxml.Parse(reader, domain.FrameworkJUnit)
}
