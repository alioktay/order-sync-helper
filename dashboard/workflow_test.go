package main

import (
	"strings"
	"testing"
	"time"
)

func TestBuildWorkflowCompletedOrder(t *testing.T) {
	now := time.Now().UTC()
	sapID := "SAP-100"
	detail := OrderDetail{
		Order: DashboardOrder{
			OrderID: "ORD-1", CustomerEmail: "test@example.com", Status: "PAID", CreatedAt: now, UpdatedAt: now,
			SyncStatus: "SYNCED", SAPInternalID: &sapID, SyncedAt: &now, ItemCount: 1,
		},
		Items:  []OrderItem{{SKU: "DIGITAL", Quantity: 1, Price: 12}},
		Events: []WebhookEvent{{EventID: "shop-1", EventType: "SHOP", ProcessedAt: &now, CreatedAt: now}, {EventID: "pay-1", EventType: "PAYMENT", ProcessedAt: &now, CreatedAt: now}},
	}
	workflow := BuildWorkflow(detail)
	if status := nodeStatus(workflow, "sap"); status != "success" {
		t.Fatalf("SAP node status = %s", status)
	}
	if status := nodeStatus(workflow, "synced"); status != "success" {
		t.Fatalf("completed node status = %s", status)
	}
	if status := nodeStatus(workflow, "delay"); status != "skipped" {
		t.Fatalf("digital delay status = %s", status)
	}
	if detail := edgeDetail(workflow, "worker-sap"); detail != "The worker posts the order payload to SAP." {
		t.Fatalf("worker-to-SAP detail = %q", detail)
	}
	if label := edgeLabel(workflow, "sap-synced"); label != "complete" {
		t.Fatalf("SAP completion label = %q", label)
	}
}

func TestBuildWorkflowRetryWaitingAndDeadBranches(t *testing.T) {
	errorMessage := "SAP returned HTTP 503"
	now := time.Now().UTC()
	for _, test := range []struct {
		name       string
		syncStatus string
		wantRetry  string
		wantDead   string
	}{
		{name: "retry", syncStatus: "PENDING", wantRetry: "active", wantDead: "skipped"},
		{name: "waiting", syncStatus: "WAITING", wantRetry: "active", wantDead: "skipped"},
		{name: "dead", syncStatus: "DEAD", wantRetry: "error", wantDead: "error"},
	} {
		t.Run(test.name, func(t *testing.T) {
			detail := OrderDetail{Order: DashboardOrder{OrderID: "ORD-ERROR", Status: "PAID", CreatedAt: now, UpdatedAt: now, SyncStatus: test.syncStatus, LastError: &errorMessage}}
			workflow := BuildWorkflow(detail)
			if status := nodeStatus(workflow, "sap"); status != "error" {
				t.Fatalf("SAP node status = %s", status)
			}
			if status := nodeStatus(workflow, "retry"); status != test.wantRetry {
				t.Fatalf("retry node status = %s, want %s", status, test.wantRetry)
			}
			if status := nodeStatus(workflow, "dead"); status != test.wantDead {
				t.Fatalf("dead node status = %s, want %s", status, test.wantDead)
			}
		})
	}
}

func TestBuildWorkflowWaitingExplainsAutomaticRecovery(t *testing.T) {
	errorMessage := "SAP returned HTTP 503"
	now := time.Now().UTC()
	workflow := BuildWorkflow(OrderDetail{Order: DashboardOrder{
		OrderID: "ORD-WAITING", Status: "PAID", CreatedAt: now, UpdatedAt: now,
		SyncStatus: "WAITING", LastError: &errorMessage,
	}})
	if status := nodeStatus(workflow, "worker"); status != "active" {
		t.Fatalf("worker status = %s, want active", status)
	}
	if detail := nodeDetail(workflow, "worker"); !strings.Contains(detail, "automatic recovery") {
		t.Fatalf("worker detail = %q, want automatic recovery", detail)
	}
	if detail := nodeDetail(workflow, "retry"); !strings.Contains(detail, "SAP recovers") {
		t.Fatalf("retry detail = %q, want SAP recovery messaging", detail)
	}
	if status := nodeStatus(workflow, "dead"); status != "skipped" {
		t.Fatalf("dead status = %s, want skipped", status)
	}
}

func TestBuildWorkflowAwaitsPayment(t *testing.T) {
	now := time.Now().UTC()
	detail := OrderDetail{Order: DashboardOrder{OrderID: "ORD-PENDING", Status: "PENDING", CreatedAt: now, UpdatedAt: now, SyncStatus: "NOT_SCHEDULED"}}
	workflow := BuildWorkflow(detail)
	if status := nodeStatus(workflow, "gate"); status != "pending" {
		t.Fatalf("gate status = %s", status)
	}
	if status := nodeStatus(workflow, "schedule"); status != "pending" {
		t.Fatalf("schedule status = %s", status)
	}
	if detail := nodeDetail(workflow, "gate"); !strings.Contains(detail, "payment") {
		t.Fatalf("gate detail = %q", detail)
	}
}

