package connector

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/grpc-ecosystem/go-grpc-middleware/logging/zap/ctxzap"
	"go.uber.org/zap"
)

const (
	// maxRetries is the maximum number of times to retry a rate-limited request.
	maxRetries = 5
	// defaultRetryWait is the default wait time between retries when Retry-After is not provided.
	defaultRetryWait = 10 * time.Second
	// maxRetryWait is the maximum time to wait before retrying.
	maxRetryWait = 2 * time.Minute
)

// rateLimitedHTTPClient wraps an HTTP client and handles rate limiting by
// retrying requests that receive 429 responses.
type rateLimitedHTTPClient struct {
	httpClient *http.Client
}

// newRateLimitedHTTPClient creates a new rate-limited HTTP client wrapper.
func newRateLimitedHTTPClient() *rateLimitedHTTPClient {
	return &rateLimitedHTTPClient{
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// Do implements the pagerduty.HTTPClient interface and handles rate limiting.
func (c *rateLimitedHTTPClient) Do(req *http.Request) (*http.Response, error) {
	var lastErr error
	var lastResp *http.Response

	// Store the original body for retries
	var bodyBytes []byte
	if req.Body != nil {
		var err error
		bodyBytes, err = io.ReadAll(req.Body)
		if err != nil {
			return nil, err
		}
		req.Body.Close()
		req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
	}

	ctx := req.Context()
	l := ctxzap.Extract(ctx)

	for attempt := 0; attempt <= maxRetries; attempt++ {
		// Reset the body for each attempt
		if bodyBytes != nil {
			req.Body = io.NopCloser(bytes.NewReader(bodyBytes))
		}

		resp, err := c.httpClient.Do(req)
		if err != nil {
			lastErr = err
			continue
		}

		// If not rate limited, return immediately
		if resp.StatusCode != http.StatusTooManyRequests {
			return resp, nil
		}

		// We're rate limited
		lastResp = resp
		lastErr = nil

		// Check if we've exhausted our retries
		if attempt == maxRetries {
			l.Warn("pagerduty-connector: rate limit exceeded, max retries reached",
				zap.Int("attempt", attempt),
				zap.String("url", req.URL.String()),
			)
			break
		}

		// Calculate wait time from Retry-After header or use exponential backoff
		waitTime := c.getRetryWait(resp, attempt)

		l.Info("pagerduty-connector: rate limited, waiting before retry",
			zap.Int("attempt", attempt+1),
			zap.Duration("wait_time", waitTime),
			zap.String("url", req.URL.String()),
		)

		// Close the response body before waiting
		resp.Body.Close()

		// Wait before retrying, but respect context cancellation
		select {
		case <-time.After(waitTime):
			// Continue to next attempt
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}

	if lastResp != nil {
		return lastResp, lastErr
	}

	return nil, lastErr
}

// getRetryWait calculates the wait time before the next retry.
// It checks the Retry-After header first, then falls back to exponential backoff.
func (c *rateLimitedHTTPClient) getRetryWait(resp *http.Response, attempt int) time.Duration {
	// Try to parse Retry-After header
	if retryAfter := resp.Header.Get("Retry-After"); retryAfter != "" {
		// Try parsing as seconds
		if seconds, err := strconv.Atoi(retryAfter); err == nil {
			waitTime := time.Duration(seconds) * time.Second
			if waitTime > maxRetryWait {
				return maxRetryWait
			}
			if waitTime > 0 {
				return waitTime
			}
		}

		// Try parsing as HTTP-date (RFC 1123)
		if t, err := http.ParseTime(retryAfter); err == nil {
			waitTime := time.Until(t)
			if waitTime > maxRetryWait {
				return maxRetryWait
			}
			if waitTime > 0 {
				return waitTime
			}
		}
	}

	// Fall back to exponential backoff
	// Base wait time * 2^attempt (e.g., 10s, 20s, 40s, 80s, 120s max)
	waitTime := defaultRetryWait * time.Duration(1<<uint(attempt))
	if waitTime > maxRetryWait {
		return maxRetryWait
	}

	return waitTime
}
