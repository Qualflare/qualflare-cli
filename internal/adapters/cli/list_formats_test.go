package cli

import (
	"io"
	"os"
	"strings"
	"testing"

	"qualflare-cli/internal/core/domain"
)

// TestBuildFrameworkDisplayGroups_IncludesEveryFramework is the direct regression
// test for the Critical `qf list-formats` bug: printFormats used to group frameworks
// by Framework.GetCategory(), which (after the per-framework category redesign)
// returns a category named after the framework itself for every specifically
// identified framework — a value the six-bucket print loop never looked at, so every
// framework but qualflare-json silently vanished from the output. This asserts every
// framework in domain.AllFrameworks() lands in exactly one of the six display buckets.
func TestBuildFrameworkDisplayGroups_IncludesEveryFramework(t *testing.T) {
	groups := buildFrameworkDisplayGroups()

	seen := make(map[domain.Framework]int, len(domain.AllFrameworks()))
	for cat, frameworks := range groups {
		for _, fw := range frameworks {
			seen[fw]++
			if _, ok := frameworkDisplayGroups[fw]; !ok {
				t.Errorf("framework %q grouped under %q via the CategoryGeneric fallback — add it to frameworkDisplayGroups explicitly", fw, cat)
			}
		}
	}

	for _, fw := range domain.AllFrameworks() {
		switch seen[fw] {
		case 0:
			t.Errorf("framework %q is missing from every display group", fw)
		case 1:
			// exactly right
		default:
			t.Errorf("framework %q appears in %d display groups, want exactly 1", fw, seen[fw])
		}
	}
}

// TestPrintFormats_ListsEveryFramework is an end-to-end regression test for the same
// bug at the actual printed-output level: `qf list-formats` (with no --category filter)
// must mention every single supported framework, not just qualflare-json.
func TestPrintFormats_ListsEveryFramework(t *testing.T) {
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stdout = w

	cli := &CLI{}
	cli.printFormats("")

	_ = w.Close()
	os.Stdout = origStdout

	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read captured stdout: %v", err)
	}
	output := string(out)

	for _, fw := range domain.AllFrameworks() {
		if !strings.Contains(output, string(fw)) {
			t.Errorf("list-formats output missing framework %q\nfull output:\n%s", fw, output)
		}
	}
}
