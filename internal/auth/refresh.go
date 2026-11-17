package auth

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// refreshWindow is how close to its expiry an access token is refreshed before
// a request, so it does not lapse on the way to the server.
const refreshWindow = 60 * time.Second

var (
	// ErrInvalidGrant is what a Refresher's error wraps when the authorization
	// server refused the refresh token (OAuth error invalid_grant): it was
	// revoked, expired, or superseded too long ago.
	ErrInvalidGrant = errors.New("invalid_grant")
	// ErrSignInAgain means an OAuth sign-in can no longer be refreshed and the
	// user has to run qf login again.
	ErrSignInAgain = errors.New("sign-in has ended")
)

// saveRefreshed saves the store after a refresh; a test makes it fail.
var saveRefreshed = (*Store).Save

// Refresher exchanges a refresh token for a new token pair. Its error wraps
// ErrInvalidGrant when the server refused the refresh token.
type Refresher func(ctx context.Context, refreshToken string) (Identifier, error)

// signInAgainError is ErrSignInAgain with the alias the user has to sign in to.
type signInAgainError struct {
	id  string
	err error
}

func (e *signInAgainError) Error() string {
	return `Your sign-in for "` + e.id + `" has ended. Run qf login ` + e.id + ` again.`
}

func (e *signInAgainError) Is(target error) bool { return target == ErrSignInAgain }
func (e *signInAgainError) Unwrap() error        { return e.err }

// FreshToken returns the bearer token to send for id, refreshing an OAuth
// access token first when it is about to expire (force false) or the server has
// just rejected it (force true, rejected being the token it rejected). A pasted
// token is returned as it is.
//
// The refresh runs under the store's file lock on the entry as it is on disk:
// the server rotates the refresh token on every use, so another qf process may
// have refreshed already, and then its token is used rather than refreshed
// again. With force, a file token other than rejected means exactly that. The
// new pair is saved before the lock is released. store itself is only read.
//
// A refused refresh token, or an expired entry without one, is ErrSignInAgain.
//
// Two failures do not fail the command; each is reported to warn (nil drops
// it), never with a token in it:
//   - a refresh that fails for any other reason (the network, a 5xx) before the
//     current access token has expired: that token is returned, and a 401 later
//     refreshes it again;
//   - a refreshed pair that cannot be saved: the new access token is returned,
//     since the server has already rotated the refresh token.
func FreshToken(ctx context.Context, store *Store, id string, refresher Refresher, force bool, rejected string, warn func(string)) (string, error) {
	if warn == nil {
		warn = func(string) {}
	}
	entry, ok := store.Entry(id)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrNotFound, id)
	}
	if entry.Kind != KindOAuth {
		return entry.Token, nil
	}
	if !force && !expiresSoon(entry) {
		return entry.AccessToken, nil
	}

	path := store.Path()
	var token string
	err := WithFileLock(path, func() error {
		fresh, err := Load(path)
		if err != nil {
			return err
		}
		e, ok := fresh.Entry(id)
		switch {
		case !ok:
			// Logged out by another process meanwhile.
			return &signInAgainError{id: id, err: ErrNotFound}
		case e.Kind != KindOAuth:
			token = e.Token
			return nil
		case force && rejected != "" && e.AccessToken != rejected:
			token = e.AccessToken
			return nil
		case !force && !expiresSoon(e):
			token = e.AccessToken
			return nil
		case e.RefreshToken == "":
			return &signInAgainError{id: id}
		}

		next, err := refresher(ctx, e.RefreshToken)
		if errors.Is(err, ErrInvalidGrant) {
			return &signInAgainError{id: id, err: err}
		}
		if err != nil && !force && time.Now().Before(e.ExpiresAt) && !errors.Is(err, context.Canceled) {
			warn(fmt.Sprintf("could not refresh the sign-in for %q (%v); using the current token until it expires", id, err))
			token = e.AccessToken
			return nil
		}
		if err != nil {
			return fmt.Errorf("refresh the sign-in for %q: %w", id, err)
		}
		if next.AccessToken == "" {
			return fmt.Errorf("refresh the sign-in for %q: no access token in the response", id)
		}
		if next.RefreshToken == "" {
			// A server that does not rotate keeps the refresh token valid.
			next.RefreshToken = e.RefreshToken
		}
		if next.Issuer == "" {
			next.Issuer = e.Issuer
		}
		fresh.SetOAuth(id, next)
		token = next.AccessToken
		if err := saveRefreshed(fresh); err != nil {
			warn(fmt.Sprintf("refreshed the sign-in for %q but could not save it (%v); "+
				"if a later command says the sign-in has ended, run qf login %s again", id, err, id))
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	return token, nil
}

// expiresSoon reports whether e's access token lapses within refreshWindow. A
// zero expiry (the server sent none) never does; a 401 refreshes it instead.
func expiresSoon(e Identifier) bool {
	return !e.ExpiresAt.IsZero() && time.Until(e.ExpiresAt) < refreshWindow
}
