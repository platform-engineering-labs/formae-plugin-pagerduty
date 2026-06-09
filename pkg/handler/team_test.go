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

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const teamResourceType = "PAGERDUTY::Core::Team"

func cleanupTeam(t *testing.T, client *pagerduty.Client, id string) {
	t.Helper()
	if id == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*1000_000_000)
	defer cancel()
	if err := client.DeleteTeamWithContext(cleanupCtx, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete team %s: %v", id, err)
	}
}

func TestTeam_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(teamResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}

	props, _ := json.Marshal(map[string]any{
		"name":        uniqueName("Create Team"),
		"description": "formae plugin test",
	})
	result, err := h.Create(ctx(t), client, props)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	t.Cleanup(func() { cleanupTeam(t, client, result.NativeID) })

	if result.NativeID == "" {
		t.Fatal("empty NativeID")
	}
	var got map[string]any
	_ = json.Unmarshal(result.ResourceProperties, &got)
	if got["description"] != "formae plugin test" {
		t.Errorf("description = %v", got["description"])
	}
}

func TestTeam_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamResourceType)

	createProps, _ := json.Marshal(map[string]any{"name": uniqueName("Read Team")})
	created, err := h.Create(ctx(t), client, createProps)
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupTeam(t, client, created.NativeID) })

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
}

func TestTeam_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamResourceType)
	read, _ := h.Read(ctx(t), client, "PXXXXXX")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestTeam_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamResourceType)

	originalName := uniqueName("Update Team")
	createProps, _ := json.Marshal(map[string]any{"name": originalName})
	created, err := h.Create(ctx(t), client, createProps)
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupTeam(t, client, created.NativeID) })

	desired, _ := json.Marshal(map[string]any{
		"name":        originalName,
		"description": "updated",
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
		t.Errorf("description = %v, want updated", got["description"])
	}
}

func TestTeam_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamResourceType)

	createProps, _ := json.Marshal(map[string]any{"name": uniqueName("Delete Team")})
	created, err := h.Create(ctx(t), client, createProps)
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

func TestTeam_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamResourceType)
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX")
	if err != nil {
		t.Fatalf("Delete error for missing team: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestTeam_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamResourceType)

	createProps, _ := json.Marshal(map[string]any{"name": uniqueName("List Team")})
	created, err := h.Create(ctx(t), client, createProps)
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupTeam(t, client, created.NativeID) })

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
		t.Errorf("created team %s not in first List page (size %d)", created.NativeID, len(listResult.NativeIDs))
	}
}
