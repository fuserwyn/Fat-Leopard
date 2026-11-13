package bot

import (
	"testing"
	"time"
)

func mustDate(t *testing.T, s string) time.Time {
	t.Helper()
	d, err := time.Parse("2006-01-02", s)
	if err != nil {
		t.Fatalf("bad date %q: %v", s, err)
	}
	return d
}

func strPtr(s string) *string { return &s }

func TestComputeStreakDays(t *testing.T) {
	today := "2026-05-29"

	cases := []struct {
		name        string
		last        *string
		prevStreak  int
		wantStreak  int
		wantSameDay bool
	}{
		{"first ever training", nil, 0, 1, false},
		{"trained yesterday -> +1", strPtr("2026-05-28"), 4, 5, false},
		{"same day repeat keeps streak", strPtr("2026-05-29"), 6, 6, true},
		{"same day repeat with zero streak", strPtr("2026-05-29"), 0, 0, true},
		{"gap of 2 days resets to 1", strPtr("2026-05-27"), 9, 1, false},
		{"big gap resets to 1", strPtr("2026-01-01"), 100, 1, false},
		// Регрессия на расхождение путей: админ начислил стрик, но тренировок ещё не было
		// (last_training_date == nil). Стрик должен продолжаться, а не сбрасываться в 1.
		{"admin-granted streak, no training yet -> continues", nil, 10, 11, false},
		// Пустая строка трактуется так же, как nil.
		{"empty last date, no streak", strPtr(""), 0, 1, false},
		{"empty last date with streak continues", strPtr("   "), 3, 4, false},
		// Граница milestone: 6 -> 7 (первая ачивка).
		{"crossing into weekly milestone", strPtr("2026-05-28"), 6, 7, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotStreak, gotSame := ComputeStreakDays(c.last, c.prevStreak, mustDate(t, today))
			if gotStreak != c.wantStreak || gotSame != c.wantSameDay {
				t.Errorf("ComputeStreakDays(%v, %d) = (%d, %v), want (%d, %v)",
					c.last, c.prevStreak, gotStreak, gotSame, c.wantStreak, c.wantSameDay)
			}
		})
	}
}

// Стрик не должен ломаться на границе месяца/года (yesterday вычисляется через time, а не вычитанием строк).
func TestComputeStreakDays_DateBoundaries(t *testing.T) {
	// 1 марта; вчера = 28 февраля (2026 — не високосный).
	got, _ := ComputeStreakDays(strPtr("2026-02-28"), 5, mustDate(t, "2026-03-01"))
	if got != 6 {
		t.Errorf("month boundary: got %d, want 6", got)
	}
	// 1 января; вчера = 31 декабря прошлого года.
	got, _ = ComputeStreakDays(strPtr("2025-12-31"), 5, mustDate(t, "2026-01-01"))
	if got != 6 {
		t.Errorf("year boundary: got %d, want 6", got)
	}
	// Не вчера через границу года — сброс.
	got, _ = ComputeStreakDays(strPtr("2025-12-30"), 5, mustDate(t, "2026-01-01"))
	if got != 1 {
		t.Errorf("year boundary gap: got %d, want 1", got)
	}
}

func TestEffectiveStreakDays(t *testing.T) {
	cases := []struct {
		stored    int
		daysSince int
		want      int
	}{
		{0, 0, 0},
		{420, -1, 420}, // дата неизвестна — показываем сохранённый
		{420, 0, 420},  // тренировался сегодня
		{420, 1, 420},  // вчера — ещё жив до полночи
		{420, 2, 0},    // пропущен день после последней — сгорел
		{420, 10, 0},
		{1, 2, 0},
	}
	for _, c := range cases {
		if got := EffectiveStreakDays(c.stored, c.daysSince); got != c.want {
			t.Errorf("EffectiveStreakDays(%d, %d) = %d, want %d", c.stored, c.daysSince, got, c.want)
		}
	}
}

