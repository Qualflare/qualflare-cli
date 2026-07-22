package cli

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

func (c *CLI) createLaunchesCommand() *cobra.Command {
	var (
		page         int
		milestoneSeq int
		environment  string
		sortBy       string
		sortDesc     bool
	)

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

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List test launches",
		RunE: func(cmd *cobra.Command, args []string) error {
			params := url.Values{}
			addPagination(params, page)
			addSorting(params, sortBy, sortDesc, cmd.Flags().Changed("sort-desc"))
			if milestoneSeq > 0 {
				params.Set("milestone", strconv.Itoa(milestoneSeq))
			}
			if environment != "" {
				params.Add("environments", environment)
			}
			return c.fetchAndPrint(apiV1+"/launches", params)
		},
	}

	listCmd.Flags().IntVar(&page, "page", 0, "Page number")
	listCmd.Flags().IntVar(&milestoneSeq, "milestone", 0, "Filter by milestone sequence number")
	listCmd.Flags().StringVar(&environment, "environment", "", "Filter by environment UID")
	listCmd.Flags().StringVar(&sortBy, "sort-by", "", "Sort by field")
	listCmd.Flags().BoolVar(&sortDesc, "sort-desc", false, "Sort in descending order")

	cmd.AddCommand(listCmd)
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
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.fetchAndPrint(fmt.Sprintf(apiV1+"/launch/%s", pathArg(args[0])), nil)
		},
	}

	cmd.AddCommand(getCmd)
	return cmd
}
