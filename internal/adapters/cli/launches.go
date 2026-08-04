package cli

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

func (c *CLI) createLaunchesCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "launches",
		Short: "List test launches",
		Long:  "List test execution launches in the project.",
		Example: `  # List all launches
  qf <id> launches list

  # Filter by milestone
  qf <id> launches list --milestone 3

  # Filter by environment
  qf <id> launches list --environment prod`,
	}

	var (
		milestoneSeq int
		environment  string
	)
	cmd.AddCommand(c.newListCommand(listSpec{
		short:     "List test launches",
		paginated: true,
		registerFilters: func(lc *cobra.Command) {
			lc.Flags().IntVar(&milestoneSeq, "milestone", 0, "Filter by milestone sequence number")
			lc.Flags().StringVar(&environment, "environment", "", "Filter by environment UID")
		},
		buildRequest: func(_ *cobra.Command, params url.Values) (string, error) {
			if milestoneSeq > 0 {
				params.Set("milestone", strconv.Itoa(milestoneSeq))
			}
			if environment != "" {
				params.Add("environments", environment)
			}
			return apiV1 + "/launches", nil
		},
	}))
	return cmd
}

func (c *CLI) createLaunchCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "launch",
		Short: "Get launch details",
		Long:  "Get details for a specific test launch.",
		Example: `  # Get launch by sequence number
  qf <id> launch get 10`,
	}

	getCmd := &cobra.Command{
		Use:   "get <seq>",
		Short: "Get a launch by sequence number",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf(apiV1+"/launch/%s", pathArg(args[0])), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
