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
		Long: `Remove the credentials saved under an identifier. For a browser sign-in, qf
first asks the server to revoke it; if that fails it warns and removes the
local credentials anyway.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]
			// Under the lock, so the refresh token revoked is the one on disk
			// now, not one another qf process has since rotated.
			err := c.updateLocked(func(s *auth.Store) error {
				entry, ok := s.Entry(id)
				if !ok {
					return auth.ErrNotFound
				}
				revokeSignIn(cmd.Context(), cmd.ErrOrStderr(), id, entry)
				return s.Delete(id)
			})
			if errors.Is(err, auth.ErrNotFound) {
				return fmt.Errorf("identifier %q not found (run `qf projects` to list)", id)
			}
			if err != nil {
				return err
			}
			c.printSuccess("Removed identifier %q.", id)
			return nil
		},
	}
}
