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

// notFoundOnItem reports whether err is a PagerDuty "not found" error for the
// single item being fetched, per pagerduty.APIError.NotFound(): true when the
// HTTP status is 404, OR when the parsed API error object has code 2100 —
// regardless of the HTTP status. Code 2100 is PagerDuty's generic not-found
// object code (not schedule-specific), so a well-formed error body carrying
// it is treated as ABSENCE even on a non-404 status; the vendor implements
// exactly this in go-pagerduty client.go (NotFound: StatusCode == 404 ||
// ErrorObject.Code == 2100). The wider-than-404 match is safe here because
// the guarded call fetches exactly one object: any not-found verdict on that
// call can only mean that object is absent. PagerDuty never uses 404 for
// permission denial (that is 403), which must NOT be skipped. This predicate is the raw-error guard used
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
