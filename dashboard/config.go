package main

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
)

const defaultHardwareDelaySeconds = 30

type Config struct {
	Port                 int
	DatabaseURL          string
	OrderSyncURL         string
	MockSAPURL           string
	MockSAPAdminToken    string
	HardwareDelaySeconds int
	WebhookSecret        string
	UIPath               string
}

func loadConfig() (Config, error) {
	port, err := parseIntEnv("PORT", 3001)
	if err != nil {
		return Config{}, err
	}
	hardwareDelay, err := parseIntEnv("HARDWARE_SYNC_DELAY_SECONDS", defaultHardwareDelaySeconds)
	if err != nil {
		return Config{}, err
	}
	cfg := Config{
		Port:                 port,
		DatabaseURL:          os.Getenv("DATABASE_URL"),
		OrderSyncURL:         stringEnv("ORDER_SYNC_URL", "http://localhost:3000"),
		MockSAPURL:           stringEnv("MOCK_SAP_URL", "http://localhost:4000"),
		MockSAPAdminToken:    stringEnv("MOCK_SAP_ADMIN_TOKEN", "local-dashboard-token"),
		HardwareDelaySeconds: hardwareDelay,
		WebhookSecret:        os.Getenv("WEBHOOK_SECRET"),
		UIPath:               stringEnv("UI_DIST_DIR", "./web/dist"),
	}
	if cfg.DatabaseURL == "" {
		return Config{}, fmt.Errorf("DATABASE_URL is required")
	}
	if cfg.Port <= 0 || cfg.HardwareDelaySeconds < 0 {
		return Config{}, fmt.Errorf("numeric configuration values are invalid")
	}
	for name, value := range map[string]string{"ORDER_SYNC_URL": cfg.OrderSyncURL, "MOCK_SAP_URL": cfg.MockSAPURL} {
		u, err := url.ParseRequestURI(value)
		if err != nil || u.Scheme == "" || u.Host == "" {
			return Config{}, fmt.Errorf("%s must be a valid URL", name)
		}
	}
	return cfg, nil
}

func stringEnv(name, fallback string) string {
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
