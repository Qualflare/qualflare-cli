package base

import (
	"math"
	"strconv"
	"strings"
	"time"
)

// MaxShardIndex is the largest shard index that can be transmitted. The server stores
// shard_index in a 32-bit signed INTEGER column, so a value outside [0, MaxShardIndex]
// is not storable there and must never leave this CLI.
const MaxShardIndex = math.MaxInt32

// ParseShardIndex parses the value of a shard property and reports whether it is
// usable. A value that is not an integer, is negative, or overflows the server's
// 32-bit signed shard_index column is reported as unusable so the caller leaves
// ShardIndex nil — "this report carries no shard information" — instead of emitting a
// number the receiving system cannot store. Surrounding whitespace is tolerated.
//
// This is a silent skip, exactly like a non-numeric value: a shard hint is optional
// metadata, and one malformed property must never fail an upload.
func ParseShardIndex(value string) (int, bool) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n < 0 || n > MaxShardIndex {
		return 0, false
	}
	return n, true
}

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
