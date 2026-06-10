// © 2026 Platform Engineering Labs Inc.
//
// SPDX-License-Identifier: FSL-1.1-ALv2

//go:build integration

package handler

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	pagerduty "github.com/PagerDuty/go-pagerduty"
	"github.com/platform-engineering-labs/formae/pkg/plugin/resource"
)

const integrationResourceType = "PAGERDUTY::Core::Integration"

// Events API v1 vendor - confirmed-existing PD vendor that returns an
// integration_key on create. Used for conformance only.
const testVendorEventsAPIv1 = "PTS9BY7"

// cleanupIntegration is best-effort; the test deletes the parent service in
// most flows which cascades. This is a safety net.
func cleanupIntegration(t *testing.T, client *pagerduty.Client, serviceID, integrationID string) {
	t.Helper()
	if serviceID == "" || integrationID == "" {
		return
	}
	cleanupCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := client.DeleteIntegrationWithContext(cleanupCtx, serviceID, integrationID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Logf("cleanup: delete integration %s/%s: %v", serviceID, integrationID, err)
	}
}

// integrationSetupService creates a Service so an Integration has somewhere to
// live. Returns the service's PD id.
func integrationSetupService(t *testing.T, client *pagerduty.Client) string {
	t.Helper()
	epID := serviceSetupEP(t, client)
	svcH, _ := Get(serviceResourceType)
	svcID := createPrereq(t, ctx(t), client, svcH, minimalServiceProps(uniqueName("Int Svc"), epID), "service")
	t.Cleanup(func() { cleanupService(t, client, svcID) })
	return svcID
}

func minimalIntegrationProps(name, serviceID, vendorID string) []byte {
	props, _ := json.Marshal(map[string]any{
		"name":      name,
		"serviceId": serviceID,
		"vendorId":  vendorID,
	})
	return props
}

func TestIntegration_Create(t *testing.T) {
	client := testClient(t)
	h, err := Get(integrationResourceType)
	if err != nil {
		t.Fatalf("handler not registered: %v", err)
	}
	svcID := integrationSetupService(t, client)

	result, err := h.Create(ctx(t), client, minimalIntegrationProps(uniqueName("Int"), svcID, testVendorEventsAPIv1))
	if err != nil {
		t.Fatalf("Create error: %v", err)
	}
	if result.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Create status = %v: %s", result.OperationStatus, result.StatusMessage)
	}
	if result.NativeID == "" {
		t.Fatal("empty NativeID")
	}
	// NativeID must encode both serviceID and integrationID so Read/Update/Delete
	// can round-trip without rediscovering the parent.
	if !strings.Contains(result.NativeID, ":") {
		t.Errorf("NativeID %q should be composite serviceID:integrationID", result.NativeID)
	}

	var got map[string]any
	_ = json.Unmarshal(result.ResourceProperties, &got)
	if key, _ := got["integrationKey"].(string); key == "" {
		t.Error("ResourceProperties should expose integrationKey for downstream Resolvable wiring")
	}
	if got["serviceId"] != svcID {
		t.Errorf("serviceId = %v, want %v", got["serviceId"], svcID)
	}
}

func TestIntegration_Read(t *testing.T) {
	client := testClient(t)
	h, _ := Get(integrationResourceType)
	svcID := integrationSetupService(t, client)

	created, err := h.Create(ctx(t), client, minimalIntegrationProps(uniqueName("Read Int"), svcID, testVendorEventsAPIv1))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}

	read, err := h.Read(ctx(t), client, created.NativeID)
	if err != nil {
		t.Fatalf("Read error: %v", err)
	}
	if read.ErrorCode != "" {
		t.Fatalf("Read ErrorCode = %q", read.ErrorCode)
	}
	var got map[string]any
	_ = json.Unmarshal([]byte(read.Properties), &got)
	if got["serviceId"] != svcID {
		t.Errorf("Read serviceId = %v, want %v", got["serviceId"], svcID)
	}
}

func TestIntegration_ReadNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(integrationResourceType)
	read, _ := h.Read(ctx(t), client, "PXXXXXX:PYYYYYY")
	if read.ErrorCode != resource.OperationErrorCodeNotFound {
		t.Errorf("ErrorCode = %q, want NotFound", read.ErrorCode)
	}
}

func TestIntegration_Update(t *testing.T) {
	client := testClient(t)
	h, _ := Get(integrationResourceType)
	svcID := integrationSetupService(t, client)

	originalName := uniqueName("Update Int")
	created, err := h.Create(ctx(t), client, minimalIntegrationProps(originalName, svcID, testVendorEventsAPIv1))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}

	newName := originalName + " RENAMED"
	desired, _ := json.Marshal(map[string]any{
		"name":      newName,
		"serviceId": svcID,
		"vendorId":  testVendorEventsAPIv1,
	})
	updated, err := h.Update(ctx(t), client, created.NativeID, created.ResourceProperties, desired)
	if err != nil {
		t.Fatalf("Update error: %v", err)
	}
	if updated.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Update status = %v: %s", updated.OperationStatus, updated.StatusMessage)
	}
	var got map[string]any
	_ = json.Unmarshal(updated.ResourceProperties, &got)
	if got["name"] != newName {
		t.Errorf("name = %v, want %v", got["name"], newName)
	}
}

func TestIntegration_Delete(t *testing.T) {
	client := testClient(t)
	h, _ := Get(integrationResourceType)
	svcID := integrationSetupService(t, client)

	created, err := h.Create(ctx(t), client, minimalIntegrationProps(uniqueName("Delete Int"), svcID, testVendorEventsAPIv1))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}
	deleted, err := h.Delete(ctx(t), client, created.NativeID)
	if err != nil {
		t.Fatalf("Delete error: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("Delete status = %v", deleted.OperationStatus)
	}
	readUntilGone(t, ctx(t), client, h, created.NativeID)
}

func TestIntegration_DeleteNotFound(t *testing.T) {
	client := testClient(t)
	h, _ := Get(integrationResourceType)
	deleted, err := h.Delete(ctx(t), client, "PXXXXXX:PYYYYYY")
	if err != nil {
		t.Fatalf("Delete error for missing: %v", err)
	}
	if deleted.OperationStatus != resource.OperationStatusSuccess {
		t.Errorf("Delete status = %v, want Success", deleted.OperationStatus)
	}
}

func TestIntegration_List(t *testing.T) {
	client := testClient(t)
	h, _ := Get(integrationResourceType)
	svcID := integrationSetupService(t, client)

	created, err := h.Create(ctx(t), client, minimalIntegrationProps(uniqueName("List Int"), svcID, testVendorEventsAPIv1))
	if err != nil || created.OperationStatus != resource.OperationStatusSuccess {
		t.Fatalf("setup: %v %s", err, created.StatusMessage)
	}

	// List can't discover Integrations natively (there's no global list endpoint
	// in the PD API - integrations are nested under services). We expose List as
	// "iterate services, expand integrations" so discovery still works.
	listResult, err := h.List(ctx(t), client, 100, nil)
	if err != nil {
		t.Fatalf("List error: %v", err)
	}
	found := false
	for _, id := range listResult.NativeIDs {
		if id == created.NativeID {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("created Integration %s not in List output (%d entries)", created.NativeID, len(listResult.NativeIDs))
	}
}
