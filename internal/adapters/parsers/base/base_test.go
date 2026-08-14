package base

import (
	"testing"
	"time"
)

// ParseDuration reads the seconds-as-string form that JUnit-family reports use
// (time="1.5"), so fractional values must survive rather than truncate to whole seconds.
func TestParseDuration(t *testing.T) {
	tests := []struct {
		in      string
		want    time.Duration
		wantErr bool
	}{
		{"", 0, false},
		{"0", 0, false},
		{"1", time.Second, false},
		{"1.5", 1500 * time.Millisecond, false},
		{"0.001", time.Millisecond, false},
		{"-2", -2 * time.Second, false},
		{"1e2", 100 * time.Second, false},
		{"abc", 0, true},
		{"1,5", 0, true},
	}
	for _, tt := range tests {
		got, err := ParseDuration(tt.in)
		if tt.wantErr {
			if err == nil {
				t.Errorf("ParseDuration(%q) = nil error, want a failure", tt.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseDuration(%q) = %v", tt.in, err)
			continue
		}
		if got != tt.want {
			t.Errorf("ParseDuration(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseDurationMs(t *testing.T) {
	tests := []struct {
		in   float64
		want time.Duration
	}{
		{0, 0},
		{1000, time.Second},
		{1.5, 1500 * time.Microsecond},
		{-250, -250 * time.Millisecond},
	}
	for _, tt := range tests {
		if got := ParseDurationMs(tt.in); got != tt.want {
			t.Errorf("ParseDurationMs(%v) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestParseDurationNs(t *testing.T) {
	if got := ParseDurationNs(1_500_000_000); got != 1500*time.Millisecond {
		t.Errorf("ParseDurationNs(1.5e9) = %v, want 1.5s", got)
	}
	if got := ParseDurationNs(0); got != 0 {
		t.Errorf("ParseDurationNs(0) = %v, want 0", got)
	}
}

func TestSafeString(t *testing.T) {
	if got := SafeString(nil); got != "" {
		t.Errorf("SafeString(nil) = %q, want empty", got)
	}
	v := "value"
	if got := SafeString(&v); got != "value" {
		t.Errorf("SafeString(&v) = %q, want %q", got, "value")
	}
	empty := ""
	if got := SafeString(&empty); got != "" {
		t.Errorf("SafeString(&\"\") = %q, want empty", got)
	}
}

func TestSafeInt(t *testing.T) {
	if got := SafeInt(nil); got != 0 {
		t.Errorf("SafeInt(nil) = %d, want 0", got)
	}
	// A pointer to 0 must be indistinguishable from nil in the result, but the
	// distinction matters to callers deciding whether a field was present at all.
	zero := 0
	if got := SafeInt(&zero); got != 0 {
		t.Errorf("SafeInt(&0) = %d, want 0", got)
	}
	n := 7
	if got := SafeInt(&n); got != 7 {
		t.Errorf("SafeInt(&7) = %d, want 7", got)
	}
}

func TestCoalesceString(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want string
	}{
		{"first wins", []string{"a", "b"}, "a"},
		{"skips empties", []string{"", "", "c"}, "c"},
		{"all empty", []string{"", ""}, ""},
		{"no args", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := CoalesceString(tt.in...); got != tt.want {
				t.Errorf("CoalesceString(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

func TestTruncateString(t *testing.T) {
	tests := []struct {
		name   string
		s      string
		maxLen int
		want   string
	}{
		{"shorter than limit", "abc", 10, "abc"},
		{"exactly at limit", "abcde", 5, "abcde"},
		{"truncates with ellipsis", "abcdefghij", 8, "abcde..."},
		// At or below 3 there is no room for the ellipsis, so it hard-cuts instead.
		{"limit of 3 hard-cuts", "abcdef", 3, "abc"},
		{"limit of 0", "abcdef", 0, ""},
		{"empty input", "", 5, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := TruncateString(tt.s, tt.maxLen)
			if got != tt.want {
				t.Errorf("TruncateString(%q, %d) = %q, want %q", tt.s, tt.maxLen, got, tt.want)
			}
			if len(got) > tt.maxLen && len(tt.s) > tt.maxLen {
				t.Errorf("TruncateString(%q, %d) = %q, which exceeds the limit", tt.s, tt.maxLen, got)
			}
		})
	}
}

// ParseShardIndex is the single bound check behind every property-driven shard hint
// (junitxml's and pytest's `<property name="shard">`). The bound is not cosmetic: the
// server stores shard_index in a 32-bit signed INTEGER column, and an out-of-range value
// is rejected there — historically rejecting the whole launch upload, not just the one
// case. So anything unstorable must be reported as unusable and dropped here.
func TestParseShardIndex(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  int
		ok    bool
	}{
		{"zero", "0", 0, true},
		{"positive", "7", 7, true},
		{"whitespace padded", "  12\n", 12, true},
		{"int32 max is the last valid value", "2147483647", 2147483647, true},
		{"one past int32 max", "2147483648", 0, false},
		{"int64 max", "9223372036854775807", 0, false},
		{"digits beyond any int", "999999999999999999999999", 0, false},
		{"negative", "-1", 0, false},
		{"large negative", "-2147483649", 0, false},
		{"non-numeric", "shard-3", 0, false},
		{"float", "1.5", 0, false},
		{"empty", "", 0, false},
		{"whitespace only", "   ", 0, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := ParseShardIndex(tt.value)
			if ok != tt.ok {
				t.Fatalf("ParseShardIndex(%q) ok = %v, want %v", tt.value, ok, tt.ok)
			}
			if got != tt.want {
				t.Errorf("ParseShardIndex(%q) = %d, want %d", tt.value, got, tt.want)
			}
			// A rejected value must never leak a usable-looking number to the caller.
			if !ok && got != 0 {
				t.Errorf("ParseShardIndex(%q) returned %d alongside ok=false", tt.value, got)
			}
		})
	}
}

// The same bound for callers that already hold an int — Playwright's JSON workerIndex.
// On the 64-bit targets this CLI ships for, decoding cannot be relied on to reject an
// out-of-range value, so this is the only thing standing between a crafted report and an
// unstorable shard_index.
func TestValidShardIndex(t *testing.T) {
	tests := []struct {
		in   int
		want bool
	}{
		{0, true},
		{7, true},
		{2147483647, true},  // int32 max, the last storable value
		{2147483648, false}, // one past it: fine for a 64-bit int, not for the column
		{9223372036854775807, false},
		{-1, false},
		{-3, false},
		{-9223372036854775808, false},
	}
	for _, tt := range tests {
		if got := ValidShardIndex(tt.in); got != tt.want {
			t.Errorf("ValidShardIndex(%d) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
