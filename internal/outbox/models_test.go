package outbox

import (
	"encoding/json"
	"testing"
)

func TestNewEventPayloadShape(t *testing.T) {
	tenantID := "tenant-1"
	e, err := newEvent(EventInstanceCreated, AggregateInstance, "instance-1", &tenantID)
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}

	if payload["event_id"] != e.ID {
		t.Errorf("payload event_id = %v, want %v", payload["event_id"], e.ID)
	}
	if payload["event_type"] != EventInstanceCreated {
		t.Errorf("payload event_type = %v, want %v", payload["event_type"], EventInstanceCreated)
	}
	if payload["tenant_id"] != tenantID {
		t.Errorf("payload tenant_id = %v, want %v", payload["tenant_id"], tenantID)
	}
	if payload["instance_id"] != "instance-1" {
		t.Errorf("payload instance_id = %v, want instance-1", payload["instance_id"])
	}
	if payload["timestamp"] == nil || payload["timestamp"] == "" {
		t.Error("payload timestamp missing")
	}
}

func TestNewEventNilTenantID(t *testing.T) {
	e, err := newEvent(EventNodeOffline, AggregateNode, "node-1", nil)
	if err != nil {
		t.Fatalf("newEvent: %v", err)
	}

	var payload map[string]any
	if err := json.Unmarshal(e.Payload, &payload); err != nil {
		t.Fatalf("unmarshal payload: %v", err)
	}
	if payload["tenant_id"] != nil {
		t.Errorf("payload tenant_id = %v, want nil", payload["tenant_id"])
	}
	if payload["node_id"] != "node-1" {
		t.Errorf("payload node_id = %v, want node-1", payload["node_id"])
	}
}
