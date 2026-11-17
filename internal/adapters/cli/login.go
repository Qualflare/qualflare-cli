package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/oauth"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// Seams the tests replace: whether stdin is a terminal, the hidden token prompt,
// and the OAuth calls.
var (
	stdinIsTerminal = func() bool { return term.IsTerminal(int(os.Stdin.Fd())) }
	readHiddenToken = func() (string, error) {
		fmt.Fprint(os.Stderr, "API token: ")
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return "", fmt.Errorf("read token: %w", err)
		}
		return strings.TrimSpace(string(b)), nil
	}
	discoverOAuth = oauth.Discover
	runOAuthLogin = oauth.Login
)

type loginFlags struct {
	force     bool
	noBrowser bool
	withToken bool
}

func (c *CLI) createLoginCommand() *cobra.Command {
	var flags loginFlags

	cmd := &cobra.Command{
		Use:   "login <identifier> [token]",
		Short: "Sign in and save credentials for a project under a local identifier",
		Long: `Sign in and save the credentials under a local identifier (alias) so other
commands can reference it as the first positional argument.

On a terminal with no token supplied, qf signs you in through the browser.
--no-browser prints a URL to open on any device and asks for the code it shows.

A token may be supplied instead (in order of precedence): as the second argument,
via the QF_TOKEN environment variable, by a hidden prompt with --with-token, or
piped on stdin. Passing it as an argument exposes it to the process list and
shell history, so prefer the prompt or a piped/env value in CI.

Examples:
  qf login myapp                 # sign in through the browser
  qf login myapp --no-browser    # sign in on another device, paste the code
  qf login myapp --with-token    # paste a token at a hidden prompt
  QF_TOKEN=qf_xxx qf login myapp # from the environment
  echo qf_xxx | qf login myapp   # piped stdin
  qf login myapp qf_xxx --force  # explicit arg (discouraged)`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			return c.runLogin(cmd, args, flags)
		},
	}

	cmd.Flags().BoolVar(&flags.force, "force", false, "Overwrite an existing identifier without confirmation")
	cmd.Flags().BoolVar(&flags.noBrowser, "no-browser", false, "Sign in without opening a browser here: open the printed URL anywhere and paste the code")
	cmd.Flags().BoolVar(&flags.withToken, "with-token", false, "Paste an API token at a hidden prompt instead of signing in through the browser")
	return cmd
}

// runLogin validates the identifier and saves credentials for it. With no token
// supplied on a terminal it signs in through the browser; otherwise it resolves a
// pasted token. An existing identifier is only replaced with --force or an
// interactive confirmation, so a re-login can't silently discard a working token.
func (c *CLI) runLogin(cmd *cobra.Command, args []string, flags loginFlags) error {
	id := args[0]
	if err := auth.Validate(id); err != nil {
		return err
	}
	if useBrowserSignIn(args, flags) {
		return c.loginWithOAuth(cmd, id, flags)
	}

	token, err := resolveLoginToken(args)
	if err != nil {
		return err
	}
	if strings.TrimSpace(token) == "" {
		return errors.New("token cannot be empty")
	}
	if ok, err := c.confirmReplace(id, flags.force); err != nil || !ok {
		return err
	}
	return c.savePastedToken(id, token)
}

// useBrowserSignIn reports whether no token source was given and a person is at
// the terminal: no token argument, no QF_TOKEN, no --with-token, stdin a TTY.
func useBrowserSignIn(args []string, flags loginFlags) bool {
	return len(args) < 2 && os.Getenv("QF_TOKEN") == "" && !flags.withToken && stdinIsTerminal()
}

