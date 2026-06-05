// © 2026 Platform Engineering Labs
//
// SPDX-License-Identifier: Apache-2.0

package handler

import (
	"context"
	"encoding/json"
	"fmt"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const scheduleType = "PAGERDUTY::Core::Schedule"

func init() {
	Register(scheduleType, &scheduleHandler{})
}

// scheduleProps mirrors the PKL Schedule schema. Field names match the PKL
// camelCase emission; the Go layer maps these to PagerDuty's snake_case SDK
// shape on the wire.
type scheduleProps struct {
	ID             string          `json:"id,omitempty"`
	Name           string          `json:"name"`
	TimeZone       string          `json:"timeZone"`
	Description    string          `json:"description,omitempty"`
	ScheduleLayers []scheduleLayer `json:"scheduleLayers"`
	Teams          []string        `json:"teams,omitempty"`
}

type scheduleLayer struct {
	ID                        string             `json:"id,omitempty"`
	Name                      string             `json:"name,omitempty"`
	Start                     string             `json:"start"`
	End                       string             `json:"end,omitempty"`
	RotationVirtualStart      string             `json:"rotationVirtualStart"`
	RotationTurnLengthSeconds uint               `json:"rotationTurnLengthSeconds"`
	Users                     []string           `json:"users"`
	Restrictions              []layerRestriction `json:"restrictions,omitempty"`
}

type layerRestriction struct {
	Type            string `json:"type"`
	StartTimeOfDay  string `json:"startTimeOfDay"`
	DurationSeconds uint   `json:"durationSeconds"`
	StartDayOfWeek  uint   `json:"startDayOfWeek,omitempty"`
}

func scheduleFromSDK(s *pagerduty.Schedule) scheduleProps {
	out := scheduleProps{
		ID:          s.ID,
		Name:        s.Name,
		TimeZone:    s.TimeZone,
		Description: s.Description,
	}
	for _, l := range s.ScheduleLayers {
		out.ScheduleLayers = append(out.ScheduleLayers, layerFromSDK(l))
	}
	for _, tm := range s.Teams {
		out.Teams = append(out.Teams, tm.ID)
	}
	return out
}

func layerFromSDK(l pagerduty.ScheduleLayer) scheduleLayer {
	out := scheduleLayer{
		ID:                        l.ID,
		Name:                      l.Name,
		Start:                     l.Start,
		End:                       l.End,
		RotationVirtualStart:      l.RotationVirtualStart,
		RotationTurnLengthSeconds: l.RotationTurnLengthSeconds,
	}
	for _, u := range l.Users {
		out.Users = append(out.Users, u.User.ID)
	}
	for _, r := range l.Restrictions {
		out.Restrictions = append(out.Restrictions, layerRestriction{
			Type:            r.Type,
			StartTimeOfDay:  r.StartTimeOfDay,
			DurationSeconds: r.DurationSeconds,
			StartDayOfWeek:  r.StartDayOfWeek,
		})
	}
	return out
}

func (p scheduleProps) toSDK() pagerduty.Schedule {
	s := pagerduty.Schedule{
		APIObject:   pagerduty.APIObject{ID: p.ID, Type: "schedule"},
		Name:        p.Name,
		TimeZone:    p.TimeZone,
		Description: p.Description,
	}
	for _, l := range p.ScheduleLayers {
		s.ScheduleLayers = append(s.ScheduleLayers, l.toSDK())
	}
	for _, tid := range p.Teams {
		s.Teams = append(s.Teams, pagerduty.APIObject{ID: tid, Type: "team_reference"})
	}
	return s
}

func (l scheduleLayer) toSDK() pagerduty.ScheduleLayer {
	out := pagerduty.ScheduleLayer{
		APIObject:                 pagerduty.APIObject{ID: l.ID, Type: "schedule_layer"},
		Name:                      l.Name,
		Start:                     l.Start,
		End:                       l.End,
		RotationVirtualStart:      l.RotationVirtualStart,
		RotationTurnLengthSeconds: l.RotationTurnLengthSeconds,
	}
	for _, uid := range l.Users {
		out.Users = append(out.Users, pagerduty.UserReference{User: pagerduty.APIObject{ID: uid, Type: "user_reference"}})
	}
	for _, r := range l.Restrictions {
		out.Restrictions = append(out.Restrictions, pagerduty.Restriction{
			Type:            r.Type,
			StartTimeOfDay:  r.StartTimeOfDay,
			DurationSeconds: r.DurationSeconds,
			StartDayOfWeek:  r.StartDayOfWeek,
		})
	}
	return out
}

type scheduleHandler struct{}

func (scheduleHandler) Create(ctx context.Context, client *pagerduty.Client, raw json.RawMessage) (*resource.ProgressResult, error) {
	var props scheduleProps
	if err := json.Unmarshal(raw, &props); err != nil {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode schedule: %v", err)), nil
	}
	if props.Name == "" || props.TimeZone == "" || len(props.ScheduleLayers) == 0 {
		return FailResult(resource.OperationCreate, resource.OperationErrorCodeInvalidRequest, "name, timeZone, and at least one scheduleLayer are required"), nil
	}
	created, err := client.CreateScheduleWithContext(ctx, props.toSDK())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(scheduleFromSDK(created))
	if err != nil {
		return nil, fmt.Errorf("marshal schedule: %w", err)
	}
	return SuccessResult(resource.OperationCreate, created.ID, out), nil
}

func (scheduleHandler) Read(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ReadResult, error) {
	s, err := client.GetScheduleWithContext(ctx, nativeID, pagerduty.GetScheduleOptions{})
	if err != nil {
		if IsNotFound(err) {
			return &resource.ReadResult{ResourceType: scheduleType, ErrorCode: resource.OperationErrorCodeNotFound}, nil
		}
		return &resource.ReadResult{ResourceType: scheduleType, ErrorCode: MapAPIError(err)}, err
	}
	out, err := json.Marshal(scheduleFromSDK(s))
	if err != nil {
		return &resource.ReadResult{ResourceType: scheduleType, ErrorCode: resource.OperationErrorCodeInternalFailure}, err
	}
	return &resource.ReadResult{ResourceType: scheduleType, Properties: string(out)}, nil
}

func (scheduleHandler) Update(ctx context.Context, client *pagerduty.Client, nativeID string, _, desired json.RawMessage) (*resource.ProgressResult, error) {
	var props scheduleProps
	if err := json.Unmarshal(desired, &props); err != nil {
		return FailResult(resource.OperationUpdate, resource.OperationErrorCodeInvalidRequest, fmt.Sprintf("decode schedule: %v", err)), nil
	}
	props.ID = nativeID
	updated, err := client.UpdateScheduleWithContext(ctx, nativeID, props.toSDK())
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(scheduleFromSDK(updated))
	if err != nil {
		return nil, fmt.Errorf("marshal schedule: %w", err)
	}
	return SuccessResult(resource.OperationUpdate, updated.ID, out), nil
}

func (scheduleHandler) Delete(ctx context.Context, client *pagerduty.Client, nativeID string) (*resource.ProgressResult, error) {
	if err := client.DeleteScheduleWithContext(ctx, nativeID); err != nil {
		if IsNotFound(err) {
			return SuccessResult(resource.OperationDelete, nativeID, nil), nil
		}
		return nil, err
	}
	return SuccessResult(resource.OperationDelete, nativeID, nil), nil
}

func (scheduleHandler) List(ctx context.Context, client *pagerduty.Client, pageSize int32, pageToken *string) (*resource.ListResult, error) {
	opts := pagerduty.ListSchedulesOptions{}
	if pageSize > 0 {
		opts.Limit = uint(pageSize)
	}
	if pageToken != nil && *pageToken != "" {
		var offset uint
		_, _ = fmt.Sscanf(*pageToken, "%d", &offset)
		opts.Offset = offset
	}
	resp, err := client.ListSchedulesWithContext(ctx, opts)
	if err != nil {
		return &resource.ListResult{NativeIDs: []string{}}, err
	}
	ids := make([]string, 0, len(resp.Schedules))
	for _, s := range resp.Schedules {
		ids = append(ids, s.ID)
	}
	var next *string
	if resp.More {
		token := fmt.Sprintf("%d", resp.Offset+resp.Limit)
		next = &token
	}
	return &resource.ListResult{NativeIDs: ids, NextPageToken: next}, nil
}
