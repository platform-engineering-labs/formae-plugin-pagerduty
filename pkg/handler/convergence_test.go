// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

//go:build unit

package handler

import (
	"context"
	"errors"
	"testing"
	"time"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

func TestRetryUntilReadable_ReadyImmediately(t *testing.T) {
	calls := 0
	v, err := retryUntilReadable(context.Background(), 5, time.Millisecond, func() (string, bool, error) {
		calls++
		return "ok", true, nil
	})
	if err != nil || v != "ok" {
		t.Fatalf("got %q,%v want ok,nil", v, err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (ready first try)", calls)
	}
}

func TestRetryUntilReadable_ConvergesAfterRetries(t *testing.T) {
	calls := 0
	v, err := retryUntilReadable(context.Background(), 5, time.Millisecond, func() (int, bool, error) {
		calls++
		return calls, calls >= 3, nil
	})
	if err != nil || v != 3 {
		t.Fatalf("got %d,%v want 3,nil", v, err)
	}
}

func TestRetryUntilReadable_ReturnsLastSeenOnExhaustion(t *testing.T) {
	// Never ready, but always returns a value: caller gets the last value, no error.
	v, err := retryUntilReadable(context.Background(), 3, time.Millisecond, func() (string, bool, error) {
		return "partial", false, nil
	})
	if err != nil || v != "partial" {
		t.Fatalf("got %q,%v want partial,nil", v, err)
	}
}

func TestRetryUntilReadable_ReturnsMostRecentValue(t *testing.T) {
	// The helper trusts fn's latest value, so on a transient change it returns
	// the most recent one. Callers that must not surface a transient zero
	// (e.g. membershipAfterWrite) keep their own last-good value in the closure.
	vals := []string{"x", ""}
	i := 0
	v, err := retryUntilReadable(context.Background(), 5, time.Millisecond, func() (string, bool, error) {
		s := vals[i]
		if i < len(vals)-1 {
			i++
		}
		return s, false, nil
	})
	if err != nil || v != "" {
		t.Fatalf("got %q,%v want \"\",nil (most recent value)", v, err)
	}
}

func TestRetryUntilReadable_TerminalErrorShortCircuits(t *testing.T) {
	sentinel := errors.New("boom")
	calls := 0
	_, err := retryUntilReadable(context.Background(), 3, time.Millisecond, func() (string, bool, error) {
		calls++
		return "", false, sentinel
	})
	if !errors.Is(err, sentinel) {
		t.Fatalf("err = %v, want sentinel", err)
	}
	if calls != 1 {
		t.Fatalf("calls = %d, want 1 (terminal error returned immediately, not retried)", calls)
	}
}

func TestRetryUntilReadable_ContextCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := retryUntilReadable(ctx, 5, 10*time.Millisecond, func() (string, bool, error) {
		return "", false, nil // never ready, forcing a wait that hits the cancelled ctx
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v, want context.Canceled", err)
	}
}

func apiErr(status, code int) error {
	e := pagerduty.APIError{StatusCode: status}
	if code != 0 {
		e.APIError = pagerduty.NullAPIErrorObject{Valid: true, ErrorObject: pagerduty.APIErrorObject{Code: code}}
	}
	return e
}

func TestMapAPIError(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want resource.OperationErrorCode
	}{
		{"400 with code 2100 is NotFound (referenced dep not yet visible)", apiErr(400, 2100), resource.OperationErrorCodeNotFound},
		{"409 with code 2100 stays AlreadyExists (not shadowed by NotFound)", apiErr(409, 2100), resource.OperationErrorCodeAlreadyExists},
		{"plain 400 is InvalidRequest", apiErr(400, 0), resource.OperationErrorCodeInvalidRequest},
		{"404 is NotFound", apiErr(404, 0), resource.OperationErrorCodeNotFound},
		{"429 is Throttling", apiErr(429, 0), resource.OperationErrorCodeThrottling},
		{"401 is InvalidCredentials", apiErr(401, 0), resource.OperationErrorCodeInvalidCredentials},
		{"503 is ServiceInternalError", apiErr(503, 0), resource.OperationErrorCodeServiceInternalError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := MapAPIError(tc.err); got != tc.want {
				t.Fatalf("MapAPIError = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestIsNotFound(t *testing.T) {
	if !IsNotFound(apiErr(404, 0)) {
		t.Error("404 should be NotFound")
	}
	// A not-yet-visible reference (400+2100) must NOT be treated as gone, or a
	// Delete would drop a resource that still exists.
	if IsNotFound(apiErr(400, 2100)) {
		t.Error("400+code2100 must NOT be NotFound")
	}
	if IsNotFound(apiErr(409, 2100)) {
		t.Error("409+code2100 must NOT be NotFound")
	}
}
