// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

package config

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// fakeRT returns the queued status codes in order and records each request body.
type fakeRT struct {
	codes  []int
	n      int
	bodies []string
}

func (f *fakeRT) RoundTrip(req *http.Request) (*http.Response, error) {
	body := ""
	if req.Body != nil {
		b, _ := io.ReadAll(req.Body)
		body = string(b)
	}
	f.bodies = append(f.bodies, body)
	code := f.codes[f.n]
	f.n++
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader("")), Header: http.Header{}}, nil
}

func fastRetry(base http.RoundTripper, maxRetries int) *retryTransport {
	return &retryTransport{base: base, maxRetries: maxRetries, baseDelay: time.Millisecond, maxDelay: 5 * time.Millisecond}
}

func TestRetryTransport_Retries429ForAnyMethod(t *testing.T) {
	// 429 is always safe to retry (request was rejected, not applied) - even POST.
	fake := &fakeRT{codes: []int{429, 429, 200}}
	req, _ := http.NewRequest(http.MethodPost, "https://api.pagerduty.com/x", strings.NewReader("payload"))
	resp, err := fastRetry(fake, 4).RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if fake.n != 3 {
		t.Fatalf("attempts = %d, want 3", fake.n)
	}
	for i, b := range fake.bodies {
		if b != "payload" {
			t.Errorf("attempt %d body = %q, want body resent", i, b)
		}
	}
}

func TestRetryTransport_Retries5xxForIdempotentMethod(t *testing.T) {
	fake := &fakeRT{codes: []int{503, 200}}
	req, _ := http.NewRequest(http.MethodPut, "https://api.pagerduty.com/x", strings.NewReader("p"))
	resp, err := fastRetry(fake, 4).RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 200 || fake.n != 2 {
		t.Fatalf("status=%d attempts=%d, want 200,2", resp.StatusCode, fake.n)
	}
}

func TestRetryTransport_DoesNotRetry5xxForPost(t *testing.T) {
	// A POST the server may have committed before answering 5xx must not be
	// replayed into a duplicate resource.
	fake := &fakeRT{codes: []int{503, 200}}
	req, _ := http.NewRequest(http.MethodPost, "https://api.pagerduty.com/x", strings.NewReader("p"))
	resp, err := fastRetry(fake, 4).RoundTrip(req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 503 || fake.n != 1 {
		t.Fatalf("status=%d attempts=%d, want 503,1 (no retry on POST 5xx)", resp.StatusCode, fake.n)
	}
}

func TestRetryTransport_GivesUpAfterMaxRetries(t *testing.T) {
	fake := &fakeRT{codes: []int{429, 429, 429, 429, 429}}
	req, _ := http.NewRequest(http.MethodGet, "https://api.pagerduty.com/x", nil)
	resp, err := fastRetry(fake, 2).RoundTrip(req) // 1 initial + 2 retries
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.StatusCode != 429 {
		t.Fatalf("status = %d, want 429 (gave up)", resp.StatusCode)
	}
	if fake.n != 3 {
		t.Fatalf("attempts = %d, want 3", fake.n)
	}
}

func TestRetryTransport_DoesNotRetry2xx(t *testing.T) {
	fake := &fakeRT{codes: []int{201}}
	req, _ := http.NewRequest(http.MethodPost, "https://api.pagerduty.com/x", strings.NewReader("x"))
	if _, err := fastRetry(fake, 4).RoundTrip(req); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if fake.n != 1 {
		t.Fatalf("attempts = %d, want 1 (no retry on success)", fake.n)
	}
}

func TestRetryAfter_HonorsRatelimitReset(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("ratelimit-reset", "2")
	d, ok := retryAfter(resp)
	if !ok || d != 2*time.Second {
		t.Fatalf("retryAfter = %v,%v want 2s,true", d, ok)
	}
}
