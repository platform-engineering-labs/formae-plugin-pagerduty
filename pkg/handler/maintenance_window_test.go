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

const maintenanceWindowResourceType = "PAGERDUTY::Core::MaintenanceWindow"

func cleanupMaintenanceWindow(t *testing.T, client *pagerduty.Client, id string) {
	t.Helper()
	if id == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeleteMaintenanceWindowWithContext(cleanupCtx, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete maintenance window %s: %v", id, err)
	}
}

func minimalMaintenanceWindowProps(serviceID string) []byte {
	props, _ := json.Marshal(map[string]any{
		"startTime":   "2027-03-01T00:00:00Z",
		"endTime":     "2027-03-01T02:00:00Z",
		"description": "Conformance maintenance",
		"serviceIds":  []string{serviceID},
	})
	return props
}

func TestMaintenanceWindow_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(maintenanceWindowResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	svcID := integrationSetupService(t, client)

	result, err := h.Create(ctx(t), client, minimalMaintenanceWindowProps(svcID))
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	if result.NativeID == "" {
		t.Fatal("empty NativeID")
	}
	t.Cleanup(func() { cleanupMaintenanceWindow(t, client, result.NativeID) })
	var got map[string]any
	_ = json.Unmarshal(result.ResourceProperties, &got)
	if got["startTime"] != "2027-03-01T00:00:00Z" {
		t.Errorf("startTime = %v, want UTC-normalized 2027-03-01T00:00:00Z", got["startTime"])
	}
	svcs, _ := got["serviceIds"].([]any)
	if len(svcs) != 1 || svcs[0] != svcID {
		t.Errorf("serviceIds = %v, want [%s]", got["serviceIds"], svcID)
	}
}

func TestMaintenanceWindow_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(maintenanceWindowResourceType)
	svcID := integrationSetupService(t, client)

	created, err := h.Create(ctx(t), client, minimalMaintenanceWindowProps(svcID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupMaintenanceWindow(t, client, created.NativeID) })

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
		t.Errorf("Read id = %v, want %v", got["id"], created.NativeID)
	}
	if got["startTime"] != "2027-03-01T00:00:00Z" {
		t.Errorf("Read startTime = %v, want UTC-normalized", got["startTime"])
	}
}

func TestMaintenanceWindow_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(maintenanceWindowResourceType)
	read, _ := h.Read(ctx(t), client, "PXXXXXX")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestMaintenanceWindow_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(maintenanceWindowResourceType)
	svcID := integrationSetupService(t, client)

	created, err := h.Create(ctx(t), client, minimalMaintenanceWindowProps(svcID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupMaintenanceWindow(t, client, created.NativeID) })

	desired, _ := json.Marshal(map[string]any{
		"startTime":   "2027-03-01T00:00:00Z",
		"endTime":     "2027-03-01T02:00:00Z",
		"description": "Updated maintenance", // CHANGED
		"serviceIds":  []string{svcID},
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
	if got["description"] != "Updated maintenance" {
		t.Errorf("description = %v", got["description"])
	}
}

func TestMaintenanceWindow_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(maintenanceWindowResourceType)
	svcID := integrationSetupService(t, client)

	created, err := h.Create(ctx(t), client, minimalMaintenanceWindowProps(svcID))
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

func TestMaintenanceWindow_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(maintenanceWindowResourceType)
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestMaintenanceWindow_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(maintenanceWindowResourceType)
	svcID := integrationSetupService(t, client)

	created, err := h.Create(ctx(t), client, minimalMaintenanceWindowProps(svcID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupMaintenanceWindow(t, client, created.NativeID) })

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
		t.Errorf("created maintenance window %s not in List output (%d entries)", created.NativeID, len(listResult.NativeIDs))
	}
}
