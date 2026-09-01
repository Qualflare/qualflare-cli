package ctrf

import (
	"strings"

	"qualflare-cli/internal/core/domain"
)

// mapStatus maps CTRF's five-value status, refined by the optional
// pre-normalization rawStatus, onto domain.Status's seven.
//
// # rawStatus first
//
// CTRF flattens every outcome into passed/failed/skipped/pending/other, but
// rawStatus preserves what the producing tool actually reported. Consulting it
// first is what recovers error, timeout and aborted from tools that HAD those
// outcomes before CTRF collapsed them.
//
// # pending is NOT folded here, unlike in the mocha-family parsers
//
// cypress.go and native/qualflare deliberately map `pending` to skipped, because
// in Mocha `pending` is what an `it.skip` fires — it means skipped, and treating
// it as StatusPending would flip a whole green launch pending for one skipped
// test (BUG-08).
//
// CTRF is different: its enum carries `skipped` AND `pending` as separate
// values, so a document saying pending means pending. Folding it here would
// discard a distinction the format draws on purpose and the server can store.
// Same word, two meanings, decided by the source.
//
// # Unknown is fail-visible
//
// `other`, and anything unrecognized, map to error rather than to passed or
// skipped. CTRF defines `other` as "does not fit the other four"; in practice it
// carries broken, errored and interrupted. Mapping it anywhere green would let a
// launch containing them report clean, which is the one outcome a test reporter
// must never produce.
func mapStatus(status, rawStatus string) domain.Status {
	if s, ok := rawStatusMap[strings.ToLower(strings.TrimSpace(rawStatus))]; ok {
		return s
	}
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "passed":
		return domain.StatusPassed
	case "failed":
		return domain.StatusFailed
	case "skipped":
		return domain.StatusSkipped
	case "pending":
		return domain.StatusPending
	default:
		return domain.StatusError
	}
}

// rawStatusMap recognises the pre-normalization values producers actually emit.
// Deliberately closed and small: an unrecognized rawStatus falls through to the
// CTRF status rather than being guessed at.
var rawStatusMap = map[string]domain.Status{
	"passed":  domain.StatusPassed,
	"failed":  domain.StatusFailed,
	"skipped": domain.StatusSkipped,
	"pending": domain.StatusPending,

	"error":   domain.StatusError,
	"errored": domain.StatusError,
	"broken":  domain.StatusError,
	"crashed": domain.StatusError,

	"timeout":   domain.StatusTimeout,
	"timedout":  domain.StatusTimeout,
	"timed_out": domain.StatusTimeout,

	"aborted":     domain.StatusAborted,
	"cancelled":   domain.StatusAborted,
	"canceled":    domain.StatusAborted,
	"interrupted": domain.StatusAborted,

	// Deliberately skipped, not pending: all three describe a test that was
	// EXCLUDED from the run, which is what skipped means.
	"todo":     domain.StatusSkipped,
	"disabled": domain.StatusSkipped,
	"ignored":  domain.StatusSkipped,

	// These describe a test that has not run YET, which is what pending means.
	"notrun":  domain.StatusPending,
	"not_run": domain.StatusPending,
}

// categoryForTool maps a CTRF results.tool.name onto the category the imported
// suite should carry.
//
// It resolves through domain.Framework so the result is always a category the
// SERVER accepts: echoing an unrecognized tool name back as a category would
// fail the server's oneof and 400 the whole launch, which is the trap
// GetCategory's own doc comment describes.
//
// Deliberately conservative. jasmine, wdio, nightwatch, codeceptjs and the .NET
// runners have no Qualflare framework, and force-fitting them onto a near
// neighbour would put a falsehood in the data model to avoid an honest
// "generic". The raw tool name survives in the suite's ctrfTool property either
// way, so declining to guess loses nothing.
func categoryForTool(toolName string) domain.FrameworkCategory {
	name := strings.ToLower(strings.TrimSpace(toolName))
	name = strings.ReplaceAll(name, "_", "-")
	name = strings.ReplaceAll(name, " ", "-")

	if f, ok := toolAliases[name]; ok {
		return f.GetCategory()
	}
	if f := domain.Framework(name); f.IsValid() && f != domain.FrameworkQualflareJSON && f != domain.FrameworkCTRF {
		return f.GetCategory()
	}
	return domain.CategoryGeneric
}

// toolAliases holds unambiguous synonyms only. Names that already match a
// framework are resolved by IsValid above and need no entry here.
var toolAliases = map[string]domain.Framework{
	"go":         domain.FrameworkGolang,
	"go-test":    domain.FrameworkGolang,
	"gotest":     domain.FrameworkGolang,
	"pytest":     domain.FrameworkPython,
	"py.test":    domain.FrameworkPython,
	"postman":    domain.FrameworkNewman,
	"owasp-zap":  domain.FrameworkZAP,
	"sonar":      domain.FrameworkSonarQube,
	"sonarcloud": domain.FrameworkSonarQube,
}
