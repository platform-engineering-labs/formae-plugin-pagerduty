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

const epType = "PAGERDUTY::Core::EscalationPolicy"

func init() {
	Register(epType, &escalationPolicyHandler{})
}

type epProps struct {
	ID              string   `json:"id,omitempty"`
	Name            string   `json:"name"`
	Description     string   `json:"description,omitempty"`
	NumLoops        uint     `json:"numLoops,omitempty"`
	EscalationRules []epRule `json:"escalationRules"`
	Teams           []string `json:"teams,omitempty"`
}

type epRule struct {
	ID                       string     `json:"id,omitempty"`
	EscalationDelayInMinutes uint       `json:"escalationDelayInMinutes"`
	Targets                  []epTarget `json:"targets"`
}

type epTarget struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

func epFromSDK(p *pagerduty.EscalationPolicy) epProps {
	out := epProps{
		ID:          p.ID,
		Name:        p.Name,
		Description: p.Description,
		NumLoops:    p.NumLoops,
	}
	for _, r := range p.EscalationRules {
		rule := epRule{
			ID:                       r.ID,
			EscalationDelayInMinutes: r.Delay,
		}
		for _, t := range r.Targets {
			rule.Targets = append(rule.Targets, epTarget{Type: t.Type, ID: t.ID})
		}
		out.EscalationRules = append(out.EscalationRules, rule)
	}
	for _, tm := range p.Teams {
		out.Teams = append(out.Teams, tm.ID)
	}
	return out
}

func (p epProps) toSDK() pagerduty.EscalationPolicy {
	ep := pagerduty.EscalationPolicy{
		APIObject:   pagerduty.APIObject{ID: p.ID, Type: "escalation_policy"},
		Name:        p.Name,
		Description: p.Description,
		NumLoops:    p.NumLoops,
	}
	for _, r := range p.EscalationRules {
		rule := pagerduty.EscalationRule{
			ID:    r.ID,
			Delay: r.EscalationDelayInMinutes,
		}
		for _, t := range r.Targets {
			rule.Targets = append(rule.Targets, pagerduty.APIObject{ID: t.ID, Type: t.Type})
		}
		ep.EscalationRules = append(ep.EscalationRules, rule)
	}
	for _, tid := range p.Teams {
		ep.Teams = append(ep.Teams, pagerduty.APIReference{ID: tid, Type: "team_reference"})
	}
	return ep
}

type escalationPolicyHandler struct{}

func (escalationPolicyHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props epProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode escalation policy: %v", err)), nil
	}
	if props.Name == "" || len(props.EscalationRules) == 0 {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "name and at least one escalationRule are required"), nil
	}
	created, err := client.CreateEscalationPolicyWithContext(ctx, props.toSDK())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(epFromSDK(created))
	if err != nil {
		return nil, fmt.Errorf("marshal escalation policy: %w", err)
	}
	return SuccessResult(resource.OperationCreate, created.ID, out), nil
}

func (escalationPolicyHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	ep, err := client.GetEscalationPolicyWithContext(ctx, nativeID, nil)
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: epType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: epType, ErrorCode: MapAPIError(err)}, err
	}
	out, err := json.Marshal(epFromSDK(ep))
	if err != nil {
		return &resource.ReadResult{ResourceType: epType, ErrorCode: resource.OperationErrorCodeInternalFailure}, err
	}
	return &resource.ReadResult{ResourceType: epType, Properties: string(out)}, nil
}

func (escalationPolicyHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	var props epProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode escalation policy: %v", err)), nil
	}
	props.ID = nativeID
	updated, err := client.UpdateEscalationPolicyWithContext(ctx, nativeID, props.toSDK())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(epFromSDK(updated))
	if err != nil {
		return nil, fmt.Errorf("marshal escalation policy: %w", err)
	}
	return SuccessResult(resource.OperationUpdate, updated.ID, out), nil
}

func (escalationPolicyHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	if err := client.DeleteEscalationPolicyWithContext(ctx, nativeID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return nil, err
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

func (escalationPolicyHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
	opts := pagerduty.ListEscalationPoliciesOptions{}
	if pageSize > 0 {
		opts.Limit = uint(pageSize)
	}
	if pageToken != nil && *pageToken != "" {
		var offset uint
		_, _ = fmt.Sscanf(*pageToken, "%d", &offset)
		opts.Offset = offset
	}
	resp, err := client.ListEscalationPoliciesWithContext(ctx, opts)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, err
	}
	ids := make([]string, 0, len(resp.EscalationPolicies))
	for _, ep := range resp.EscalationPolicies {
		ids = append(ids, ep.ID)
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
