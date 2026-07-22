package factory

import (
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
