// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

	pagerduty "github.com/PagerDuty/go-pagerduty"

	"github.com/platform-engineering-labs/formae/pkg/model"
	"github.com/platform-engineering-labs/formae/pkg/plugin"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"

	"github.com/platform-engineering-labs/formae-plugin-pagerduty/pkg/config"
	"github.com/platform-engineering-labs/formae-plugin-pagerduty/pkg/handler"
)

// Plugin implements the formae ResourcePlugin interface for PagerDuty. It
// dispatches each CRUD call to a handler registered in pkg/handler.
type Plugin struct {
	mu      sync.Mutex
	clients map[string]*pagerduty.Client
}

var _ plugin.ResourcePlugin = &Plugin{}

// getClient returns a PagerDuty SDK client built from the Target Config, cached
// by the raw target-config JSON so per-Target connection state is reused. The
// token is resolved at NewClient time from env/file and never persisted.
func (p *Plugin) getClient(targetConfig json.RawMessage) (*pagerduty.Client, error) {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.clients == nil {
		p.clients = make(map[string]*pagerduty.Client)
	}

	key := string(targetConfig)
	if c, ok := p.clients[key]; ok {
		return c, nil
	}

	cfg, err := config.ParseTargetConfig(targetConfig)
	if err != nil {
		return nil, err
	}
	c, err := config.NewClient(cfg)
	if err != nil {
		return nil, err
	}
	p.clients[key] = c
	return c, nil
}

func (p *Plugin) RateLimit() model.RateLimitConfig {
	// PagerDuty's REST API limit is ~120 requests/minute (~2/s) per account by
	// default; some endpoints higher. 5 RPS leaves headroom for parallel apply.
	return model.RateLimitConfig{
		Scope:                            model.RateLimitScopeNamespace,
		MaxRequestsPerSecondForNamespace: 5,
	}
}

func (p *Plugin) DiscoveryFilters() []model.MatchFilter {
	return nil
}

func (p *Plugin) LabelConfig() model.LabelConfig {
	return model.LabelConfig{
		DefaultQuery: "$.Name",
		ResourceOverrides: map[string]string{
			"PAGERDUTY::Core::User": "$.Email",
		},
	}
}

func (p *Plugin) Create(ctx context.Context, req *resource.CreateRequest) (*resource.CreateResult, error) {
	h, err := handler.Get(req.ResourceType)
	if err != nil {
		return &resource.CreateResult{
			ProgressResult: handler.FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, err.Error()),
		}, nil
	}
	client, err := p.getClient(req.TargetConfig)
	if err != nil {
		return &resource.CreateResult{
			ProgressResult: handler.FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidCredentials, fmt.Sprintf("pagerduty client: %v", err)),
		}, nil
	}
	result, err := h.Create(ctx, client, req.Properties)
	if err != nil {
		return &resource.CreateResult{
			ProgressResult: handler.FailResult(resource.OperationCreate, handler.MapAPIError(err), err.Error()),
		}, nil
	}
	if result == nil {
		return &resource.CreateResult{
			ProgressResult: handler.FailResult(resource.OperationCreate, resource.OperationErrorCodeInternalFailure, "handler returned nil result"),
		}, nil
	}
	return &resource.CreateResult{ProgressResult: result}, nil
}

func (p *Plugin) Read(ctx context.Context, req *resource.ReadRequest) (*resource.ReadResult, error) {
	h, err := handler.Get(req.ResourceType)
	if err != nil {
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeInvalidRequest}, nil
	}
	client, err := p.getClient(req.TargetConfig)
	if err != nil {
		return &resource.ReadResult{ResourceType: req.ResourceType, ErrorCode: resource.OperationErrorCodeInvalidCredentials}, nil
	}
	return h.Read(ctx, client, req.NativeID)
}

