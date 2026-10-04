package bot

import (
	"testing"
	"time"
)

func TestDashCountDelta(t *testing.T) {
	cases := []struct {
		cur, prev    int64
		delta, trend string
	}{
		{0, 0, "", ""},
		{5, 0, "новое", "up"},
		{15, 10, "+50%", "up"},
		{5, 10, "−50%", "down"},
		{10, 10, "0%", "flat"},
	}
	for _, c := range cases {
		if d, tr := dashCountDelta(c.cur, c.prev); d != c.delta || tr != c.trend {
			t.Errorf("%d к %d: %q %q, ждали %q %q", c.cur, c.prev, d, tr, c.delta, c.trend)
		}
	}
}

func TestDashRateDelta(t *testing.T) {
	if d, tr := dashRateDelta(1, 2, 1, 4); d != "+25.0 п.п." || tr != "up" {
		t.Errorf("рост: %q %q", d, tr)
	}
	if d, tr := dashRateDelta(1, 4, 1, 2); d != "−25.0 п.п." || tr != "down" {
		t.Errorf("падение: %q %q", d, tr)
	}
	if d, tr := dashRateDelta(1, 2, 2, 4); d != "0.0 п.п." || tr != "flat" {
		t.Errorf("без изменений: %q %q", d, tr)
	}
	if d, _ := dashRateDelta(1, 2, 0, 0); d != "" {
		t.Errorf("нет базы для сравнения: %q", d)
	}
}

func TestDashSeriesBuckets(t *testing.T) {
	// Среда, 7 октября 2026.
	now := time.Date(2026, 10, 7, 15, 0, 0, 0, time.UTC)
	weekly, b := dashSeriesBuckets(now, 7)
	if weekly || len(b) != 7 || b[0] != "2026-10-01" || b[6] != "2026-10-07" {
		t.Errorf("7 дней: %v %v", weekly, b)
	}
	weekly, b = dashSeriesBuckets(now, 90)
	if !weekly || len(b) != 13 || b[12] != "2026-10-05" || b[11] != "2026-09-28" {
		t.Errorf("90 дней: %v %v", weekly, b)
	}
	weekly, b = dashSeriesBuckets(now, 0)
	if !weekly || len(b) != dashMaxWeekBuckets || b[len(b)-1] != "2026-10-05" {
		t.Errorf("всё время: %v %v", weekly, b)
	}
	// Воскресенье относится к неделе, начавшейся в понедельник.
	if _, b = dashSeriesBuckets(time.Date(2026, 10, 11, 1, 0, 0, 0, time.UTC), 90); b[len(b)-1] != "2026-10-05" {
		t.Errorf("воскресенье: %v", b)
	}
}

func TestDashCohortRate(t *testing.T) {
	now := time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC)
	// Неделя 05.10–11.10: спустя 7 дней срок вышел 19.10, спустя 30 — ещё нет.
	if got := dashCohortRate(3, 4, "2026-10-05", 7, now); got != "75%" {
		t.Errorf("созревшая когорта: %q", got)
	}
	if got := dashCohortRate(0, 4, "2026-10-05", 30, now); got != "—" {
		t.Errorf("несозревшая когорта: %q", got)
	}
	if got := dashCohortRate(0, 0, "2026-10-05", 1, now); got != "—" {
		t.Errorf("пустая когорта: %q", got)
	}
}
