package cli

import (
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createSuitesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suites",
		Short: "List test suites",
		Long:  "List test suites in the project.",
		Example: `  # List all suites
  qf <id> suites list

  # List suites with search
  qf <id> suites list --query "login"

  # List suites sorted by name
  qf <id> suites list --sort-by name`,
	}

	var query string
	cmd.AddCommand(c.newListCommand(listSpec{
		short:     "List test suites",
		paginated: true,
		registerFilters: func(lc *cobra.Command) {
			lc.Flags().StringVar(&query, "query", "", "Search query")
		},
		buildRequest: func(_ *cobra.Command, params url.Values) (string, error) {
			if query != "" {
				params.Set("q", query)
			}
			return apiV1 + "/suites", nil
		},
	}))
	return cmd
}

func (c *CLI) createSuiteCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suite",
		Short: "Get test suite details",
		Long:  "Get details for a specific test suite.",
		Example: `  # Get suite by sequence number
  qf <id> suite get 42`,
	}

	getCmd := c.newDetailCommand("get <seq>", "Get a test suite by sequence number", apiV1+"/suite/%s")

	cmd.AddCommand(getCmd)
	return cmd
}
