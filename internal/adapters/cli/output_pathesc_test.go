package cli

import (
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

// TestPathArg_EscapesMetacharacters (SEC-03) confirms a malicious identifier
// cannot inject query/fragment/path metacharacters into the request URL.
func TestPathArg_EscapesMetacharacters(t *testing.T) {
	cases := map[string]string{
		"123":       "123",         // normal seq passes through
		"a?admin=1": "a%3Fadmin=1", // ? cannot start a query
		"a#frag":    "a%23frag",    // # cannot start a fragment
		"..":        "..",          // literal, but harmless — see path check below
		"a/b":       "a%2Fb",       // / cannot add a path segment
		"a b":       "a%20b",       // space escaped
	}
	for in, want := range cases {
		if got := pathArg(in); got != want {
			t.Errorf("pathArg(%q) = %q, want %q", in, got, want)
		}
	}

	// The escaped segment must not introduce a new path separator into the URL.
	url := "/api/v1/suite/" + pathArg("../../admin")
	if strings.Contains(url, "/admin") {
		t.Fatalf("path traversal reached the URL: %q", url)
	}
}

// TestDetailCommands_EscapeUserSuppliedSegment is the test that was missing: the one
// above proves pathArg works, but nothing proved the command handlers actually call it.
// They didn't — `plan get` and `plan cases` interpolated args[0] raw, so a traversal
// reached the request path. Driving every detail command end to end is what catches the
// next copy-paste, since these handlers are duplicated across seven files.
func TestDetailCommands_EscapeUserSuppliedSegment(t *testing.T) {
	const traversal = "../../admin"

	tests := []struct {
		name   string
		cmd    func(c *CLI) *cobra.Command
		args   []string
		prefix string // the path segment the request must stay under
	}{
		{"cluster get", (*CLI).createClusterCommand, []string{"get"}, apiV1 + "/cluster/"},
		{"defect get", (*CLI).createDefectCommand, []string{"get"}, apiV1 + "/defect/"},
		{"milestone get", (*CLI).createMilestoneCommand, []string{"get"}, apiV1 + "/milestone/"},
		{"suite get", (*CLI).createSuiteCommand, []string{"get"}, apiV1 + "/suite/"},
		{"launch get", (*CLI).createLaunchCommand, []string{"get"}, apiV1 + "/launch/"},
		{"case get", (*CLI).createCaseCommand, []string{"get"}, apiV1 + "/case/"},
		{"case steps", (*CLI).createCaseCommand, []string{"steps"}, apiV1 + "/case/"},
		{"plan get", (*CLI).createPlanCommand, []string{"get"}, apiV1 + "/test-plan/"},
		{"plan cases", (*CLI).createPlanCommand, []string{"cases"}, apiV1 + "/test-plan/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, api := newAPICLI(t)
			if err := run(t, tt.cmd(c), append(tt.args, traversal)...); err != nil {
				t.Fatalf("%s = %v", tt.name, err)
			}
			if api.calls != 1 {
				t.Fatalf("api called %d times, want 1", api.calls)
			}

			// The traversal must not have escaped the resource prefix.
			if !strings.HasPrefix(api.path, tt.prefix) {
				t.Errorf("path = %q, want it to stay under %q", api.path, tt.prefix)
			}
			// ".." must survive only as escaped text, never as real path segments.
			if strings.Contains(api.path, "/admin") {
				t.Errorf("path = %q — traversal reached the request path", api.path)
			}
			rest := strings.TrimPrefix(api.path, tt.prefix)
			if strings.Contains(strings.TrimSuffix(rest, "/steps"), "/") &&
				!strings.HasSuffix(rest, "/cases") {
				t.Errorf("user segment %q introduced a path separator", rest)
			}
		})
	}
}
