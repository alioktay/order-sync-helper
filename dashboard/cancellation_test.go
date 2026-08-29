package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCleanupWhenTerminalStopsForCancelledOrder(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/orders/order":
			w.Header().Set("content-type", "application/json")
			_, _ = w.Write([]byte(`{"status":"CANCELLED","sync_status":"NOT_SCHEDULED"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	simulator := &Simulator{
		client:       server.Client(),
		orderSyncURL: server.URL,
	}
	done := make(chan struct{})
	go func() {
		simulator.cleanupWhenTerminal("order")
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("cancelled scenario was not cleaned up")
	}
}
