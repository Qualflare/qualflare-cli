package detox

import (
	"strings"
	"testing"
)

// The rules are the npm package's own (parshap/node-sanitize-filename):
// illegal chars / ? < > \ : * | " ; C0 and C1 control codes; a name of only
// dots; Windows reserved device names; trailing dots and spaces; then a
// truncation to 255 BYTES.
//
// The subtlety worth keeping: when a replacement is given the package runs its
// whole sanitise pass TWICE — once with the replacement, then again with the
// empty string. A port that runs it once agrees on ordinary names and diverges
// on the awkward ones, which is the worst possible place to differ.
func TestSanitizeFilename(t *testing.T) {
	tests := []struct {
		name, in, replacement, want string
	}{
		{"illegal characters", `a/b?c<d>e\f:g*h|i"j`, "_", "a_b_c_d_e_f_g_h_i_j"},
		{"control codes C0 and C1", "a" + string(rune(0x00)) + "b" + string(rune(0x1f)) + "c" + string(rune(0x80)) + "d", "_", "a_b_c_d"},
		{"only dots is reserved", "..", "_", "_"},
		{"windows device name", "CON", "_", "_"},
		{"windows device name with extension", "con.txt", "_", "_"},
		{"trailing dots and spaces", "name.. ", "_", "name_"},
		{"ordinary name untouched", "Login should sign in", "_", "Login should sign in"},
		{"empty replacement drops the character", "a/b", "", "ab"},
		// Truncation is by BYTES, not runes: three-byte characters cap out at 85.
		{"truncates to 255 bytes", str("ü", 200), "_", str("ü", 127)},
		// Non-ASCII test cases: multi-byte characters must not be corrupted by byte-level checks
		{"Turkish: ş not corrupted", "İstanbul günaydın çalışıyor", "_", "İstanbul günaydın çalışıyor"},
		{"Cyrillic: Привет unchanged", "Привет", "_", "Привет"},
		{"Greek: Καλημέρα unchanged", "Καλημέρα", "_", "Καλημέρα"},
		{"CJK: こんにちは unchanged", "こんにちは", "_", "こんにちは"},
		{"Emoji: 😀 unchanged", "😀", "_", "😀"},
		// C1 control code point U+0080 and U+009F properly encoded in UTF-8
		{"encoded U+0080 replaced", "a" + string(rune(0x0080)) + "b" + string(rune(0x009f)) + "c", "_", "a_b_c"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sanitizeFilename(tt.in, tt.replacement); got != tt.want {
				t.Errorf("sanitizeFilename(%q, %q) = %q, want %q", tt.in, tt.replacement, got, tt.want)
			}
		})
	}
}

func str(s string, n int) string {
	return strings.Repeat(s, n)
}
