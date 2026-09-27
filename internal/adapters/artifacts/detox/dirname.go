package detox

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// DirectoryName returns the directory Detox writes a test's artifacts into,
// computed the same way Detox computes it:
//
//	dirname = sanitize(prefix + fullName.slice(-(255 - len(prefix) - len(suffix))) + suffix,
//	                   "_").replace("$", "_")
//
// The two truncations differ in unit and that is deliberate, not an oversight:
// Detox trims the name by JavaScript string length (UTF-16 code units, which
// for the BMP is runes), while sanitize-filename truncates the result by UTF-8
// BYTES. Both are reproduced rather than unified, because agreeing with Detox
// matters more than being internally tidy.
func DirectoryName(fullName, status string, invocation int) string {
	prefix := statusGlyph(status)
	suffix := ""
	if invocation > 1 {
		suffix = fmt.Sprintf(" (%d)", invocation)
	}

	// slice(-N) using UTF-16 code units (JavaScript string length), not runes.
	// This matters for astral-plane characters (emoji, some CJK extensions) that
	// encode as surrogate pairs in UTF-16. Count in code units to match Detox.
	prefixUnits := len(utf16.Encode([]rune(prefix)))
	suffixUnits := len(utf16.Encode([]rune(suffix)))
	budget := maxFilenameBytes - prefixUnits - suffixUnits
	units := utf16.Encode([]rune(fullName))
	if budget < 0 {
		budget = 0
	}
	if len(units) > budget {
		units = units[len(units)-budget:]
	}
	// When the trim boundary falls inside a surrogate pair, utf16.Decode
	// substitutes U+FFFD; JavaScript would keep a lone surrogate and write it
	// as WTF-8. Those bytes cannot be made to agree portably across systems.
	// We accept the U+FFFD substitution as the best Go can do.
	trimmedName := string(utf16.Decode(units))

	unsafe := prefix + trimmedName + suffix
	// Detox replaces "$" itself, after sanitize-filename has run.
	return strings.ReplaceAll(sanitizeFilename(unsafe, "_"), "$", "_")
}

func statusGlyph(status string) string {
	switch status {
	case "passed":
		return "✓ "
	case "failed":
		return "✗ "
	default:
		return ""
	}
}
