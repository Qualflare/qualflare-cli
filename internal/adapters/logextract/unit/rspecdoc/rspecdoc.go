// Package rspecdoc extracts test cases and Given/When/Then steps from
// RSpec's built-in --format documentation output — a nested
// describe/context/it indentation tree, unlike pytest-bdd's flat
// boundary-line convention.
package rspecdoc

import (
	"bufio"
	"bytes"
	"regexp"
	"strconv"
	"strings"

	"qualflare-cli/internal/adapters/logextract/shared/ansi"
	"qualflare-cli/internal/adapters/logextract/shared/indenttree"
	"qualflare-cli/internal/core/domain"
)

type Extractor struct{}

func New() *Extractor {
	return &Extractor{}
}

var (
	keywordLine  = regexp.MustCompile(`^(Given|When|Then|And|But)\s+(.+?)\s*$`)
	failedSuffix = regexp.MustCompile(`\s*\(FAILED - (\d+)\)\s*$`)
	summaryLine  = regexp.MustCompile(`^\d+ examples?, \d+ failures?`)
	failureIndex = regexp.MustCompile(`^\s*(\d+)\)\s`)
	finishedLine = regexp.MustCompile(`^Finished in `)
)

// ExtractCases builds an indentation tree from the lines up to (not
// including) the first blank line — RSpec's documentation formatter emits
// exactly one blank line separating the hierarchical body from its
// summary/failure-detail footer. Each leaf of that tree is one example;
// its ancestor chain (filtered to Given/When/Then/And/But lines) becomes
// its Steps.
func (e *Extractor) ExtractCases(output []byte) ([]domain.Case, error) {
	body, footer := splitBodyAndFooter(output)
	failures := parseFailures(footer)

	lines := toIndentLines(body)
	roots := indenttree.Build(lines)
	leaves := indenttree.Leaves(roots)

	cases := make([]domain.Case, 0, len(leaves))
	for _, leaf := range leaves {
		cases = append(cases, caseFromLeaf(leaf, failures))
	}
	return cases, nil
}

func caseFromLeaf(leaf *indenttree.Node, failures map[int]string) domain.Case {
	ancestors := indenttree.Ancestors(leaf)

	var nameParts []string
	var steps []domain.Step
	status := domain.StatusPassed
	var failureNum int

	for i, n := range ancestors {
		text := n.Line.Text
		if i == len(ancestors)-1 {
			if m := failedSuffix.FindStringSubmatch(text); m != nil {
				status = domain.StatusFailed
				failureNum, _ = strconv.Atoi(m[1])
				text = failedSuffix.ReplaceAllString(text, "")
			}
		}
		nameParts = append(nameParts, text)

		if m := keywordLine.FindStringSubmatch(text); m != nil {
			steps = append(steps, domain.Step{Keyword: m[1], Name: m[2]})
		}
	}

	c := domain.Case{
		Name:   strings.Join(nameParts, " "),
		Status: status,
	}
	if status == domain.StatusFailed {
		if msg, ok := failures[failureNum]; ok {
			c.Error = msg
		}
		for i := range steps {
			steps[i].Status = domain.StatusFailed
		}
	} else {
		for i := range steps {
			steps[i].Status = domain.StatusPassed
		}
	}
	c.Steps = steps
	return c
}

