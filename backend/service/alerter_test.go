package service

import (
	"testing"
	"time"
)

func TestAlerterDeduplicatesWithinCooldown(t *testing.T) {
	a := &Alerter{
		active: make(map[string]*AlertState),
	}
	cfg := AlertConfig{CooldownMinutes: 15}

	a.fire(cfg, "disk_usage", "critical", "disk_usage", "disk high", "91%")
	a.fire(cfg, "disk_usage", "critical", "disk_usage", "disk high", "91%")

	if len(a.history) != 1 {
		t.Fatalf("expected one history event during cooldown, got %d", len(a.history))
	}
	state := a.active["disk_usage"]
	if state == nil {
		t.Fatal("expected active state")
	}
	if state.Count != 2 {
		t.Fatalf("expected alert count 2, got %d", state.Count)
	}
}

func TestAlerterResolvesMissingAlerts(t *testing.T) {
	a := &Alerter{
		active: make(map[string]*AlertState),
	}
	cfg := AlertConfig{CooldownMinutes: 0}

	a.fire(cfg, "connections", "warning", "connections", "connections high", "95%")
	a.resolveMissing(map[string]struct{}{})

	state := a.active["connections"]
	if state == nil {
		t.Fatal("expected stored state")
	}
	if state.Status != "resolved" {
		t.Fatalf("expected resolved state, got %s", state.Status)
	}
	if state.ResolvedAt == nil || state.ResolvedAt.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("expected recent resolved timestamp, got %#v", state.ResolvedAt)
	}
	if len(a.history) != 2 {
		t.Fatalf("expected open and resolved history entries, got %d", len(a.history))
	}
	if a.history[1].Level != "resolved" {
		t.Fatalf("expected resolved event, got %s", a.history[1].Level)
	}
}
