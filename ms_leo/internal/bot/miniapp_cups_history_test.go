package bot

import (
	"testing"
	"time"
)

func TestMoscowCalendarDate(t *testing.T) {
	// 22:30 UTC — уже следующий календарный день по Москве (UTC+3).
	got := moscowCalendarDate(time.Date(2026, 9, 27, 22, 30, 0, 0, time.UTC))
	if got != "2026-09-28" {
		t.Fatalf("date=%s", got)
	}
}
