package cli

import (
	"net/url"

	"github.com/spf13/cobra"
)

// listSpec declares a read-only "list" subcommand. newListCommand wires the shared
// page/sort flags and the common request params (pagination + sorting); each resource
// supplies only its endpoint path and its own filter flags/params via the hooks. This
// removes the copy-pasted scaffolding that had drifted across the seven list commands
// (clusters/cases/launches/defects/milestones/suites/plans) — API-05.
type listSpec struct {
	short string
	// paginated is false for the one endpoint that does not paginate (cases-by-suite);
	// that command then gets no --page flag rather than advertising a silent no-op.
	paginated bool
	// registerFilters adds the resource-specific filter flags (severity, query, ...).
	// It closes over the caller's flag variables, which buildRequest reads.
	registerFilters func(cmd *cobra.Command)
	// buildRequest applies the resource-specific filters to params (which already carry
	// pagination + sorting) and returns the endpoint path. It runs inside RunE, so it may
	// read parsed flag values and validate them (e.g. cases requires --suite).
	buildRequest func(cmd *cobra.Command, params url.Values) (string, error)
}

// newListCommand builds a standard "list" subcommand from a listSpec. The shared
// page/sort-by/sort-desc flags and the pagination+sorting params are handled here once;
// the RunE delegates to the spec for resource-specific filters and the endpoint path.
func (c *CLI) newListCommand(spec listSpec) *cobra.Command {
	var (
		page     int
		sortBy   string
		sortDesc bool
	)

	listCmd := &cobra.Command{
		Use:   "list",
		Short: spec.short,
		RunE: func(cmd *cobra.Command, _ []string) error {
			params := url.Values{}
			if spec.paginated {
				addPagination(params, page)
			}
			addSorting(params, sortBy, sortDesc, cmd.Flags().Changed("sort-desc"))
			path, err := spec.buildRequest(cmd, params)
			if err != nil {
				return err
			}
			return c.fetchAndPrint(path, params)
		},
	}

	if spec.paginated {
		listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	}
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")
	if spec.registerFilters != nil {
		spec.registerFilters(listCmd)
	}

	return listCmd
}
