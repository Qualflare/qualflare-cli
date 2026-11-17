package oauth

import (
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenBrowser opens rawURL in the user's default browser. It returns once the
// opener has started; the caller prints the URL anyway, so a failure here only
// means the user opens it by hand.
//
// The opener is not tied to the sign-in's context: it is reaped in the
// background and left to finish even after qf login returns, since killing an
// opener such as xdg-open part-way can leave no browser open.
func OpenBrowser(rawURL string) error {
	name, args, err := browserCommandFor(runtime.GOOS, rawURL)
	if err != nil {
		return err
	}
	// No shell is involved, and browserCommand only passes an http(s) URL.
	cmd := exec.Command(name, args...) //nolint:noctx // the opener must outlive the sign-in (see above)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

// browserCommandFor is browserCommand; a test swaps it for a stand-in opener.
var browserCommandFor = browserCommand

// browserCommand returns the platform's URL opener. Only absolute http(s) URLs
// are accepted, so nothing else reaches the opener as an argument.
func browserCommand(goos, rawURL string) (string, []string, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return "", nil, errors.New("open browser: not an http(s) URL")
	}
	switch goos {
	case "darwin":
		return "open", []string{rawURL}, nil
	case "windows":
		return "rundll32", []string{"url.dll,FileProtocolHandler", rawURL}, nil
	default:
		return "xdg-open", []string{rawURL}, nil
	}
}
