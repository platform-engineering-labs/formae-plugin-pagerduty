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

const scheduleOverrideType = "PAGERDUTY::Core::ScheduleOverride"

func init() {
	Register(scheduleOverrideType, &scheduleOverrideHandler{})
}

// scheduleOverrideProps mirrors the PKL ScheduleOverride schema. PagerDuty
// overrides are immutable - no update endpoint, every field createOnly - so any
// change goes through Replace.
type scheduleOverrideProps struct {
	ID         string `json:"id,omitempty"`
	ScheduleID string `json:"scheduleId"`
	UserID     string `json:"userId"`
	Start      string `json:"start"`
	End        string `json:"end"`
}

func scheduleOverrideFromSDK(scheduleID string, o *pagerduty.Override) scheduleOverrideProps {
	// PagerDuty returns override times as UTC "Z" on create but in the schedule's
	// offset on list; normalize to UTC so create and read agree byte-for-byte.
	return scheduleOverrideProps{
		ID:         o.ID,
		ScheduleID: scheduleID,
		UserID:     o.User.ID,
		Start:      toUTCRFC3339(o.Start),
		End:        toUTCRFC3339(o.End),
	}
}

func (p scheduleOverrideProps) toSDK() pagerduty.Override {
	return pagerduty.Override{
		Start: p.Start,
		End:   p.End,
		User:  pagerduty.APIObject{ID: p.UserID, Type: "user_reference"},
	}
}

// overrideListWindow bounds the overrides list. Overrides have no get-by-id
// endpoint, so Read and List enumerate within a window and filter; a ±1 year
// span covers typical use. Overrides more than a year out won't be readable for
// drift detection.
func overrideListWindow() pagerduty.ListOverridesOptions {
	now := time.Now().UTC()
	return pagerduty.ListOverridesOptions{
		Since: now.AddDate(-1, 0, 0).Format(time.RFC3339),
		Until: now.AddDate(1, 0, 0).Format(time.RFC3339),
	}
}

type scheduleOverrideHandler struct{}

func (scheduleOverrideHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props scheduleOverrideProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode schedule override: %v", err)), nil
	}
	if props.ScheduleID == "" || props.UserID == "" || props.Start == "" || props.End == "" {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "scheduleId, userId, start, and end are required"), nil
	}
	created, err := client.CreateOverrideWithContext(ctx, props.ScheduleID, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(scheduleOverrideFromSDK(props.ScheduleID, created))
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal schedule override: %v", err)), nil
	}
	return SuccessResult(resource.OperationCreate, compositeNativeID(props.ScheduleID, created.ID), out), nil
}

func (scheduleOverrideHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	scheduleID, overrideID, err := splitNativeID(nativeID)
	if err != nil {
		return &resource.ReadResult{ResourceType: scheduleOverrideType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	resp, err := client.ListOverridesWithContext(ctx, scheduleID, overrideListWindow())
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: scheduleOverrideType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: scheduleOverrideType, ErrorCode: MapAPIError(err)}, nil
	}
	for i := range resp.Overrides {
		o := &resp.Overrides[i]
		if o.ID == overrideID {
			out, err := json.Marshal(scheduleOverrideFromSDK(scheduleID, o))
			if err != nil {
				return &resource.ReadResult{ResourceType: scheduleOverrideType, ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
			}
			return &resource.ReadResult{ResourceType: scheduleOverrideType, Properties: string(out)}, nil
		}
	}
	return &resource.ReadResult{ResourceType: scheduleOverrideType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
}

// Update is unsupported: overrides are immutable (every field createOnly, so
// formae drives changes through Replace). This guard catches a stray Update.
func (scheduleOverrideHandler) Update(_ context.Context, _ *pagerduty.Client, _ string, _, _ json.RawMessage) (*resource.ProgressResult, error) {
	return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, "schedule overrides are immutable; change requires replacement"), nil
}

func (scheduleOverrideHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	scheduleID, overrideID, err := splitNativeID(nativeID)
	if err != nil {
		// Malformed nativeID for a Delete is treated as already-gone for idempotency.
		return SuccessResult(resource.OperationDelete, nativeID, nil), nil
	}
	if err := client.DeleteOverrideWithContext(ctx, scheduleID, overrideID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return FailResult(resource.OperationDelete, MapAPIError(err), err.Error()), nil
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

// List: PagerDuty has no account-wide overrides endpoint, so discovery iterates
// schedules and expands each one's overrides within the read window.
func (scheduleOverrideHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
	opts := pagerduty.ListSchedulesOptions{}
	if pageSize > 0 {
		opts.Limit = uint(pageSize)
	}
	if pageToken != nil && *pageToken != "" {
		var offset uint
		_, _ = fmt.Sscanf(*pageToken, "%d", &offset)
		opts.Offset = offset
	}
	resp, err := client.ListSchedulesWithContext(ctx, opts)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, err
	}
	window := overrideListWindow()
	ids := []string{}
	for _, s := range resp.Schedules {
		oResp, err := client.ListOverridesWithContext(ctx, s.ID, window)
		if err != nil {
			continue
		}
		for i := range oResp.Overrides {
			ids = append(ids, compositeNativeID(s.ID, oResp.Overrides[i].ID))
		}
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
