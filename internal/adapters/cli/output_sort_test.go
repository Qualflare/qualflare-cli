package cli

import (
	"net/url"
	"testing"
)

// API-02: addSorting must send sortDir ONLY when the user explicitly passed
// --sort-desc. Always sending it (the old behavior) pinned every list to the
// client-supplied direction, so the server could never apply its newest-first
// default for launches/defects. sortBy is still sent whenever non-empty.
func TestAddSorting(t *testing.T) {
	tests := []struct {
		name          string
		sortBy        string
		sortDesc      bool
		sortDirSet    bool
		wantSortByVal string // "" => key absent
		wantSortDir   string // "" => key absent
	}{
		{"nothing set -> no params", "", false, false, "", ""},
		{"column only -> sortBy, no sortDir", "name", false, false, "name", ""},
		{"explicit --sort-desc -> sortDir=true", "", true, true, "", "true"},
		{"explicit ascending -> sortDir=false", "", false, true, "", "false"},
		{"column + explicit desc", "seq", true, true, "seq", "true"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			params := url.Values{}
			addSorting(params, tt.sortBy, tt.sortDesc, tt.sortDirSet)

			if got := params.Get("sortBy"); got != tt.wantSortByVal {
				t.Errorf("sortBy = %q, want %q", got, tt.wantSortByVal)
			}
			if !tt.sortDirSet {
				if _, present := params["sortDir"]; present {
					t.Errorf("sortDir must be absent when not explicitly set, got %q", params.Get("sortDir"))
				}
			} else if got := params.Get("sortDir"); got != tt.wantSortDir {
				t.Errorf("sortDir = %q, want %q", got, tt.wantSortDir)
			}
		})
	}
}
