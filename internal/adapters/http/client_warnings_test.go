package http

import (
	"bytes"
	"context"
	"net/http"
	"reflect"
	"testing"

	"qualflare-cli/internal/core/domain"
)

func TestParseCollectWarnings(t *testing.T) {
	cases := []struct {
		name string
		body string
		want []string
	}{
		{
			name: "one warning",
			body: `{"seq":7,"warnings":[{"path":"attachments","reason":"Storage is full (1.2 GB of 1.0 GB). 2 file(s) were not stored."}]}`,
			want: []string{"Storage is full (1.2 GB of 1.0 GB). 2 file(s) were not stored."},
		},
		{
			name: "several warnings keep their order",
			body: `{"seq":7,"warnings":[{"path":"a","reason":"one"},{"path":"b","reason":"two"}]}`,
			want: []string{"one", "two"},
		},
		{name: "no warnings key", body: `{"seq":7}`},
		{name: "empty body", body: ``},
		{name: "not json", body: `not json`},
		{name: "empty list", body: `{"warnings":[]}`},
		{name: "empty reason is skipped", body: `{"seq":7,"warnings":[{"path":"a","reason":""}]}`},
		{
			name: "only the empty reason is skipped",
			body: `{"warnings":[{"reason":""},{"reason":"kept"}]}`,
			want: []string{"kept"},
		},
		{name: "warnings is not a list", body: `{"seq":7,"warnings":"x"}`},
		{name: "null", body: `null`},
		{name: "array body", body: `[]`},
		{name: "string body", body: `"ok"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := parseCollectWarnings([]byte(tc.body))
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("parseCollectWarnings(%q) = %#v, want %#v", tc.body, got, tc.want)
			}
		})
	}
}

// collectClient answers /collect with the given status and body and returns the client
// together with the buffer its operator-facing lines go to.
func collectClient(t *testing.T, status int, body string) (*Client, *bytes.Buffer) {
	t.Helper()
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	})
	var buf bytes.Buffer
	c.warn = &buf
	return c, &buf
}

func TestSendReport_PrintsTheServersWarnings(t *testing.T) {
	const reason = "Storage is full (1.2 GB of 1.0 GB). Results are still saved, but new files are not. Delete old runs, wait for run files older than 30 days to expire, or upgrade the plan. 2 files were not stored."
	c, buf := collectClient(t, http.StatusOK,
		`{"seq":7,"warnings":[{"path":"attachments","reason":"`+reason+`"}]}`)

	if err := c.SendReport(context.Background(), &domain.Launch{}); err != nil {
		t.Fatalf("SendReport() = %v, want nil", err)
	}
	if want := "warning: " + reason + "\n"; buf.String() != want {
		t.Errorf("warning output = %q, want %q", buf.String(), want)
	}
}

func TestSendReport_SuccessWithoutWarningsPrintsNothing(t *testing.T) {
	c, buf := collectClient(t, http.StatusOK, `{"seq":7}`)
	if err := c.SendReport(context.Background(), &domain.Launch{}); err != nil {
		t.Fatalf("SendReport() = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("output = %q, want nothing", buf.String())
	}
}

func TestSendReport_SuccessWithEmptyBodyIsStillSuccess(t *testing.T) {
	for _, status := range []int{http.StatusOK, http.StatusNoContent} {
		c, buf := collectClient(t, status, "")
		if err := c.SendReport(context.Background(), &domain.Launch{}); err != nil {
			t.Fatalf("status %d: SendReport() = %v, want nil", status, err)
		}
		if buf.Len() != 0 {
			t.Errorf("status %d: output = %q, want nothing", status, buf.String())
		}
	}
}

func TestSendReport_SuccessWithUnreadableBodyIsStillSuccess(t *testing.T) {
	c, buf := collectClient(t, http.StatusOK, "not json")
	if err := c.SendReport(context.Background(), &domain.Launch{}); err != nil {
		t.Fatalf("SendReport() = %v, want nil", err)
	}
	if buf.Len() != 0 {
		t.Errorf("output = %q, want nothing", buf.String())
	}
}
