package oauth

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
)

// OpenBrowser opens rawURL in the user's default browser. It returns once the
// opener has started; the caller prints the URL anyway, so a failure here only
// means the user opens it by hand.
func OpenBrowser(ctx context.Context, rawURL string) error {
	name, args, err := browserCommand(runtime.GOOS, rawURL)
	if err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, name, args...)
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open browser: %w", err)
	}
	go func() { _ = cmd.Wait() }()
	return nil
}

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
