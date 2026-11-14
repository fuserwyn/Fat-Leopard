package bot

import (
	"strings"
	"testing"
	"time"

	"leo-bot/internal/i18n"
)

func TestInactiveRemovalWarningTextLang(t *testing.T) {
	ru := inactiveRemovalWarningText(i18n.RU, "@leo", "5 марта, 00:00")
	if !strings.Contains(ru, "Ты давно не занимался") || !strings.Contains(ru, "@leo") || !strings.Contains(ru, "5 марта, 00:00") {
		t.Fatalf("ru: %q", ru)
	}
	en := inactiveRemovalWarningText(i18n.EN, "@leo", "5 March, 00:00")
	if !strings.Contains(en, "You haven't worked out in a while") {
		t.Fatalf("en: %q", en)
	}
	at := time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)
	if got := formatRemovalAtLocalHuman(i18n.RU, at, time.UTC); got != "5 марта, 00:00" {
		t.Fatalf("ru date: %q", got)
	}
	if got := formatRemovalAtLocalHuman(i18n.EN, at, time.UTC); got != "5 March, 00:00" {
		t.Fatalf("en date: %q", got)
	}
}

func TestStreakRewardTextLang(t *testing.T) {
	ru := streakRewardText(i18n.RU, "@leo", 22, 42, 100, "🏆 X", "sub")
	if !strings.Contains(ru, "стрик: 22 дня подряд") || !strings.Contains(ru, "+42 кубка (всего: 100)") {
		t.Fatalf("ru: %q", ru)
	}
	en := streakRewardText(i18n.EN, "@leo", 1, 1, 5, "🏆 X", "sub")
	if !strings.Contains(en, "streak: 1 day in a row") || !strings.Contains(en, "+1 cup (total: 5)") {
		t.Fatalf("en: %q", en)
	}
	for _, m := range streakMilestones {
		if m.TitleRU == "" || m.SubtitleRU == "" {
			t.Fatalf("milestone %d: нет русского текста", m.Days)
		}
	}
	if got := trainingsWordFormLang(i18n.RU, 5); got != "тренировок" {
		t.Fatalf("ru trainings: %q", got)
	}
	if got := trainingsWordFormLang(i18n.EN, 5); got != "workouts" {
		t.Fatalf("en trainings: %q", got)
	}
}

func TestChallengeTextsLang(t *testing.T) {
	ru := challengeCompletedText(i18n.RU, "Неделя", 7, true)
	if !strings.Contains(ru, "Челлендж «Неделя» пройден: 7 дней подряд!") || !strings.Contains(ru, "создавать свои челленджи") {
		t.Fatalf("ru: %q", ru)
	}
	en := challengeCompletedText(i18n.EN, "Week", 7, false)
	if !strings.Contains(en, "completed: 7 days in a row") || strings.Contains(en, "create your own") {
		t.Fatalf("en: %q", en)
	}
	if got := challengeFailedText(i18n.RU, "Неделя", 3, 7); !strings.Contains(got, "прерван: пропущен день. Засчитано 3 из 7") {
		t.Fatalf("ru fail: %q", got)
	}
	if got := challengeFailedText(i18n.EN, "Week", 3, 7); !strings.Contains(got, "ended: a day was missed. Progress: 3 of 7") {
		t.Fatalf("en fail: %q", got)
	}
}

func TestSickLeaveRemovalNoticeRU(t *testing.T) {
	onSick := sickLeaveRemovalNotice(i18n.RU, "6 дней 22 ч.", "08.07.2026, 00:00", false)
	if !strings.Contains(onSick, "⏳ До удаления: 6 дней 22 ч. — столько останется после выздоровления") ||
		!strings.Contains(onSick, "📅 Крайний срок: 08.07.2026, 00:00 (твоё время)") {
		t.Fatalf("ru on sick: %q", onSick)
	}
	if after := sickLeaveRemovalNotice(i18n.RU, "22 ч.", "", true); after != "⏳ До удаления осталось: 22 ч." {
		t.Fatalf("ru after: %q", after)
	}
}

func TestFormatDurationLang(t *testing.T) {
	b := &Bot{}
	cases := map[time.Duration]string{
		6*24*time.Hour + 22*time.Hour: "6 дней 22 ч.",
		2 * 24 * time.Hour:            "2 дня",
		3*time.Hour + 5*time.Minute:   "3 ч. 5 мин.",
		3 * time.Hour:                 "3 ч.",
		7 * time.Minute:               "7 мин.",
	}
	for d, want := range cases {
		if got := b.formatDurationLang(i18n.RU, d); got != want {
			t.Errorf("ru %v: %q, want %q", d, got, want)
		}
	}
	if got := b.formatDurationLang(i18n.EN, 2*24*time.Hour); got != "2 days" {
		t.Errorf("en: %q", got)
	}
}
