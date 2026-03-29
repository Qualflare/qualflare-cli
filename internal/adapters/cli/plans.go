package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createPlansCommand() *cobra.Command {
	var (
		page     int
		query    string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "plans",
		Short: "List test plans",
		Long:  "List test plans in the project.",
		Example: `  # List all test plans
  qf plans list

  # Search test plans
  qf plans list --query "regression"`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List test plans",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			if query != "" {
				params.Set("q", query)
			}
			return c.fetchAndPrint("/api/v1/test-plans", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringVar(&query, "query", "", "Search query")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createPlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Get test plan details or cases",
		Long:  "Get details or cases for a specific test plan.",
		Example: `  # Get plan details
  qf plan get 5

  # Get plan cases
  qf plan cases 5`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a test plan by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/test-plan/%s", args[0]), nil)
		},
	}

	casesCmd := &cobra.Command{
		Use:   "cases <seq>",
		Short: "Get cases in a test plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/test-plan/%s/cases", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	cmd.AddCommand(casesCmd)
	return cmd
}
