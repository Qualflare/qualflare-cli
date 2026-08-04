// Build-tool dependencies, deliberately kept in their own module so pinning a linter
// never perturbs the CLI's own dependency graph. Versions here are locked by
// tools/go.sum, which is what makes `go install` reproducible (githubactions:S8545).
module qualflare-cli/tools

go 1.25.0

require (
	golang.org/x/mod v0.35.0 // indirect
	golang.org/x/sync v0.20.0 // indirect
	golang.org/x/sys v0.43.0 // indirect
	golang.org/x/telemetry v0.0.0-20260421165255-392afab6f40e // indirect
	golang.org/x/tools v0.44.0 // indirect
	golang.org/x/vuln v1.3.0 // indirect
)
