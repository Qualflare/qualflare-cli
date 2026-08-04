package cli

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"qualflare-cli/internal/auth"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

func (c *CLI) createLoginCommand() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "login <identifier> [token]",
		Short: "Save credentials for a project under a local identifier",
		Long: `Save an API token under a local identifier (alias) so other commands
can reference it as the first positional argument.

The token may be supplied (in order of precedence): as the second argument, via
the QF_TOKEN environment variable, by an interactive hidden prompt on a terminal,
or piped on stdin. Passing it as an argument exposes it to the process list and
shell history, so prefer the prompt or a piped/env value in CI.

Examples:
  qf login myapp                 # prompt (hidden) or read QF_TOKEN
  QF_TOKEN=qf_xxx qf login myapp # from the environment
  echo qf_xxx | qf login myapp   # piped stdin
  qf login myapp qf_xxx --force  # explicit arg (discouraged)`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(_ *cobra.Command, args []string) error {
			return c.runLogin(args, force)
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Overwrite an existing identifier without confirmation")
	return cmd
}

// runLogin validates the identifier, resolves the token, and persists the pair. An
// existing identifier is only replaced with --force or an interactive confirmation, so a
// re-login can't silently discard a working token.
func (c *CLI) runLogin(args []string, force bool) error {
	id := args[0]
	if err := auth.Validate(id); err != nil {
		return err
	}
	token, err := resolveLoginToken(args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return fmt.Errorf("token cannot be empty")
	}

	if c.store.Has(id) && !force {
		ok, err := confirmOverwrite(id)
		if err != nil {
			return err
		}
		if !ok {
			c.printInfo("Aborted; identifier %q unchanged.", id)
			return nil
		}
	}

	c.store.Set(id, token)
	if err := c.store.Save(); err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	c.printSuccess("Saved identifier %q. Use it as the first arg, e.g. `qf %s cases`.", id, id)
	return nil
}

// resolveLoginToken resolves the API token WITHOUT requiring it on the command
// line (where it leaks to the process list and shell history) — SEC-01. Order:
// explicit argv (back-compat) → QF_TOKEN → hidden interactive prompt on a TTY →
// piped stdin.
func resolveLoginToken(args []string) (string, error) {
	if len(args) == 2 {
		return args[1], nil
	}
	if t := os.Getenv("QF_TOKEN"); t != "" {
		return t, nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprint(os.Stderr, "API token: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read token from stdin: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// confirmOverwrite prompts on stdin for [y/N]. Fails closed on non-TTY stdin.
func confirmOverwrite(id string) (bool, error) {
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return false, fmt.Errorf("identifier %q already exists; cannot prompt because stdin is not a terminal — use --force to overwrite", id)
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
