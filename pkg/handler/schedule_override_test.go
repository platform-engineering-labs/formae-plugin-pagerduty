// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

//go:build integration

package handler

import (
	"encoding/json"
	"strings"
	"testing"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const scheduleOverrideResourceType = "PAGERDUTY::Core::ScheduleOverride"

// Future times in the schedule's own UTC offset (PST). PagerDuty rejects/clamps
// past overrides; a future window in the zone's offset round-trips predictably.
const overrideStart = "2027-03-01T00:00:00-08:00"
const overrideEnd = "2027-03-02T00:00:00-08:00"

// scheduleOverrideSetup creates a user and a schedule for an override to live
// on, returning their ids.
func scheduleOverrideSetup(t *testing.T, client *pagerduty.Client) (scheduleID, userID string) {
	t.Helper()
	userID = scheduleSetupUser(t, client)
	sh, _ := Get(scheduleResourceType)
	scheduleID = createPrereq(t, ctx(t), client, sh, minimalSchedulePropsWithDailyRestriction(uniqueName("Override Sched"), userID), "schedule")
	t.Cleanup(func() { cleanupSchedule(t, client, scheduleID) })
	return scheduleID, userID
}

func minimalScheduleOverrideProps(scheduleID, userID string) []byte {
	props, _ := json.Marshal(map[string]any{
		"scheduleId": scheduleID,
		"userId":     userID,
		"start":      overrideStart,
		"end":        overrideEnd,
	})
	return props
}

func TestScheduleOverride_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(scheduleOverrideResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	scheduleID, userID := scheduleOverrideSetup(t, client)

	result, err := h.Create(ctx(t), client, minimalScheduleOverrideProps(scheduleID, userID))
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	if !strings.Contains(result.NativeID, ":") {
		t.Errorf("NativeID %q should be composite scheduleID:overrideID", result.NativeID)
	}
	// Log the round-tripped times so conformance testdata can match PD's echo.
	t.Logf("override created props: %s", string(result.ResourceProperties))
	var got map[string]any
	_ = json.Unmarshal(result.ResourceProperties, &got)
	if got["userId"] != userID {
		t.Errorf("userId = %v, want %v", got["userId"], userID)
	}
}

func TestScheduleOverride_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleOverrideResourceType)
	scheduleID, userID := scheduleOverrideSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalScheduleOverrideProps(scheduleID, userID))
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
	t.Logf("override read props: %s", read.Properties)
	var got map[string]any
	_ = json.Unmarshal([]byte(read.Properties), &got)
	if got["userId"] != userID {
		t.Errorf("Read userId = %v, want %v", got["userId"], userID)
	}
}

func TestScheduleOverride_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleOverrideResourceType)
	scheduleID, _ := scheduleOverrideSetup(t, client)
	// Valid schedule, missing override id.
	read, _ := h.Read(ctx(t), client, scheduleID+":PXXXXXX")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestScheduleOverride_UpdateImmutable(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleOverrideResourceType)
	scheduleID, userID := scheduleOverrideSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalScheduleOverrideProps(scheduleID, userID))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	// Overrides are immutable: Update must report a failure rather than silently
	// no-op or duplicate. The schema marks every field createOnly so formae
	// drives changes through Replace, but a defensive Update still guards.
	res, err := h.Update(ctx(t), client, created.NativeID, created.ResourceProperties, minimalScheduleOverrideProps(scheduleID, userID))
	if err == nil && res.OperationStatus == resource.OperationStatusSuccess {
		t.Errorf("Update should fail for immutable override, got success")
	}
}

func TestScheduleOverride_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleOverrideResourceType)
	scheduleID, userID := scheduleOverrideSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalScheduleOverrideProps(scheduleID, userID))
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

func TestScheduleOverride_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleOverrideResourceType)
	scheduleID, _ := scheduleOverrideSetup(t, client)
	deleted, err := h.Delete(ctx(t), client, scheduleID+":PXXXXXX")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestScheduleOverride_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(scheduleOverrideResourceType)
	scheduleID, userID := scheduleOverrideSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalScheduleOverrideProps(scheduleID, userID))
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
		t.Errorf("created override %s not in List output (%d entries)", created.NativeID, len(listResult.NativeIDs))
	}
}
