package oauth

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestLoopback_BindsLoopbackWithHeaderTimeout(t *testing.T) {
	lb, err := startLoopback(t.Context(), "state", "https://api.example.test")
	if err != nil {
		t.Fatalf("startLoopback: %v", err)
	}
	defer lb.shutdown()

	host, port, err := net.SplitHostPort(lb.addr.String())
	if err != nil {
		t.Fatal(err)
	}
	if host != "127.0.0.1" {
		t.Errorf("listener host = %q, want 127.0.0.1", host)
	}
	if p, _ := strconv.Atoi(port); p == 0 {
		t.Errorf("listener port = %q, want an OS-assigned port", port)
	}
	if lb.srv.ReadHeaderTimeout <= 0 {
		t.Error("http.Server has no ReadHeaderTimeout")
	}
	if want := "http://127.0.0.1:" + port + "/callback"; lb.redirectURI != want {
		t.Errorf("redirectURI = %q, want %q", lb.redirectURI, want)
	}
	if strings.Contains(lb.redirectURI, "localhost") {
		t.Error("redirect must use the literal loopback IP")
	}
}
