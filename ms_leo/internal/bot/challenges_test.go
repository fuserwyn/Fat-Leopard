package bot

import (
	"strings"
	"testing"
	"time"
)

func TestChallengeStepOnTraining(t *testing.T) {
	cases := []struct {
		name        string
		lastCounted string
		today       string
		continued   bool
		want        challengeStep
	}{
		{"первый день", "", "2026-03-10", false, challengeStepCount},
		{"следующий день", "2026-03-09", "2026-03-10", false, challengeStepCount},
		{"повтор в тот же день", "2026-03-10", "2026-03-10", true, challengeStepKeep},
		{"дата в прошлом (смена пояса)", "2026-03-11", "2026-03-10", true, challengeStepKeep},
		{"пропуск, стрик сгорел", "2026-03-07", "2026-03-10", false, challengeStepFail},
		{"пропуск, стрик спасён попыткой или больничным", "2026-03-07", "2026-03-10", true, challengeStepCount},
	}
	for _, c := range cases {
		if got := challengeStepOnTraining(c.lastCounted, c.today, c.continued); got != c.want {
			t.Errorf("%s: %v, ждали %v", c.name, got, c.want)
		}
	}
}

func TestChallengeStepIdle(t *testing.T) {
	cases := []struct {
		name            string
		lastCounted     string
		alive, saveOpen bool
		want            challengeStep
	}{
		{"ещё не начинал", "", false, false, challengeStepKeep},
		{"вчера засчитан", "2026-03-09", false, false, challengeStepKeep},
		{"сегодня засчитан", "2026-03-10", false, false, challengeStepKeep},
		{"пропуск, стрик жив", "2026-03-07", true, false, challengeStepKeep},
		{"пропуск, можно спасти", "2026-03-08", false, true, challengeStepKeep},
		{"пропуск, стрик сгорел", "2026-03-07", false, false, challengeStepFail},
	}
	for _, c := range cases {
		if got := challengeStepIdle(c.lastCounted, "2026-03-10", c.alive, c.saveOpen); got != c.want {
			t.Errorf("%s: %v, ждали %v", c.name, got, c.want)
		}
	}
}

// streakContinues читает ответ ComputeStreakDays, а не считает стрик заново.
func TestStreakContinuesFollowsComputeStreakDays(t *testing.T) {
	today := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)
	date := func(s string) *string { return &s }
	cases := []struct {
		last *string
		prev int
		want bool
	}{
		{date("2026-03-09"), 5, true},
		{date("2026-03-10"), 5, true},
		{date("2026-03-08"), 5, false},
		{date("2026-03-09"), 0, false},
		{nil, 3, true},
	}
	for _, c := range cases {
		n, same := ComputeStreakDays(c.last, c.prev, today)
		if got := streakContinues(c.prev, n, same); got != c.want {
			t.Errorf("last=%v prev=%d: %v, ждали %v", c.last, c.prev, got, c.want)
		}
	}
}

func TestParseChallengeStartPayload(t *testing.T) {
	cases := map[string]string{
		"ch-days7":                      "days7",
		" ch-AbC23 ":                    "abc23",
		"ch-":                           "",
		"src-vk":                        "",
		"":                              "",
		"ch-bad code":                   "",
		"ch-bad_code":                   "",
		"ch-" + strings.Repeat("a", 33): "",
	}
	for in, want := range cases {
		if got := parseChallengeStartPayload(in); got != want {
			t.Errorf("%q: %q, ждали %q", in, got, want)
		}
	}
	// Источник в аналитике — сам параметр ссылки.
	if got := parseStartSource("ch-days7"); got != "ch-days7" {
		t.Errorf("источник: %q", got)
	}
}

func TestNormalizeChallengeTitle(t *testing.T) {
	if got, ok := normalizeChallengeTitle("  Утренний\nбег  "); !ok || got != "Утренний бег" {
		t.Errorf("пробелы и перенос: %q %v", got, ok)
	}
	if _, ok := normalizeChallengeTitle("   "); ok {
		t.Error("пустое название")
	}
	if _, ok := normalizeChallengeTitle(strings.Repeat("я", 40)); !ok {
		t.Error("40 символов — можно")
	}
	if _, ok := normalizeChallengeTitle(strings.Repeat("я", 41)); ok {
		t.Error("41 символ — нельзя")
	}
}

func TestNewChallengeCodeAndLink(t *testing.T) {
	code, err := newChallengeCode()
	if err != nil || len(code) != challengeCodeLen || parseChallengeStartPayload(challengeStartPrefix+code) != code {
		t.Fatalf("код %q: %v", code, err)
	}
	if got := challengeLink("leo_bot", code); got != "https://t.me/leo_bot?start=ch-"+code {
		t.Errorf("ссылка: %q", got)
	}
	if challengeLink("", code) != "" || challengeLink("leo_bot", "") != "" {
		t.Error("без имени бота или кода ссылки нет")
	}
	if challengeDaysBetween("2026-03-01", "2026-03-31") != 30 || challengeDaysBetween("bad", "2026-03-31") != 0 {
		t.Error("дней между датами")
	}
}
