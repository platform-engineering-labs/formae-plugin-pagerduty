// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

// Package handler defines the per-resource CRUD+List interface and the registry
// that the top-level Plugin dispatches into. One implementation per resource
// type lives in this directory (user.go, team.go, ...).
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

// ResourceHandler implements CRUD+List for one PagerDuty resource type.
type ResourceHandler interface {
	Create(ctx context.Context, client *pagerduty.Client, props json.RawMessage) (*resource.ProgressResult, error)
	Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error)
	Update(ctx context.Context, client *pagerduty.Client, nativeID string, prior, desired json.RawMessage) (*resource.ProgressResult, error)
	Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error)
	List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error)
}

var registry = map[string]ResourceHandler{}

// Register adds a handler for a resource type. Handlers self-register in init().
func Register(resourceType string, h ResourceHandler) {
	registry[resourceType] = h
}

// Get returns the handler for the given resource type.
func Get(resourceType string) (ResourceHandler, error) {
	h, ok := registry[resourceType]
	if !ok {
		return nil, fmt.Errorf("no handler registered for resource type %q", resourceType)
	}
	return h, nil
}

// FailResult builds a failure ProgressResult.
func FailResult(op resource.Operation, code resource.OperationErrorCode, msg string) *resource.ProgressResult {
	return &resource.ProgressResult{
		Operation:       op,
		OperationStatus: resource.OperationStatusFailure,
		ErrorCode:       code,
		StatusMessage:   msg,
	}
}

// SuccessResult builds a success ProgressResult with the resource's native ID
// and post-operation properties.
func SuccessResult(op resource.Operation, nativeID string, props json.RawMessage) *resource.ProgressResult {
	return &resource.ProgressResult{
		Operation:          op,
		OperationStatus:    resource.OperationStatusSuccess,
		NativeID:           nativeID,
		ResourceProperties: props,
	}
}

// MapAPIError converts a PagerDuty SDK error into a formae OperationErrorCode.
// The go-pagerduty client returns *pagerduty.APIError on non-2xx responses.
func MapAPIError(err error) resource.OperationErrorCode {
	if err == nil {
		return ""
	}
	var apiErr pagerduty.APIError
	if errors.As(err, &apiErr) {
		switch apiErr.StatusCode {
		case 400:
			return resource.OperationErrorCodeInvalidRequest
		case 401:
			return resource.OperationErrorCodeInvalidCredentials
		case 403:
			return resource.OperationErrorCodeAccessDenied
		case 404:
			return resource.OperationErrorCodeNotFound
		case 409:
			return resource.OperationErrorCodeAlreadyExists
		case 429:
			return resource.OperationErrorCodeThrottling
		case 500, 502, 503, 504:
			return resource.OperationErrorCodeServiceInternalError
		}
	}
	errStr := strings.ToLower(err.Error())
	switch {
	case strings.Contains(errStr, "not found"):
		return resource.OperationErrorCodeNotFound
	case strings.Contains(errStr, "unauthorized"):
		return resource.OperationErrorCodeInvalidCredentials
	case strings.Contains(errStr, "forbidden"):
		return resource.OperationErrorCodeAccessDenied
	case strings.Contains(errStr, "rate limit"):
		return resource.OperationErrorCodeThrottling
	default:
		return resource.OperationErrorCodeInternalFailure
	}
}

// IsNotFound reports whether an error from the PagerDuty SDK is a 404. Useful
// for Read (return ErrorCodeNotFound) and Delete (treat as success).
func IsNotFound(err error) bool {
	return MapAPIError(err) == resource.OperationErrorCodeNotFound
}
