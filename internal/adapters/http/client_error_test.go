package http

import (
	"strings"
	"testing"
)

// TestActionHint (OBS-05) asserts common auth/authz failures carry an actionable
// next step, not just a description.
func TestActionHint(t *testing.T) {
	if got := (&APIError{StatusCode: 401, Message: "invalid token"}).Error(); !strings.Contains(got, "qf login") {
		t.Fatalf("401 should hint `qf login`: %q", got)
	}
	if got := (&APIError{StatusCode: 403, Message: "nope"}).Error(); !strings.Contains(got, "access to this project") {
		t.Fatalf("403 should hint about project access: %q", got)
	}
	if got := (&APIError{StatusCode: 500, Message: "boom"}).Error(); strings.Contains(got, "qf login") {
		t.Fatalf("500 must not carry an auth hint: %q", got)
	}
}

// TestPlanLimitReason pins what counts as a plan-limit refusal: the 402 with the
// code quota.exceeded, and nothing that merely shares one of the two. The server
// also sends quota.exceeded with a 400 for limits that are not about the plan.
func TestPlanLimitReason(t *testing.T) {
	cases := []struct {
		name string
		err  APIError
		want string
		ok   bool
	}{
		{
			name: "402 quota.exceeded",
			err:  APIError{StatusCode: 402, Code: "quota.exceeded", Message: "Launches quota exceeded (100/100)"},
			want: "Launches quota exceeded (100/100)",
			ok:   true,
		},
		{
			name: "the request id is kept for support",
			err:  APIError{StatusCode: 402, Code: "quota.exceeded", Message: "limit", RequestID: "req-1"},
			want: "limit [request_id: req-1]",
			ok:   true,
		},
		{
			name: "server text cannot forge a log line",
			err:  APIError{StatusCode: 402, Code: "quota.exceeded", Message: "limit\nOK done", RequestID: "req\n1"},
			want: "limit OK done [request_id: req 1]",
			ok:   true,
		},
		{name: "frozen workspace", err: APIError{StatusCode: 402, Code: "billing.workspace_frozen", Message: "x"}},
		{name: "storage full", err: APIError{StatusCode: 402, Code: "quota.storage_exceeded", Message: "x"}},
		{name: "402 without a code", err: APIError{StatusCode: 402, Message: "x"}},
		{name: "400 with the code", err: APIError{StatusCode: 400, Code: "quota.exceeded", Message: "x"}},
		{name: "403 with the code", err: APIError{StatusCode: 403, Code: "quota.exceeded", Message: "x"}},
		{name: "no response at all", err: APIError{Code: "quota.exceeded", Message: "x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.err.PlanLimitReason()
			if got != tc.want || ok != tc.ok {
				t.Fatalf("PlanLimitReason() = %q, %v; want %q, %v", got, ok, tc.want, tc.ok)
			}
		})
	}
}

// TestResolveErrorMessage (SYNC-01/10) pins the error-message selection: the
// server's message wins, and the generic 404 code common.resource_not_found no
// longer renders as "Language not found".
func TestResolveErrorMessage(t *testing.T) {
	cases := []struct {
		name string
		resp ErrorResponse
		code int
		want string
	}{
		{
			name: "generic 404 uses server message, not a language error",
			resp: ErrorResponse{Code: "common.resource_not_found", Message: "cluster not found"},
			code: 404,
			want: "cluster not found",
		},
		{
			name: "generic 404 with no message falls back to a generic status line",
			resp: ErrorResponse{Code: "common.resource_not_found"},
			code: 404,
			want: "API request failed with status 404",
		},
		{
			name: "server message always preferred over the friendly hint",
			resp: ErrorResponse{Code: "environment.not_found", Message: "environment 'prod' not found"},
			code: 404,
			want: "environment 'prod' not found",
		},
		{
			name: "friendly hint used only when the server sent no message",
			resp: ErrorResponse{Code: "environment.not_found"},
			code: 404,
			want: getUserFriendlyMessage("environment.not_found"),
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolveErrorMessage(tc.resp, tc.code); got != tc.want {
				t.Fatalf("resolveErrorMessage = %q, want %q", got, tc.want)
			}
		})
	}
}
