package cli

import (
	"fmt"
	"net/url"
	"strconv"

	"github.com/spf13/cobra"
)

// createOverviewCommand reads the project overview: the six numbers the
// dashboard's Overview shows, over the website's 7- or 28-day windows.
func (c *CLI) createOverviewCommand() *cobra.Command {
	var days int
	cmd := &cobra.Command{
		Use:   "overview",
		Short: "Show the project overview: the dashboard's six numbers",
		Long: `Show the project overview, as the dashboard's Overview shows it.

Six numbers, each with the previous period's value, the change, the trend,
the daily values and the project's target:

  successRate     passed / (passed + failed), percent
  flakeRate       flaky runs / all runs, percent
  launchP90       launch duration p90, seconds
  automationRate  automated cases, percent
  defectsOpened   defects opened today (UTC)
  launches        launches today (UTC)

dataThrough says how current the numbers are: they lag a finished run by a
few minutes.`,
		Example: `  # The last 7 days
  qf <id> overview

  # The last 28 days
  qf <id> overview --days 28`,
		Args: cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			if days != 7 && days != 28 {
				return fmt.Errorf("--days must be 7 or 28, got %d", days)
			}
			params := url.Values{}
			params.Set("days", strconv.Itoa(days))
			return c.fetchAndPrint(apiV1+"/overview", params)
		},
	}
	cmd.Flags().IntVar(&days, "days", 7, "Window in days: 7 or 28")
	return cmd
}
