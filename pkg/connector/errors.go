package connector

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/PagerDuty/go-pagerduty"
	"github.com/conductorone/baton-sdk/pkg/uhttp"
	"google.golang.org/grpc/codes"
)

func wrapPagerDutyError(msg string, err error) error {
	if err == nil {
		return nil
	}

	var apiErr pagerduty.APIError
	if errors.As(err, &apiErr) {
		switch {
		case apiErr.RateLimited():
			return uhttp.WrapErrors(codes.Unavailable, fmt.Sprintf("pagerduty-connector: %s", msg), err)
		case apiErr.NotFound():
			return uhttp.WrapErrors(codes.NotFound, fmt.Sprintf("pagerduty-connector: %s", msg), err)
		case apiErr.StatusCode == http.StatusUnauthorized:
			return uhttp.WrapErrors(codes.Unauthenticated, fmt.Sprintf("pagerduty-connector: %s", msg), err)
		case apiErr.StatusCode == http.StatusForbidden:
			return uhttp.WrapErrors(codes.PermissionDenied, fmt.Sprintf("pagerduty-connector: %s", msg), err)
		case apiErr.Temporary():
			return uhttp.WrapErrors(codes.Unavailable, fmt.Sprintf("pagerduty-connector: %s", msg), err)
		default:
			return uhttp.WrapErrors(codes.Internal, fmt.Sprintf("pagerduty-connector: %s", msg), err)
		}
	}

	return uhttp.WrapErrors(codes.Unavailable, fmt.Sprintf("pagerduty-connector: %s", msg), err)
}

// notFoundOnItem reports whether err is a PagerDuty 404 for the single item
// being fetched: HTTP 404 or API error code 2100 ("schedule not found"), both
// detected by pagerduty.APIError.NotFound(). PagerDuty returns 404 for
// ABSENCE — a deleted/unresolvable object — not for permission denial, which
// is 403 and must NOT be skipped. This predicate is the raw-error guard used
// before wrapPagerDutyError maps the error; it changes no existing mapping.
// Rule basis: BUGBOT.md pattern 5; .claude/skills/ci-review.md S5+S6;
// .claude/skills/connector/patterns-error-handling.md Rule 4.
func notFoundOnItem(err error) bool {
	if err == nil {
		return false
	}
	var apiErr pagerduty.APIError
	return errors.As(err, &apiErr) && apiErr.NotFound()
}
