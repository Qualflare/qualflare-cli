package parsers

import (
	"fmt"
	"path/filepath"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
	"strings"
)

type ParserFactory struct {
	parsers map[domain.Framework]ports.Parser
}

func NewParserFactory() *ParserFactory {
	return &ParserFactory{
		parsers: map[domain.Framework]ports.Parser{
			domain.FrameworkJUnit:    NewJUnitParser(),
			domain.FrameworkPython:   NewPythonParser(),
			domain.FrameworkGolang:   NewGolangParser(),
			domain.FrameworkCucumber: NewCucumberParser(),
		},
	}
}

func (f *ParserFactory) GetParser(framework domain.Framework) (ports.Parser, error) {
	parser, exists := f.parsers[framework]
	if !exists {
		return nil, fmt.Errorf("unsupported framework: %s", framework)
	}
	return parser, nil
}

func (f *ParserFactory) DetectFramework(filename string) (domain.Framework, error) {
	ext := strings.ToLower(filepath.Ext(filename))
	base := strings.ToLower(filepath.Base(filename))

	// Try to detect based on filename patterns
	switch {
	case strings.Contains(base, "cucumber") || strings.Contains(base, "feature"):
		return domain.FrameworkCucumber, nil
	case strings.Contains(base, "pytest") || strings.Contains(base, "python"):
		return domain.FrameworkPython, nil
	case strings.Contains(base, "go") && (ext == ".json" || ext == ".out"):
		return domain.FrameworkGolang, nil
	case ext == ".xml":
		// Default to JUnit for XML files
		return domain.FrameworkJUnit, nil
	case ext == ".json":
		// Could be Cucumber or Go tests, default to Cucumber
		return domain.FrameworkCucumber, nil
	}

	return "", fmt.Errorf("unable to detect framework for file: %s", filename)
}

func (f *ParserFactory) GetSupportedFrameworks() []domain.Framework {
	frameworks := make([]domain.Framework, 0, len(f.parsers))
	for framework := range f.parsers {
		frameworks = append(frameworks, framework)
	}
	return frameworks
}
