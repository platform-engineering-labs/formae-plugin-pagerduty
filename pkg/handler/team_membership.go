// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const teamMembershipType = "PAGERDUTY::Core::TeamMembership"

func init() {
	Register(teamMembershipType, &teamMembershipHandler{})
}

// teamMembershipProps mirrors the PKL TeamMembership schema. A membership has no
// id of its own - identity is the (teamId, userId) pair (both immutable).
type teamMembershipProps struct {
	TeamID string `json:"teamId"`
	UserID string `json:"userId"`
	Role   string `json:"role,omitempty"`
}

type teamMembershipHandler struct{}

// findMembership scans a team's members for userID and returns the membership
// (with its server-side role). found is false if the user is not a member.
func findMembership(ctx context.Context, client *pagerduty.Client, teamID, userID string) (teamMembershipProps, bool, error) {
	var offset uint
	for {
		resp, err := client.ListTeamMembers(ctx, teamID, pagerduty.ListTeamMembersOptions{Limit: 100, Offset: offset})
		if err != nil {
			return teamMembershipProps{}, false, err
		}
		for _, m := range resp.Members {
			if m.User.ID == userID {
				return teamMembershipProps{TeamID: teamID, UserID: userID, Role: m.Role}, true, nil
			}
		}
		if !resp.More {
			return teamMembershipProps{}, false, nil
		}
		offset += 100
	}
}

// membershipAfterWrite reads back a membership after an add, absorbing
// PagerDuty's brief read-after-write lag: the just-written member can momentarily
// be missing or carry the old role. Returns the converged membership, or the
// last seen one if the role hasn't propagated within the window. A transient
// disappearance never clobbers a membership already seen; if it is never seen,
// that's an error rather than a bogus zero-value success.
func membershipAfterWrite(ctx context.Context, client *pagerduty.Client, teamID, userID, wantRole string) (teamMembershipProps, error) {
	var found teamMembershipProps
	var seen bool
	m, err := retryUntilReadable(ctx, 5, 500*time.Millisecond, func() (teamMembershipProps, bool, error) {
		cur, ok, err := findMembership(ctx, client, teamID, userID)
		if err != nil {
			return found, false, err
		}
		if !ok {
			return found, false, nil // keep the last seen membership, if any
		}
		found, seen = cur, true
		return cur, wantRole == "" || cur.Role == wantRole, nil
	})
	if err != nil {
		return teamMembershipProps{}, err
	}
	if !seen {
		return teamMembershipProps{}, fmt.Errorf("team membership %s:%s not found after add", teamID, userID)
	}
	return m, nil
}

func (teamMembershipHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props teamMembershipProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode team membership: %v", err)), nil
	}
	if props.TeamID == "" || props.UserID == "" {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "teamId and userId are required"), nil
	}
	opts := pagerduty.AddUserToTeamOptions{TeamID: props.TeamID, UserID: props.UserID, Role: pagerduty.TeamUserRole(props.Role)}
	if err := client.AddUserToTeamWithContext(ctx, opts); err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	m, err := membershipAfterWrite(ctx, client, props.TeamID, props.UserID, props.Role)
	if err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal team membership: %v", err)), nil
	}
	return SuccessResult(resource.OperationCreate, compositeNativeID(props.TeamID, props.UserID), out), nil
}

func (teamMembershipHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	teamID, userID, err := splitNativeID(nativeID)
	if err != nil {
		return &resource.ReadResult{ResourceType: teamMembershipType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	m, found, err := findMembership(ctx, client, teamID, userID)
	if err != nil {
		if IsNotFound(err) {
			// The team itself is gone, so the membership is too.
			return &resource.ReadResult{ResourceType: teamMembershipType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: teamMembershipType, ErrorCode: MapAPIError(err)}, nil
	}
	if !found {
		return &resource.ReadResult{ResourceType: teamMembershipType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return &resource.ReadResult{ResourceType: teamMembershipType, ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
	}
	return &resource.ReadResult{ResourceType: teamMembershipType, Properties: string(out)}, nil
}

// Update changes the role. PagerDuty has no distinct update endpoint for
// membership: re-adding the user with a new role upserts it.
func (teamMembershipHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	teamID, userID, err := splitNativeID(nativeID)
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	var props teamMembershipProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode team membership: %v", err)), nil
	}
	opts := pagerduty.AddUserToTeamOptions{TeamID: teamID, UserID: userID, Role: pagerduty.TeamUserRole(props.Role)}
	if err := client.AddUserToTeamWithContext(ctx, opts); err != nil {
		return FailResult(resource.OperationUpdate, MapAPIError(err), err.Error()), nil
	}
	m, err := membershipAfterWrite(ctx, client, teamID, userID, props.Role)
	if err != nil {
		return FailResult(resource.OperationUpdate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(m)
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal team membership: %v", err)), nil
	}
	return SuccessResult(resource.OperationUpdate, compositeNativeID(teamID, userID), out), nil
}

func (teamMembershipHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	teamID, userID, err := splitNativeID(nativeID)
	if err != nil {
		// Malformed nativeID for a Delete is treated as already-gone for idempotency.
		return SuccessResult(resource.OperationDelete, nativeID, nil), nil
	}
	if err := client.RemoveUserFromTeamWithContext(ctx, teamID, userID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return FailResult(resource.OperationDelete, MapAPIError(err), err.Error()), nil
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

// List: PagerDuty has no account-wide memberships endpoint, so discovery
// iterates teams and expands each one's members.
func (teamMembershipHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
	opts := pagerduty.ListTeamOptions{}
	if pageSize > 0 {
		opts.Limit = uint(pageSize)
	}
	if pageToken != nil && *pageToken != "" {
		var offset uint
		_, _ = fmt.Sscanf(*pageToken, "%d", &offset)
		opts.Offset = offset
	}
	resp, err := client.ListTeamsWithContext(ctx, opts)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, err
	}
	ids := []string{}
	for _, tm := range resp.Teams {
		var mOffset uint
		for {
			mResp, err := client.ListTeamMembers(ctx, tm.ID, pagerduty.ListTeamMembersOptions{Limit: 100, Offset: mOffset})
			if err != nil {
				break
			}
			for _, m := range mResp.Members {
				ids = append(ids, compositeNativeID(tm.ID, m.User.ID))
			}
			if !mResp.More {
				break
			}
			mOffset += 100
		}
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
