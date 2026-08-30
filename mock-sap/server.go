package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"go.uber.org/fx"
)

type Config struct {
	Port       int
	Mode       string
	DelayMS    int
	AdminToken string
}

const maxOverrideDelayMS = 60_000

type ResponseOverride struct {
	StatusCode int             `json:"status_code"`
	Body       json.RawMessage `json:"body"`
	RetryAfter *string         `json:"retry_after,omitempty"`
	DelayMS    *int            `json:"delay_ms,omitempty"`
}

type received struct {
	Status        string `json:"status"`
	Message       string `json:"message"`
	SAPInternalID string `json:"sap_internal_id"`
}

type Server struct {
	cfg      Config
	logger   *slog.Logger
	received map[string]received
	override *ResponseOverride
	mu       sync.Mutex
}

func LoadConfig() (Config, error) {
	port, err := parseIntEnv("PORT", 4000)
	if err != nil {
		return Config{}, err
	}
	delay, err := parseIntEnv("MOCK_SAP_DELAY_MS", 0)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:       port,
		Mode:       strings.ToLower(envOrDefault("MOCK_SAP_MODE", "success")),
		DelayMS:    delay,
		AdminToken: os.Getenv("MOCK_SAP_ADMIN_TOKEN"),
	}
	if cfg.Port <= 0 || cfg.DelayMS < 0 {
		return Config{}, fmt.Errorf("numeric configuration values are invalid")
	}
	if cfg.Mode != "success" && cfg.Mode != "error" && cfg.Mode != "timeout" {
		return Config{}, fmt.Errorf("MOCK_SAP_MODE must be success, error, or timeout")
	}
	return cfg, nil
}

func NewServer(cfg Config) *Server {
	return &Server{cfg: cfg, logger: slog.Default(), received: map[string]received{}}
}

func NewRouter(server *Server) *gin.Engine {
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(server.requestResponseLogger())
	router.GET("/health", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	router.POST("/api/orders", server.orders)
	router.GET("/api/orders", server.list)
	router.GET("/api/admin/response", server.getResponseOverride)
	router.PUT("/api/admin/response", server.putResponseOverride)
	router.DELETE("/api/admin/response", server.deleteResponseOverride)
	return router
}

const maxLoggedBodyBytes = 1 << 20

type loggingResponseWriter struct {
	gin.ResponseWriter
	body bytes.Buffer
}

func (w *loggingResponseWriter) Write(body []byte) (int, error) {
	if w.body.Len() < maxLoggedBodyBytes {
		remaining := maxLoggedBodyBytes - w.body.Len()
		if len(body) > remaining {
			_, _ = w.body.Write(body[:remaining])
		} else {
			_, _ = w.body.Write(body)
		}
	}
	return w.ResponseWriter.Write(body)
}

func (w *loggingResponseWriter) WriteString(body string) (int, error) {
	return w.Write([]byte(body))
}

func (s *Server) requestResponseLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		requestBody, truncated := readAndRestoreRequestBody(c.Request)
		writer := &loggingResponseWriter{ResponseWriter: c.Writer}
		c.Writer = writer

		c.Next()

		responseBody := writer.body.String()
		if writer.body.Len() >= maxLoggedBodyBytes {
			responseBody += " [truncated]"
		}
		requestBodyText := string(requestBody)
		if truncated {
			requestBodyText += " [truncated]"
		}
		s.logger.Info("mock-sap request completed",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"query", c.Request.URL.RawQuery,
			"request_body", requestBodyText,
			"response_status", c.Writer.Status(),
			"response_body", responseBody,
			"duration_ms", time.Since(start).Milliseconds(),
		)
	}
}

func readAndRestoreRequestBody(request *http.Request) ([]byte, bool) {
	if request.Body == nil {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(request.Body, maxLoggedBodyBytes+1))
	if err != nil {
		return []byte("[unreadable: " + err.Error() + "]"), false
	}
	request.Body = io.NopCloser(bytes.NewReader(body))
	if len(body) > maxLoggedBodyBytes {
		return body[:maxLoggedBodyBytes], true
	}
	return body, false
}

func NewHTTPServer(router *gin.Engine, cfg Config) *http.Server {
	return &http.Server{
		Addr:              ":" + strconv.Itoa(cfg.Port),
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
		MaxHeaderBytes:    1 << 20,
	}
}

