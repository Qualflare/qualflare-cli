package cli

import (
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createPlansCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plans",
		Short: "List test plans",
		Long:  "List test plans in the project.",
		Example: `  # List all test plans
  qf <id> plans list

  # Search test plans
  qf <id> plans list --query "regression"`,
	}

	var query string
	cmd.AddCommand(c.newListCommand(listSpec{
		short:     "List test plans",
		paginated: true,
		registerFilters: func(lc *cobra.Command) {
			lc.Flags().StringVar(&query, "query", "", "Search query")
		},
		buildRequest: func(_ *cobra.Command, params url.Values) (string, error) {
			if query != "" {
				params.Set("q", query)
			}
			return apiV1 + "/test-plans", nil
		},
	}))
	return cmd
}

func (c *CLI) createPlanCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan",
		Short: "Get test plan details or cases",
		Long:  "Get details or cases for a specific test plan.",
		Example: `  # Get plan details
  qf <id> plan get 5

  # Get plan cases
  qf <id> plan cases 5`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a test plan by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf(apiV1+"/test-plan/%s", args[0]), nil)
		},
	}

	casesCmd := &cobra.Command{
		Use:   "cases <seq>",
		Short: "Get cases in a test plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf(apiV1+"/test-plan/%s/cases", args[0]), nil)
		},
	}

	cmd.AddCommand(getCmd)
	cmd.AddCommand(casesCmd)
	return cmd
}
