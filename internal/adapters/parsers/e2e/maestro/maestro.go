// Package maestro parses Maestro mobile flow test results: the JUnit XML
// output every input goes through, optionally enriched with per-command
// step data from a sibling commands.json (see ParsePath/commands.go) when
// one is present next to the JUnit XML.
package maestro

import (
	"io"
	"os"

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

// Parse reads Maestro JUnit XML from reader and returns a Suite. This is
// the case-level-only path (no steps) — ParsePath is preferred when a
// filesystem path is available, since it can also pick up a sibling
// commands.json.
func (p *Parser) Parse(reader io.Reader) (*domain.Suite, error) {
	return junitxml.Parse(reader, domain.FrameworkMaestro)
}

// ParsePath implements ports.PathAwareParser: parses the JUnit XML at path
// exactly as Parse does, then best-effort enriches the result with step
// data from a sibling commands.json, if one is present and well-formed.
func (p *Parser) ParsePath(path string) (*domain.Suite, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()

	suite, err := p.Parse(file)
	if err != nil {
		return nil, err
	}
	enrichWithCommandSteps(suite, path)
	return suite, nil
}
