package cli

import (
	"net/url"
	"testing"

	"github.com/spf13/cobra"
)

// API-05: newListCommand must wire the shared page/sort flags for every list command
// and layer the resource-specific filters on top, while the unpaginated variant omits
// --page. This locks the shared scaffolding so a future edit can't silently drop a flag.
func TestNewListCommand_WiresFlags(t *testing.T) {
	c := &CLI{}

	var severity []string
	paginated := c.newListCommand(listSpec{
		short:     "List things",
		paginated: true,
		registerFilters: func(lc *cobra.Command) {
			lc.Flags().StringSliceVar(&severity, "severity", nil, "")
		},
		buildRequest: func(_ *cobra.Command, _ url.Values) (string, error) { return "/things", nil },
	})
	for _, name := range []string{"page", "sort-by", "sort-desc", "severity"} {
		if paginated.Flags().Lookup(name) == nil {
			t.Errorf("paginated list command missing --%s", name)
		}
	}

	unpaginated := c.newListCommand(listSpec{
		short:        "List sub-things",
		paginated:    false,
		buildRequest: func(_ *cobra.Command, _ url.Values) (string, error) { return "/sub", nil },
	})
	if unpaginated.Flags().Lookup("page") != nil {
		t.Error("unpaginated list command must not register --page")
	}
	for _, name := range []string{"sort-by", "sort-desc"} {
		if unpaginated.Flags().Lookup(name) == nil {
			t.Errorf("unpaginated list command missing --%s", name)
		}
	}
}

// TestListCommands_PreserveFilters (API-05 regression) asserts every converted resource
// command still exposes exactly the flags it did before the shared-builder refactor —
// so the extraction didn't silently drop a filter from any of the seven commands.
func TestListCommands_PreserveFilters(t *testing.T) {
	c := &CLI{}
	tests := []struct {
		name string
		cmd  *cobra.Command
		want []string
	}{
		{"clusters", c.createClustersCommand(), []string{"page", "sort-by", "sort-desc", "severity"}},
		{"defects", c.createDefectsCommand(), []string{"page", "sort-by", "sort-desc", "severity", "status"}},
		{"launches", c.createLaunchesCommand(), []string{"page", "sort-by", "sort-desc", "milestone", "environment"}},
		{"milestones", c.createMilestonesCommand(), []string{"page", "sort-by", "sort-desc", "query"}},
		{"suites", c.createSuitesCommand(), []string{"page", "sort-by", "sort-desc", "query"}},
		{"plans", c.createPlansCommand(), []string{"page", "sort-by", "sort-desc", "query"}},
		// cases is the unpaginated, suite-scoped one: no --page (SYNC-08), has --suite.
		{"cases", c.createCasesCommand(), []string{"sort-by", "sort-desc", "suite", "query", "state", "priority"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var list *cobra.Command
			for _, sub := range tt.cmd.Commands() {
				if sub.Name() == "list" {
					list = sub
				}
			}
			if list == nil {
				t.Fatalf("%s has no 'list' subcommand", tt.name)
			}
			for _, f := range tt.want {
				if list.Flags().Lookup(f) == nil {
					t.Errorf("%s list: missing --%s", tt.name, f)
				}
			}
			// cases must NOT have --page (its endpoint is unpaginated).
			if tt.name == "cases" && list.Flags().Lookup("page") != nil {
				t.Error("cases list must not expose --page")
			}
		})
	}
}
