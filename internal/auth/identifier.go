package auth

import (
	"fmt"
	"regexp"
)

// Maintainer note: when adding a new top-level (flat) command to the CLI,
// add its name (and any aliases) here so users cannot register an identifier
// that would collide with the command at the root. Existing configs with
// such identifiers continue to load (Validate runs at login time only) but
// new logins are rejected.
var reservedNames = map[string]struct{}{
	"login":        {},
	"logout":       {},
	"projects":     {},
	"version":      {},
	"list-formats": {},
	"formats":      {},
	"lf":           {},
	"help":         {},
	"completion":   {},
	"__complete":   {},
}

var identifierPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,62}$`)

// Validate reports whether id is acceptable as a saved project identifier.
func Validate(id string) error {
	if id == "" {
		return fmt.Errorf("identifier cannot be empty")
	}
	if _, ok := reservedNames[id]; ok {
		return fmt.Errorf("identifier %q is reserved", id)
	}
	if !identifierPattern.MatchString(id) {
		return fmt.Errorf("identifier %q is invalid: must start with [a-z0-9] and contain only lowercase letters, digits, hyphens, or underscores (max 63 chars)", id)
	}
	return nil
}
