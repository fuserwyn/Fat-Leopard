package bot

import (
	"testing"
	"time"
)

// Белград летом МСК−1, после перехода на зимнее время (25.10.2026) — МСК−2.
// Со старым смещением 0 напоминание на 23:00 приходило Стасу в 21:00.
func TestTimezoneOffsetFromMoscowFollowsDST(t *testing.T) {
	summer := time.Date(2026, time.August, 1, 12, 0, 0, 0, time.UTC)
	winter := time.Date(2026, time.November, 1, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name string
		at   time.Time
		want int
	}{
		{"Europe/Belgrade", summer, -1},
		{"Europe/Belgrade", winter, -2},
		{"Europe/Moscow", winter, 0},
		{"Asia/Novosibirsk", winter, 4},
		{"Asia/Kolkata", winter, 3}, // +2:30 округляем как клиент (Math.round)
	}
	for _, c := range cases {
		got, ok := timezoneOffsetFromMoscow(c.name, c.at)
		if !ok || got != c.want {
			t.Errorf("%s %s: %d %v, want %d", c.name, c.at.Format("01-02"), got, ok, c.want)
		}
	}
	for _, bad := range []string{"", "Mars/Olympus", "Etc/GMT+12"} {
		if _, ok := timezoneOffsetFromMoscow(bad, winter); ok {
			t.Errorf("%q: зона не должна приниматься", bad)
		}
	}
}

func TestWorkoutReminderAt23InBelgradeWinter(t *testing.T) {
	off, _ := timezoneOffsetFromMoscow("Europe/Belgrade", time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC))
	at21 := time.Date(2026, time.November, 1, 20, 0, 0, 0, time.UTC) // 21:00 Белград
	at23 := time.Date(2026, time.November, 1, 22, 0, 0, 0, time.UTC) // 23:00 Белград
	if due, _ := localDailySendDue(at21, off, 23, 0, ""); due {
		t.Error("в 21:00 по Белграду напоминание на 23 не шлём")
	}
	if due, _ := localDailySendDue(at23, off, 23, 0, ""); !due {
		t.Error("в 23:00 по Белграду — шлём")
	}
}
