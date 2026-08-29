package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRunOrderSendsEditableShopPayload(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/webhooks/shop" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"message":"Order stored"}`))
	}))
	defer server.Close()

	simulator := NewSimulator(Config{OrderSyncURL: server.URL})
	result, err := simulator.RunOrder(context.Background(), OrderSimulationRequest{EventID: "shop-event", OrderID: "order-1", CustomerEmail: "test@example.com", Items: []SimulationItem{{SKU: "SKU", Quantity: 1, Price: 4}}})
	if err != nil {
		t.Fatal(err)
	}
	if result.EventID != "shop-event" || result.OrderID != "order-1" || result.UpstreamStatus != http.StatusCreated {
		t.Fatalf("result = %+v", result)
	}
	if got["event_id"] != "shop-event" || got["order_id"] != "order-1" {
		t.Fatalf("payload = %+v", got)
	}
}

func TestRunPaymentSendsEditablePayloadAndPreservesErrorResponse(t *testing.T) {
	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/webhooks/payment" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatal(err)
		}
		w.WriteHeader(http.StatusConflict)
		_, _ = w.Write([]byte(`{"error":"payment is finalized"}`))
	}))
	defer server.Close()

	simulator := NewSimulator(Config{OrderSyncURL: server.URL})
	result, err := simulator.RunPayment(context.Background(), PaymentSimulationRequest{EventID: "payment-event", ReferenceOrderID: "other-order", PaymentStatus: "CANCELLED", Timestamp: "bad-timestamp"})
	if err != nil {
		t.Fatal(err)
	}
	if result.EventID != "payment-event" || result.ReferenceOrderID != "other-order" || result.UpstreamStatus != http.StatusConflict {
		t.Fatalf("result = %+v", result)
	}
	if got["event_id"] != "payment-event" || got["reference_order_id"] != "other-order" || got["timestamp"] != "bad-timestamp" {
		t.Fatalf("payload = %+v", got)
	}
	if result.ResponseBody.(map[string]any)["error"] != "payment is finalized" {
		t.Fatalf("body = %+v", result.ResponseBody)
	}
}
