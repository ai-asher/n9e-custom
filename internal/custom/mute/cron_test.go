package mute

import (
	"testing"
	"time"
)

func mustParseTime(t *testing.T, layout, value string) time.Time {
	t.Helper()
	tt, err := time.Parse(layout, value)
	if err != nil {
		t.Fatalf("parse %q: %v", value, err)
	}
	return tt
}

func TestCompileCron_RejectsInvalidExpr(t *testing.T) {
	if _, err := CompileCron("not a cron", "", 60); err == nil {
		t.Fatalf("invalid cron must be rejected")
	}
}

func TestCompileCron_RejectsEmptyExpr(t *testing.T) {
	if _, err := CompileCron("", "", 60); err == nil {
		t.Fatalf("empty cron must be rejected")
	}
}

func TestCompileCron_RejectsZeroDuration(t *testing.T) {
	if _, err := CompileCron("0 * * * *", "", 0); err == nil {
		t.Fatalf("duration=0 must be rejected")
	}
}

func TestCompileCron_RejectsBadTimezone(t *testing.T) {
	if _, err := CompileCron("0 * * * *", "Mars/Olympus", 60); err == nil {
		t.Fatalf("invalid timezone must be rejected")
	}
}

func TestIsActive_InsideWindow(t *testing.T) {
	// Cron fires at top of every hour; duration = 30 min (1800s).
	cc, err := CompileCron("0 * * * *", "UTC", 1800)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	// 14:15 UTC: 15 min after the 14:00 firing → inside window.
	now := mustParseTime(t, time.RFC3339, "2026-04-28T14:15:00Z")
	if !cc.IsActive(now) {
		t.Fatalf("expected active at 14:15 (cron 14:00 + 30min window)")
	}
}

func TestIsActive_OutsideWindow(t *testing.T) {
	cc, _ := CompileCron("0 * * * *", "UTC", 1800)
	// 14:45 UTC: 45 min past 14:00 firing, well outside the 30min window.
	now := mustParseTime(t, time.RFC3339, "2026-04-28T14:45:00Z")
	if cc.IsActive(now) {
		t.Fatalf("expected inactive at 14:45 (cron 14:00 + 30min window expired)")
	}
}

func TestIsActive_ExactlyAtFireTime(t *testing.T) {
	cc, _ := CompileCron("0 * * * *", "UTC", 60)
	now := mustParseTime(t, time.RFC3339, "2026-04-28T14:00:00Z")
	if !cc.IsActive(now) {
		t.Fatalf("expected active exactly at fire time")
	}
}

func TestIsActive_TimezoneAffectsFireTime(t *testing.T) {
	// "0 22 * * *" = 22:00 every day, but interpreted in Asia/Shanghai (UTC+8).
	// 22:00 Shanghai = 14:00 UTC.
	cc, err := CompileCron("0 22 * * *", "Asia/Shanghai", 1800)
	if err != nil {
		t.Fatalf("compile: %v", err)
	}

	// 14:15 UTC = 22:15 Shanghai → 15 min into window → active.
	at1415UTC := mustParseTime(t, time.RFC3339, "2026-04-28T14:15:00Z")
	if !cc.IsActive(at1415UTC) {
		t.Fatalf("expected active at 14:15 UTC (= 22:15 Shanghai)")
	}

	// 13:45 UTC = 21:45 Shanghai → before fire → inactive.
	at1345UTC := mustParseTime(t, time.RFC3339, "2026-04-28T13:45:00Z")
	if cc.IsActive(at1345UTC) {
		t.Fatalf("expected inactive at 13:45 UTC (= 21:45 Shanghai, before fire)")
	}
}

func TestIsActive_DayOfWeekRespected(t *testing.T) {
	// "0 22 * * 1-5" = weekdays 22:00. 2026-04-28 is a Tuesday.
	cc, _ := CompileCron("0 22 * * 1-5", "UTC", 3600)

	// Tuesday 22:30 UTC → active.
	tue := mustParseTime(t, time.RFC3339, "2026-04-28T22:30:00Z")
	if !cc.IsActive(tue) {
		t.Fatalf("expected weekday window to be active on Tuesday")
	}

	// Saturday 22:30 UTC (2026-05-02 is Saturday) → not in cron's day list.
	sat := mustParseTime(t, time.RFC3339, "2026-05-02T22:30:00Z")
	if cc.IsActive(sat) {
		t.Fatalf("Saturday must not match weekday-only cron")
	}
}

func TestIsActive_NilSafe(t *testing.T) {
	var cc *CompiledCron
	if cc.IsActive(time.Now()) {
		t.Fatalf("nil receiver must not match")
	}
}
