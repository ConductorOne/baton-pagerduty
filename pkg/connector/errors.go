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