func (p *Plugin) Update(ctx context.Context, req *resource.UpdateRequest) (*resource.UpdateResult, error) {
	h, err := handler.Get(req.ResourceType)
	if err != nil {
		return &resource.UpdateResult{
			ProgressResult: handler.FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, err.Error()),
		}, nil
	}
	client, err := p.getClient(req.TargetConfig)
	if err != nil {
		return &resource.UpdateResult{
			ProgressResult: handler.FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidCredentials, fmt.Sprintf("pagerduty client: %v", err)),
		}, nil
	}
	result, err := h.Update(ctx, client, req.NativeID, req.PriorProperties, req.DesiredProperties)
	if err != nil {
		return &resource.UpdateResult{
			ProgressResult: handler.FailResult(resource.OperationUpdate, handler.MapAPIError(err), err.Error()),
		}, nil
	}
	if result == nil {
		return &resource.UpdateResult{
			ProgressResult: handler.FailResult(resource.OperationUpdate, resource.OperationErrorCodeInternalFailure, "handler returned nil result"),
		}, nil
	}
	return &resource.UpdateResult{ProgressResult: result}, nil
}

func (p *Plugin) Delete(ctx context.Context, req *resource.DeleteRequest) (*resource.DeleteResult, error) {
	h, err := handler.Get(req.ResourceType)
	if err != nil {
		return &resource.DeleteResult{
			ProgressResult: handler.FailResult(resource.OperationDelete, resource.OperationErrorCodeInvalidRequest, err.Error()),
		}, nil
	}
	client, err := p.getClient(req.TargetConfig)
	if err != nil {
		return &resource.DeleteResult{
			ProgressResult: handler.FailResult(resource.OperationDelete, resource.OperationErrorCodeInvalidCredentials, fmt.Sprintf("pagerduty client: %v", err)),
		}, nil
	}
	result, err := h.Delete(ctx, client, req.NativeID)
	if err != nil {
		return &resource.DeleteResult{
			ProgressResult: handler.FailResult(resource.OperationDelete, handler.MapAPIError(err), err.Error()),
		}, nil
	}
	if result == nil {
		return &resource.DeleteResult{
			ProgressResult: handler.FailResult(resource.OperationDelete, resource.OperationErrorCodeInternalFailure, "handler returned nil result"),
		}, nil
	}
	return &resource.DeleteResult{ProgressResult: result}, nil
}

// Status resolves current state with a Read. PagerDuty's REST API is
// synchronous, so CRUD returns terminal results and Status isn't exercised on
// the hot path; implementing it as a Read keeps the answer correct if formae
// ever does poll (e.g. drift or a recoverable retry).
func (p *Plugin) Status(ctx context.Context, req *resource.StatusRequest) (*resource.StatusResult, error) {
	h, err := handler.Get(req.ResourceType)
	if err != nil {
		return &resource.StatusResult{ProgressResult: handler.FailResult(resource.OperationCheckStatus, resource.OperationErrorCodeInvalidRequest, err.Error())}, nil
	}
	client, err := p.getClient(req.TargetConfig)
	if err != nil {
		return &resource.StatusResult{ProgressResult: handler.FailResult(resource.OperationCheckStatus, resource.OperationErrorCodeInvalidCredentials, fmt.Sprintf("pagerduty client: %v", err))}, nil
	}
	read, err := h.Read(ctx, client, req.NativeID)
	if err != nil {
		return &resource.StatusResult{ProgressResult: handler.FailResult(resource.OperationCheckStatus, handler.MapAPIError(err), err.Error())}, nil
	}
	if read.ErrorCode != "" {
		return &resource.StatusResult{ProgressResult: &resource.ProgressResult{
			Operation:       resource.OperationCheckStatus,
			OperationStatus: resource.OperationStatusFailure,
			RequestID:       req.RequestID,
			ErrorCode:       read.ErrorCode,
		}}, nil
	}
	return &resource.StatusResult{ProgressResult: &resource.ProgressResult{
		Operation:          resource.OperationCheckStatus,
		OperationStatus:    resource.OperationStatusSuccess,
		RequestID:          req.RequestID,
		NativeID:           req.NativeID,
		ResourceProperties: json.RawMessage(read.Properties),
	}}, nil
}

func (p *Plugin) List(ctx context.Context, req *resource.ListRequest) (*resource.ListResult, error) {
	h, err := handler.Get(req.ResourceType)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, nil
	}
	client, err := p.getClient(req.TargetConfig)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, nil
	}
	return h.List(ctx, client, req.PageSize, req.PageToken)
}
