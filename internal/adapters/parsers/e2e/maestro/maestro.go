// Package maestro parses Maestro mobile flow test results (JUnit XML output).
package maestro

import (
	"io"

	"qualflare-cli/internal/adapters/parsers/shared/junitxml"
	"qualflare-cli/internal/core/domain"
)

// Parser parses Maestro mobile test output (JUnit XML format).
type Parser struct{}

// New creates a new Maestro parser.
func New() *Parser { return &Parser{} }

// GetFramework returns the framework identifier.
func (p *Parser) GetFramework() domain.Framework { return domain.FrameworkMaestro }

// SupportedFileExtensions returns the file extensions this parser handles.
func (p *Parser) SupportedFileExtensions() []string { return []string{".xml"} }

// Parse reads Maestro JUnit XML from reader and returns a Suite.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	return junitxml.Parse(reader, domain.FrameworkMaestro)
}
