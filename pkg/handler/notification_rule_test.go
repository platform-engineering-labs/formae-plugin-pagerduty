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

const notificationRuleResourceType = "PAGERDUTY::Core::NotificationRule"

func cleanupNotificationRule(t *testing.T, client *pagerduty.Client, userID, ruleID string) {
	t.Helper()
	if userID == "" || ruleID == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeleteUserNotificationRuleWithContext(cleanupCtx, userID, ruleID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete notification rule %s/%s: %v", userID, ruleID, err)
	}
}

// notificationRuleSetup creates a user and a contact method on it, returning the
// user id and the contact method id (the rule points at the contact method).
func notificationRuleSetup(t *testing.T, client *pagerduty.Client) (userID, cmID string) {
	t.Helper()
	userID = scheduleSetupUser(t, client)
	cmComposite := setupContactMethod(t, client, userID)
	_, cmID, err := splitNativeID(cmComposite)
	if err != nil {
		t.Fatalf("unexpected contact method nativeID %q: %v", cmComposite, err)
	}
	return userID, cmID
}

func minimalNotificationRuleProps(userID, cmID string) []byte {
	props, _ := json.Marshal(map[string]any{
		"userId":              userID,
		"contactMethodId":     cmID,
		"startDelayInMinutes": 0,
		"urgency":             "high",
	})
	return props
}

func TestNotificationRule_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(notificationRuleResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	userID, cmID := notificationRuleSetup(t, client)

	result, err := h.Create(ctx(t), client, minimalNotificationRuleProps(userID, cmID))
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	if !strings.Contains(result.NativeID, ":") {
		t.Errorf("NativeID %q should be composite userID:ruleID", result.NativeID)
	}
	var got map[string]any
	_ = json.Unmarshal(result.ResourceProperties, &got)
	if got["contactMethodId"] != cmID {
		t.Errorf("contactMethodId = %v, want %v", got["contactMethodId"], cmID)
	}
	if got["urgency"] != "high" {
		t.Errorf("urgency = %v", got["urgency"])
	}
}

func TestNotificationRule_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(notificationRuleResourceType)
	userID, cmID := notificationRuleSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalNotificationRuleProps(userID, cmID))
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

func TestNotificationRule_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(notificationRuleResourceType)
	read, _ := h.Read(ctx(t), client, "PXXXXXX:PYYYYYY")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestNotificationRule_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(notificationRuleResourceType)
	userID, cmID := notificationRuleSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalNotificationRuleProps(userID, cmID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}

	desired, _ := json.Marshal(map[string]any{
		"userId":              userID,
		"contactMethodId":     cmID,
		"startDelayInMinutes": 5, // CHANGED
		"urgency":             "high",
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
	if v, _ := got["startDelayInMinutes"].(float64); v != 5 {
		t.Errorf("startDelayInMinutes = %v, want 5", got["startDelayInMinutes"])
	}
}

func TestNotificationRule_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(notificationRuleResourceType)
	userID, cmID := notificationRuleSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalNotificationRuleProps(userID, cmID))
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
	readUntilGone(t, ctx(t), client, h, created.NativeID)
}

func TestNotificationRule_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(notificationRuleResourceType)
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX:PYYYYYY")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestNotificationRule_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(notificationRuleResourceType)
	userID, cmID := notificationRuleSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalNotificationRuleProps(userID, cmID))
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
		t.Errorf("created notification rule %s not in List output (%d entries)", created.NativeID, len(listResult.NativeIDs))
	}
}
