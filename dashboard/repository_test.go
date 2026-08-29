package main

import (
	"encoding/json"
	"testing"
)

func TestSummaryJSONIncludesHardwareDelaySeconds(t *testing.T) {
	payload, err := json.Marshal(Summary{HardwareDelaySeconds: 30})
	if err != nil {
		t.Fatal(err)
	}

	var decoded map[string]any
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatal(err)
	}
	if got, ok := decoded["hardware_delay_seconds"].(float64); !ok || got != 30 {
		t.Fatalf("hardware_delay_seconds = %#v, want 30", decoded["hardware_delay_seconds"])
	}
}