func TestStreakSaveAttemptsMaxForLevel(t *testing.T) {
	cases := []struct {
		level int
		want  int
	}{
		{0, 1},  // защита от <1
		{-3, 1}, // защита от отрицательных
		{1, 1},
		{2, 2},
		{6, 6},
		{7, 6},   // кап = MaxLevel
		{8, 6},   // кап
		{100, 6}, // кап
	}
	for _, c := range cases {
		if got := StreakSaveAttemptsMaxForLevel(c.level); got != c.want {
			t.Errorf("StreakSaveAttemptsMaxForLevel(%d) = %d, want %d", c.level, got, c.want)
		}
	}
}

func TestStreakSaveWindowError(t *testing.T) {
	cases := []struct {
		days, avail int
		want        string
	}{
		{2, 1, ""},            // окно открыто
		{2, 0, "no_attempts"}, // попыток нет
		{2, -1, "no_attempts"},
		{0, 1, "not_needed"}, // тренировался сегодня
		{1, 1, "not_needed"}, // догон — ещё можно потренироваться
		{3, 1, "too_late"},   // пропущено больше одного дня
		{10, 2, "too_late"},
		{-1, 1, "no_training_history"},
		{-1, 0, "no_attempts"}, // attempts проверяется первым
	}
	for _, c := range cases {
		if got := StreakSaveWindowError(c.days, c.avail); got != c.want {
			t.Errorf("StreakSaveWindowError(%d, %d) = %q, want %q", c.days, c.avail, got, c.want)
		}
	}
}

func TestRestoreStreakForSave(t *testing.T) {
	cases := []struct {
		stored, max, want int
	}{
		{107, 107, 107}, // обычный случай: в БД ещё лежит сгоревший стрик
		{0, 107, 107},   // после reconcile — берём рекорд
		{50, 100, 50},   // stored > 0 важнее max
		{0, 0, 0},       // нечего спасать
		{-1, 10, 10},
	}
	for _, c := range cases {
		if got := restoreStreakForSave(c.stored, c.max); got != c.want {
			t.Errorf("restoreStreakForSave(%d, %d) = %d, want %d", c.stored, c.max, got, c.want)
		}
	}
}

// Полный сценарий UI: effective streak + окно спасения (кейс Анечки / пропуск дня).
func TestStreakSaveScenarioMatrix(t *testing.T) {
	const avail = 1
	cases := []struct {
		name       string
		stored     int
		daysSince  int
		wantShown  int // EffectiveStreakDays
		wantWindow string
	}{
		{"trained today", 107, 0, 107, "not_needed"},
		{"grace day (Анечка 18.07 после отчёта 17.07)", 107, 1, 107, "not_needed"},
		{"burned — окно спасения", 107, 2, 0, ""},
		{"too late after burn", 107, 3, 0, "too_late"},
		{"no attempts on burn day", 107, 2, 0, "no_attempts"}, // overridden below
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			shown := EffectiveStreakDays(c.stored, c.daysSince)
			if shown != c.wantShown {
				t.Fatalf("EffectiveStreakDays = %d, want %d", shown, c.wantShown)
			}
			a := avail
			wantWin := c.wantWindow
			if c.name == "no attempts on burn day" {
				a = 0
				wantWin = "no_attempts"
			}
			if got := StreakSaveWindowError(c.daysSince, a); got != wantWin {
				t.Fatalf("window = %q, want %q", got, wantWin)
			}
			if wantWin == "" {
				if restoreStreakForSave(c.stored, c.stored) != c.stored {
					t.Fatal("restore should keep stored streak")
				}
			}
		})
	}
}

