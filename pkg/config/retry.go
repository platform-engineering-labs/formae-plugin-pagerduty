// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

package config

import (
	"bytes"
	"io"
	"math/rand/v2"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// retryTransport retries 429 and 5xx responses (and transient network errors)
// with exponential backoff and full jitter. go-pagerduty does no retry of its
// own, and PagerDuty rate-limits account-wide at a few RPS, so a parallel apply
// can self-throttle; retrying here keeps that off the handlers. PagerDuty's
// ratelimit-reset hint is honored when present.
type retryTransport struct {
	base       http.RoundTripper
	maxRetries int
	baseDelay  time.Duration
	maxDelay   time.Duration
}

func newRetryTransport(base http.RoundTripper) *retryTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &retryTransport{base: base, maxRetries: 4, baseDelay: 500 * time.Millisecond, maxDelay: 20 * time.Second}
}

func (rt *retryTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Buffer the body so it can be resent on each attempt.
	var body []byte
	if req.Body != nil {
		b, err := io.ReadAll(req.Body)
		_ = req.Body.Close()
		if err != nil {
			return nil, err
		}
		body = b
	}

	var resp *http.Response
	var err error
	for attempt := 0; ; attempt++ {
		if body != nil {
			req.Body = io.NopCloser(bytes.NewReader(body))
		}
		resp, err = rt.base.RoundTrip(req)
		if attempt >= rt.maxRetries || !retryable(req, resp, err) {
			return resp, err
		}
		wait := rt.backoff(attempt, resp)
		drain(resp)
		select {
		case <-req.Context().Done():
			return nil, req.Context().Err()
		case <-time.After(wait):
		}
	}
}

// retryable decides whether to retry. 429 is always safe (the request was
// rejected, not applied). 5xx responses and network errors are retried only for
// idempotent methods: a non-idempotent POST the server may have committed before
// answering 5xx (or dropping the connection) must not be replayed into a
// duplicate resource, and go-pagerduty has no idempotency-key support.
func retryable(req *http.Request, resp *http.Response, err error) bool {
	if err != nil {
		return idempotent(req.Method)
	}
	if resp.StatusCode == http.StatusTooManyRequests {
		return true
	}
	if resp.StatusCode >= 500 && resp.StatusCode <= 599 {
		return idempotent(req.Method)
	}
	return false
}

func idempotent(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodPut, http.MethodDelete, http.MethodOptions:
		return true
	default:
		return false
	}
}

// backoff honors PagerDuty's ratelimit-reset / Retry-After header when present,
// else exponential baseDelay*2^attempt capped at maxDelay, with full jitter.
func (rt *retryTransport) backoff(attempt int, resp *http.Response) time.Duration {
	if d, ok := retryAfter(resp); ok {
		if d > rt.maxDelay {
			return rt.maxDelay
		}
		return d
	}
	d := rt.baseDelay << attempt
	if d <= 0 || d > rt.maxDelay {
		d = rt.maxDelay
	}
	return time.Duration(rand.Int64N(int64(d) + 1))
}

func retryAfter(resp *http.Response) (time.Duration, bool) {
	if resp == nil {
		return 0, false
	}
	for _, key := range []string{"ratelimit-reset", "Retry-After"} {
		if v := strings.TrimSpace(resp.Header.Get(key)); v != "" {
			if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
				return time.Duration(secs) * time.Second, true
			}
		}
	}
	return 0, false
}

// drain empties and closes a response body so the connection can be reused
// before the next attempt.
func drain(resp *http.Response) {
	if resp == nil || resp.Body == nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
