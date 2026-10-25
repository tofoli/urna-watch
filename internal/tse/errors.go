package tse

import (
	"fmt"
	"net/http"
	"time"
)

// HTTPError describes a non-success response from the TSE CDN. It never
// contains request headers or credentials.
type HTTPError struct {
	StatusCode int
	URL        string
	RetryAfter time.Duration
	Body       string
}

func (e *HTTPError) Error() string {
	if e.RetryAfter > 0 {
		return fmt.Sprintf("TSE respondeu HTTP %d em %s (retry-after %s)", e.StatusCode, e.URL, e.RetryAfter)
	}
	return fmt.Sprintf("TSE respondeu HTTP %d em %s", e.StatusCode, e.URL)
}

// IsRetryable reports whether the polling service should try again.
func (e *HTTPError) IsRetryable() bool {
	switch e.StatusCode {
	case http.StatusTooManyRequests, http.StatusRequestTimeout, http.StatusInternalServerError,
		http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return e.StatusCode >= 500
	}
}

// RetryAfterValue implements the generic interface consumed by the poller.
func (e *HTTPError) RetryAfterValue() time.Duration { return e.RetryAfter }

// ParseError indicates that the source returned a body that is not a valid
// result for the requested election. The previous good snapshot must be kept.
type ParseError struct {
	Reason string
	Err    error
}

func (e *ParseError) Error() string {
	if e.Err != nil {
		return "resposta inválida do TSE: " + e.Reason + ": " + e.Err.Error()
	}
	return "resposta inválida do TSE: " + e.Reason
}

func (e *ParseError) Unwrap() error { return e.Err }

// IsRetryable returns false: retrying an invalid body in the same cycle would
// not make it valid. The next scheduled cycle will try again.
func (e *ParseError) IsRetryable() bool { return false }
