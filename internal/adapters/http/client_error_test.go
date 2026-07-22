package http

import "testing"

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