func TestBuildWorkflowExplainsDueJobState(t *testing.T) {
	now := time.Now().UTC()
	futureDue := now.Add(10 * time.Minute)
	dueNow := now.Add(-10 * time.Minute)

	for _, test := range []struct {
		name  string
		dueAt *time.Time
		want  string
	}{
		{name: "future", dueAt: &futureDue, want: "Waiting for the due time"},
		{name: "ready", dueAt: &dueNow, want: "Due job is ready for worker pickup"},
	} {
		t.Run(test.name, func(t *testing.T) {
			workflow := BuildWorkflow(OrderDetail{Order: DashboardOrder{
				OrderID: "ORD-DUE", Status: "PAID", CreatedAt: now, UpdatedAt: now,
				SyncStatus: "PENDING", DueAt: test.dueAt,
			}})
			if detail := nodeDetail(workflow, "worker"); detail != test.want {
				t.Fatalf("worker detail = %q, want %q", detail, test.want)
			}
		})
	}
}

func TestBuildWorkflowHardwareDelayUsesConfiguredSeconds(t *testing.T) {
	now := time.Now().UTC()
	detail := OrderDetail{Order: DashboardOrder{
		OrderID: "ORD-HARDWARE", Status: "PAID", CreatedAt: now, UpdatedAt: now,
		SyncStatus: "PENDING", HasHardware: true,
	}}
	workflow := BuildWorkflow(detail, 45)
	if detail := nodeDetail(workflow, "delay"); detail != "Hardware hold: 45s" {
		t.Fatalf("delay detail = %q, want %q", detail, "Hardware hold: 45s")
	}
}

func TestBuildWorkflowCancelledOrderHasDedicatedTerminalState(t *testing.T) {
	now := time.Now().UTC()
	detail := OrderDetail{Order: DashboardOrder{
		OrderID: "ORD-CANCELLED", Status: "CANCELLED", CreatedAt: now, UpdatedAt: now,
		SyncStatus: "NOT_SCHEDULED", HasHardware: true,
	}}
	workflow := BuildWorkflow(detail)
	if status := nodeStatus(workflow, "order"); status != "cancelled" {
		t.Fatalf("order status = %s, want cancelled", status)
	}
	if status := nodeStatus(workflow, "synced"); status != "cancelled" {
		t.Fatalf("outcome status = %s, want cancelled", status)
	}
	if status := nodeStatus(workflow, "sap"); status != "skipped" {
		t.Fatalf("SAP status = %s, want skipped", status)
	}
	if status := nodeStatus(workflow, "retry"); status != "skipped" {
		t.Fatalf("retry status = %s, want skipped", status)
	}
}

func TestBuildWorkflowEdgesDescribeEveryTransition(t *testing.T) {
	workflow := BuildWorkflow(OrderDetail{Order: DashboardOrder{Status: "PAID", SyncStatus: "PENDING"}})
	wantLabels := map[string]string{
		"shop-order": "persist", "payment-gate": "match", "order-gate": "check",
		"gate-schedule": "paid", "schedule-delay": "hold", "delay-worker": "dispatch",
		"worker-sap": "POST", "sap-synced": "complete", "sap-retry": "retry", "retry-dead": "exhausted",
	}
	for id, wantLabel := range wantLabels {
		if label := edgeLabel(workflow, id); label != wantLabel {
			t.Errorf("edge %s label = %q, want %q", id, label, wantLabel)
		}
		if detail := edgeDetail(workflow, id); detail == "" {
			t.Errorf("edge %s has no detail", id)
		}
	}
}

func nodeStatus(workflow Workflow, id string) string {
	return nodeDetailWith(workflow, id, func(node WorkflowNode) string { return node.Status })
}

func nodeDetail(workflow Workflow, id string) string {
	return nodeDetailWith(workflow, id, func(node WorkflowNode) string { return node.Detail })
}

func nodeDetailWith(workflow Workflow, id string, value func(WorkflowNode) string) string {
	for _, node := range workflow.Nodes {
		if node.ID == id {
			return value(node)
		}
	}
	return ""
}

func edgeLabel(workflow Workflow, id string) string {
	return edgeValue(workflow, id, func(edge WorkflowEdge) string { return edge.Label })
}

func edgeDetail(workflow Workflow, id string) string {
	return edgeValue(workflow, id, func(edge WorkflowEdge) string { return edge.Detail })
}

func edgeValue(workflow Workflow, id string, value func(WorkflowEdge) string) string {
	for _, edge := range workflow.Edges {
		if edge.ID == id {
			return value(edge)
		}
	}
	return ""
}
