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

func init() {
	Register("PAGERDUTY::Core::Team", &teamHandler{})
}

type teamProps struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

func teamPropsFromSDK(tm *pagerduty.Team) teamProps {
	return teamProps{
		ID:          tm.ID,
		Name:        tm.Name,
		Description: tm.Description,
	}
}

func (p teamProps) toSDK() *pagerduty.Team {
	return &pagerduty.Team{
		APIObject:   pagerduty.APIObject{ID: p.ID, Type: "team"},
		Name:        p.Name,
		Description: p.Description,
	}
}

type teamHandler struct{}

func (teamHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props teamProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode team props: %v", err)), nil
	}
	if props.Name == "" {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "name is required"), nil
	}
	created, err := client.CreateTeamWithContext(ctx, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(teamPropsFromSDK(created))
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal team: %v", err)), nil
	}
	return SuccessResult(resource.OperationCreate, created.ID, out), nil
}

func (teamHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	tm, err := client.GetTeamWithContext(ctx, nativeID)
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: "PAGERDUTY::Core::Team", ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: "PAGERDUTY::Core::Team", ErrorCode: MapAPIError(err)}, nil
	}
	out, err := json.Marshal(teamPropsFromSDK(tm))
	if err != nil {
		return &resource.ReadResult{ResourceType: "PAGERDUTY::Core::Team", ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
	}
	return &resource.ReadResult{ResourceType: "PAGERDUTY::Core::Team", Properties: string(out)}, nil
}

func (teamHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	var props teamProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode team props: %v", err)), nil
	}
	props.ID = nativeID
	updated, err := client.UpdateTeamWithContext(ctx, nativeID, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationUpdate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(teamPropsFromSDK(updated))
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal team: %v", err)), nil
	}
	return SuccessResult(resource.OperationUpdate, updated.ID, out), nil
}

func (teamHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	if err := client.DeleteTeamWithContext(ctx, nativeID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return FailResult(resource.OperationDelete, MapAPIError(err), err.Error()), nil
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

func (teamHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
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
	ids := make([]string, 0, len(resp.Teams))
	for _, tm := range resp.Teams {
		ids = append(ids, tm.ID)
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
