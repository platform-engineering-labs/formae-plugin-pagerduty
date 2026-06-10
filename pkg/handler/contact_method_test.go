// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

//go:build integration

package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const contactMethodResourceType = "PAGERDUTY::Core::ContactMethod"

// cleanupContactMethod best-effort deletes a contact method. Deleting the
// parent user also cascades, so this is mostly a safety net.
func cleanupContactMethod(t *testing.T, client *pagerduty.Client, userID, cmID string) {
	t.Helper()
	if userID == "" || cmID == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeleteUserContactMethodWithContext(cleanupCtx, userID, cmID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete contact method %s/%s: %v", userID, cmID, err)
	}
}

func minimalContactMethodProps(userID string) []byte {
	props, _ := json.Marshal(map[string]any{
		"userId":      userID,
		"methodType":  "email_contact_method",
		"methodLabel": "Work",
		"address":     uniqueEmail("cm"),
	})
	return props
}

// setupContactMethod creates a contact method on the given user and returns its
// composite NativeID (userID:cmID). Used by the notification-rule tests, which
// require an existing contact method to point at.
func setupContactMethod(t *testing.T, client *pagerduty.Client, userID string) string {
	t.Helper()
	h, _ := Get(contactMethodResourceType)
	return createPrereq(t, ctx(t), client, h, minimalContactMethodProps(userID), "contact method")
}

func TestContactMethod_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(contactMethodResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	userID := scheduleSetupUser(t, client)

	result, err := h.Create(ctx(t), client, minimalContactMethodProps(userID))
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	if !strings.Contains(result.NativeID, ":") {
		t.Errorf("NativeID %q should be composite userID:contactMethodID", result.NativeID)
	}
	var got map[string]any
	_ = json.Unmarshal(result.ResourceProperties, &got)
	if got["userId"] != userID {
		t.Errorf("userId = %v, want %v", got["userId"], userID)
	}
	if got["methodType"] != "email_contact_method" {
		t.Errorf("methodType = %v", got["methodType"])
	}
	if _, ok := got["id"]; !ok {
		t.Errorf("ResourceProperties missing id field; got keys %v", keys(got))
	}
}

func TestContactMethod_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(contactMethodResourceType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, minimalContactMethodProps(userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}

	read, err := h.Read(ctx(t), client, created.NativeID)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if read.ErrorCode != "" {
		t.Fatalf("Read ErrorCode = %q", read.ErrorCode)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(read.Properties), &got)
	if got["userId"] != userID {
		t.Errorf("Read userId = %v, want %v", got["userId"], userID)
	}
}

func TestContactMethod_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(contactMethodResourceType)
	read, _ := h.Read(ctx(t), client, "PXXXXXX:PYYYYYY")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestContactMethod_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(contactMethodResourceType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, minimalContactMethodProps(userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	var base map[string]any
	_ = json.Unmarshal(created.ResourceProperties, &base)

	desired, _ := json.Marshal(map[string]any{
		"userId":      userID,
		"methodType":  "email_contact_method",
		"methodLabel": "Personal", // CHANGED
		"address":     base["address"],
	})
	updated, err := h.Update(ctx(t), client, created.NativeID, created.ResourceProperties, desired)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Update status = %v: %s", updated.OperationStatus, updated.StatusMessage)
	}
	var got map[string]any
	_ = json.Unmarshal(updated.ResourceProperties, &got)
	if got["methodLabel"] != "Personal" {
		t.Errorf("methodLabel = %v, want Personal", got["methodLabel"])
	}
}

func TestContactMethod_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(contactMethodResourceType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, minimalContactMethodProps(userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	deleted, err := h.Delete(ctx(t), client, created.NativeID)
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Delete status = %v", deleted.OperationStatus)
	}
	read, _ := h.Read(ctx(t), client, created.NativeID)
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("after delete, Read ErrorCode = %q", read.ErrorCode)
	}
}

func TestContactMethod_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(contactMethodResourceType)
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX:PYYYYYY")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestContactMethod_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(contactMethodResourceType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, minimalContactMethodProps(userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}

	listResult, err := h.List(ctx(t), client, 100, nil)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	found := false
	for _, id := range listResult.NativeIDs {
		if id == created.NativeID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created contact method %s not in List output (%d entries)", created.NativeID, len(listResult.NativeIDs))
	}
}
