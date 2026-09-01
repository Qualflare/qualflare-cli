package factory

import (
	"testing"

	"qualflare-cli/internal/core/domain"
)

// Both CTRF document shapes must be recognised from content.
//
// The legacy shape matters as much as the current one: it has no root-level
// marker at all, and it is what every published README and every reporter
// pinned to an older ctrf release still emits.
func TestDetectCTRFFromContent(t *testing.T) {
	f := NewParserFactory()

	cases := []struct {
		name    string
		file    string
		content string
		want    domain.Framework
	}{
		{
			"current shape, identified by reportFormat",
			"results.json",
			`{"reportFormat":"CTRF","specVersion":"0.0.0","results":{"tool":{"name":"jest"},"summary":{},"tests":[]}}`,
			domain.FrameworkCTRF,
		},
		{
			"legacy shape, identified by the results trio",
			"results.json",
			`{"results":{"tool":{"name":"jest"},"summary":{"tests":1},"tests":[{"name":"a","status":"passed"}]}}`,
			domain.FrameworkCTRF,
		},
		{
			"reportFormat is matched case-insensitively",
			"results.json",
			`{"reportFormat":"ctrf","results":{"tests":[]}}`,
			domain.FrameworkCTRF,
		},
		{
			// A run with no tests is legal, so PRESENCE of the key is the
			// discriminator rather than non-emptiness.
			"an empty tests array still identifies as CTRF",
			"results.json",
			`{"results":{"tool":{"name":"jest"},"summary":{},"tests":[]}}`,
			domain.FrameworkCTRF,
		},

		// The anti-regression that matters: Cypress's detector also keys on
		// "results". It requires "stats" alongside, so the two cannot both
		// match — but nothing enforced that before CTRF existed.
		{
			"a Cypress report still detects as Cypress, not CTRF",
			"results.json",
			`{"stats":{"suites":1,"tests":2},"results":[{"file":"a.js"}]}`,
			domain.FrameworkCypress,
		},
		{
			// results without the trio is not CTRF.
			"results alone does not make a document CTRF",
			"results.json",
			`{"results":{"somethingElse":1}}`,
			domain.FrameworkJest,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := f.DetectFrameworkFromContent(tc.file, []byte(tc.content))
			if tc.want == domain.FrameworkJest {
				// This row only asserts "not CTRF"; the actual fallback depends
				// on the other detectors and the filename, which is not the
				// point being pinned here.
				if err == nil && got == domain.FrameworkCTRF {
					t.Errorf("a bare results object must not be treated as CTRF, got %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("DetectFrameworkFromContent: %v", err)
			}
			if got != tc.want {
				t.Errorf("detected %q, want %q", got, tc.want)
			}
		})
	}
}

// CTRF files are routinely named after BOTH the producing tool and the format.
// The ctrf token must win, or the file is routed to a parser that cannot read
// it even though the document is perfectly valid.
func TestDetectCTRFFromFilename(t *testing.T) {
	f := NewParserFactory()
	cases := map[string]domain.Framework{
		"ctrf-report.json":     domain.FrameworkCTRF,
		"ctrf-playwright.json": domain.FrameworkCTRF,
		"playwright-ctrf.json": domain.FrameworkCTRF,
		"CTRF.json":            domain.FrameworkCTRF,
	}
	for name, want := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := f.DetectFramework(name)
			if err != nil {
				t.Fatalf("DetectFramework(%q): %v", name, err)
			}
			if got != want {
				t.Errorf("DetectFramework(%q) = %q, want %q", name, got, want)
			}
		})
	}
}

// The parser must be registered, or --format ctrf resolves to nothing.
func TestCTRFParserIsRegistered(t *testing.T) {
	f := NewParserFactory()
	p, err := f.GetParser(domain.FrameworkCTRF)
	if err != nil {
		t.Fatalf("GetParser(ctrf): %v", err)
	}
	if p.GetFramework() != domain.FrameworkCTRF {
		t.Errorf("registered parser reports %q", p.GetFramework())
	}
}
