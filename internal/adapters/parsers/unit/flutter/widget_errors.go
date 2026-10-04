package flutter

import (
	"regexp"
	"strings"
)

const (
	exceptionHeader = "══╡ EXCEPTION CAUGHT BY FLUTTER TEST FRAMEWORK ╞"
	exceptionStack  = "When the exception was thrown, this was the stack:"
	exceptionClose  = "════"
	genericFailure  = "Test failed. See exception logs above."
	testFailureKind = "TestFailure"
)

var thrownRe = regexp.MustCompile(`^The following (.+?) was thrown`)

// exceptionBlock is the framework's own report of a widget-test exception,
// printed to the test's output instead of being carried by the error event.
type exceptionBlock struct {
	kind, message, stack string
}

// flattenPrints splits print messages (one line each, or several in one
// message) into lines.
func flattenPrints(prints []string) []string {
	var lines []string
	for _, msg := range prints {
		lines = append(lines, strings.Split(strings.TrimSuffix(msg, "\n"), "\n")...)
	}
	return lines
}

// parseExceptionBlock finds the first EXCEPTION CAUGHT block in the prints and
// returns it with the remaining output lines (block removed). ok is false when
// there is no well-formed block, and rest is then the flattened prints.
func parseExceptionBlock(prints []string) (block exceptionBlock, rest []string, ok bool) {
	lines := flattenPrints(prints)
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, exceptionHeader) {
			start = i
			break
		}
	}
	if start < 0 || start+1 >= len(lines) {
		return exceptionBlock{}, lines, false
	}
	m := thrownRe.FindStringSubmatch(lines[start+1])
	if m == nil {
		return exceptionBlock{}, lines, false
	}

	end := len(lines)
	for i := start + 2; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], exceptionClose) {
			end = i
			break
		}
	}
	body := lines[start+2 : end]
	msgLines, stackLines := body, []string(nil)
	for i, l := range body {
		if l == exceptionStack {
			msgLines, stackLines = body[:i], body[i+1:]
			break
		}
	}

	block = exceptionBlock{
		kind:    m[1],
		message: trimBlank(msgLines),
		stack:   trimBlank(stackLines),
	}
	rest = append(rest, lines[:start]...)
	if end < len(lines) {
		rest = append(rest, lines[end+1:]...)
	}
	return block, rest, true
}

// trimBlank joins lines, dropping blank lines at both ends.
func trimBlank(lines []string) string {
	for len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}
