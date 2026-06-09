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
	"time"

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
		// 404, or a 400 whose PD error code 2100 means a referenced resource
		// isn't yet visible after create. Mapping that case (only) to NotFound
		// keeps it recoverable so the operator can re-drive past the brief
		// consistency lag, without shadowing a genuine 409/401/403 that happens
		// to carry code 2100.
		switch {
		case apiErr.StatusCode == 404 || (apiErr.StatusCode == 400 && apiErr.APIError.Valid && apiErr.APIError.ErrorObject.Code == 2100):
			return resource.OperationErrorCodeNotFound
		case apiErr.RateLimited(): // 429
			return resource.OperationErrorCodeThrottling
		case apiErr.StatusCode == 401:
			return resource.OperationErrorCodeInvalidCredentials
		case apiErr.StatusCode == 403:
			return resource.OperationErrorCodeAccessDenied
		case apiErr.StatusCode == 409:
			return resource.OperationErrorCodeAlreadyExists
		case apiErr.StatusCode == 400:
			return resource.OperationErrorCodeInvalidRequest
		case apiErr.Temporary(): // remaining 5xx
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

// IsNotFound reports whether an error is a true 404. Read uses it to return
// NotFound and Delete to treat the resource as already gone, so it must fire
// only on a real 404 - never on a 400+code-2100 (a referenced resource not yet
// visible), which would otherwise drop a still-existing resource on Delete.
func IsNotFound(err error) bool {
	var apiErr pagerduty.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode == 404
	}
	return strings.Contains(strings.ToLower(err.Error()), "not found")
}

// compositeNativeID packs a parent/child id pair into one string. PagerDuty
// nests several resources under a parent (e.g. an integration under a service),
// but the formae model carries a single NativeID - so both ids ride together.
func compositeNativeID(parentID, childID string) string {
	return parentID + ":" + childID
}

// splitNativeID is the inverse of compositeNativeID.
func splitNativeID(nativeID string) (parentID, childID string, err error) {
	parts := strings.SplitN(nativeID, ":", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("composite nativeID %q must be in \"parent:child\" form", nativeID)
	}
	return parts[0], parts[1], nil
}

// toUTCRFC3339 normalizes a timestamp to UTC RFC3339. PagerDuty echoes the same
// instant differently across endpoints (UTC "Z" on create vs the schedule's
// offset on list), which would otherwise read as drift on every sync.
// Unparseable or empty input is returned unchanged.
func toUTCRFC3339(s string) string {
	if s == "" {
		return s
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return s
	}
	return t.UTC().Format(time.RFC3339)
}

// retryUntilReadable polls fn until it reports the resource ready, absorbing
// PagerDuty's brief read-after-write lag. PD's REST API is synchronous with
// short eventual consistency (no async operation to poll), so convergence is a
// tight client-side loop here, not a formae Status poll.
//
// fn returns (val, ready, err): ready means converged and is returned at once;
// a non-nil err is terminal (returned as-is), so fn must swallow retryable
// conditions like "not visible yet" into (val, false, nil); otherwise fn is
// retried. Steps are linear (step, 2*step, ...) and ctx-cancellable. After the
// final attempt the most recent value is returned with no error, so a caller
// that needs to distinguish "never converged" must inspect that value.
func retryUntilReadable[T any](ctx context.Context, attempts int, step time.Duration, fn func() (val T, ready bool, err error)) (T, error) {
	var last T
	for attempt := 0; attempt < attempts; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return last, ctx.Err()
			case <-time.After(time.Duration(attempt) * step):
			}
		}
		val, ready, err := fn()
		if err != nil {
			return val, err
		}
		last = val
		if ready {
			return val, nil
		}
	}
	return last, nil
}
