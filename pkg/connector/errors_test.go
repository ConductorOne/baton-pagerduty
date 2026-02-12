package connector

import (
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
