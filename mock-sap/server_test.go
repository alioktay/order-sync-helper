package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type lifecycleRecorder struct{ hooks []fx.Hook }

func (r *lifecycleRecorder) Append(hook fx.Hook) { r.hooks = append(r.hooks, hook) }

func TestMockSAPIsIdempotent(t *testing.T) {
	server := NewServer(Config{})
	router := NewRouter(server)
	// The handler accepts the body-less request and falls back to the idempotency header.
	request := httptest.NewRequest("POST", "/api/orders", nil)
	request.Header.Set("idempotency-key", "order-1")
	first := httptest.NewRecorder()
	router.ServeHTTP(first, request)
	request = httptest.NewRequest("POST", "/api/orders", nil)
	request.Header.Set("idempotency-key", "order-1")
	second := httptest.NewRecorder()
	router.ServeHTTP(second, request)
	if first.Code != 201 || second.Code != 201 || len(server.received) != 1 {
		t.Fatalf("unexpected idempotency result: %d, %d, %d", first.Code, second.Code, len(server.received))
	}
}

func TestMockSAPDoesNotExposeScenarioAdminRoutes(t *testing.T) {
	router := NewRouter(NewServer(Config{}))
	for _, method := range []string{http.MethodPost, http.MethodDelete} {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(method, "/admin/scenarios/order-1", nil)
		router.ServeHTTP(response, request)
		if response.Code != http.StatusNotFound {
			t.Fatalf("%s scenario route status = %d, want 404", method, response.Code)
		}
	}
}

func TestRequestResponseLoggerIncludesBodiesAndStatus(t *testing.T) {
	var logs bytes.Buffer
	server := NewServer(Config{})
	server.logger = slog.New(slog.NewJSONHandler(&logs, nil))
	router := NewRouter(server)

	request := httptest.NewRequest(http.MethodPost, "/api/orders?mode=error", strings.NewReader(`{"order_id":"order-log"}`))
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)

	logText := logs.String()
	for _, expected := range []string{`"method":"POST"`, `"path":"/api/orders"`, `order-log`, `"response_status":503`, `SAP temporarily unavailable`} {
		if !strings.Contains(logText, expected) {
			t.Fatalf("log does not contain %q: %s", expected, logText)
		}
	}
}

func TestLoadConfig(t *testing.T) {
	for _, name := range []string{"PORT", "MOCK_SAP_MODE", "MOCK_SAP_DELAY_MS"} {
		t.Setenv(name, "")
	}
	if cfg, err := LoadConfig(); err != nil || cfg != (Config{Port: 4000, Mode: "success", DelayMS: 0}) {
		t.Fatalf("defaults = %+v", cfg)
	}

	t.Setenv("PORT", "4100")
	t.Setenv("MOCK_SAP_MODE", "error")
	t.Setenv("MOCK_SAP_DELAY_MS", "25")
	if cfg, err := LoadConfig(); err != nil || cfg != (Config{Port: 4100, Mode: "error", DelayMS: 25}) {
		t.Fatalf("overrides = %+v", cfg)
	}

	t.Setenv("PORT", "invalid")
	if _, err := LoadConfig(); err == nil {
		t.Fatal("invalid port should fail configuration parsing")
	}
}

func TestLoadConfigRejectsInvalidValues(t *testing.T) {
	for _, name := range []string{"PORT", "MOCK_SAP_DELAY_MS"} {
		t.Run(name, func(t *testing.T) {
			t.Setenv(name, "not-an-integer")
			if _, err := LoadConfig(); err == nil {
				t.Fatalf("LoadConfig() should reject malformed %s", name)
			}
		})
	}
}

func TestMockSAPRoutesAndModes(t *testing.T) {
	t.Run("health", func(t *testing.T) {
		response := httptest.NewRecorder()
		NewRouter(NewServer(Config{})).ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/health", nil))
		if response.Code != http.StatusOK {
			t.Fatalf("health status = %d", response.Code)
		}
	})

	t.Run("error", func(t *testing.T) {
		response := httptest.NewRecorder()
		NewRouter(NewServer(Config{Mode: "error"})).ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/orders", nil))
		if response.Code != http.StatusServiceUnavailable {
			t.Fatalf("error status = %d", response.Code)
		}
	})

	t.Run("timeout observes cancellation", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		request := httptest.NewRequest(http.MethodPost, "/api/orders?mode=timeout", nil).WithContext(ctx)
		NewRouter(NewServer(Config{})).ServeHTTP(httptest.NewRecorder(), request)
	})

	t.Run("delayed order and list", func(t *testing.T) {
		server := NewServer(Config{DelayMS: 1})
		router := NewRouter(server)
		request := httptest.NewRequest(http.MethodPost, "/api/orders?delay_ms=invalid", strings.NewReader(`{"order_id":"order-2"}`))
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if response.Code != http.StatusCreated {
			t.Fatalf("create status = %d", response.Code)
		}

		response = httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/orders", nil))
		var entries []map[string]any
		if err := json.Unmarshal(response.Body.Bytes(), &entries); err != nil || len(entries) != 1 || entries[0]["key"] != "order-2" {
			t.Fatalf("list response = %s, %v", response.Body.String(), err)
		}
	})
}

func TestHTTPServerAndLifecycle(t *testing.T) {
	router := gin.New()
	server := NewHTTPServer(router, Config{Port: 4400})
	if server.Addr != ":4400" || server.Handler != router {
		t.Fatalf("unexpected server: addr=%q handler=%T", server.Addr, server.Handler)
	}

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = listener.Close() }()
	lifecycle := &lifecycleRecorder{}
	RegisterLifecycle(lifecycle, &http.Server{Addr: listener.Addr().String()})
	if len(lifecycle.hooks) != 1 {
		t.Fatalf("hooks = %d, want 1", len(lifecycle.hooks))
	}
	if err = lifecycle.hooks[0].OnStart(context.Background()); err == nil {
		t.Fatal("expected occupied address to fail")
	}
}
