package ctrf

import (
	"strings"

	"qualflare-cli/internal/core/domain"
)

// mapStatus maps CTRF's five-value status, refined by the optional
// pre-normalization rawStatus, onto the five this CLI's wire model has.
//
// # rawStatus first
//
// CTRF flattens every outcome into passed/failed/skipped/pending/other, but
// rawStatus preserves what the producing tool actually reported. Consulting it
// first is what recovers `error` from tools that HAD an error outcome before
// CTRF collapsed it into `failed`.
//
// # The mapping matches the native parser exactly
//
// timeout -> failed, aborted -> error and pending -> skipped are not choices
// made here; they are the conventions native/qualflare's mapStatus already
// applies to the very same status words. Diverging would mean the same input
// produced different results depending on which file extension it arrived in.
//
// Worth knowing: the SERVER has seven statuses including timeout, aborted and a
// first-class pending, so the api-service's own CTRF endpoint preserves all
// three. This CLI's wire model has five, so it cannot — the value is kept in
// ctrfRawStatus either way, but the typed status is folded.
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
	case "skipped", "pending":
		return domain.StatusSkipped
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
	"pending": domain.StatusSkipped,

	"error":   domain.StatusError,
	"errored": domain.StatusError,
	"broken":  domain.StatusError,
	"crashed": domain.StatusError,

	// The native parser folds these the same way; see the doc comment.
	"timeout":   domain.StatusFailed,
	"timedout":  domain.StatusFailed,
	"timed_out": domain.StatusFailed,

	"aborted":     domain.StatusError,
	"cancelled":   domain.StatusError,
	"canceled":    domain.StatusError,
	"interrupted": domain.StatusError,

	"todo":     domain.StatusSkipped,
	"disabled": domain.StatusSkipped,
	"ignored":  domain.StatusSkipped,
	"notrun":   domain.StatusSkipped,
	"not_run":  domain.StatusSkipped,
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
