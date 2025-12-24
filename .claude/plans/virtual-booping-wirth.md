# Make Project Optional - API Key Determines Project

## Summary

Remove the project name requirement from the CLI. The project is determined from the API key on the server side.

---

## Files to Modify

### 1. `internal/config/config.go`

**Line 238-242**: Remove project validation

```go
// Before:
func (c *Config) Validate() error {
    if c.ProjectName == "" {
        return &ValidationError{Field: "project", Message: "project name is required. Set QF_PROJECT or use --project flag"}
    }
    return nil
}

// After:
func (c *Config) Validate() error {
    return nil
}
```

### 2. `internal/adapters/cli/command.go`

**Line 119**: Update flag description to not say "required"

```go
// Before:
cmd.Flags().StringVarP(&project, "project", "p", "", "Project name (required, or set QF_PROJECT)")

// After:
cmd.Flags().StringVarP(&project, "project", "p", "", "Project name (optional, defaults to API key project)")
```

### 3. `Makefile`

**Lines 187, 289**: Update help text

```makefile
# Before:
@echo "Example Upload (requires QF_PROJECT, QF_API_KEY, QF_API_ENDPOINT):"
# Example Upload Targets (requires QF_PROJECT, QF_API_KEY, QF_API_ENDPOINT)

# After:
@echo "Example Upload (requires QF_API_KEY, QF_API_ENDPOINT):"
# Example Upload Targets (requires QF_API_KEY, QF_API_ENDPOINT)
```

---

## Implementation Steps

1. Edit `internal/config/config.go` - remove project validation
2. Edit `internal/adapters/cli/command.go` - update flag description
3. Edit `Makefile` - update help text
4. Test with `make upload-junit` (should fail on missing API endpoint, not project)
