package services

import "testing"

// TestDedupeFiles (BUG-40) ensures a file passed twice — directly or via
// overlapping globs / "./x" vs "x" — is only counted once, preserving order.
func TestDedupeFiles(t *testing.T) {
	in := []string{"results.xml", "./results.xml", "other.xml", "results.xml"}
	out := dedupeFiles(in)
	if len(out) != 2 {
		t.Fatalf("got %d files, want 2 (deduped): %v", len(out), out)
	}
	if out[0] != "results.xml" || out[1] != "other.xml" {
		t.Fatalf("order/content wrong: %v", out)
	}
}
