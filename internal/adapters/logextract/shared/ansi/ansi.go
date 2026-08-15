// Package ansi strips ANSI SGR (color/style) escape sequences from captured
// console output before a log-step extractor pattern-matches it. Most test
// frameworks auto-detect a non-TTY pipe and disable color, but --color or
// FORCE_COLOR=1 can force it back on, so every extractor strips defensively
// regardless.
package ansi

import "regexp"

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// Strip removes ANSI SGR escape sequences from s.
func Strip(s string) string {
	return sgrPattern.ReplaceAllString(s, "")
}
