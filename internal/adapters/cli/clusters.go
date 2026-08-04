package cli

import (
	"net/url"

	"github.com/spf13/cobra"
)

func (c *CLI) createClustersCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clusters",
		Short: "List failure clusters",
		Long:  "List failure clusters detected in the project.",
		Example: `  # List all failure clusters
  qf <id> clusters list

  # Filter by severity
  qf <id> clusters list --severity critical`,
	}

	var severity []string
	cmd.AddCommand(c.newListCommand(listSpec{
		short:     "List failure clusters",
		paginated: true,
		registerFilters: func(lc *cobra.Command) {
			lc.Flags().StringSliceVar(&severity, "severity", nil, "Filter by severity (critical,high,medium,low)")
		},
		buildRequest: func(_ *cobra.Command, params url.Values) (string, error) {
			addSliceParam(params, "severity[]", severity)
			return apiV1 + "/clusters", nil
		},
	}))
	return cmd
}

func (c *CLI) createClusterCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cluster",
		Short: "Get cluster details",
		Long:  "Get details for a specific failure cluster.",
		Example: `  # Get cluster by ID
  qf <id> cluster get 15`,
	}

	getCmd := c.newDetailCommand("get <id>", "Get a failure cluster by ID", apiV1+"/cluster/%s")

	cmd.AddCommand(getCmd)
	return cmd
}
