package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createMilestonesCommand() *cobra.Command {
	var (
		page     int
		query    string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "milestones",
		Short: "List milestones",
		Long:  "List milestones/releases in the project.",
		Example: `  # List all milestones
  qf milestones list

  # Search milestones
  qf milestones list --query "v2.0"`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List milestones",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			if query != "" {
				params.Set("q", query)
			}
			return c.fetchAndPrint("/api/v1/milestones", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringVar(&query, "query", "", "Search query")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createMilestoneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestone",
		Short: "Get milestone details",
		Long:  "Get details for a specific milestone.",
		Example: `  # Get milestone by sequence number
  qf milestone get 3`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a milestone by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/milestone/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
