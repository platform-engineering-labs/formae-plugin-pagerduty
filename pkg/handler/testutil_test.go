// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

//go:build integration

package handler

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"

	"github.com/platform-engineering-labs/formae-plugin-pagerduty/pkg/config"
)

// testClient returns a real PagerDuty SDK client using PAGERDUTY_TOKEN.
// The test is skipped if the env var is unset.
func testClient(t *testing.T) *pagerduty.Client {
	t.Helper()
	if os.Getenv("PAGERDUTY_TOKEN") == "" {
		t.Skip("PAGERDUTY_TOKEN not set; skipping integration test")
	}
	cfg, err := config.ParseTargetConfig([]byte(`{"Type":"PAGERDUTY","Subdomain":"test"}`))
	if err != nil {
		t.Fatalf("ParseTargetConfig: %v", err)
	}
	c, err := config.NewClient(cfg)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return c
}

// uniqueName builds a name unique to this test invocation, useful for resources
// PagerDuty requires unique names/emails for (Users).
func uniqueName(prefix string) string {
	return fmt.Sprintf("%s-%d", prefix, time.Now().UnixNano())
}

func uniqueEmail(prefix string) string {
	// The PagerDuty account under test enforces a domain allow-list, so test
	// emails must use the configured domain. Override via PAGERDUTY_TEST_DOMAIN
	// if your sandbox is on a different domain.
	domain := os.Getenv("PAGERDUTY_TEST_DOMAIN")
	if domain == "" {
		domain = "platform.engineering"
	}
	return fmt.Sprintf("formae-pd-test-%s-%d@%s", prefix, time.Now().UnixNano(), domain)
}

// ctx returns a context with a default timeout for SDK calls.
func ctx(t *testing.T) context.Context {
	t.Helper()
	c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	t.Cleanup(cancel)
	return c
}

// cleanupUser best-effort deletes a user by ID. Use in t.Cleanup.
func cleanupUser(t *testing.T, client *pagerduty.Client, id string) {
	t.Helper()
	if id == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeleteUserWithContext(cleanupCtx, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete user %s: %v", id, err)
	}
}

// createPrereq runs a setup Create that references a just-created dependency,
// retrying on transient NotFound (PagerDuty read-after-write lag, e.g. a layer
// referencing a user that isn't visible yet) so prerequisite setup doesn't
// flake. Returns the new resource's NativeID; fatals on a non-transient failure.
func createPrereq(t *testing.T, c context.Context, client *pagerduty.Client, h ResourceHandler, props []byte, label string) string {
	t.Helper()
	var msg string
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}
		res, err := h.Create(c, client, props)
		switch {
		case err != nil:
			msg = err.Error()
		case res.OperationStatus == resource.OperationStatusSuccess:
			return res.NativeID
		default:
			msg = res.StatusMessage
			if res.ErrorCode != resource.OperationErrorCodeNotFound {
				t.Fatalf("setup %s: %s", label, msg)
			}
		}
	}
	t.Fatalf("setup %s: %s", label, msg)
	return ""
}

// readUntilGone polls Read until it reports NotFound, absorbing PagerDuty's
// read-after-delete lag: a removed member or override can linger briefly in a
// list-and-filter read. Fails if the resource is still readable after the window.
func readUntilGone(t *testing.T, c context.Context, client *pagerduty.Client, h ResourceHandler, nativeID string) {
	t.Helper()
	var last resource.OperationErrorCode
	for attempt := 0; attempt < 5; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}
		read, _ := h.Read(c, client, nativeID)
		last = read.ErrorCode
		if last == resource.OperationErrorCodeNotFound {
			return
		}
	}
	t.Errorf("after delete, Read ErrorCode = %q, want NotFound (still visible after retries)", last)
}
