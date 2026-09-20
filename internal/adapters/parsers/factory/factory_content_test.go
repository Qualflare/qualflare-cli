package factory

import (
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

// TestDetectFrameworkFromContent (TEST-01) exercises the RUNTIME detection path
// (report_service calls DetectFrameworkFromContent for every auto-detected file);
// the prior tests only covered filename-based DetectFramework.
func TestDetectFrameworkFromContent(t *testing.T) {
	f := NewParserFactory()
	cases := []struct {
		name    string
		file    string
		content string
		want    domain.Framework
		wantErr bool
	}{
		{"junit xml", "r.xml", `<testsuites><testsuite name="s"/></testsuites>`, domain.FrameworkJUnit, false},
		{"pytest xml", "r.xml", `<testsuites><testsuite><properties><property name="pytest" value="7"/></properties></testsuite></testsuites>`, domain.FrameworkPython, false},
		{"zap xml", "r.xml", `<OWASPZAPReport version="2"/>`, domain.FrameworkZAP, false},
		// Unrecognized XML content falls back to filename detection, and a .xml
		// extension resolves to JUnit — documents the actual fallback behavior.
		{"unknown xml falls back to junit via .xml", "r.xml", `<somethingElse/>`, domain.FrameworkJUnit, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fw, err := f.DetectFrameworkFromContent(tc.file, []byte(tc.content))
			if tc.wantErr {
				if err == nil {
					t.Fatalf("expected an error, got framework %q", fw)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if fw != tc.want {
				t.Fatalf("framework = %q, want %q", fw, tc.want)
			}
		})
	}
}

// A filename keyword can name a framework whose parser cannot read the file at
// all: every native reporter writes `qualflare-<framework>-<token>.json`, and
// `qualflare-maestro-1-2.json` matches the bare-substring "maestro" rule, whose
// parser reads JUnit XML. Routing there produced a bare `EOF` — an error about a
// file format, for a file in the right format with the wrong parser.
//
// Content detection catches the reporters' real output first, so this was latent
// rather than broken. It stops being latent the moment a report shape the
// detectors do not recognise arrives under such a name.
func TestDetectFrameworkFromContent_FilenameCannotNameAnIncompatibleParser(t *testing.T) {
	f := NewParserFactory()

	// JSON content that matches no detector, under a name containing "maestro".
	fw, err := f.DetectFrameworkFromContent("qualflare-maestro-1-2.json", []byte(`{"unknown":"shape"}`))
	if err == nil {
		t.Fatalf("framework = %q, want an error: the Maestro parser reads XML", fw)
	}
	for _, want := range []string{"maestro", ".xml", "--format"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q does not mention %q", err.Error(), want)
		}
	}

	// The same name with content the detectors DO recognise still resolves by
	// content, which is the behaviour that saved this case until now.
	fw, err = f.DetectFrameworkFromContent("qualflare-maestro-1-2.json",
		[]byte(`{"framework":"maestro","metadata":{},"suites":[]}`))
	if err != nil || fw != domain.FrameworkQualflareJSON {
		t.Errorf("content detection: framework = %q, err = %v; want %q", fw, err, domain.FrameworkQualflareJSON)
	}

	// An .xml file whose name says jest is the same mistake mirrored.
	if fw, err := f.DetectFrameworkFromContent("jest-results.xml", []byte(`<notATestSuite/>`)); err == nil && fw == domain.FrameworkJest {
		t.Errorf("framework = %q for XML content named jest — the Jest parser reads .json", fw)
	}
}
