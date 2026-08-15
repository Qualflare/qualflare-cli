// Package registry registers all supported log-step extractors and detects
// which one applies to a captured console output blob, mirroring
// parsers/factory's registration/detection pattern for the file-based
// parsers.
package registry

import (
	"fmt"

	"qualflare-cli/internal/adapters/logextract/bdd/pytestbdd"
	"qualflare-cli/internal/adapters/logextract/unit/mochaspec"
	"qualflare-cli/internal/adapters/logextract/unit/rspecdoc"
	"qualflare-cli/internal/core/domain"
	"qualflare-cli/internal/core/ports"
)

// Registry manages log-step-extractor registration and detection.
type Registry struct {
	extractors map[domain.Framework]ports.LogStepExtractor
	// order preserves registration order so DetectExtractor's "first match"
	// behavior is deterministic rather than dependent on map iteration order.
	order []domain.Framework
}

// New creates a registry with all supported log-step extractors registered.
func New() *Registry {
	r := newEmpty()
	r.RegisterExtractor(pytestbdd.New())
	r.RegisterExtractor(rspecdoc.New())
	r.RegisterExtractor(mochaspec.New())
	return r
}

// newEmpty creates a registry with no extractors registered — used by this
// package's own tests to exercise registration/detection in isolation from
// New()'s real, "batteries included" extractor set.
func newEmpty() *Registry {
	return &Registry{
		extractors: make(map[domain.Framework]ports.LogStepExtractor),
	}
}

func (r *Registry) RegisterExtractor(extractor ports.LogStepExtractor) {
	framework := extractor.GetFramework()
	if _, exists := r.extractors[framework]; !exists {
		r.order = append(r.order, framework)
	}
	r.extractors[framework] = extractor
}

func (r *Registry) GetExtractor(framework domain.Framework) (ports.LogStepExtractor, error) {
	extractor, exists := r.extractors[framework]
	if !exists {
		return nil, fmt.Errorf("no log-step extractor registered for framework: %s", framework)
	}
	return extractor, nil
}

// DetectExtractor returns the first registered extractor (in registration
// order) whose Detect matches output.
func (r *Registry) DetectExtractor(output []byte) (ports.LogStepExtractor, error) {
	for _, framework := range r.order {
		extractor := r.extractors[framework]
		if extractor.Detect(output) {
			return extractor, nil
		}
	}
	return nil, fmt.Errorf("could not detect a supported framework from the captured output; pass --format explicitly")
}

func (r *Registry) GetSupportedFrameworks() []domain.Framework {
	frameworks := make([]domain.Framework, 0, len(r.order))
	frameworks = append(frameworks, r.order...)
	return frameworks
}
