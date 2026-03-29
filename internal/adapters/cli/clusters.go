package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createClustersCommand() *cobra.Command {
	var (
		page     int
		severity []string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "List failure clusters",
		Long:  "List failure clusters detected in the project.",
		Example: `  # List all failure clusters
  qf clusters list

  # Filter by severity
  qf clusters list --severity critical`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List failure clusters",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			addSliceParam(params, "severity[]", severity)
			return c.fetchAndPrint("/api/v1/clusters", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringSliceVar(&severity, "severity", nil, "Filter by severity (critical,high,medium,low)")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createClusterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Get cluster details",
		Long:  "Get details for a specific failure cluster.",
		Example: `  # Get cluster by ID
  qf cluster get 15`,
	}

	getCmd := &cobra.Command{
		Use:   "get <id>",
		Short: "Get a failure cluster by ID",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/cluster/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
