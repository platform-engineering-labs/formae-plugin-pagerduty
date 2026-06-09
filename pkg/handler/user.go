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

const userResourceType = "PAGERDUTY::Core::User"

func init() {
	Register(userResourceType, &userHandler{})
}

// userProps mirrors the PKL User schema. JSON tags are PagerDuty's snake_case
// field names so the Properties JSON the SDK exchanges with formae matches the
// upstream API shape directly.
type userProps struct {
	ID          string `json:"id,omitempty"`
	Name        string `json:"name"`
	Email       string `json:"email"`
	Role        string `json:"role,omitempty"`
	Description string `json:"description,omitempty"`
	TimeZone    string `json:"time_zone,omitempty"`
	JobTitle    string `json:"job_title,omitempty"`
}

func userPropsFromSDK(u *pagerduty.User) userProps {
	return userProps{
		ID:          u.ID,
		Name:        u.Name,
		Email:       u.Email,
		Role:        u.Role,
		Description: u.Description,
		TimeZone:    u.Timezone,
		JobTitle:    u.JobTitle,
	}
}

func (p userProps) toSDK() pagerduty.User {
	return pagerduty.User{
		APIObject:   pagerduty.APIObject{ID: p.ID, Type: "user"},
		Name:        p.Name,
		Email:       p.Email,
		Role:        p.Role,
		Description: p.Description,
		Timezone:    p.TimeZone,
		JobTitle:    p.JobTitle,
	}
}

type userHandler struct{}

func (userHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props userProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode user props: %v", err)), nil
	}
	if props.Name == "" || props.Email == "" {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "name and email are required"), nil
	}
	created, err := client.CreateUserWithContext(ctx, props.toSDK())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(userPropsFromSDK(created))
	if err != nil {
		return nil, fmt.Errorf("marshal created user: %w", err)
	}
	return SuccessResult(resource.OperationCreate, created.ID, out), nil
}

func (userHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	u, err := client.GetUserWithContext(ctx, nativeID, pagerduty.GetUserOptions{})
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: userResourceType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: userResourceType, ErrorCode: MapAPIError(err)}, err
	}
	out, err := json.Marshal(userPropsFromSDK(u))
	if err != nil {
		return &resource.ReadResult{ResourceType: userResourceType, ErrorCode: resource.OperationErrorCodeInternalFailure}, err
	}
	return &resource.ReadResult{
		ResourceType: userResourceType,
		Properties:   string(out),
	}, nil
}

func (userHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	var props userProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode user props: %v", err)), nil
	}
	props.ID = nativeID
	updated, err := client.UpdateUserWithContext(ctx, props.toSDK())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(userPropsFromSDK(updated))
	if err != nil {
		return nil, fmt.Errorf("marshal updated user: %w", err)
	}
	return SuccessResult(resource.OperationUpdate, updated.ID, out), nil
}

func (userHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	if err := client.DeleteUserWithContext(ctx, nativeID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return nil, err
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

func (userHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
	opts := pagerduty.ListUsersOptions{}
	if pageSize > 0 {
		opts.Limit = uint(pageSize)
	}
	if pageToken != nil && *pageToken != "" {
		var offset uint
		_, _ = fmt.Sscanf(*pageToken, "%d", &offset)
		opts.Offset = offset
	}
	resp, err := client.ListUsersWithContext(ctx, opts)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, err
	}
	ids := make([]string, 0, len(resp.Users))
	for _, u := range resp.Users {
		ids = append(ids, u.ID)
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
