package flutter

import (
	"strings"

	"qualflare-cli/internal/adapters/parsers/base"
	"qualflare-cli/internal/core/domain"
)

const retryPrefix = "Retry: "

// attemptSlice is the errors and print output of one attempt of a test.
type attemptSlice struct {
	errors []testError
	prints []string
}

// splitAttempts cuts a test's errors and prints at the recorded `Retry:`
// boundaries. It always returns at least one slice; the last is the attempt
// that produced the final testDone.
func splitAttempts(st *testState) []attemptSlice {
	var out []attemptSlice
	e, p := 0, 0
	for _, m := range st.retries {
		out = append(out, attemptSlice{errors: st.errors[e:m.errors], prints: st.prints[p:m.prints]})
		e, p = m.errors, m.prints
	}
	return append(out, attemptSlice{errors: st.errors[e:], prints: st.prints[p:]})
}

// outcome is what one attempt amounts to once its exception block (if any) is
// taken into account.
type outcome struct {
	status         domain.Status
	message, trace string
	text           string // message and trace formatted for Case.Error
	out            []string
}

// resolve derives an attempt's status, error text and output. result is the
// reporter's result for the attempt ("failure", "error"; "" for an earlier
// attempt, whose result is inferred).
func (a attemptSlice) resolve(result string) outcome {
	block, rest, ok := parseExceptionBlock(a.prints)
	o := outcome{out: rest}

	var msgs, traces, texts []string
	status := domain.StatusFailed
	switch {
	case ok:
		if block.kind != testFailureKind {
			status = domain.StatusError
		}
		kind := block.kind
		if kind == testFailureKind {
			kind = ""
		}
		msgs = append(msgs, domain.FormatError(block.message, "", kind))
		texts = append(texts, domain.FormatError(block.message, block.stack, kind))
		if block.stack != "" {
			traces = append(traces, block.stack)
		}
		for _, e := range a.errors {
			if strings.HasPrefix(e.message, genericFailure) {
				continue
			}
			msgs = append(msgs, e.message)
			if e.stack != "" {
				traces = append(traces, e.stack)
			}
			texts = append(texts, domain.FormatError(e.message, e.stack, ""))
		}
	default:
		status = domain.StatusFailed
		switch result {
		case "error":
			status = domain.StatusError
		case "":
			for _, e := range a.errors {
				if !e.isFailure {
					status = domain.StatusError
				}
			}
		}
		for _, e := range a.errors {
			if t := domain.FormatError(e.message, e.stack, ""); t != "" {
				texts = append(texts, t)
			}
			if e.message != "" {
				msgs = append(msgs, e.message)
			}
			if e.stack != "" {
				traces = append(traces, e.stack)
			}
		}
	}
	if result == "failure" {
		status = domain.StatusFailed
	}
	o.status = status
	o.message = capText(strings.Join(msgs, "\n\n"), base.MaxAttemptMessageRunes)
	o.trace = capText(strings.Join(traces, "\n\n"), base.MaxAttemptTraceRunes)
	o.text = capText(strings.Join(texts, "\n\n"), base.MaxAttemptMessageRunes+base.MaxAttemptTraceRunes)
	return o
}

// capText drops trailing whitespace (the stream's text ends in a newline) and
// bounds the text to max bytes.
func capText(s string, max int) string {
	return base.TruncateString(strings.TrimRight(s, " \t\r\n"), max)
}

// buildAttempts renders every attempt but the last as a failed attempt, and the
// last with the test's final status.
func buildAttempts(slices []attemptSlice, final domain.Status, finalOutcome outcome) []domain.Attempt {
	attempts := make([]domain.Attempt, 0, len(slices))
	for i, s := range slices {
		o := finalOutcome
		status := final
		if i < len(slices)-1 {
			o = s.resolve("")
			status = o.status
		}
		a := domain.Attempt{Number: i + 1, Status: status, Message: o.message, Trace: o.trace}
		if status == domain.StatusPassed {
			a.Message, a.Trace = "", ""
		}
		a.Stdout = base.ClampOutput(o.out)
		attempts = append(attempts, a)
	}
	return attempts
}
