package utils

import (
	"testing"
	"time"
)

func TestWeekStartMondayMSK(t *testing.T) {
	loc := moscowLocation
	// Monday 2026-09-14
	mon := time.Date(2026, 9, 14, 12, 0, 0, 0, loc)
	if got := WeekStartMondayMSK(mon); got != "2026-09-14" {
		t.Fatalf("monday: %q", got)
	}
	// Wednesday 2026-09-16 → week started 2026-09-14
	wed := time.Date(2026, 9, 16, 8, 0, 0, 0, loc)
	if got := WeekStartMondayMSK(wed); got != "2026-09-14" {
		t.Fatalf("wednesday: %q", got)
	}
	// Sunday 2026-09-20
	sun := time.Date(2026, 9, 20, 23, 59, 0, 0, loc)
	if got := WeekStartMondayMSK(sun); got != "2026-09-14" {
		t.Fatalf("sunday: %q", got)
	}
	// Sunday 2026-09-13 → previous week started 2026-09-07
	prevSun := time.Date(2026, 9, 13, 0, 0, 0, 0, loc)
	if got := WeekStartMondayMSK(prevSun); got != "2026-09-07" {
		t.Fatalf("previous sunday: %q", got)
	}
}

func TestWeekEndSundayMSK(t *testing.T) {
	loc := moscowLocation
	mon := time.Date(2026, 9, 14, 12, 0, 0, 0, loc)
	if got := WeekEndSundayMSK(mon); got != "2026-09-20" {
		t.Fatalf("from monday: %q", got)
	}
	wed := time.Date(2026, 9, 16, 8, 0, 0, 0, loc)
	if got := WeekEndSundayMSK(wed); got != "2026-09-20" {
		t.Fatalf("from wednesday: %q", got)
	}
	sun := time.Date(2026, 9, 20, 23, 59, 0, 0, loc)
	if got := WeekEndSundayMSK(sun); got != "2026-09-20" {
		t.Fatalf("from sunday: %q", got)
	}
}
