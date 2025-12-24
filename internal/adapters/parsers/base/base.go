package base

import (
	"strconv"
	"time"
)

// ParseDuration parses a duration string (in seconds) to time.Duration
func ParseDuration(timeStr string) (time.Duration, error) {
	if timeStr == "" {
		return 0, nil
	}

	seconds, err := strconv.ParseFloat(timeStr, 64)
	if err != nil {
		return 0, err
	}

	return time.Duration(seconds * float64(time.Second)), nil
}

// ParseDurationMs parses a duration in milliseconds to time.Duration
func ParseDurationMs(ms float64) time.Duration {
	return time.Duration(ms * float64(time.Millisecond))
}

// ParseDurationNs parses a duration in nanoseconds to time.Duration
func ParseDurationNs(ns int64) time.Duration {
	return time.Duration(ns) * time.Nanosecond
}

// SafeString returns the string or empty if nil
func SafeString(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// SafeInt returns the int or 0 if nil
func SafeInt(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}

// CoalesceString returns the first non-empty string
func CoalesceString(strings ...string) string {
	for _, s := range strings {
		if s != "" {
			return s
		}
	}
	return ""
}

// TruncateString truncates a string to the specified length
func TruncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	if maxLen <= 3 {
		return s[:maxLen]
	}
	return s[:maxLen-3] + "..."
}

// ContentSignature defines signature for format detection
type ContentSignature struct {
	// JSONKeys are keys that should be present in JSON
	JSONKeys []string
	// XMLRoots are valid root element names for XML
	XMLRoots []string
	// FilenamePatterns are patterns to match against filenames
	FilenamePatterns []string
}
