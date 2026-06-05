// © 2026 Platform Engineering Labs
//
// SPDX-License-Identifier: Apache-2.0

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

const escalationPolicyType = "PAGERDUTY::Core::EscalationPolicy"

func cleanupEscalationPolicy(t *testing.T, client *pagerduty.Client, id string) {
	t.Helper()
	if id == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeleteEscalationPolicyWithContext(cleanupCtx, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete escalation policy %s: %v", id, err)
	}
}

func epUserTargetProps(name, userID string) []byte {
	props, _ := json.Marshal(map[string]any{
		"name": name,
		"escalationRules": []map[string]any{
			{
				"escalationDelayInMinutes": 10,
				"targets": []map[string]any{
					{"type": "user_reference", "id": userID},
				},
			},
		},
	})
	return props
}

func TestEscalationPolicy_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(escalationPolicyType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	userID := scheduleSetupUser(t, client)

	result, err := h.Create(ctx(t), client, epUserTargetProps(uniqueName("Create EP"), userID))
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	t.Cleanup(func() { cleanupEscalationPolicy(t, client, result.NativeID) })
	if result.NativeID == "" {
		t.Fatal("empty NativeID")
	}
}

func TestEscalationPolicy_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(escalationPolicyType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, epUserTargetProps(uniqueName("Read EP"), userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupEscalationPolicy(t, client, created.NativeID) })

	read, err := h.Read(ctx(t), client, created.NativeID)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if read.ErrorCode != "" {
		t.Fatalf("Read ErrorCode = %q", read.ErrorCode)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(read.Properties), &got)
	if got["id"] != created.NativeID {
		t.Errorf("id = %v", got["id"])
	}
}

func TestEscalationPolicy_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(escalationPolicyType)
	read, _ := h.Read(ctx(t), client, "PXXXXXX")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestEscalationPolicy_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(escalationPolicyType)
	userID := scheduleSetupUser(t, client)

	name := uniqueName("Update EP")
	created, err := h.Create(ctx(t), client, epUserTargetProps(name, userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupEscalationPolicy(t, client, created.NativeID) })

	desired, _ := json.Marshal(map[string]any{
		"name":        name,
		"description": "updated",
		"escalationRules": []map[string]any{
			{
				"escalationDelayInMinutes": 15,
				"targets": []map[string]any{
					{"type": "user_reference", "id": userID},
				},
			},
		},
	})
	updated, err := h.Update(ctx(t), client, created.NativeID, created.ResourceProperties, desired)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Update status = %v: %s", updated.OperationStatus, updated.StatusMessage)
	}
}

func TestEscalationPolicy_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(escalationPolicyType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, epUserTargetProps(uniqueName("Delete EP"), userID))
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

func TestEscalationPolicy_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(escalationPolicyType)
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestEscalationPolicy_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(escalationPolicyType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, epUserTargetProps(uniqueName("List EP"), userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupEscalationPolicy(t, client, created.NativeID) })

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
		t.Errorf("created EP %s not in first List page (size %d)", created.NativeID, len(listResult.NativeIDs))
	}
}
