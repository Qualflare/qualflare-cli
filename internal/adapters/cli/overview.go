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

Six numbers:

  successRate     passed / (passed + failed), percent
  flakeRate       flaky runs / all runs, percent
  launchP90       launch duration p90, seconds
  automationRate  automated cases, percent
  defectsOpened   defects opened, per day
  launches        launches, per day

For each one, latest is its most recent day with a reading (for
defectsOpened and launches, today's UTC count so far), not a total over the
window. previous is the daily average over the 7 or 28 days before the
window; percentChange and trend compare the two. values holds one reading
per day in buckets, and target is the project's target (null when unset).

warnings carries the server's notes about the data. dataThrough says how
current the numbers are: they lag a finished run by a few minutes.`,
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
