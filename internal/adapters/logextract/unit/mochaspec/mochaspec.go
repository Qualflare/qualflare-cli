// Package mochaspec extracts test cases and given/when/then steps from
// Mocha's default "spec" reporter output — the same nested-describe
// indentation-tree convention RSpec's documentation formatter uses, but
// JS/Mocha BDD style conventionally lowercases given/when/then (natural-
// English-sentence flow), so keyword matching here is case-insensitive.
package mochaspec

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
	keywordLine = regexp.MustCompile(`(?i)^(given|when|then|and|but)\s+(.+?)\s*$`)
	passMarker  = regexp.MustCompile(`^✔\s+(.+?)(?:\s*\(\d+m?s\))?\s*$`)
	failMarker  = regexp.MustCompile(`^(\d+)\)\s+(.+?)\s*$`)
	pendMarker  = regexp.MustCompile(`^-\s+(.+?)\s*$`)
	summaryLine = regexp.MustCompile(`^\d+ passing \(\d+m?s\)`)
	failIndex   = regexp.MustCompile(`^\s*(\d+)\)\s`)
)

// ExtractCases builds an indentation tree from the lines up to the first
// blank line (Mocha's spec reporter emits a blank line before its
// passing/failing summary), same split point convention as rspecdoc. Each
// leaf is one test; its ancestor chain (filtered to given/when/then/and/but
// lines) becomes its Steps.
func (e *Extractor) ExtractCases(output []byte) ([]domain.Case, error) {
	body, footer := splitBodyAndFooter(output)
	failures := parseFailures(footer)

	lines := toIndentLines(body)
	roots := indenttree.Build(lines)
	leaves := indenttree.Leaves(roots)

	cases := make([]domain.Case, 0, len(leaves))
	for _, leaf := range leaves {
		if c, ok := caseFromLeaf(leaf, failures); ok {
			cases = append(cases, c)
		}
	}
	return cases, nil
}

func caseFromLeaf(leaf *indenttree.Node, failures map[int]string) (domain.Case, bool) {
	ancestors := indenttree.Ancestors(leaf)

	var nameParts []string
	var steps []domain.Step
	status := domain.StatusPassed
	var failureNum int

	for i, n := range ancestors {
		text := n.Line.Text
		if i == len(ancestors)-1 {
			switch {
			case pendMarker.MatchString(text):
				status = domain.StatusSkipped
				text = pendMarker.FindStringSubmatch(text)[1]
			case passMarker.MatchString(text):
				text = passMarker.FindStringSubmatch(text)[1]
			default:
				if m := failMarker.FindStringSubmatch(text); m != nil {
					status = domain.StatusFailed
					failureNum, _ = strconv.Atoi(m[1])
					text = m[2]
				}
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
			steps[i].Status = status
		}
	}
	c.Steps = steps
	return c, true
}

// splitBodyAndFooter finds the first blank line AFTER real content starts —
// not simply the first blank line in the output. Mocha's spec reporter (and
// wrapper noise like npm/npx banners) can print blank lines BEFORE the tree
// as well as between the tree and the passing/failing footer; splitting on
// the very first blank line anywhere mistakes a leading blank line for the
// boundary and discards the entire real tree into the footer, which
// ExtractCases never turns into cases (this is a real, reproduced-from-a-
// live-npx-run bug, not a hypothetical). Skipping past any leading
// blank-only prefix first, then taking the next blank line, finds the true
// boundary regardless of what precedes the tree.
func splitBodyAndFooter(output []byte) (body, footer []byte) {
	lines := bytes.Split(output, []byte("\n"))

	start := 0
	for start < len(lines) && len(bytes.TrimSpace(lines[start])) == 0 {
		start++
	}

	boundary := len(lines)
	for i := start; i < len(lines); i++ {
		if len(bytes.TrimSpace(lines[i])) == 0 {
			boundary = i
			break
		}
	}

	body = bytes.Join(lines[:boundary], []byte("\n"))
	footer = bytes.Join(lines[boundary:], []byte("\n"))
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

// parseFailures reads Mocha's numbered failure blocks in the footer
// (Mocha has no "Failures:" header — the blocks appear directly after the
// passing/failing counts). Within each block, the actual assertion message
// is everything after the last description-echo line (which ends in ":"),
// so the nested "given/when/then" echo lines are not mistaken for the error
// text itself.
func parseFailures(footer []byte) map[int]string {
	out := map[int]string{}
	if len(footer) == 0 {
		return out
	}

	blocks := map[int][]string{}
	var order []int
	var currentNum int

	scanner := bufio.NewScanner(bytes.NewReader(footer))
	for scanner.Scan() {
		raw := ansi.Strip(scanner.Text())
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			continue
		}
		if m := failIndex.FindStringSubmatch(raw); m != nil {
			currentNum, _ = strconv.Atoi(m[1])
			order = append(order, currentNum)
			blocks[currentNum] = append(blocks[currentNum], trimmed)
			continue
		}
		if currentNum != 0 {
			blocks[currentNum] = append(blocks[currentNum], trimmed)
		}
	}

	for _, n := range order {
		lines := blocks[n]
		lastDescIdx := -1
		for i, l := range lines {
			if strings.HasSuffix(l, ":") {
				lastDescIdx = i
			}
		}
		var msgLines []string
		if lastDescIdx >= 0 && lastDescIdx+1 < len(lines) {
			msgLines = lines[lastDescIdx+1:]
		} else {
			msgLines = lines
		}
		out[n] = strings.TrimSpace(strings.Join(msgLines, "\n"))
	}
	return out
}

// Detect reports whether output looks like Mocha's own summary line
// ("N passing (Nms)").
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
	return domain.FrameworkMocha
}
