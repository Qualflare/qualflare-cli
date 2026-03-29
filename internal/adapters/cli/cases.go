package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createCasesCommand() *cobra.Command {
	var (
		suiteSeq int
		page     int
		query    string
		states   []string
		priority []string
		sortBy   string
		sortDesc bool
	)

	cmd := &cobra.Command{
		Use:   "cases",
		Short: "List test cases",
		Long:  "List test cases in a suite.",
		Example: `  # List cases in suite 5
  qf cases list --suite 5

  # Filter by state
  qf cases list --suite 5 --state passed,failed`,
	}

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List test cases in a suite",
		RunE: func(cmd *cobra.Command, args []string) error {
			if suiteSeq <= 0 {
				return fmt.Errorf("--suite flag is required")
			}
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc)
			if query != "" {
				params.Set("q", query)
			}
			addSliceParam(params, "state[]", states)
			addSliceParam(params, "priority[]", priority)
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/suite/%d/cases", suiteSeq), params)
		},
	}

	listCmd.Flags().IntVar(&suiteSeq, "suite", 0, "Suite sequence number (required)")
	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().StringVar(&query, "query", "", "Search query")
	listCmd.Flags().StringSliceVar(&states, "state", nil, "Filter by state (passed,failed,skipped,...)")
	listCmd.Flags().StringSliceVar(&priority, "priority", nil, "Filter by priority (low,medium,high,critical)")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
	return cmd
}

func (c *CLI) createCaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "case",
		Short: "Get test case details or steps",
		Long:  "Get details or steps for a specific test case.",
		Example: `  # Get case details
  qf case get 123

  # Get case steps
  qf case steps 123`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a test case by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/case/%s", args[0]), nil)
		},
	}

	stepsCmd := &cobra.Command{
		Use:   "steps <seq>",
		Short: "Get steps for a test case",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf("/api/v1/case/%s/steps", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	cmd.AddCommand(stepsCmd)
	return cmd
}