// splitBodyAndFooter anchors on the footer's own markers — the literal
// "Failures:" header when present, else the "Finished in ..." line RSpec's
// documentation formatter always emits right before its summary — rather
// than guessing the boundary from blank-line position. Blank-line position
// is ambiguous: wrapper noise (a shell banner, an npm/yarn warning) can
// itself be followed by a blank line before the real tree even starts, so
// "the first blank line after any leading blank-only prefix" still mistakes
// noise for the tree and discards the real tree into the unused footer —
// this reproduced from a real `npx mocha`/wrapped-rspec run, not a
// hypothetical. Anchoring on a marker that can only occur once, near the
// end, sidesteps the ambiguity entirely: body is then read backward from
// that anchor as the single contiguous block of non-blank lines immediately
// preceding it — the real tree, regardless of what (noise, blank lines)
// precedes that block.
func splitBodyAndFooter(output []byte) (body, footer []byte) {
	lines := bytes.Split(output, []byte("\n"))

	footerStart := len(lines)
	for i, l := range lines {
		trimmed := strings.TrimSpace(ansi.Strip(string(l)))
		if trimmed == "Failures:" || finishedLine.MatchString(trimmed) {
			footerStart = i
			break
		}
	}
	if footerStart == len(lines) {
		// No recognizable footer marker (e.g. a formatter variant without
		// "Finished in ..."): fall back to the first blank line after any
		// leading blank-only prefix.
		start := 0
		for start < len(lines) && len(bytes.TrimSpace(lines[start])) == 0 {
			start++
		}
		for i := start; i < len(lines); i++ {
			if len(bytes.TrimSpace(lines[i])) == 0 {
				footerStart = i
				break
			}
		}
	}

	bodyEnd := footerStart
	for bodyEnd > 0 && len(bytes.TrimSpace(lines[bodyEnd-1])) == 0 {
		bodyEnd--
	}
	bodyStart := bodyEnd
	for bodyStart > 0 && len(bytes.TrimSpace(lines[bodyStart-1])) != 0 {
		bodyStart--
	}

	body = bytes.Join(lines[bodyStart:bodyEnd], []byte("\n"))
	footer = bytes.Join(lines[footerStart:], []byte("\n"))
	return body, footer
}

func toIndentLines(body []byte) []indenttree.Line {
	var lines []indenttree.Line
	scanner := bufio.NewScanner(bytes.NewReader(body))
	for scanner.Scan() {
		raw := ansi.Strip(scanner.Text())
		trimmed := strings.TrimLeft(raw, " ")
		if strings.TrimSpace(trimmed) == "" {
			continue
		}
		indent := len(raw) - len(trimmed)
		lines = append(lines, indenttree.Line{Indent: indent, Text: strings.TrimRight(trimmed, " \t\r"), Raw: raw})
	}
	return lines
}

// parseFailures reads the "Failures:" footer and returns failure-number ->
// message, matching the "(FAILED - N)" tags the documentation formatter
// stamps on each failing example line. A failure block starts at a line
// matching "  N) ..." and runs until the next such line or the end of the
// failures section.
func parseFailures(footer []byte) map[int]string {
	out := map[int]string{}
	if len(footer) == 0 {
		return out
	}

	scanner := bufio.NewScanner(bytes.NewReader(footer))
	var currentNum int
	var lines []string
	flush := func() {
		if currentNum != 0 {
			out[currentNum] = strings.TrimSpace(strings.Join(lines, "\n"))
		}
		lines = nil
	}

	for scanner.Scan() {
		raw := ansi.Strip(scanner.Text())
		trimmed := strings.TrimSpace(raw)

		if summaryLine.MatchString(trimmed) {
			break
		}
		if m := failureIndex.FindStringSubmatch(raw); m != nil {
			flush()
			currentNum, _ = strconv.Atoi(m[1])
			continue
		}
		if currentNum != 0 && trimmed != "" {
			lines = append(lines, trimmed)
		}
	}
	flush()
	return out
}

// Detect reports whether output looks like RSpec's own summary line
// ("N examples, N failures"), a low-false-positive signal RSpec's own
// documentation and progress formatters both emit.
func (e *Extractor) Detect(output []byte) bool {
	scanner := bufio.NewScanner(bytes.NewReader(output))
	for scanner.Scan() {
		if summaryLine.MatchString(strings.TrimSpace(ansi.Strip(scanner.Text()))) {
			return true
		}
	}
	return false
}

func (e *Extractor) GetFramework() domain.Framework {
	return domain.FrameworkRSpec
}
