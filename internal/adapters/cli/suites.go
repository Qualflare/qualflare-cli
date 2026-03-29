package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createSuitesCommand() *cobra.Command {
	var (
		page     int
		sortBy   string
		sortDesc bool
		query    string
	)

	cmd := &cobra.Command{
		Use:   "suites",
		Short: "List test suites",
		Long:  "List test suites in the project.",
		Example: `  # List all suites
  qf suites list

  # List suites with search
  qf suites list --query "login"

  # List suites sorted by name
  qf suites list --sort-by name`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List test suites",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			if query != "" {
				params.Set("q", query)
			}
			return c.fetchAndPrint("/api/v1/suites", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")
	listCmd.Flags().StringVar(&query, "query", "", "Search query")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createSuiteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suite",
		Short: "Get test suite details",
		Long:  "Get details for a specific test suite.",
		Example: `  # Get suite by sequence number
  qf suite get 42`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a test suite by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/suite/%s", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
