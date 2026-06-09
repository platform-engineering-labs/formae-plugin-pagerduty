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

const integrationType = "PAGERDUTY::Core::Integration"

func init() {
	Register(integrationType, &integrationHandler{})
}

// integrationProps mirrors the PKL Integration schema. An Integration belongs
// to exactly one Service (PD's REST API nests integrations under services), so
// serviceId is required and immutable. vendorId selects the integration kind
// (Events API V2, Datadog, etc.); changing it requires a replacement.
// integrationKey is the routing key downstream systems use to send events -
// exposed as an output so other plugins (Grafana, Datadog) can resolve it.
type integrationProps struct {
	ID             string `json:"id,omitempty"`
	Name           string `json:"name"`
	ServiceID      string `json:"serviceId"`
	VendorID       string `json:"vendorId"`
	IntegrationKey string `json:"integrationKey,omitempty"`
}

func integrationFromSDK(serviceID string, i *pagerduty.Integration) integrationProps {
	out := integrationProps{
		ID:             i.ID,
		Name:           i.Name,
		ServiceID:      serviceID,
		IntegrationKey: i.IntegrationKey,
	}
	if i.Vendor != nil {
		out.VendorID = i.Vendor.ID
	}
	return out
}

func (p integrationProps) toSDK() pagerduty.Integration {
	out := pagerduty.Integration{
		APIObject: pagerduty.APIObject{ID: p.ID, Type: "integration"},
		Name:      p.Name,
	}
	if p.ServiceID != "" {
		out.Service = &pagerduty.APIObject{ID: p.ServiceID, Type: "service_reference"}
	}
	if p.VendorID != "" {
		out.Vendor = &pagerduty.APIObject{ID: p.VendorID, Type: "vendor_reference"}
	}
	return out
}

type integrationHandler struct{}

func (integrationHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props integrationProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode integration: %v", err)), nil
	}
	if props.Name == "" || props.ServiceID == "" || props.VendorID == "" {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "name, serviceId, and vendorId are required"), nil
	}
	created, err := client.CreateIntegrationWithContext(ctx, props.ServiceID, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(integrationFromSDK(props.ServiceID, created))
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal integration: %v", err)), nil
	}
	return SuccessResult(resource.OperationCreate, compositeNativeID(props.ServiceID, created.ID), out), nil
}

func (integrationHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	serviceID, integrationID, err := splitNativeID(nativeID)
	if err != nil {
		return &resource.ReadResult{ResourceType: integrationType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	got, err := client.GetIntegrationWithContext(ctx, serviceID, integrationID, pagerduty.GetIntegrationOptions{})
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: integrationType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: integrationType, ErrorCode: MapAPIError(err)}, nil
	}
	out, err := json.Marshal(integrationFromSDK(serviceID, got))
	if err != nil {
		return &resource.ReadResult{ResourceType: integrationType, ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
	}
	return &resource.ReadResult{ResourceType: integrationType, Properties: string(out)}, nil
}

func (integrationHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	serviceID, integrationID, err := splitNativeID(nativeID)
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	var props integrationProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode integration: %v", err)), nil
	}
	props.ID = integrationID
	if props.ServiceID == "" {
		props.ServiceID = serviceID
	}
	updated, err := client.UpdateIntegrationWithContext(ctx, serviceID, props.toSDK())
	if err != nil {
		return FailResult(resource.OperationUpdate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(integrationFromSDK(serviceID, updated))
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal integration: %v", err)), nil
	}
	return SuccessResult(resource.OperationUpdate, compositeNativeID(serviceID, updated.ID), out), nil
}

func (integrationHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	serviceID, integrationID, err := splitNativeID(nativeID)
	if err != nil {
		// Malformed nativeID for a Delete is treated as already-gone for idempotency.
		return SuccessResult(resource.OperationDelete, nativeID, nil), nil
	}
	if err := client.DeleteIntegrationWithContext(ctx, serviceID, integrationID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return FailResult(resource.OperationDelete, MapAPIError(err), err.Error()), nil
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

// List paginates services and expands their integrations. PagerDuty has no
// account-wide /integrations endpoint, so discovery happens through services.
// Includes are limited to integrations to keep payloads small.
func (integrationHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
	opts := pagerduty.ListServiceOptions{Includes: []string{"integrations"}}
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
	ids := []string{}
	for _, svc := range resp.Services {
		for _, integ := range svc.Integrations {
			ids = append(ids, compositeNativeID(svc.ID, integ.ID))
		}
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
