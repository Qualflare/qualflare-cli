package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createDefectsCommand() *cobra.Command {
	var (
		page     int
		severity []string
		status   []string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "defects",
		Short: "List defects",
		Long:  "List defects detected in the project.",
		Example: `  # List all defects
  qf defects list

  # Filter by severity
  qf defects list --severity critical,high

  # Filter by status
  qf defects list --status active`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List defects",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			addSliceParam(params, "severity[]", severity)
			addSliceParam(params, "status[]", status)
			return c.fetchAndPrint("/api/v1/defects", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringSliceVar(&severity, "severity", nil, "Filter by severity (critical,high,medium,low)")
	listCmd.Flags().StringSliceVar(&status, "status", nil, "Filter by status (active,closed,...)")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createDefectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "defect",
		Short: "Get defect details",
		Long:  "Get details for a specific defect.",
		Example: `  # Get defect by sequence number
  qf defect get 7`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a defect by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/defect/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
