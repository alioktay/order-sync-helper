package main

import (
	"fmt"
	"time"
)

type Workflow struct {
	Nodes []WorkflowNode `json:"nodes"`
	Edges []WorkflowEdge `json:"edges"`
}

type WorkflowNode struct {
	ID        string     `json:"id"`
	Label     string     `json:"label"`
	Status    string     `json:"status"`
	Detail    string     `json:"detail,omitempty"`
	Timestamp *time.Time `json:"timestamp,omitempty"`
}

type WorkflowEdge struct {
	ID     string `json:"id"`
	Source string `json:"source"`
	Target string `json:"target"`
	Label  string `json:"label,omitempty"`
	Detail string `json:"detail,omitempty"`
	Status string `json:"status"`
}

func BuildWorkflow(detail OrderDetail, hardwareDelaySeconds ...int) Workflow {
	order := detail.Order
	delaySeconds := defaultHardwareDelaySeconds
	if len(hardwareDelaySeconds) > 0 {
		delaySeconds = hardwareDelaySeconds[0]
	}
	shop := firstEvent(detail.Events, "SHOP")
	payment := firstEvent(detail.Events, "PAYMENT")
	hasHardware := order.HasHardware

	shopStatus, shopDetail := eventState(shop)
	paymentStatus, paymentDetail := eventState(payment)
	orderStatus := "success"
	orderDetail := "Order row persisted"
	isCancelled := order.Status == "CANCELLED" || order.SyncStatus == "CANCELLED"
	if isCancelled {
		orderStatus = "cancelled"
		orderDetail = "Order cancelled"
	}

	gateStatus := "pending"
	gateDetail := "Awaiting a completed payment"
	if isCancelled {
		gateStatus = "cancelled"
		gateDetail = "Payment cancellation finalized the order"
	} else if order.Status == "PAID" {
		gateStatus = "success"
		gateDetail = "Payment matched to the order"
	} else if payment != nil {
		gateStatus = "active"
		gateDetail = "Payment event is recorded but not completed"
	}

	scheduleStatus := "pending"
	scheduleDetail := "No SAP sync job scheduled"
	if order.SyncStatus != "NOT_SCHEDULED" {
		scheduleStatus = "success"
		scheduleDetail = "Sync job exists"
	}

	delayStatus := "skipped"
	delayDetail := "Digital order: no hardware delay"
	if isCancelled {
		delayDetail = "Order cancelled: no hardware delay"
	} else if hasHardware {
		delayStatus = "pending"
		delayDetail = fmt.Sprintf("Hardware hold: %ds", delaySeconds)
		if order.DueAt != nil && !order.DueAt.After(time.Now()) {
			delayStatus = "success"
			delayDetail = fmt.Sprintf("Hardware hold: %ds complete", delaySeconds)
		}
	}

	jobDue := order.DueAt != nil && !order.DueAt.After(time.Now())
	workerStatus, workerDetail := "pending", "Waiting for the due time"
	if jobDue {
		workerDetail = "Due job is ready for worker pickup"
	}
	sapStatus, sapDetail := "pending", "Waiting for worker dispatch"
	outcomeStatus, outcomeDetail := "pending", "No terminal SAP result yet"
	retryStatus, retryDetail := "skipped", "No retry recorded"
	deadStatus, deadDetail := "skipped", "No dead-letter result"
	outcomeLabel := "Completed"
	switch {
	case isCancelled:
		outcomeLabel = "Cancelled"
		scheduleStatus, scheduleDetail = "skipped", "Order cancelled before SAP synchronization"
		workerStatus, workerDetail = "skipped", "Order cancellation stopped SAP dispatch"
		sapStatus, sapDetail = "skipped", "No SAP request was sent"
		outcomeStatus, outcomeDetail = "cancelled", "Order cancelled"
	case order.SyncStatus == "SYNCED":
		workerStatus, workerDetail = "success", "Worker marked the job synced"
		sapStatus, sapDetail = "success", "SAP returned an internal ID"
		outcomeStatus, outcomeDetail = "success", "Order synchronized successfully"
	case order.SyncStatus == "PROCESSING":
		workerStatus, workerDetail = "active", "Worker is dispatching the job"
		sapStatus, sapDetail = "active", "Waiting for SAP response"
	case order.SyncStatus == "WAITING":
		workerStatus, workerDetail = "active", "SAP unavailable; automatic recovery is scheduled"
		if jobDue {
			workerDetail = "SAP unavailable; recovery job is ready for worker pickup"
		}
		sapStatus, sapDetail = "error", errorDetail(order.LastError, "SAP request failed")
		outcomeStatus, outcomeDetail = "pending", "Awaiting SAP recovery"
		retryStatus, retryDetail = "active", "Retrying while SAP recovers"
	case order.SyncStatus == "DEAD":
		workerStatus, workerDetail = "error", "Maximum attempts reached"
		sapStatus, sapDetail = "error", errorDetail(order.LastError, "SAP request failed")
		retryStatus, retryDetail = "error", "Retries exhausted"
		deadStatus, deadDetail = "error", "Job marked DEAD"
		outcomeStatus, outcomeDetail = "error", "SAP synchronization stopped"
	case order.LastError != nil:
		workerStatus, workerDetail = "active", "Job will retry after the backoff"
		if jobDue {
			workerDetail = "Retry is due; waiting for worker pickup"
		}
		sapStatus, sapDetail = "error", errorDetail(order.LastError, "SAP request failed")
		retryStatus, retryDetail = "active", "Retry branch active"
	}

	nodes := []WorkflowNode{
		{ID: "shop", Label: "Shop webhook", Status: shopStatus, Detail: shopDetail, Timestamp: eventTime(shop)},
		{ID: "order", Label: "Order persisted", Status: orderStatus, Detail: orderDetail, Timestamp: &order.CreatedAt},
		{ID: "payment", Label: "Payment webhook", Status: paymentStatus, Detail: paymentDetail, Timestamp: eventTime(payment)},
		{ID: "gate", Label: "Payment gate", Status: gateStatus, Detail: gateDetail, Timestamp: order.PaidAt},
		{ID: "schedule", Label: "Sync scheduled", Status: scheduleStatus, Detail: scheduleDetail, Timestamp: order.DueAt},
		{ID: "delay", Label: "Hardware delay", Status: delayStatus, Detail: delayDetail, Timestamp: order.DueAt},
		{ID: "worker", Label: "Worker", Status: workerStatus, Detail: workerDetail, Timestamp: order.UpdatedAtPtr()},
		{ID: "sap", Label: "SAP integration", Status: sapStatus, Detail: sapDetail, Timestamp: order.SyncedAt},
		{ID: "synced", Label: outcomeLabel, Status: outcomeStatus, Detail: outcomeDetail, Timestamp: order.SyncedAt},
		{ID: "retry", Label: "Retry", Status: retryStatus, Detail: retryDetail},
		{ID: "dead", Label: "Dead letter", Status: deadStatus, Detail: deadDetail},
	}
	edges := []WorkflowEdge{
		workflowEdge("shop-order", "shop", "order", "persist", "Shop webhook creates the order record.", shopStatus),
		workflowEdge("payment-gate", "payment", "gate", "match", "Payment webhook is matched to the persisted order.", paymentStatus),
		workflowEdge("order-gate", "order", "gate", "check", "Order and payment state are evaluated together.", gateStatus),
		workflowEdge("gate-schedule", "gate", "schedule", "paid", "Completed payment unlocks SAP sync scheduling.", gateStatus),
		workflowEdge("schedule-delay", "schedule", "delay", "hold", "Hardware orders wait for the configured hold before dispatch.", delayStatus),
		workflowEdge("delay-worker", "delay", "worker", "dispatch", "The due job is handed to the sync worker.", workerStatus),
		workflowEdge("worker-sap", "worker", "sap", "POST", "The worker posts the order payload to SAP.", sapStatus),
		workflowEdge("sap-synced", "sap", "synced", "complete", "A successful SAP response completes synchronization.", outcomeStatus),
		workflowEdge("sap-retry", "sap", "retry", "retry", "A SAP failure enters automatic retry and backoff.", retryStatus),
		workflowEdge("retry-dead", "retry", "dead", "exhausted", "The retry limit moves the job to dead letter.", deadStatus),
	}
	return Workflow{Nodes: nodes, Edges: edges}
}

func (o DashboardOrder) UpdatedAtPtr() *time.Time { return &o.UpdatedAt }

func workflowEdge(id, source, target, label, detail, status string) WorkflowEdge {
	return WorkflowEdge{ID: id, Source: source, Target: target, Label: label, Detail: detail, Status: status}
}

func firstEvent(events []WebhookEvent, eventType string) *WebhookEvent {
	for i := range events {
		if events[i].EventType == eventType {
			return &events[i]
		}
	}
	return nil
}

func eventState(event *WebhookEvent) (string, string) {
	if event == nil {
		return "pending", "No event recorded"
	}
	if event.ProcessedAt == nil {
		return "active", "Event recorded but not processed"
	}
	return "success", "Event processed"
}

func eventTime(event *WebhookEvent) *time.Time {
	if event == nil {
		return nil
	}
	return &event.CreatedAt
}

func errorDetail(value *string, fallback string) string {
	if value == nil || *value == "" {
		return fallback
	}
	return *value
}
