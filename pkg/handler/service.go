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

const serviceType = "PAGERDUTY::Core::Service"

func init() {
	Register(serviceType, &serviceHandler{})
}

type serviceProps struct {
	ID                     string   `json:"id,omitempty"`
	Name                   string   `json:"name"`
	Description            string   `json:"description,omitempty"`
	EscalationPolicyID     string   `json:"escalationPolicyId"`
	AutoResolveTimeout     *uint    `json:"autoResolveTimeout,omitempty"`
	AcknowledgementTimeout *uint    `json:"acknowledgementTimeout,omitempty"`
	AlertCreation          string   `json:"alertCreation,omitempty"`
	Teams                  []string `json:"teams,omitempty"`
}

func serviceFromSDK(s *pagerduty.Service) serviceProps {
	out := serviceProps{
		ID:                     s.ID,
		Name:                   s.Name,
		Description:            s.Description,
		EscalationPolicyID:     s.EscalationPolicy.ID,
		AutoResolveTimeout:     s.AutoResolveTimeout,
		AcknowledgementTimeout: s.AcknowledgementTimeout,
		AlertCreation:          s.AlertCreation,
	}
	for _, tm := range s.Teams {
		out.Teams = append(out.Teams, tm.ID)
	}
	return out
}

func (p serviceProps) toSDK() pagerduty.Service {
	s := pagerduty.Service{
		APIObject:              pagerduty.APIObject{ID: p.ID, Type: "service"},
		Name:                   p.Name,
		Description:            p.Description,
		EscalationPolicy:       pagerduty.EscalationPolicy{APIObject: pagerduty.APIObject{ID: p.EscalationPolicyID, Type: "escalation_policy_reference"}},
		AutoResolveTimeout:     p.AutoResolveTimeout,
		AcknowledgementTimeout: p.AcknowledgementTimeout,
		AlertCreation:          p.AlertCreation,
	}
	for _, tid := range p.Teams {
		s.Teams = append(s.Teams, pagerduty.Team{APIObject: pagerduty.APIObject{ID: tid, Type: "team_reference"}})
	}
	return s
}

type serviceHandler struct{}

func (serviceHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props serviceProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode service: %v", err)), nil
	}
	if props.Name == "" || props.EscalationPolicyID == "" {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "name and escalationPolicyId are required"), nil
	}
	created, err := client.CreateServiceWithContext(ctx, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(serviceFromSDK(created))
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal service: %v", err)), nil
	}
	return SuccessResult(resource.OperationCreate, created.ID, out), nil
}

func (serviceHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	s, err := client.GetServiceWithContext(ctx, nativeID, nil)
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: serviceType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: serviceType, ErrorCode: MapAPIError(err)}, nil
	}
	out, err := json.Marshal(serviceFromSDK(s))
	if err != nil {
		return &resource.ReadResult{ResourceType: serviceType, ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
	}
	return &resource.ReadResult{ResourceType: serviceType, Properties: string(out)}, nil
}

func (serviceHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	var props serviceProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode service: %v", err)), nil
	}
	props.ID = nativeID
	updated, err := client.UpdateServiceWithContext(ctx, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationUpdate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(serviceFromSDK(updated))
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal service: %v", err)), nil
	}
	return SuccessResult(resource.OperationUpdate, updated.ID, out), nil
}

func (serviceHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	if err := client.DeleteServiceWithContext(ctx, nativeID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return FailResult(resource.OperationDelete, MapAPIError(err), err.Error()), nil
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

func (serviceHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
	opts := pagerduty.ListServiceOptions{}
	if pageSize > 0 {
		opts.Limit = uint(pageSize)
	}
	if pageToken != nil && *pageToken != "" {
		var offset uint
		_, _ = fmt.Sscanf(*pageToken, "%d", &offset)
		opts.Offset = offset
	}
	resp, err := client.ListServicesWithContext(ctx, opts)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, err
	}
	ids := make([]string, 0, len(resp.Services))
	for _, s := range resp.Services {
		ids = append(ids, s.ID)
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
