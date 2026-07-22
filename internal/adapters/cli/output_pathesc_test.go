package cli

import (
	"fmt"
	"strings"
	"testing"
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
	url := fmt.Sprintf("/api/v1/suite/%s", pathArg("../../admin"))
	if strings.Contains(url, "/admin") {
		t.Fatalf("path traversal reached the URL: %q", url)
	}
}
