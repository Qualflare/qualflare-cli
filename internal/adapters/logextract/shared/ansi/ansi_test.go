package ansi

import "testing"

func TestStripRemovesSGRSequences(t *testing.T) {
	in := "\x1b[32mpassed\x1b[0m"
	want := "passed"
	if got := Strip(in); got != want {
		t.Errorf("Strip(%q) = %q, want %q", in, got, want)
	}
}

func TestStripLeavesPlainTextUnchanged(t *testing.T) {
	in := "no escapes here"
	if got := Strip(in); got != in {
		t.Errorf("Strip(%q) = %q, want unchanged", in, got)
	}
}

func TestStripHandlesMultipleSequencesInOneLine(t *testing.T) {
	in := "\x1b[1m\x1b[31mfailed\x1b[0m: \x1b[2mexpected true\x1b[0m"
	want := "failed: expected true"
	if got := Strip(in); got != want {
		t.Errorf("Strip(%q) = %q, want %q", in, got, want)
	}
}

func TestStripEmptyString(t *testing.T) {
	if got := Strip(""); got != "" {
		t.Errorf("Strip(\"\") = %q, want empty", got)
	}
}
