package http

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestUploadVideoRoundTrip exercises the full presigned-URL flow against two
// httptest servers: one standing in for the Qualflare API (the presign POST)
// and one standing in for R2 (the PUT target the presign response points at).
// Built in dependency order — the PUT-target server first, so its URL is
// known before the presign server's handler is written.
func TestUploadVideoRoundTrip(t *testing.T) {
	var gotPutBody []byte
	var gotPutContentType string
	var gotPutToken string

	putServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading PUT body: %v", err)
		}
		gotPutBody = body
		gotPutContentType = r.Header.Get("Content-Type")
		gotPutToken = r.Header.Get("QF_TOKEN")
		w.WriteHeader(http.StatusOK)
	}))
	defer putServer.Close()

	var gotPresignBody []byte
	presignServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var err error
		gotPresignBody, err = io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading presign body: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uploadUrl":"` + putServer.URL + `","storageKey":"case-run-attachments/proj/1.mp4"}`))
	}))
	defer presignServer.Close()

	client := NewHTTPClient(&stubConfig{endpoint: presignServer.URL, apiKey: "test-token"})
	defer client.Close()

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(videoPath, []byte("fake-video-bytes"), 0o600); err != nil {
		t.Fatal(err)
	}

	storageKey, fileSize, err := client.UploadVideo(context.Background(), videoPath, "video/mp4")
	if err != nil {
		t.Fatalf("UploadVideo error: %v", err)
	}
	if storageKey != "case-run-attachments/proj/1.mp4" {
		t.Errorf("storageKey = %q", storageKey)
	}
	if fileSize != int64(len("fake-video-bytes")) {
		t.Errorf("fileSize = %d, want %d", fileSize, len("fake-video-bytes"))
	}
	if string(gotPutBody) != "fake-video-bytes" {
		t.Errorf("PUT body = %q", gotPutBody)
	}
	if gotPutContentType != "video/mp4" {
		t.Errorf("PUT Content-Type = %q", gotPutContentType)
	}
	// The presigned URL is itself the credential; forwarding QF_TOKEN to R2
	// would leak the Qualflare API token to a third-party host.
	if gotPutToken != "" {
		t.Errorf("QF_TOKEN was sent on the PUT = %q, want no auth header", gotPutToken)
	}
	presignBody := string(gotPresignBody)
	if !strings.Contains(presignBody, `"filename":"clip.mp4"`) ||
		!strings.Contains(presignBody, `"mimeType":"video/mp4"`) ||
		!strings.Contains(presignBody, `"fileSize":16`) {
		t.Errorf("presign request body = %q, missing expected fields", presignBody)
	}
}

// TestUploadVideoRespectsShorterParentDeadline guards UploadVideo's internal
// 5-minute upload budget: it must be DERIVED from the caller's ctx
// (context.WithTimeout(ctx, 5*time.Minute), composing with whatever deadline
// ctx already carries) rather than replacing it with a detached timeout built
// from context.Background(). A parent context that is already past its
// deadline is passed in; both httptest servers would otherwise answer
// instantly, so the only thing that can make the call fail is the expired
// parent deadline still being honored. If UploadVideo instead built its
// 5-minute timeout from a fresh context.Background(), the expired parent
// deadline would be silently dropped and this call would succeed.
func TestUploadVideoRespectsShorterParentDeadline(t *testing.T) {
	putServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer putServer.Close()

	presignServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"uploadUrl":"` + putServer.URL + `","storageKey":"x"}`))
	}))
	defer presignServer.Close()

	client := NewHTTPClient(&stubConfig{endpoint: presignServer.URL, apiKey: "test-token"})
	defer client.Close()

	dir := t.TempDir()
	videoPath := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(videoPath, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()

	start := time.Now()
	_, _, err := client.UploadVideo(ctx, videoPath, "video/mp4")
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("UploadVideo() = nil error, want the already-expired parent deadline to fail the call")
	}
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("UploadVideo() error = %v, want it to wrap context.DeadlineExceeded", err)
	}
	// Generous bound: the point is "failed immediately", not "waited anywhere
	// close to the internal 5-minute timeout".
	if elapsed > 5*time.Second {
		t.Errorf("UploadVideo() took %v, want it to fail immediately on the expired parent deadline", elapsed)
	}
}
