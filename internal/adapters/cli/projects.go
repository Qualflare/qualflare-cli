package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

func (c *CLI) createProjectsCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "projects",
		Short: "List locally saved project identifiers",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			ids := c.store.List()
			if len(ids) == 0 {
				fmt.Println("No projects configured. Run 'qf login <identifier> <token>' to get started.")
				return nil
			}
			for _, id := range ids {
				fmt.Println(id)
			}
			return nil
		},
	}
}