// loginWithOAuth confirms an overwrite first, then signs in through the
// authorization server the API advertises. A server without one falls back to
// the hidden token prompt.
func (c *CLI) loginWithOAuth(cmd *cobra.Command, id string, flags loginFlags) error {
	if ok, err := c.confirmReplace(id, flags.force); err != nil || !ok {
		return err
	}
	out := cmd.ErrOrStderr()
	ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt)
	defer stop()

	md, err := discoverOAuth(ctx, c.config.GetAPIEndpoint())
	if errors.Is(err, oauth.ErrNoAuthServer) {
		stop() // let Ctrl-C end the prompt as it does elsewhere
		_, _ = fmt.Fprintln(out, "This server has no browser sign-in; paste a token instead.")
		token, err := readHiddenToken()
		if err != nil {
			return err
		}
		if strings.TrimSpace(token) == "" {
			return errors.New("token cannot be empty")
		}
		return c.savePastedToken(id, strings.TrimSpace(token))
	}
	if err != nil {
		return fmt.Errorf("sign in: %w (qf login %s --with-token pastes a token instead)", err, id)
	}

	cred, err := runOAuthLogin(ctx, md, oauth.LoginOptions{
		NoBrowser:   flags.noBrowser,
		OpenBrowser: func(u string) error { return oauth.OpenBrowser(ctx, u) },
		Prompt:      linePrompt(out),
		Out:         out,
		Timeout:     oauth.DefaultTimeout,
	})
	if err != nil {
		return err
	}
	entry := auth.Identifier{
		Kind:         auth.KindOAuth,
		AccessToken:  cred.AccessToken,
		RefreshToken: cred.RefreshToken,
		ExpiresAt:    cred.ExpiresAt,
		Issuer:       md.Issuer,
	}
	if err := c.saveLocked(func(s *auth.Store) { s.SetOAuth(id, entry) }); err != nil {
		return err
	}
	if !c.config.IsQuiet() {
		_, _ = fmt.Fprintf(out, "Signed in. Saved identifier %q. Use it as the first arg, e.g. qf %s cases.\n", id, id)
	}
	return nil
}

// confirmReplace returns true when id may be written: it is new, --force was
// given, or the user confirmed. A declined confirmation prints why and returns
// false with no error.
func (c *CLI) confirmReplace(id string, force bool) (bool, error) {
	if !c.store.Has(id) || force {
		return true, nil
	}
	ok, err := confirmOverwrite(id)
	if err != nil {
		return false, err
	}
	if !ok {
		c.printInfo("Aborted; identifier %q unchanged.", id)
	}
	return ok, nil
}

func (c *CLI) savePastedToken(id, token string) error {
	if err := c.saveLocked(func(s *auth.Store) { s.Set(id, token) }); err != nil {
		return err
	}
	c.printSuccess("Saved identifier %q. Use it as the first arg, e.g. `qf %s cases`.", id, id)
	return nil
}

// saveLocked applies change to the store as it is on disk, under the store's
// file lock, so a concurrent qf process writing another entry is not undone.
func (c *CLI) saveLocked(change func(*auth.Store)) error {
	path := c.store.Path()
	err := auth.WithFileLock(path, func() error {
		fresh, err := auth.Load(path)
		if err != nil {
			return err
		}
		change(fresh)
		if err := fresh.Save(); err != nil {
			return err
		}
		c.store = fresh
		return nil
	})
	if err != nil {
		return fmt.Errorf("save credentials: %w", err)
	}
	return nil
}

// linePrompt writes label to w and reads one visible line from stdin. The
// copy-the-code flow uses it: the code is single-use and bound to this
// process's PKCE verifier, so echoing it is harmless.
func linePrompt(w io.Writer) func(string) (string, error) {
	return func(label string) (string, error) {
		_, _ = fmt.Fprint(w, label)
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && (!errors.Is(err, io.EOF) || line == "") {
			return "", err
		}
		return line, nil
	}
}

// resolveLoginToken resolves the API token WITHOUT requiring it on the command
// line (where it leaks to the process list and shell history) — SEC-01. Order:
// explicit argv (back-compat) → QF_TOKEN → hidden interactive prompt on a TTY →
// piped stdin.
func resolveLoginToken(args []string) (string, error) {
	// cobra.RangeArgs(1, 2) already bounds this, and the length check makes the index
	// safe regardless; gosec's analysis cannot see through either.
	if len(args) == 2 {
		return args[1], nil //nolint:gosec // G602: guarded by the length check above
	}
	if t := os.Getenv("QF_TOKEN"); t != "" {
		return t, nil
	}
	if stdinIsTerminal() {
		return readHiddenToken()
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("read token from stdin: %w", err)
	}
	return strings.TrimSpace(string(b)), nil
}

// confirmOverwrite prompts on stdin for [y/N]. Fails closed on non-TTY stdin.
func confirmOverwrite(id string) (bool, error) {
	if !stdinIsTerminal() {
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
