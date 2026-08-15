package registry

import (
	"testing"

	"qualflare-cli/internal/core/domain"
)

type fakeExtractor struct {
	framework  domain.Framework
	detectHits bool
}

func (f *fakeExtractor) ExtractCases(_ []byte) ([]domain.Case, error) { return nil, nil }
func (f *fakeExtractor) GetFramework() domain.Framework                { return f.framework }
func (f *fakeExtractor) Detect(_ []byte) bool                          { return f.detectHits }

func TestGetExtractor_ReturnsRegistered(t *testing.T) {
	r := newEmpty()
	e := &fakeExtractor{framework: domain.FrameworkRSpec}
	r.RegisterExtractor(e)

	got, err := r.GetExtractor(domain.FrameworkRSpec)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != e {
		t.Errorf("expected the registered extractor back")
	}
}

func TestGetExtractor_UnregisteredFrameworkErrors(t *testing.T) {
	r := newEmpty()
	_, err := r.GetExtractor(domain.FrameworkMocha)
	if err == nil {
		t.Fatal("expected an error for an unregistered framework")
	}
}

func TestDetectExtractor_ReturnsFirstMatch(t *testing.T) {
	r := newEmpty()
	noMatch := &fakeExtractor{framework: domain.FrameworkRSpec, detectHits: false}
	match := &fakeExtractor{framework: domain.FrameworkMocha, detectHits: true}
	r.RegisterExtractor(noMatch)
	r.RegisterExtractor(match)

	got, err := r.DetectExtractor([]byte("some output"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got != match {
		t.Errorf("expected the matching extractor")
	}
}

func TestDetectExtractor_NoMatchErrors(t *testing.T) {
	r := newEmpty()
	r.RegisterExtractor(&fakeExtractor{framework: domain.FrameworkRSpec, detectHits: false})

	_, err := r.DetectExtractor([]byte("unrecognized output"))
	if err == nil {
		t.Fatal("expected an error when no extractor matches")
	}
}

func TestGetSupportedFrameworks(t *testing.T) {
	r := newEmpty()
	r.RegisterExtractor(&fakeExtractor{framework: domain.FrameworkRSpec})
	r.RegisterExtractor(&fakeExtractor{framework: domain.FrameworkMocha})

	frameworks := r.GetSupportedFrameworks()
	if len(frameworks) != 2 {
		t.Fatalf("expected 2 frameworks, got %d", len(frameworks))
	}
}
