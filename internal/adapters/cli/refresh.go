package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"qualflare-cli/internal/auth"
	"qualflare-cli/internal/oauth"
)

// Seams the tests replace: the token refresh and the revocation. Discovery is
// discoverOAuth (login.go).
var (
	refreshOAuth = oauth.Refresh
	revokeOAuth  = oauth.Revoke
)

// logoutRevokeTimeout bounds the best-effort revocation on logout.
const logoutRevokeTimeout = 10 * time.Second

// oauthRefresher refreshes against the authorization server that issued the
// sign-in: the entry's issuer, not the API endpoint configured now.
func oauthRefresher(issuer string) auth.Refresher {
	return func(ctx context.Context, refreshToken string) (auth.Identifier, error) {
		md, err := discoverOAuth(ctx, issuer)
		if err != nil {
			return auth.Identifier{}, err
		}
		cred, err := refreshOAuth(ctx, md, refreshToken)
		if errors.Is(err, oauth.ErrInvalidGrant) {
			return auth.Identifier{}, fmt.Errorf("%w: %w", auth.ErrInvalidGrant, err)
		}
		if err != nil {
			return auth.Identifier{}, err
		}
		return auth.Identifier{
			AccessToken:  cred.AccessToken,
			RefreshToken: cred.RefreshToken,
			ExpiresAt:    cred.ExpiresAt,
			Issuer:       md.Issuer,
		}, nil
	}
}

// tokenFor returns the token to send for id: a pasted token as it is, an OAuth
// access token refreshed first when it is about to expire (force false) or
// after the server rejected it (force true, rejected being that token).
func (c *CLI) tokenFor(ctx context.Context, id string, force bool, rejected string) (string, error) {
	entry, ok := c.store.Entry(id)
	if !ok || entry.Kind != auth.KindOAuth {
		token, _ := c.store.Get(id)
		return token, nil
	}
	return auth.FreshToken(ctx, c.store, id, oauthRefresher(entry.Issuer), force, rejected, c.warn)
}

// warn writes a warning line to stderr.
func (c *CLI) warn(msg string) {
	out := c.errOut
	if out == nil {
		out = os.Stderr
	}
	_, _ = fmt.Fprintln(out, "warning: "+msg)
}

// RetryUnauthorized is the HTTP client's 401 hook. For an OAuth alias it
// refreshes the token the server rejected (unless another process already
// has) and returns the new one; for a pasted token, or with no alias
// selected, it returns "" so the 401 stands.
func (c *CLI) RetryUnauthorized(ctx context.Context, rejected string) (string, error) {
	id := c.activeID
	if id == "" {
		return "", nil
	}
	if entry, ok := c.store.Entry(id); !ok || entry.Kind != auth.KindOAuth {
		return "", nil
	}
	return c.tokenFor(ctx, id, true, rejected)
}

// revokeSignIn asks the issuer of an OAuth entry to revoke its refresh token.
// Best effort: a failure is reported to out as a warning and the caller goes on
// to delete the alias.
func revokeSignIn(ctx context.Context, out io.Writer, id string, entry auth.Identifier) {
	if entry.Kind != auth.KindOAuth || entry.RefreshToken == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, logoutRevokeTimeout)
	defer cancel()
	md, err := discoverOAuth(ctx, entry.Issuer)
	if err == nil {
		err = revokeOAuth(ctx, md, entry.RefreshToken)
	}
	if err != nil {
		_, _ = fmt.Fprintf(out, "warning: could not revoke the sign-in for %q on the server (%v); removing it here anyway.\n", id, err)
	}
}
