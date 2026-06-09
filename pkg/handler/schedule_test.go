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

const scheduleResourceType = "PAGERDUTY::Core::Schedule"

func cleanupSchedule(t *testing.T, client *pagerduty.Client, id string) {
	t.Helper()
	if id == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeleteScheduleWithContext(cleanupCtx, id); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete schedule %s: %v", id, err)
	}
}

// scheduleSetupUser creates a user to anchor a schedule layer, returning its id.
func scheduleSetupUser(t *testing.T, client *pagerduty.Client) string {
	t.Helper()
	h, _ := Get("PAGERDUTY::Core::User")
	props, _ := json.Marshal(map[string]any{
		"name":  uniqueName("Schedule Anchor"),
		"email": uniqueEmail("sched"),
		"role":  "user",
	})
	res, err := h.Create(ctx(t), client, props)
	if err != nil || res.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup user: %v %s", err, res.StatusMessage)
	}
	id := res.NativeID
	t.Cleanup(func() { cleanupUser(t, client, id) })
	return id
}

func minimalSchedulePropsWithDailyRestriction(name string, userID string) []byte {
	props, _ := json.Marshal(map[string]any{
		"name":     name,
		"timeZone": "America/Los_Angeles",
		"scheduleLayers": []map[string]any{
			{
				"name":                      "Layer 1",
				"start":                     "2026-01-01T00:00:00Z",
				"rotationVirtualStart":      "2026-01-01T00:00:00Z",
				"rotationTurnLengthSeconds": 86400,
				"users":                     []string{userID},
				"restrictions": []map[string]any{
					{
						"type":            "daily_restriction",
						"startTimeOfDay":  "08:00:00",
						"durationSeconds": 28800,
					},
				},
			},
		},
	})
	return props
}

func TestSchedule_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(scheduleResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	userID := scheduleSetupUser(t, client)

	props := minimalSchedulePropsWithDailyRestriction(uniqueName("Create Sched"), userID)
	result, err := h.Create(ctx(t), client, props)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	t.Cleanup(func() { cleanupSchedule(t, client, result.NativeID) })

	var got map[string]any
	_ = json.Unmarshal(result.ResourceProperties, &got)
	if got["timeZone"] != "America/Los_Angeles" {
		t.Errorf("timeZone = %v", got["timeZone"])
	}
}

func TestSchedule_Create_WeeklyRestriction(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleResourceType)
	userID := scheduleSetupUser(t, client)

	props, _ := json.Marshal(map[string]any{
		"name":     uniqueName("Weekly Sched"),
		"timeZone": "America/Los_Angeles",
		"scheduleLayers": []map[string]any{
			{
				"start":                     "2026-01-01T00:00:00Z",
				"rotationVirtualStart":      "2026-01-01T00:00:00Z",
				"rotationTurnLengthSeconds": 604800,
				"users":                     []string{userID},
				"restrictions": []map[string]any{
					{
						"type":            "weekly_restriction",
						"startTimeOfDay":  "09:00:00",
						"durationSeconds": 28800,
						"startDayOfWeek":  2,
					},
				},
			},
		},
	})
	result, err := h.Create(ctx(t), client, props)
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	t.Cleanup(func() { cleanupSchedule(t, client, result.NativeID) })
}

func TestSchedule_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleResourceType)
	userID := scheduleSetupUser(t, client)

	props := minimalSchedulePropsWithDailyRestriction(uniqueName("Read Sched"), userID)
	created, err := h.Create(ctx(t), client, props)
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupSchedule(t, client, created.NativeID) })

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
		t.Errorf("Read id = %v", got["id"])
	}
}

func TestSchedule_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleResourceType)
	read, _ := h.Read(ctx(t), client, "PXXXXXX")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestSchedule_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleResourceType)
	userID := scheduleSetupUser(t, client)

	name := uniqueName("Update Sched")
	created, err := h.Create(ctx(t), client, minimalSchedulePropsWithDailyRestriction(name, userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupSchedule(t, client, created.NativeID) })

	desired, _ := json.Marshal(map[string]any{
		"name":        name,
		"timeZone":    "America/Los_Angeles",
		"description": "updated",
		"scheduleLayers": []map[string]any{
			{
				"name":                      "Layer 1",
				"start":                     "2026-01-01T00:00:00Z",
				"rotationVirtualStart":      "2026-01-01T00:00:00Z",
				"rotationTurnLengthSeconds": 86400,
				"users":                     []string{userID},
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
	var got map[string]any
	_ = json.Unmarshal(updated.ResourceProperties, &got)
	if got["description"] != "updated" {
		t.Errorf("description = %v", got["description"])
	}
}

func TestSchedule_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleResourceType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, minimalSchedulePropsWithDailyRestriction(uniqueName("Delete Sched"), userID))
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

func TestSchedule_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleResourceType)
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestSchedule_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleResourceType)
	userID := scheduleSetupUser(t, client)

	created, err := h.Create(ctx(t), client, minimalSchedulePropsWithDailyRestriction(uniqueName("List Sched"), userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupSchedule(t, client, created.NativeID) })

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
		t.Errorf("created schedule %s not in first List page (size %d)", created.NativeID, len(listResult.NativeIDs))
	}
}