// Больничный не должен жечь стрик: дни болезни вычитаются из промежутка последняя-тренировка → сегодня.
func TestSickAdjustedLastTrainingDate(t *testing.T) {
	cases := []struct {
		name                            string
		last, today, sickStart, sickEnd string
		want                            string
	}{
		// Активный больничный (sickEnd == today): тренировка 01-го, болезнь со 2-го, сегодня 6-е.
		// 4 дня болезни (2..6) сдвигают «последнюю тренировку» на 01+4 = 05 → до сегодня всего 1 день (grace).
		{"active sick leave freezes streak", "2026-05-01", "2026-05-06", "2026-05-02", "2026-05-06", "2026-05-05"},
		// Завершённый больничный ровно закрыл промежуток: вернулся и тренируется в день выздоровления.
		{"recovered same day bridges gap", "2026-05-01", "2026-05-05", "2026-05-02", "2026-05-05", "2026-05-04"},
		// Больничный + реальное безделье после выздоровления: болезнь 02→04 (2 дня), потом ничего
		// до 08 → защищаются только дни болезни (сдвиг на 2 дня, дальше стрик догорает).
		{"sick then slacking keeps only sick days", "2026-05-01", "2026-05-08", "2026-05-02", "2026-05-04", "2026-05-03"},
		// Больничного не было в промежутке (начался ещё до последней тренировки) — дата не меняется.
		{"sick before last training has no effect", "2026-05-10", "2026-05-12", "2026-05-01", "2026-05-03", "2026-05-10"},
		// Битые даты — возвращаем исходную.
		{"unparseable dates fall back to original", "bad", "2026-05-06", "2026-05-02", "2026-05-06", "bad"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := sickAdjustedLastTrainingDate(c.last, c.today, c.sickStart, c.sickEnd); got != c.want {
				t.Errorf("sickAdjustedLastTrainingDate(%q,%q,%q,%q) = %q, want %q",
					c.last, c.today, c.sickStart, c.sickEnd, got, c.want)
			}
		})
	}
}

// Сквозной сценарий: болел, вернулся — стрик продолжается (+1), а не обнуляется.
func TestSickAdjustedLastTrainingDate_ContinuesStreakOnReturn(t *testing.T) {
	// Стрик 20, тренировка 01-го, болезнь 02..05 (активна), возвращается и тренируется 06-го.
	adj := sickAdjustedLastTrainingDate("2026-05-01", "2026-05-06", "2026-05-02", "2026-05-06")
	newStreak, sameDay := ComputeStreakDays(strPtr(adj), 20, mustDate(t, "2026-05-06"))
	if sameDay {
		t.Fatalf("return day must count as a training day, got sameDay=true (adj=%q)", adj)
	}
	if newStreak != 21 {
		t.Errorf("streak after sick leave = %d, want 21 (adj=%q)", newStreak, adj)
	}
}

func TestDaysWordForm(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "day"},
		{2, "days"},
		{3, "days"},
		{4, "days"},
		{5, "days"},
		{10, "days"},
		{11, "days"},
		{12, "days"},
		{14, "days"},
		{21, "days"},
		{22, "days"},
		{25, "days"},
		{100, "days"},
		{101, "days"},
		{111, "days"},
	}
	for _, c := range cases {
		if got := daysWordForm(c.n); got != c.want {
			t.Errorf("daysWordForm(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestCupsWordForm(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "cup"},
		{2, "cups"},
		{5, "cups"},
		{11, "cups"},
		{21, "cups"},
		{42, "cups"},
		{420, "cups"},
	}
	for _, c := range cases {
		if got := cupsWordForm(c.n); got != c.want {
			t.Errorf("cupsWordForm(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

// MilestoneCups должен начислять бонус только при первой тренировке дня (EarnRewards) и точном совпадении порога.
func TestTrainingOutcome_MilestoneCups(t *testing.T) {
	cases := []struct {
		name string
		o    TrainingOutcome
		want int
	}{
		{"7-day milestone", TrainingOutcome{EarnRewards: true, NewStreakDays: 7}, 42},
		{"30-day milestone", TrainingOutcome{EarnRewards: true, NewStreakDays: 30}, 420},
		{"100-day milestone", TrainingOutcome{EarnRewards: true, NewStreakDays: 100}, 4200},
		{"non-milestone day", TrainingOutcome{EarnRewards: true, NewStreakDays: 8}, 0},
		{"milestone but no rewards (same day)", TrainingOutcome{EarnRewards: false, NewStreakDays: 7}, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := c.o.MilestoneCups(); got != c.want {
				t.Errorf("MilestoneCups() = %d, want %d", got, c.want)
			}
		})
	}
}
