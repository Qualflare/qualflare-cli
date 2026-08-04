package cli

import (
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createDefectsCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "defects",
		Short: "List defects",
		Long:  "List defects detected in the project.",
		Example: `  # List all defects
  qf <id> defects list

  # Filter by severity
  qf <id> defects list --severity critical,high

  # Filter by status
  qf <id> defects list --status active`,
	}

	var severity, status []string
	cmd.AddCommand(c.newListCommand(listSpec{
		short:     "List defects",
		paginated: true,
		registerFilters: func(lc *cobra.Command) {
			lc.Flags().StringSliceVar(&severity, "severity", nil, "Filter by severity (critical,high,medium,low)")
			lc.Flags().StringSliceVar(&status, "status", nil, "Filter by status (active,closed,...)")
		},
		buildRequest: func(_ *cobra.Command, params url.Values) (string, error) {
			addSliceParam(params, "severity[]", severity)
			addSliceParam(params, "status[]", status)
			return apiV1 + "/defects", nil
		},
	}))
	return cmd
}

func (c *CLI) createDefectCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "defect",
		Short: "Get defect details",
		Long:  "Get details for a specific defect.",
		Example: `  # Get defect by sequence number
  qf <id> defect get 7`,
	}

	getCmd := c.newDetailCommand("get <seq>", "Get a defect by sequence number", apiV1+"/defect/%s")

	cmd.AddCommand(getCmd)
	return cmd
}
