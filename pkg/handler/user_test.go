// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

//go:build integration

package handler

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

func TestUser_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get("PAGERDUTY::Core::User")
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}

	email := uniqueEmail("create")
	props, _ := json.Marshal(map[string]any{
		"name":  uniqueName("Create User"),
		"email": email,
		"role":  "user",
	})

	result, err := h.Create(ctx(t), client, props)
	if err != nil {
		t.Fatalf("Create returned error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v, want Success; message=%q", result.OperationStatus, result.StatusMessage)
	}
	if result.NativeID == "" {
		t.Fatal("Create returned empty NativeID")
	}
	t.Cleanup(func() { cleanupUser(t, client, result.NativeID) })

	// ResourceProperties should round-trip the name and email back.
	var got map[string]any
	if err := json.Unmarshal(result.ResourceProperties, &got); err != nil {
		t.Fatalf("ResourceProperties not valid JSON: %v", err)
	}
	if got["email"] != email {
		t.Errorf("email = %v, want %v", got["email"], email)
	}
	if _, ok := got["id"]; !ok {
		t.Errorf("ResourceProperties missing id field; got keys %v", keys(got))
	}
}

func keys(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func TestUser_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get("PAGERDUTY::Core::User")

	createProps, _ := json.Marshal(map[string]any{
		"name":  uniqueName("Read User"),
		"email": uniqueEmail("read"),
		"role":  "user",
	})
	created, err := h.Create(ctx(t), client, createProps)
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup create failed: err=%v msg=%s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupUser(t, client, created.NativeID) })

	read, err := h.Read(ctx(t), client, created.NativeID)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if read.ErrorCode != "" {
		t.Fatalf("Read ErrorCode = %q", read.ErrorCode)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(read.Properties), &got); err != nil {
		t.Fatalf("Read Properties not valid JSON: %v", err)
	}
	if got["id"] != created.NativeID {
		t.Errorf("Read id = %v, want %v", got["id"], created.NativeID)
	}
}

func TestUser_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get("PAGERDUTY::Core::User")

	// PagerDuty user IDs are 7-char base32-ish strings. A made-up id should 404.
	read, err := h.Read(ctx(t), client, "PXXXXXX")
	if err != nil && read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Fatalf("Read of missing user: err=%v code=%q", err, read.ErrorCode)
	}
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestUser_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get("PAGERDUTY::Core::User")

	originalName := uniqueName("Update User")
	createProps, _ := json.Marshal(map[string]any{
		"name":  originalName,
		"email": uniqueEmail("update"),
		"role":  "user",
	})
	created, err := h.Create(ctx(t), client, createProps)
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup create failed: err=%v msg=%s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupUser(t, client, created.NativeID) })

	newDescription := "Updated by formae test"
	desired, _ := json.Marshal(map[string]any{
		"name":        originalName,
		"email":       jsonField(created.ResourceProperties, "email"),
		"role":        "user",
		"description": newDescription,
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
	if got["description"] != newDescription {
		t.Errorf("description = %v, want %v", got["description"], newDescription)
	}
}

func TestUser_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get("PAGERDUTY::Core::User")

	createProps, _ := json.Marshal(map[string]any{
		"name":  uniqueName("Delete User"),
		"email": uniqueEmail("delete"),
		"role":  "user",
	})
	created, err := h.Create(ctx(t), client, createProps)
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup create failed: err=%v msg=%s", err, created.StatusMessage)
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
		t.Errorf("after delete, Read ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestUser_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get("PAGERDUTY::Core::User")

	// Deleting a non-existent user must succeed (idempotency).
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX")
	if err != nil {
		t.Fatalf("Delete returned error for missing user: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestUser_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get("PAGERDUTY::Core::User")

	createProps, _ := json.Marshal(map[string]any{
		"name":  uniqueName("List User"),
		"email": uniqueEmail("list"),
		"role":  "user",
	})
	created, err := h.Create(ctx(t), client, createProps)
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup create failed: err=%v msg=%s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupUser(t, client, created.NativeID) })

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
		t.Errorf("created user %s not in first List page (size %d)", created.NativeID, len(listResult.NativeIDs))
	}
}

// jsonField pulls a string field out of a JSON object. Helper for Update tests
// that need to round-trip an immutable field.
func jsonField(raw json.RawMessage, key string) string {
	var m map[string]any
	_ = json.Unmarshal(raw, &m)
	s, _ := m[key].(string)
	return s
}

// Sanity: when Create is malformed, handler should return a Failure result, not panic.
func TestUser_Create_RejectsMalformedProps(t *testing.T) {
	client := testClient(t)
	h, err := Get("PAGERDUTY::Core::User")
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	result, err := h.Create(ctx(t), client, json.RawMessage(`{"not_a_real_field": "x"}`))
	if err == nil && result.OperationStatus == resource.OperationStatusSuccess {
		t.Fatalf("expected failure for malformed props; got success: %s", result.StatusMessage)
	}
	// Best-effort cleanup if PD somehow accepted it
	if result != nil && result.NativeID != "" {
		t.Cleanup(func() { cleanupUser(t, client, result.NativeID) })
	}
	if err != nil {
		_ = fmt.Errorf("(propagating-fail-OK) %v", err)
	}
}
