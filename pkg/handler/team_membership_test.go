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

const teamMembershipResourceType = "PAGERDUTY::Core::TeamMembership"

func cleanupTeamMembership(t *testing.T, client *pagerduty.Client, teamID, userID string) {
	t.Helper()
	if teamID == "" || userID == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.RemoveUserFromTeamWithContext(cleanupCtx, teamID, userID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: remove user %s from team %s: %v", userID, teamID, err)
	}
}

// teamMembershipSetup creates a team and a user to join it, returning their ids.
func teamMembershipSetup(t *testing.T, client *pagerduty.Client) (teamID, userID string) {
	t.Helper()
	userID = scheduleSetupUser(t, client)
	th, _ := Get(teamResourceType)
	props, _ := json.Marshal(map[string]any{"name": uniqueName("Membership Team")})
	res, err := th.Create(ctx(t), client, props)
	if err != nil || res.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup team: %v %s", err, res.StatusMessage)
	}
	teamID = res.NativeID
	t.Cleanup(func() { cleanupTeam(t, client, teamID) })
	return teamID, userID
}

func minimalTeamMembershipProps(teamID, userID, role string) []byte {
	props, _ := json.Marshal(map[string]any{
		"teamId": teamID,
		"userId": userID,
		"role":   role,
	})
	return props
}

func TestTeamMembership_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(teamMembershipResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	teamID, userID := teamMembershipSetup(t, client)

	result, err := h.Create(ctx(t), client, minimalTeamMembershipProps(teamID, userID, "manager"))
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	t.Cleanup(func() { cleanupTeamMembership(t, client, teamID, userID) })
	if !strings.Contains(result.NativeID, ":") {
		t.Errorf("NativeID %q should be composite teamID:userID", result.NativeID)
	}
	var got map[string]any
	_ = json.Unmarshal(result.ResourceProperties, &got)
	if got["role"] != "manager" {
		t.Errorf("role = %v, want manager", got["role"])
	}
}

func TestTeamMembership_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamMembershipResourceType)
	teamID, userID := teamMembershipSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalTeamMembershipProps(teamID, userID, "manager"))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupTeamMembership(t, client, teamID, userID) })

	read, err := h.Read(ctx(t), client, created.NativeID)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if read.ErrorCode != "" {
		t.Fatalf("Read ErrorCode = %q", read.ErrorCode)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(read.Properties), &got)
	if got["userId"] != userID || got["teamId"] != teamID {
		t.Errorf("Read = %v, want teamId=%s userId=%s", got, teamID, userID)
	}
}

func TestTeamMembership_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamMembershipResourceType)
	teamID, _ := teamMembershipSetup(t, client)
	// Valid team, user that is not a member.
	read, _ := h.Read(ctx(t), client, teamID+":PXXXXXX")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestTeamMembership_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamMembershipResourceType)
	teamID, userID := teamMembershipSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalTeamMembershipProps(teamID, userID, "manager"))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupTeamMembership(t, client, teamID, userID) })

	updated, err := h.Update(ctx(t), client, created.NativeID, created.ResourceProperties, minimalTeamMembershipProps(teamID, userID, "responder"))
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Update status = %v: %s", updated.OperationStatus, updated.StatusMessage)
	}
	var got map[string]any
	_ = json.Unmarshal(updated.ResourceProperties, &got)
	if got["role"] != "responder" {
		t.Errorf("role = %v, want responder", got["role"])
	}
}

func TestTeamMembership_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamMembershipResourceType)
	teamID, userID := teamMembershipSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalTeamMembershipProps(teamID, userID, "manager"))
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

func TestTeamMembership_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamMembershipResourceType)
	teamID, _ := teamMembershipSetup(t, client)
	deleted, err := h.Delete(ctx(t), client, teamID+":PXXXXXX")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestTeamMembership_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(teamMembershipResourceType)
	teamID, userID := teamMembershipSetup(t, client)

	created, err := h.Create(ctx(t), client, minimalTeamMembershipProps(teamID, userID, "manager"))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	t.Cleanup(func() { cleanupTeamMembership(t, client, teamID, userID) })

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
		t.Errorf("created membership %s not in List output (%d entries)", created.NativeID, len(listResult.NativeIDs))
	}
}
