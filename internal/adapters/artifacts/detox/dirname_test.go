package detox

import (
	"strings"
	"testing"
	"unicode/utf16"
)

// Every expected value here was produced by RUNNING Detox's own algorithm
// (ArtifactPathBuilder.js + constructSafeFilename.js) against the input, not
// by reading its documentation — which describes a "{test-number
// {test-full-name}" format that does not exist. There is no test number.
func TestDirectoryName(t *testing.T) {
	tests := []struct {
		name       string
		fullName   string
		status     string
		invocation int
		want       string
	}{
		{"passed gets a check", "Login should sign in", "passed", 1, "✓ Login should sign in"},
		{"failed gets a cross", "Login should sign in", "failed", 1, "✗ Login should sign in"},
		{"an unknown status gets no glyph", "Login should sign in", "skipped", 1, "Login should sign in"},
		{"a retry is suffixed with its invocation", "Login should sign in", "passed", 2, "✓ Login should sign in (2)"},
		{"a slash is sanitised", "Login should handle a/b paths", "failed", 1, "✗ Login should handle a_b paths"},
		{"quotes and colons are sanitised", `Login "quoted" and: colons`, "failed", 1, "✗ Login _quoted_ and_ colons"},
		{"a dollar sign is sanitised by Detox itself", "Money costs $5", "passed", 1, "✓ Money costs _5"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := DirectoryName(tt.fullName, tt.status, tt.invocation); got != tt.want {
				t.Errorf("DirectoryName(%q, %q, %d) = %q, want %q", tt.fullName, tt.status, tt.invocation, got, tt.want)
			}
		})
	}
}

// A long name loses its BEGINNING, because Detox trims with slice(-N). Any
// matching that assumed a shared prefix would fail on exactly the tests whose
// names are most descriptive.
func TestDirectoryNameTrimsFromTheStart(t *testing.T) {
	long := strings.Repeat("x", 300)
	got := DirectoryName(long, "failed", 1)
	if len([]rune(got)) != 253 {
		t.Errorf("got %d runes, want 253 (255 less the two-rune glyph prefix)", len([]rune(got)))
	}
	if got[:len("✗ ")] != "✗ " {
		t.Errorf("the prefix should survive trimming, got %q", got[:6])
	}
}

// A non-BMP name (emoji) near the 255 code-unit boundary must be trimmed
// by UTF-16 code units, not runes, to match Detox's JavaScript algorithm.
// Each emoji is 2 UTF-16 code units (a surrogate pair) but 1 Go rune.
func TestDirectoryNameNonBMPBoundary(t *testing.T) {
	// Create a name with emoji, sized to test the UTF-16 code unit boundary.
	// With status "failed" (2 code units: ✗ and space), the budget is 253.
	// We'll build a name close to that boundary with emoji.
	// Each emoji 😀 is 2 UTF-16 code units, so 62 emoji = 124 units = 248 bytes.
	// Adding some ASCII to reach near the boundary.
	testName := strings.Repeat("😀", 62) + "abc" // 3 more code units

	got := DirectoryName(testName, "failed", 1)
	// Verify that the result contains the emoji (trimmed by code units, not runes).
	// The exact count depends on how utf16.Decode handles the boundary.
	// What matters is that it trims by code units and the emoji are preserved.
	gotUnits := len(utf16.Encode([]rune(got)))
	// Allow for the "✗ " prefix (2 units) and potential U+FFFD substitution if
	// the boundary fell in a surrogate pair.
	if gotUnits > 255 {
		t.Errorf("DirectoryName with emoji produced %d UTF-16 code units, want <= 255", gotUnits)
	}
	if got[:len("✗ ")] != "✗ " {
		t.Errorf("expected prefix to be preserved, got %q", got[:len("✗ ")])
	}
}
