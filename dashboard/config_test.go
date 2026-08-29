package main

import "testing"

var dashboardConfigEnvironment = []string{
	"PORT",
	"DATABASE_URL",
	"ORDER_SYNC_URL",
	"MOCK_SAP_URL",
	"HARDWARE_SYNC_DELAY_SECONDS",
	"WEBHOOK_SECRET",
	"UI_DIST_DIR",
}

func clearDashboardConfigEnvironment(t *testing.T) {
	t.Helper()
	for _, name := range dashboardConfigEnvironment {
		t.Setenv(name, "")
	}
}

func setDashboardConfigRequirements(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgresql://localhost/orders")
	t.Setenv("ORDER_SYNC_URL", "http://localhost:3000")
	t.Setenv("MOCK_SAP_URL", "http://localhost:4000")
}

func TestLoadConfigHardwareDelayDefaultsAndOverrides(t *testing.T) {
	t.Run("default", func(t *testing.T) {
		clearDashboardConfigEnvironment(t)
		setDashboardConfigRequirements(t)

		cfg, err := loadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.HardwareDelaySeconds != 30 {
			t.Fatalf("HardwareDelaySeconds = %d, want 30", cfg.HardwareDelaySeconds)
		}
	})

	t.Run("custom", func(t *testing.T) {
		clearDashboardConfigEnvironment(t)
		setDashboardConfigRequirements(t)
		t.Setenv("HARDWARE_SYNC_DELAY_SECONDS", "45")

		cfg, err := loadConfig()
		if err != nil {
			t.Fatal(err)
		}
		if cfg.HardwareDelaySeconds != 45 {
			t.Fatalf("HardwareDelaySeconds = %d, want 45", cfg.HardwareDelaySeconds)
		}
	})
}

func TestLoadConfigRejectsNegativeHardwareDelay(t *testing.T) {
	clearDashboardConfigEnvironment(t)
	setDashboardConfigRequirements(t)
	t.Setenv("HARDWARE_SYNC_DELAY_SECONDS", "-1")

	if _, err := loadConfig(); err == nil {
		t.Fatal("expected negative hardware delay to fail")
	}
}

func TestLoadConfigRejectsMalformedNumericValues(t *testing.T) {
	clearDashboardConfigEnvironment(t)
	setDashboardConfigRequirements(t)
	t.Setenv("PORT", "not-an-integer")

	if _, err := loadConfig(); err == nil {
		t.Fatal("expected malformed port to fail")
	}
}
