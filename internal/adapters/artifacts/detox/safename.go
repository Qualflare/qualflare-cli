package detox

import (
	"regexp"
	"unicode/utf8"
)

// A faithful port of the npm `sanitize-filename` package, which is what Detox
// runs its directory names through (detox/src/utils/constructSafeFilename.js).
// Matching computes names forward, so any divergence here shows up as
// artifacts that silently fail to attach.
var (
	illegalRe       = regexp.MustCompile(`[/?<>\\:*|"]`)
	onlyDotsRe      = regexp.MustCompile(`^\.+$`)
	windowsDeviceRe = regexp.MustCompile(`(?i)^(con|prn|aux|nul|com[0-9]|lpt[0-9])(\..*)?$`)
)

// maxFilenameBytes is a BYTE limit, not a rune limit: the package truncates
// with truncate-utf8-bytes.
const maxFilenameBytes = 255

func sanitizeFilename(input, replacement string) string {
	out := sanitizeOnce(input, replacement)
	if replacement == "" {
		return out
	}
	// The package runs the whole pass a second time with an empty replacement,
	// so a replacement that itself introduced something illegal cannot survive.
	return sanitizeOnce(out, "")
}

func sanitizeOnce(input, replacement string) string {
	out := illegalRe.ReplaceAllString(input, replacement)
	out = replaceControlChars(out, replacement)
	out = onlyDotsRe.ReplaceAllString(out, replacement)
	out = windowsDeviceRe.ReplaceAllString(out, replacement)
	out = replaceTrailingDotsAndSpaces(out, replacement)
	return truncateUTF8Bytes(out, maxFilenameBytes)
}

// replaceControlChars replaces C0 (0x00-0x1F) and C1 (0x80-0x9F) control code points.
// Must iterate through runes (code points), not bytes, to avoid corrupting multi-byte
// UTF-8 characters whose continuation bytes happen to fall in 0x80-0x9F.
func replaceControlChars(s, replacement string) string {
	var result []rune
	for _, r := range s {
		if (r >= 0x00 && r <= 0x1f) || (r >= 0x80 && r <= 0x9f) {
			for _, cr := range replacement {
				result = append(result, cr)
			}
		} else {
			result = append(result, r)
		}
	}
	return string(result)
}

// replaceTrailingDotsAndSpaces mirrors the package's own loop, which avoids a
// regex deliberately (its CWE-1333 fix). Note it replaces the whole trailing
// run with ONE replacement, not one per character.
func replaceTrailingDotsAndSpaces(s, replacement string) string {
	end := len(s)
	for end > 0 && (s[end-1] == '.' || s[end-1] == ' ') {
		end--
	}
	if end == len(s) {
		return s
	}
	return s[:end] + replacement
}

// truncateUTF8Bytes cuts to at most limit bytes without splitting a rune.
func truncateUTF8Bytes(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}
