package cli

import (
	"errors"
	"fmt"
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createCasesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cases",
		Short: "List test cases",
		Long:  "List test cases in a suite.",
		Example: `  # List cases in suite 5
  qf <id> cases list --suite 5

  # Filter by state
  qf <id> cases list --suite 5 --state active,review`,
	}

	var (
		suiteSeq         int
		query            string
		states, priority []string
	)
	// Unpaginated: the suite-cases endpoint returns the whole suite, so no --page flag
	// (SYNC-08). --suite is required and the path is suite-scoped.
	cmd.AddCommand(c.newListCommand(listSpec{
		short:     "List test cases in a suite",
		paginated: false,
		registerFilters: func(lc *cobra.Command) {
			lc.Flags().IntVar(&suiteSeq, "suite", 0, "Suite sequence number (required)")
			lc.Flags().StringVar(&query, "query", "", "Search query")
			lc.Flags().StringSliceVar(&states, "state", nil, "Filter by state (active,review,outdated,draft)")
			lc.Flags().StringSliceVar(&priority, "priority", nil, "Filter by priority (low,medium,high,critical)")
		},
		buildRequest: func(_ *cobra.Command, params url.Values) (string, error) {
			if suiteSeq <= 0 {
				return "", errors.New("--suite flag is required")
			}
			if query != "" {
				params.Set("q", query)
			}
			addSliceParam(params, "state", states)
			addSliceParam(params, "priority", priority)
			return fmt.Sprintf(apiV1+"/suite/%d/cases", suiteSeq), nil
		},
	}))
	return cmd
}

func (c *CLI) createCaseCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "case",
		Short: "Get test case details or steps",
		Long:  "Get details or steps for a specific test case.",
		Example: `  # Get case details
  qf <id> case get 123

  # Get case steps
  qf <id> case steps 123`,
	}

	getCmd := c.newDetailCommand("get <seq>", "Get a test case by sequence number", apiV1+"/case/%s")

	stepsCmd := c.newDetailCommand("steps <seq>", "Get steps for a test case", apiV1+"/case/%s/steps")

	cmd.AddCommand(getCmd)
	cmd.AddCommand(stepsCmd)
	return cmd
}
