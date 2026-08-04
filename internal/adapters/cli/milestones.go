package cli

import (
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createMilestonesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestones",
		Short: "List milestones",
		Long:  "List milestones/releases in the project.",
		Example: `  # List all milestones
  qf <id> milestones list

  # Search milestones
  qf <id> milestones list --query "v2.0"`,
	}

	var query string
	cmd.AddCommand(c.newListCommand(listSpec{
		short:     "List milestones",
		paginated: true,
		registerFilters: func(lc *cobra.Command) {
			lc.Flags().StringVar(&query, "query", "", "Search query")
		},
		buildRequest: func(_ *cobra.Command, params url.Values) (string, error) {
			if query != "" {
				params.Set("q", query)
			}
			return apiV1 + "/milestones", nil
		},
	}))
	return cmd
}

func (c *CLI) createMilestoneCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "milestone",
		Short: "Get milestone details",
		Long:  "Get details for a specific milestone.",
		Example: `  # Get milestone by sequence number
  qf <id> milestone get 3`,
	}

	getCmd := c.newDetailCommand("get <seq>", "Get a milestone by sequence number", apiV1+"/milestone/%s")

	cmd.AddCommand(getCmd)
	return cmd
}
