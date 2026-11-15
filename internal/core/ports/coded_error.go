package ports

// The two refusals the CLI reacts to by code. They must match the server's
// result.Code constants.
const (
	CodeWorkspaceFrozen = "billing.workspace_frozen"
	CodeStorageExceeded = "quota.storage_exceeded"
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
