package http

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"qualflare-cli/internal/core/domain"
)

// TestSendReport_BodyIsUncompressedJSONEncoding pins the wire format that
// services.collectBodySize measures against /collect's 10 MB limit: the launch
// encoded by encoding/json with HTML escaping (json.Marshal bytes plus the
// encoder's trailing newline), with no Content-Encoding. If this changes —
// compression, a different encoder — the body-limit measurement must change
// with it, or it will drop attachments it need not, or let a 413 through.
func TestSendReport_BodyIsUncompressedJSONEncoding(t *testing.T) {
	launch := &domain.Launch{Suites: []domain.Suite{{
		Name: "s <&>",
		Cases: []domain.Case{{
			Name:   "c",
			Status: domain.StatusPassed,
			Attachments: []domain.Attachment{
				{Name: "log.json", MimeType: "application/json", Content: "eyJhIjoiPGI+In0="},
			},
		}},
	}}}

	var got []byte
	var encoding string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		encoding = r.Header.Get("Content-Encoding")
		got, _ = io.ReadAll(r.Body)
		w.WriteHeader(http.StatusOK)
	})
	if err := c.SendReport(context.Background(), launch); err != nil {
		t.Fatalf("SendReport() = %v", err)
	}

	var want bytes.Buffer
	enc := json.NewEncoder(&want)
	enc.SetEscapeHTML(true)
	if err := enc.Encode(launch); err != nil {
		t.Fatal(err)
	}
	if encoding != "" {
		t.Errorf("Content-Encoding = %q, want none", encoding)
	}
	if !bytes.Equal(got, want.Bytes()) {
		t.Errorf("wire body differs from the encoding the body-limit check measures\n got: %s\nwant: %s", got, want.Bytes())
	}
}
