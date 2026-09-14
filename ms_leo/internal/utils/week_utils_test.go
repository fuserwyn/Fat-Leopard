package utils

import (
	"testing"
	"time"
)

func TestWeekStartSundayMSK(t *testing.T) {
	loc := moscowLocation
	// Sunday 2026-09-13
	sun := time.Date(2026, 9, 13, 12, 0, 0, 0, loc)
	if got := WeekStartSundayMSK(sun); got != "2026-09-13" {
		t.Fatalf("sunday: %q", got)
	}
	// Wednesday 2026-09-16 → week started 2026-09-13
	wed := time.Date(2026, 9, 16, 8, 0, 0, 0, loc)
	if got := WeekStartSundayMSK(wed); got != "2026-09-13" {
		t.Fatalf("wednesday: %q", got)
	}
	// Saturday 2026-09-19
	sat := time.Date(2026, 9, 19, 23, 59, 0, 0, loc)
	if got := WeekStartSundayMSK(sat); got != "2026-09-13" {
		t.Fatalf("saturday: %q", got)
	}
}

func TestWeekEndSaturdayMSK(t *testing.T) {
	loc := moscowLocation
	sun := time.Date(2026, 9, 13, 12, 0, 0, 0, loc)
	if got := WeekEndSaturdayMSK(sun); got != "2026-09-19" {
		t.Fatalf("from sunday: %q", got)
	}
	wed := time.Date(2026, 9, 16, 8, 0, 0, 0, loc)
	if got := WeekEndSaturdayMSK(wed); got != "2026-09-19" {
		t.Fatalf("from wednesday: %q", got)
	}
}
