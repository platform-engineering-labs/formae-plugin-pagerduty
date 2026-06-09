// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

package handler

import (
	"context"
	"encoding/json"
	"fmt"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const maintenanceWindowType = "PAGERDUTY::Core::MaintenanceWindow"

func init() {
	Register(maintenanceWindowType, &maintenanceWindowHandler{})
}

// maintenanceWindowProps mirrors the PKL MaintenanceWindow schema. A window
// silences one or more services for a time range (e.g. during a deploy). Times
// are normalized to UTC.
type maintenanceWindowProps struct {
	ID          string   `json:"id,omitempty"`
	StartTime   string   `json:"startTime"`
	EndTime     string   `json:"endTime"`
	Description string   `json:"description,omitempty"`
	ServiceIDs  []string `json:"serviceIds"`
	Teams       []string `json:"teams,omitempty"`
}

func maintenanceWindowFromSDK(mw *pagerduty.MaintenanceWindow) maintenanceWindowProps {
	out := maintenanceWindowProps{
		ID:          mw.ID,
		StartTime:   toUTCRFC3339(mw.StartTime),
		EndTime:     toUTCRFC3339(mw.EndTime),
		Description: mw.Description,
	}
	for _, s := range mw.Services {
		out.ServiceIDs = append(out.ServiceIDs, s.ID)
	}
	for _, tm := range mw.Teams {
		out.Teams = append(out.Teams, tm.ID)
	}
	return out
}

func (p maintenanceWindowProps) toSDK() pagerduty.MaintenanceWindow {
	mw := pagerduty.MaintenanceWindow{
		APIObject:   pagerduty.APIObject{ID: p.ID, Type: "maintenance_window"},
		StartTime:   p.StartTime,
		EndTime:     p.EndTime,
		Description: p.Description,
	}
	for _, sid := range p.ServiceIDs {
		mw.Services = append(mw.Services, pagerduty.APIObject{ID: sid, Type: "service_reference"})
	}
	for _, tid := range p.Teams {
		mw.Teams = append(mw.Teams, pagerduty.APIObject{ID: tid, Type: "team_reference"})
	}
	return mw
}

type maintenanceWindowHandler struct{}

func (maintenanceWindowHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props maintenanceWindowProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode maintenance window: %v", err)), nil
	}
	if props.StartTime == "" || props.EndTime == "" || len(props.ServiceIDs) == 0 {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "startTime, endTime, and at least one serviceId are required"), nil
	}
	// The "from" header (acting user email) is optional for maintenance windows;
	// leave it empty rather than require a per-Target fromEmail.
	created, err := client.CreateMaintenanceWindowWithContext(ctx, "", props.toSDK())
	if err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(maintenanceWindowFromSDK(created))
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal maintenance window: %v", err)), nil
	}
	return SuccessResult(resource.OperationCreate, created.ID, out), nil
}

func (maintenanceWindowHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	mw, err := client.GetMaintenanceWindowWithContext(ctx, nativeID, pagerduty.GetMaintenanceWindowOptions{})
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: maintenanceWindowType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: maintenanceWindowType, ErrorCode: MapAPIError(err)}, nil
	}
	out, err := json.Marshal(maintenanceWindowFromSDK(mw))
	if err != nil {
		return &resource.ReadResult{ResourceType: maintenanceWindowType, ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
	}
	return &resource.ReadResult{ResourceType: maintenanceWindowType, Properties: string(out)}, nil
}

func (maintenanceWindowHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	var props maintenanceWindowProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode maintenance window: %v", err)), nil
	}
	props.ID = nativeID
	updated, err := client.UpdateMaintenanceWindowWithContext(ctx, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationUpdate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(maintenanceWindowFromSDK(updated))
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal maintenance window: %v", err)), nil
	}
	return SuccessResult(resource.OperationUpdate, updated.ID, out), nil
}

func (maintenanceWindowHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	if err := client.DeleteMaintenanceWindowWithContext(ctx, nativeID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return FailResult(resource.OperationDelete, MapAPIError(err), err.Error()), nil
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

func (maintenanceWindowHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
	opts := pagerduty.ListMaintenanceWindowsOptions{}
	if pageSize > 0 {
		opts.Limit = uint(pageSize)
	}
	if pageToken != nil && *pageToken != "" {
		var offset uint
		_, _ = fmt.Sscanf(*pageToken, "%d", &offset)
		opts.Offset = offset
	}
	resp, err := client.ListMaintenanceWindowsWithContext(ctx, opts)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, err
	}
	ids := make([]string, 0, len(resp.MaintenanceWindows))
	for _, mw := range resp.MaintenanceWindows {
		ids = append(ids, mw.ID)
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
