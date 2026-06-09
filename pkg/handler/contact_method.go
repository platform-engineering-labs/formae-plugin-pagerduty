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

const contactMethodType = "PAGERDUTY::Core::ContactMethod"

func init() {
	Register(contactMethodType, &contactMethodHandler{})
}

// contactMethodProps mirrors the PKL ContactMethod schema. A contact method is
// nested under one user, so userId is immutable.
type contactMethodProps struct {
	ID     string `json:"id,omitempty"`
	UserID string `json:"userId"`
	// methodType/methodLabel map to PagerDuty's contact method type/label; they
	// avoid `type` and `label`, which formae reserves for the resource-type
	// discriminator and the resource label respectively.
	Type           string `json:"methodType"`
	Label          string `json:"methodLabel"`
	Address        string `json:"address"`
	CountryCode    int    `json:"countryCode,omitempty"`
	SendShortEmail bool   `json:"sendShortEmail,omitempty"`
}

func contactMethodFromSDK(userID string, cm *pagerduty.ContactMethod) contactMethodProps {
	return contactMethodProps{
		ID:             cm.ID,
		UserID:         userID,
		Type:           cm.Type,
		Label:          cm.Label,
		Address:        cm.Address,
		CountryCode:    cm.CountryCode,
		SendShortEmail: cm.SendShortEmail,
	}
}

func (p contactMethodProps) toSDK() pagerduty.ContactMethod {
	return pagerduty.ContactMethod{
		ID:             p.ID,
		Type:           p.Type,
		Label:          p.Label,
		Address:        p.Address,
		CountryCode:    p.CountryCode,
		SendShortEmail: p.SendShortEmail,
	}
}

type contactMethodHandler struct{}

func (contactMethodHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props contactMethodProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode contact method: %v", err)), nil
	}
	if props.UserID == "" || props.Type == "" || props.Address == "" {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "userId, type, and address are required"), nil
	}
	created, err := client.CreateUserContactMethodWithContext(ctx, props.UserID, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(contactMethodFromSDK(props.UserID, created))
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal contact method: %v", err)), nil
	}
	return SuccessResult(resource.OperationCreate, compositeNativeID(props.UserID, created.ID), out), nil
}

func (contactMethodHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	userID, cmID, err := splitNativeID(nativeID)
	if err != nil {
		return &resource.ReadResult{ResourceType: contactMethodType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	cm, err := client.GetUserContactMethodWithContext(ctx, userID, cmID)
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: contactMethodType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: contactMethodType, ErrorCode: MapAPIError(err)}, nil
	}
	out, err := json.Marshal(contactMethodFromSDK(userID, cm))
	if err != nil {
		return &resource.ReadResult{ResourceType: contactMethodType, ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
	}
	return &resource.ReadResult{ResourceType: contactMethodType, Properties: string(out)}, nil
}

func (contactMethodHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	userID, cmID, err := splitNativeID(nativeID)
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	var props contactMethodProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode contact method: %v", err)), nil
	}
	props.ID = cmID
	if props.UserID == "" {
		props.UserID = userID
	}
	// SDK method name has an upstream typo (Wth, not With) - that's the real
	// exported symbol in go-pagerduty v1.8.0.
	updated, err := client.UpdateUserContactMethodWthContext(ctx, userID, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationUpdate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(contactMethodFromSDK(userID, updated))
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal contact method: %v", err)), nil
	}
	return SuccessResult(resource.OperationUpdate, compositeNativeID(userID, updated.ID), out), nil
}

func (contactMethodHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	userID, cmID, err := splitNativeID(nativeID)
	if err != nil {
		// Malformed nativeID for a Delete is treated as already-gone for idempotency.
		return SuccessResult(resource.OperationDelete, nativeID, nil), nil
	}
	if err := client.DeleteUserContactMethodWithContext(ctx, userID, cmID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return FailResult(resource.OperationDelete, MapAPIError(err), err.Error()), nil
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

// List: PagerDuty has no account-wide contact-methods endpoint, so discovery
// iterates users and expands each one's contact methods.
func (contactMethodHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
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
	ids := []string{}
	for _, u := range resp.Users {
		cmResp, err := client.ListUserContactMethodsWithContext(ctx, u.ID)
		if err != nil {
			continue
		}
		for _, cm := range cmResp.ContactMethods {
			ids = append(ids, compositeNativeID(u.ID, cm.ID))
		}
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
