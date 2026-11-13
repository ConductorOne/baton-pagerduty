package connector

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/PagerDuty/go-pagerduty"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func newMockServer(t *testing.T, statusCode int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)
		_, _ = w.Write([]byte(body))
	}))
}

func TestWrapPagerDutyError_RateLimit(t *testing.T) {
	srv := newMockServer(t, http.StatusTooManyRequests,
		`{"error":{"code":2020,"message":"Rate Limit Exceeded","errors":["Too many requests"]}}`)
	defer srv.Close()

	client := pagerduty.NewClient("fake-token", pagerduty.WithAPIEndpoint(srv.URL))
	_, err := client.ListUsersWithContext(t.Context(), pagerduty.ListUsersOptions{})
	if err == nil {
		t.Fatal("expected error from mock server, got nil")
	}

	wrapped := wrapPagerDutyError("failed to list users", err)

	st, ok := status.FromError(wrapped)
	if !ok {
		t.Fatalf("expected gRPC status error, got: %v", wrapped)
	}
	if st.Code() != codes.Unavailable {
		t.Errorf("rate limit: got gRPC code %v, want %v", st.Code(), codes.Unavailable)
	}
}

func TestNotFoundOnItem(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		body       string
		want       bool
	}{
		{
			name:       "http 404 schedule not found",
			statusCode: http.StatusNotFound,
			body:       `{"error":{"code":2001,"message":"Not Found","errors":["Schedule Not Found"]}}`,
			want:       true,
		},
		{
			name:       "api code 2100 schedule not found",
			statusCode: http.StatusNotFound,
			body:       `{"error":{"code":2100,"message":"Not Found","errors":["Schedule Not Found"]}}`,
			want:       true,
		},
		{
			// Isolates the vendor's code==2100 arm of APIError.NotFound():
			// a well-formed error body with code 2100 is "not found" even
			// when the HTTP status itself is not 404 (APIError.Valid is set
			// on any parsed error body).
			name:       "api code 2100 with non-404 status still matches code arm",
			statusCode: http.StatusBadRequest,
			body:       `{"error":{"code":2100,"message":"Not Found","errors":["Schedule Not Found"]}}`,
			want:       true,
		},
		{
			name:       "403 forbidden is not skippable",
			statusCode: http.StatusForbidden,
			body:       `{"error":{"code":2003,"message":"Forbidden","errors":["You are not allowed to access this resource"]}}`,
			want:       false,
		},
		{
			name:       "429 rate limited is not skippable",
			statusCode: http.StatusTooManyRequests,
			body:       `{"error":{"code":2020,"message":"Rate Limit Exceeded","errors":["Too many requests"]}}`,
			want:       false,
		},
		{
			name:       "500 is not skippable",
			statusCode: http.StatusInternalServerError,
			body:       `{"error":{"code":5000,"message":"Internal Server Error","errors":["Something went wrong"]}}`,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := newMockServer(t, tt.statusCode, tt.body)
			defer srv.Close()

			client := pagerduty.NewClient("fake-token", pagerduty.WithAPIEndpoint(srv.URL))
			_, err := client.ListUsersWithContext(t.Context(), pagerduty.ListUsersOptions{})
			if err == nil {
				t.Fatal("expected error from mock server, got nil")
			}

			if got := notFoundOnItem(err); got != tt.want {
				t.Errorf("notFoundOnItem() = %v, want %v", got, tt.want)
			}
		})
	}

	// non-APIError errors are not skippable
	if got := notFoundOnItem(context.Canceled); got != false {
		t.Errorf("notFoundOnItem(context.Canceled) = %v, want false", got)
	}
	// nil is not skippable
	if got := notFoundOnItem(nil); got != false {
		t.Errorf("notFoundOnItem(nil) = %v, want false", got)
	}
}