func RegisterLifecycle(lc fx.Lifecycle, server *http.Server) {
	lc.Append(fx.Hook{
		OnStart: func(context.Context) error {
			listener, err := net.Listen("tcp", server.Addr)
			if err != nil {
				return err
			}
			go func() { _ = server.Serve(listener) }()
			return nil
		},
		OnStop: func(ctx context.Context) error { return server.Shutdown(ctx) },
	})
}

func (s *Server) orders(c *gin.Context) {
	override := s.currentOverride()
	var body struct {
		OrderID string `json:"order_id"`
	}
	_ = c.ShouldBindJSON(&body)

	if override != nil {
		delay := s.cfg.DelayMS
		if override.DelayMS != nil {
			delay = *override.DelayMS
		}
		if !waitDelay(c, delay) {
			return
		}
		if override.RetryAfter != nil {
			c.Header("Retry-After", *override.RetryAfter)
		}
		c.Data(override.StatusCode, "application/json; charset=utf-8", override.Body)
		return
	}

	mode := c.Query("mode")
	if mode == "" {
		mode = s.cfg.Mode
	}
	delay := s.cfg.DelayMS
	if value := c.Query("delay_ms"); value != "" {
		delay, _ = strconv.Atoi(value)
	}
	if !waitDelay(c, delay) {
		return
	}
	if mode == "timeout" {
		<-c.Request.Context().Done()
		return
	}
	if mode == "error" {
		c.JSON(http.StatusServiceUnavailable, gin.H{"status": "error", "message": "SAP temporarily unavailable"})
		return
	}
	key := c.GetHeader("idempotency-key")
	if key == "" {
		key = body.OrderID
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	result, ok := s.received[key]
	if !ok {
		result = received{Status: "success", Message: "Order successfully synchronized with SAP", SAPInternalID: "SAP-" + strconv.Itoa(10000000+randomNumber(89999999))}
		s.received[key] = result
	}
	c.JSON(http.StatusCreated, result)
}

func waitDelay(c *gin.Context, delay int) bool {
	if delay <= 0 {
		return true
	}
	timer := time.NewTimer(time.Duration(delay) * time.Millisecond)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-c.Request.Context().Done():
		return false
	}
}

func (s *Server) currentOverride() *ResponseOverride {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.override == nil {
		return nil
	}
	override := *s.override
	if s.override.Body != nil {
		override.Body = append(json.RawMessage(nil), s.override.Body...)
	}
	return &override
}

func (s *Server) authorized(c *gin.Context) bool {
	if s.cfg.AdminToken == "" || c.GetHeader("X-Mock-SAP-Admin-Token") != s.cfg.AdminToken {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid admin token"})
		return false
	}
	return true
}

func (s *Server) getResponseOverride(c *gin.Context) {
	if !s.authorized(c) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.override == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "no response override pending"})
		return
	}
	c.JSON(http.StatusOK, s.override)
}

func (s *Server) putResponseOverride(c *gin.Context) {
	if !s.authorized(c) {
		return
	}
	var override ResponseOverride
	if err := c.ShouldBindJSON(&override); err != nil || override.StatusCode < 200 || override.StatusCode > 599 || len(override.Body) == 0 || !json.Valid(override.Body) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "status_code must be 200-599 and body must be valid JSON"})
		return
	}
	if override.DelayMS != nil && (*override.DelayMS < 0 || *override.DelayMS > maxOverrideDelayMS) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "delay_ms must be between 0 and 60000"})
		return
	}
	s.mu.Lock()
	s.override = &override
	s.mu.Unlock()
	c.JSON(http.StatusOK, override)
}

func (s *Server) deleteResponseOverride(c *gin.Context) {
	if !s.authorized(c) {
		return
	}
	s.mu.Lock()
	s.override = nil
	s.mu.Unlock()
	c.Status(http.StatusNoContent)
}

func (s *Server) list(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	result := make([]gin.H, 0, len(s.received))
	for key, order := range s.received {
		result = append(result, gin.H{"key": key, "status": order.Status, "message": order.Message, "sap_internal_id": order.SAPInternalID})
	}
	c.JSON(http.StatusOK, result)
}

func randomNumber(max uint32) int {
	var bytes [4]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return 1
	}
	return int(binary.BigEndian.Uint32(bytes[:]) % max)
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func parseIntEnv(name string, fallback int) (int, error) {
	value := os.Getenv(name)
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s must be an integer: %w", name, err)
	}
	return parsed, nil
}
