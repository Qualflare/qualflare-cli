package cli

import (
	"bufio"
	"fmt"
	"os"
	"qualflare-cli/internal/auth"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (c *CLI) createLoginCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "login <identifier> <token>",
		Short: "Save credentials for a project under a local identifier",
		Long: `Save an API token under a local identifier (alias) so other commands
can reference it as the first positional argument.

Examples:
  qf login myapp qf_xxx
  qf login prod-app qf_yyy --force
  qf logout myapp
  qf projects`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id, token := args[0], args[1]
			if err := auth.Validate(id); err != nil {
				return err
			}
			if strings.TrimSpace(token) == "" {
				return fmt.Errorf("token cannot be empty")
			}

			if c.store.Has(id) {
				if !force {
					ok, err := confirmOverwrite(id)
					if err != nil {
						return err
					}
					if !ok {
						c.printInfo("Aborted; identifier %q unchanged.", id)
						return nil
					}
				}
			}

			c.store.Set(id, token)
			if err := c.store.Save(); err != nil {
				return fmt.Errorf("save credentials: %w", err)
			}
			c.printSuccess("Saved identifier %q. Use it as the first arg, e.g. `qf %s cases`.", id, id)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing identifier without confirmation")
	return cmd
}

// confirmOverwrite prompts on stdin for [y/N]. Fails closed on non-TTY stdin.
func confirmOverwrite(id string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("identifier %q already exists; cannot prompt because stdin is not a terminal. Use --force to overwrite.", id)
	}
	fmt.Fprintf(os.Stderr, "Identifier %q already exists. Overwrite? [y/N]: ", id)
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err != nil {
		return false, fmt.Errorf("read confirmation: %w", err)
	}
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes", nil
}
