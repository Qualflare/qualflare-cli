package ports

// The refusals the CLI reacts to by code. They must match the server's
// result.Code constants.
const (
	CodeWorkspaceFrozen = "billing.workspace_frozen"
	CodeStorageExceeded = "quota.storage_exceeded"
	// CodeQuotaExceeded is not by itself a plan limit: the server also sends it
	// with a 400 for limits that have nothing to do with the plan. See
	// PlanLimitRefusal.
	CodeQuotaExceeded = "quota.exceeded"
)

// CodedError is an error that carries the server's error code and message.
//
// It exists so a caller outside the HTTP adapter (the services package, which
// cannot import the adapter) can tell why a request was refused and show the
// server's own words, without depending on the adapter's concrete error type.
// Reach it with errors.As.
type CodedError interface {
	error
	ErrorCode() string    // the server's error code, "" when it sent none
	ErrorMessage() string // the server's message alone, on one line: no operation, no status, no hint
}

// PlanLimitRefusal is an error that can say whether the server refused the request
// because the workspace reached a limit of its plan: the monthly reports, or the
// test cases or test suites an upload would add. Reach it with errors.As.
//
// A frozen workspace and a full storage are refusals of their own and do not count.
type PlanLimitRefusal interface {
	error
	// PlanLimitReason returns the server's reason on one line, and whether this
	// is such a refusal at all.
	PlanLimitReason() (reason string, ok bool)
}
