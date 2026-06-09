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

const notificationRuleType = "PAGERDUTY::Core::NotificationRule"

func init() {
	Register(notificationRuleType, &notificationRuleHandler{})
}

// notificationRuleProps mirrors the PKL NotificationRule schema. A rule is
// nested under one user (userId immutable) and points at one of that user's
// contact methods.
type notificationRuleProps struct {
	ID                  string `json:"id,omitempty"`
	UserID              string `json:"userId"`
	ContactMethodID     string `json:"contactMethodId"`
	StartDelayInMinutes uint   `json:"startDelayInMinutes"`
	Urgency             string `json:"urgency"`
}

func notificationRuleFromSDK(userID string, nr *pagerduty.NotificationRule) notificationRuleProps {
	return notificationRuleProps{
		ID:                  nr.ID,
		UserID:              userID,
		ContactMethodID:     nr.ContactMethod.ID,
		StartDelayInMinutes: nr.StartDelayInMinutes,
		Urgency:             nr.Urgency,
	}
}

// toSDK needs the contact method's concrete PD type alongside its id; the forma
// carries only the id, so the caller resolves the type via contactMethodTypeFor.
func (p notificationRuleProps) toSDK(contactMethodType string) pagerduty.NotificationRule {
	return pagerduty.NotificationRule{
		ID:                  p.ID,
		Type:                "assignment_notification_rule",
		StartDelayInMinutes: p.StartDelayInMinutes,
		Urgency:             p.Urgency,
		ContactMethod: pagerduty.ContactMethod{
			ID:   p.ContactMethodID,
			Type: contactMethodType,
		},
	}
}

// contactMethodTypeFor resolves a contact method's PagerDuty type. The
// notification-rule API requires the contact method's concrete type alongside
// its id; the forma only carries the id (or a Resolvable to one).
//
// PagerDuty reads are eventually consistent, so a contact method created moments
// earlier - e.g. as a dependency in the same apply - can briefly return 404.
// Retry a few times on NotFound before giving up.
func contactMethodTypeFor(ctx context.Context, client *pagerduty.Client, userID, cmID string) (string, error) {
	var lastErr error
	for attempt := 0; attempt < 4; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(time.Duration(attempt) * 500 * time.Millisecond):
			}
		}
		cm, err := client.GetUserContactMethodWithContext(ctx, userID, cmID)
		if err == nil {
			return cm.Type, nil
		}
		lastErr = err
		if !IsNotFound(err) {
			return "", err
		}
	}
	return "", lastErr
}

type notificationRuleHandler struct{}

func (notificationRuleHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props notificationRuleProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode notification rule: %v", err)), nil
	}
	if props.UserID == "" || props.ContactMethodID == "" || props.Urgency == "" {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "userId, contactMethodId, and urgency are required"), nil
	}
	cmType, err := contactMethodTypeFor(ctx, client, props.UserID, props.ContactMethodID)
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("resolve contact method type: %v", err)), nil
	}
	created, err := client.CreateUserNotificationRuleWithContext(ctx, props.UserID, props.toSDK(cmType))
	if err != nil {
		return FailResult(resource.OperationCreate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(notificationRuleFromSDK(props.UserID, created))
	if err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal notification rule: %v", err)), nil
	}
	return SuccessResult(resource.OperationCreate, compositeNativeID(props.UserID, created.ID), out), nil
}

func (notificationRuleHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	userID, ruleID, err := splitNativeID(nativeID)
	if err != nil {
		return &resource.ReadResult{ResourceType: notificationRuleType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	nr, err := client.GetUserNotificationRuleWithContext(ctx, userID, ruleID)
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: notificationRuleType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: notificationRuleType, ErrorCode: MapAPIError(err)}, nil
	}
	out, err := json.Marshal(notificationRuleFromSDK(userID, nr))
	if err != nil {
		return &resource.ReadResult{ResourceType: notificationRuleType, ErrorCode: resource.OperationErrorCodeInternalFailure}, nil
	}
	return &resource.ReadResult{ResourceType: notificationRuleType, Properties: string(out)}, nil
}

func (notificationRuleHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	userID, ruleID, err := splitNativeID(nativeID)
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, err.Error()), nil
	}
	var props notificationRuleProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode notification rule: %v", err)), nil
	}
	props.ID = ruleID
	if props.UserID == "" {
		props.UserID = userID
	}
	cmType, err := contactMethodTypeFor(ctx, client, userID, props.ContactMethodID)
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("resolve contact method type: %v", err)), nil
	}
	updated, err := client.UpdateUserNotificationRuleWithContext(ctx, userID, props.toSDK(cmType))
	if err != nil {
		return FailResult(resource.OperationUpdate, MapAPIError(err), err.Error()), nil
	}
	out, err := json.Marshal(notificationRuleFromSDK(userID, updated))
	if err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, fmt.Sprintf("marshal notification rule: %v", err)), nil
	}
	return SuccessResult(resource.OperationUpdate, compositeNativeID(userID, updated.ID), out), nil
}

func (notificationRuleHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	userID, ruleID, err := splitNativeID(nativeID)
	if err != nil {
		// Malformed nativeID for a Delete is treated as already-gone for idempotency.
		return SuccessResult(resource.OperationDelete, nativeID, nil), nil
	}
	if err := client.DeleteUserNotificationRuleWithContext(ctx, userID, ruleID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return FailResult(resource.OperationDelete, MapAPIError(err), err.Error()), nil
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

// List: PagerDuty has no account-wide notification-rules endpoint, so discovery
// iterates users and expands each one's rules.
func (notificationRuleHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
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
		nrResp, err := client.ListUserNotificationRulesWithContext(ctx, u.ID)
		if err != nil {
			continue
		}
		for _, nr := range nrResp.NotificationRules {
			ids = append(ids, compositeNativeID(u.ID, nr.ID))
		}
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
