package oauth

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	callbackPath      = "/callback"
	readHeaderTimeout = 10 * time.Second
	shutdownTimeout   = 2 * time.Second
	// maxErrorText caps what of a server-sent error parameter reaches the terminal.
	maxErrorText = 200
)

type callbackResult struct {
	code string
	err  error
}

// loopback is the RFC 8252 redirect listener: one http.Server on 127.0.0.1 with
// an OS-assigned port. Only /callback answers; the first callback ends the wait.
type loopback struct {
	srv         *http.Server
	addr        net.Addr
	redirectURI string
	results     chan callbackResult
}

func startLoopback(ctx context.Context, state, issuer string) (*loopback, error) {
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen for the sign-in callback: %w", err)
	}
	lb := &loopback{
		addr:        ln.Addr(),
		redirectURI: "http://" + ln.Addr().String() + callbackPath,
		results:     make(chan callbackResult, 1),
	}
	mux := http.NewServeMux()
	mux.HandleFunc(callbackPath, func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		code, err := checkCallback(r.URL.Query(), state, issuer)
		writeTab(w, err)
		select {
		case lb.results <- callbackResult{code: code, err: err}:
		default: // a result is already waiting; later callbacks change nothing
		}
	})
	lb.srv = &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: readHeaderTimeout,
	}
	go func() { _ = lb.srv.Serve(ln) }()
	return lb, nil
}

// shutdown stops the listener, letting an in-flight tab response finish.
func (l *loopback) shutdown() {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := l.srv.Shutdown(ctx); err != nil {
		_ = l.srv.Close()
	}
}

// checkCallback validates the redirect's parameters and returns the code. The
// state must match and iss must equal the issuer (RFC 9207; the server always
// sends it), so a callback from another sign-in or another server is refused.
func checkCallback(q url.Values, state, issuer string) (string, error) {
	if q.Get("state") != state {
		return "", errors.New("sign-in refused: the callback's state does not match this sign-in")
	}
	if q.Get("iss") != issuer {
		return "", fmt.Errorf("sign-in refused: the callback's issuer %q is not %q", clip(q.Get("iss")), issuer)
	}
	if e := q.Get("error"); e != "" {
		if e == "access_denied" {
			return "", ErrCancelled
		}
		if d := q.Get("error_description"); d != "" {
			return "", fmt.Errorf("sign-in failed: %q: %q", clip(e), clip(d))
		}
		return "", fmt.Errorf("sign-in failed: %q", clip(e))
	}
	code := q.Get("code")
	if code == "" {
		return "", errors.New("sign-in failed: the callback carried no code")
	}
	return code, nil
}

func writeTab(w http.ResponseWriter, err error) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	switch {
	case err == nil:
		_, _ = io.WriteString(w, "Qualflare sign-in received. You can close this tab and return to the terminal.\n")
	case errors.Is(err, ErrCancelled):
		_, _ = io.WriteString(w, "Sign-in cancelled. Return to the terminal.\n")
	default:
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, "Sign-in failed. Return to the terminal for details.\n")
	}
}

// clip bounds server- or attacker-supplied text before it is printed.
func clip(s string) string {
	r := []rune(s)
	if len(r) > maxErrorText {
		return string(r[:maxErrorText]) + "..."
	}
	return s
}
