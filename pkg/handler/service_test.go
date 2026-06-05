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

const serviceResourceType = "PAGERDUTY::Core::Service"

func cleanupService(t *testing.T, client *pagerduty.Client, id string) {
	t.Helper()
	if id == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeleteServiceWithContext(cleanupCtx, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete service %s: %v", id, err)
	}
}

// serviceSetupEP creates a User + minimal EP for service tests. Returns EP id.
func serviceSetupEP(t *testing.T, client *pagerduty.Client) string {
	t.Helper()
	userID := scheduleSetupUser(t, client)
	h, _ := Get(escalationPolicyType)
	res, err := h.Create(ctx(t), client, epUserTargetProps(uniqueName("Svc EP"), userID))
	if err != nil || res.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup EP: %v %s", err, res.StatusMessage)
	}
	epID := res.NativeID
	t.Cleanup(func() { cleanupEscalationPolicy(t, client, epID) })
	return epID
}

func minimalServiceProps(name, epID string) []byte {
	props, _ := json.Marshal(map[string]any{
		"name":               name,
		"escalationPolicyId": epID,
	})
	return props
}

func TestService_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(serviceResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	epID := serviceSetupEP(t, client)

	result, err := h.Create(ctx(t), client, minimalServiceProps(uniqueName("Create Svc"), epID))
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	t.Cleanup(func() { cleanupService(t, client, result.NativeID) })

	if result.NativeID == "" {
		t.Fatal("empty NativeID")
	}
}

func TestService_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(serviceResourceType)
	epID := serviceSetupEP(t, client)

	created, err := h.Create(ctx(t), client, minimalServiceProps(uniqueName("Read Svc"), epID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupService(t, client, created.NativeID) })

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
	if got["escalationPolicyId"] != epID {
		t.Errorf("escalationPolicyId = %v, want %v", got["escalationPolicyId"], epID)
	}
}

func TestService_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(serviceResourceType)
	read, _ := h.Read(ctx(t), client, "PXXXXXX")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestService_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(serviceResourceType)
	epID := serviceSetupEP(t, client)

	name := uniqueName("Update Svc")
	created, err := h.Create(ctx(t), client, minimalServiceProps(name, epID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupService(t, client, created.NativeID) })

	desired, _ := json.Marshal(map[string]any{
		"name":               name,
		"description":        "updated",
		"escalationPolicyId": epID,
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
	if got["description"] != "updated" {
		t.Errorf("description = %v", got["description"])
	}
}

func TestService_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(serviceResourceType)
	epID := serviceSetupEP(t, client)

	created, err := h.Create(ctx(t), client, minimalServiceProps(uniqueName("Delete Svc"), epID))
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

func TestService_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(serviceResourceType)
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestService_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(serviceResourceType)
	epID := serviceSetupEP(t, client)

	created, err := h.Create(ctx(t), client, minimalServiceProps(uniqueName("List Svc"), epID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupService(t, client, created.NativeID) })

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
		t.Errorf("created Service %s not in first List page (size %d)", created.NativeID, len(listResult.NativeIDs))
	}
}
