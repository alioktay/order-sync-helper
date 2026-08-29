package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Simulator struct {
	client        *http.Client
	orderSyncURL  string
	webhookSecret string
}

type OrderSimulationRequest struct {
	EventID       string           `json:"event_id"`
	OrderID       string           `json:"order_id"`
	CustomerEmail string           `json:"customer_email"`
	Items         []SimulationItem `json:"items"`
}

type PaymentSimulationRequest struct {
	EventID          string `json:"event_id"`
	ReferenceOrderID string `json:"reference_order_id"`
	PaymentStatus    string `json:"payment_status"`
	Timestamp        string `json:"timestamp"`
}

type SimulationItem struct {
	SKU        string  `json:"sku"`
	Quantity   int     `json:"quantity"`
	Price      float64 `json:"price"`
	IsHardware bool    `json:"isHardware"`
}

type WebhookSimulationResponse struct {
	Kind             string `json:"kind"`
	EventID          string `json:"event_id"`
	OrderID          string `json:"order_id,omitempty"`
	ReferenceOrderID string `json:"reference_order_id,omitempty"`
	UpstreamStatus   int    `json:"upstream_status"`
	ResponseBody     any    `json:"response_body,omitempty"`
}

func NewSimulator(cfg Config) *Simulator {
	return &Simulator{
		client:        &http.Client{Timeout: 15 * time.Second},
		orderSyncURL:  strings.TrimRight(cfg.OrderSyncURL, "/"),
		webhookSecret: cfg.WebhookSecret,
	}
}

func (s *Simulator) RunOrder(ctx context.Context, request OrderSimulationRequest) (WebhookSimulationResponse, error) {
	payload := map[string]any{
		"event_id": request.EventID, "order_id": request.OrderID, "customer_email": request.CustomerEmail, "items": request.Items,
	}
	response, err := s.postWebhook(ctx, "/api/webhooks/shop", payload)
	response.Kind = "order"
	response.EventID = request.EventID
	response.OrderID = request.OrderID
	return response, err
}

func (s *Simulator) RunPayment(ctx context.Context, request PaymentSimulationRequest) (WebhookSimulationResponse, error) {
	payload := map[string]any{
		"event_id": request.EventID, "reference_order_id": request.ReferenceOrderID,
		"payment_status": request.PaymentStatus, "timestamp": request.Timestamp,
	}
	response, err := s.postWebhook(ctx, "/api/webhooks/payment", payload)
	response.Kind = "payment"
	response.EventID = request.EventID
	response.ReferenceOrderID = request.ReferenceOrderID
	return response, err
}

func (s *Simulator) postWebhook(ctx context.Context, path string, payload any) (WebhookSimulationResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return WebhookSimulationResponse{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.orderSyncURL+path, bytes.NewReader(body))
	if err != nil {
		return WebhookSimulationResponse{}, err
	}
	req.Header.Set("content-type", "application/json")
	if s.webhookSecret != "" {
		req.Header.Set("x-webhook-secret", s.webhookSecret)
	}
	response, err := s.client.Do(req)
	if err != nil {
		return WebhookSimulationResponse{}, err
	}
	defer func() { _ = response.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(response.Body, 1<<20))
	if err != nil {
		return WebhookSimulationResponse{}, err
	}
	var responseBody any
	if len(bytes.TrimSpace(data)) > 0 {
		if err := json.Unmarshal(data, &responseBody); err != nil {
			responseBody = string(data)
		}
	}
	return WebhookSimulationResponse{UpstreamStatus: response.StatusCode, ResponseBody: responseBody}, nil
}

func (s *Simulator) cleanupWhenTerminal(orderID string) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		if status, syncStatus, ok := s.orderSyncStatus(ctx, orderID); ok && (status == "CANCELLED" || syncStatus == "SYNCED" || syncStatus == "DEAD" || syncStatus == "CANCELLED") {
			return
		}
		select {
		case <-ticker.C:
		case <-ctx.Done():
			return
		}
	}
}

func (s *Simulator) orderSyncStatus(ctx context.Context, orderID string) (string, string, bool) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.orderSyncURL+"/api/orders/"+url.PathEscape(orderID), nil)
	if err != nil {
		return "", "", false
	}
	response, err := s.client.Do(req)
	if err != nil {
		return "", "", false
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", "", false
	}
	var result struct {
		Status     string `json:"status"`
		SyncStatus string `json:"sync_status"`
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(&result); err != nil {
		return "", "", false
	}
	return result.Status, result.SyncStatus, true
}
