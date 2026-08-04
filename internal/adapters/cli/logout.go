package cli

import (
	"errors"
	"fmt"
	"qualflare-cli/internal/auth"

	"github.com/spf13/cobra"
)

func (c *CLI) createLogoutCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "logout <identifier>",
		Short: "Remove saved credentials for an identifier",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := args[0]
			if err := c.store.Delete(id); err != nil {
				if errors.Is(err, auth.ErrNotFound) {
					return fmt.Errorf("identifier %q not found (run `qf projects` to list)", id)
				}
				return err
			}
			if err := c.store.Save(); err != nil {
				return fmt.Errorf("save credentials: %w", err)
			}
			c.printSuccess("Removed identifier %q.", id)
			return nil
		},
	}
}
