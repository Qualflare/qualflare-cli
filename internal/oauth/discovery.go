// Package oauth signs the CLI in through Qualflare's OAuth 2.1 authorization
// server: metadata discovery (RFC 8414), the authorization code flow with PKCE
// S256, and a loopback redirect (RFC 8252) or a copy-the-code fallback.
package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// ErrNoAuthServer means the API has no authorization server: the metadata
// document is absent or does not describe the authorization code flow. The
// caller falls back to a pasted token.
var ErrNoAuthServer = errors.New("no OAuth authorization server")

const (
	wellKnownPath   = "/.well-known/oauth-authorization-server"
	discoverTimeout = 15 * time.Second
	maxMetadataSize = 1 << 20
)

// Metadata is the part of the authorization server metadata the CLI uses.
type Metadata struct {
	Issuer                string
	AuthorizationEndpoint string
	TokenEndpoint         string
	RevocationEndpoint    string
}

type metadataDoc struct {
	Issuer                string `json:"issuer"`
	AuthorizationEndpoint string `json:"authorization_endpoint"`
	TokenEndpoint         string `json:"token_endpoint"`
	RevocationEndpoint    string `json:"revocation_endpoint"`
}

// noRedirectClient refuses redirects, so a metadata or token request is never
// steered to another host.
func noRedirectClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

// Discover fetches the authorization server metadata for apiEndpoint. The
// issuer must be apiEndpoint's origin, so tokens are never requested from a
// host other than the API the user configured.
func Discover(ctx context.Context, apiEndpoint string) (Metadata, error) {
	origin, err := originOf(apiEndpoint)
	if err != nil {
		return Metadata{}, fmt.Errorf("API endpoint %q: %w", apiEndpoint, err)
	}
	ctx, cancel := context.WithTimeout(ctx, discoverTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, origin+wellKnownPath, nil)
	if err != nil {
		return Metadata{}, fmt.Errorf("build metadata request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := noRedirectClient(discoverTimeout).Do(req)
	if err != nil {
		return Metadata{}, fmt.Errorf("fetch sign-in metadata: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	switch {
	case resp.StatusCode == http.StatusNotFound:
		return Metadata{}, ErrNoAuthServer
	case resp.StatusCode != http.StatusOK:
		return Metadata{}, fmt.Errorf("fetch sign-in metadata: unexpected status %d from %s", resp.StatusCode, origin+wellKnownPath)
	}

	var doc metadataDoc
	if err := json.NewDecoder(io.LimitReader(resp.Body, maxMetadataSize)).Decode(&doc); err != nil {
		return Metadata{}, fmt.Errorf("parse sign-in metadata: %w", err)
	}
	if doc.AuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return Metadata{}, ErrNoAuthServer
	}
	if doc.Issuer != origin {
		return Metadata{}, fmt.Errorf("sign-in metadata names issuer %q, want %q", doc.Issuer, origin)
	}
	for name, raw := range map[string]string{
		"authorization_endpoint": doc.AuthorizationEndpoint,
		"token_endpoint":         doc.TokenEndpoint,
	} {
		if err := checkEndpoint(raw); err != nil {
			return Metadata{}, fmt.Errorf("sign-in metadata %s %q: %w", name, raw, err)
		}
	}
	if doc.RevocationEndpoint != "" {
		if err := checkEndpoint(doc.RevocationEndpoint); err != nil {
			return Metadata{}, fmt.Errorf("sign-in metadata revocation_endpoint %q: %w", doc.RevocationEndpoint, err)
		}
	}
	return Metadata(doc), nil
}

// originOf returns scheme://host[:port] of an absolute http(s) URL, with the
// host lower-cased as servers print it.
func originOf(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", errors.New("want an absolute http(s) URL")
	}
	return u.Scheme + "://" + strings.ToLower(u.Host), nil
}

// SameOrigin reports whether a and b are absolute http(s) URLs on the same
// origin: scheme, host (any case) and port, a scheme's default port written or
// not. Paths and trailing slashes are ignored.
func SameOrigin(a, b string) bool {
	oa, okA := canonicalOrigin(a)
	ob, okB := canonicalOrigin(b)
	return okA && okB && oa == ob
}

func canonicalOrigin(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" {
		return "", false
	}
	port := u.Port()
	if port == "" {
		port = map[string]string{"https": "443", "http": "80"}[u.Scheme]
	}
	return u.Scheme + "://" + strings.ToLower(u.Hostname()) + ":" + port, true
}

// checkEndpoint accepts an absolute https URL, or http on a loopback host (a
// local server, as QF_API_URL allows).
func checkEndpoint(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return err
	}
	switch {
	case u.Host == "":
		return errors.New("not an absolute URL")
	case u.Scheme == "https":
		return nil
	case u.Scheme == "http" && isLoopbackHost(u.Hostname()):
		return nil
	default:
		return errors.New("must be https")
	}
}

func isLoopbackHost(host string) bool {
	switch strings.ToLower(host) {
	case "127.0.0.1", "::1", "localhost":
		return true
	}
	return false
}
