package bot

import (
	"strings"
	"testing"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/utils"
)

// Баг-сценарий: остаток 3 дня на старте больничного, болел месяц, вышел через #healthy
// → должно остаться ~3 дня (а раньше из-за рассинхрона флагов кикало сразу).
func TestInactivityKickDeadline_SickLeaveLeftoverPreserved(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	lastTraining := time.Date(2026, 6, 1, 12, 0, 0, 0, moscow)
	d0 := removalDeadlineLocal(lastTraining, 0)        // 9 июня 00:00 MSK
	sickStart := d0.Add(-3 * 24 * time.Hour)           // остаток = 3 дня
	sickEnd := sickStart.Add(30 * 24 * time.Hour)      // болел месяц
	now := sickEnd.Add(time.Minute)                    // только что #healthy

	ml := &domain.MessageLog{
		TimerStartTime:           strPtr(utils.FormatMoscowTime(lastTraining)),
		TimezoneOffsetFromMoscow: 0,
		HasSickLeave:             false,
		HasHealthy:               true,
		SickLeaveStartTime:       strPtr(utils.FormatMoscowTime(sickStart)),
		SickLeaveEndTime:         strPtr(utils.FormatMoscowTime(sickEnd)),
	}
	deadline, ok := inactivityKickDeadline(ml, now)
	if !ok {
		t.Fatal("expected ok=true")
	}
	remaining := deadline.Sub(now)
	if remaining < 2*24*time.Hour || remaining > 4*24*time.Hour {
		t.Errorf("expected ~3 days remaining after recovery, got %v", remaining)
	}
}

// Рассинхрон флагов (легаси-импорт ставит оба флага, end_time нет) не должен давать
// мгновенный кик по старому дедлайну — защитный грейс до ближайшей полуночи.
func TestInactivityKickDeadline_InconsistentFlagsNoImmediateKick(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	lastTraining := time.Date(2026, 5, 1, 12, 0, 0, 0, moscow) // D0 давно в прошлом
	sickStart := time.Date(2026, 5, 5, 12, 0, 0, 0, moscow)
	now := time.Date(2026, 6, 1, 10, 0, 0, 0, moscow)

	ml := &domain.MessageLog{
		TimerStartTime:     strPtr(utils.FormatMoscowTime(lastTraining)),
		HasSickLeave:       true, // оба флага — рассинхрон
		HasHealthy:         true,
		SickLeaveStartTime: strPtr(utils.FormatMoscowTime(sickStart)),
		SickLeaveEndTime:   nil,
	}
	deadline, ok := inactivityKickDeadline(ml, now)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if !deadline.After(now) {
		t.Errorf("expected future grace deadline, got %v (now %v) — immediate-kick regression", deadline, now)
	}
}

func TestRemovalDeadlineLocal(t *testing.T) {
	moscowLoc := time.FixedZone("MSK", 3*3600)

	cases := []struct {
		name        string
		lastTraining time.Time
		tzOffset    int // hours relative to Moscow
		wantDay     int // expected day-of-month of deadline (00:00 local)
		wantMonth   time.Month
		wantHour    int // must be 0 (midnight)
	}{
		{
			name:        "MSK noon → kick 9th at 00:00 MSK",
			lastTraining: time.Date(2024, 1, 1, 12, 0, 0, 0, moscowLoc),
			tzOffset:    0,
			wantDay:     9,
			wantMonth:   time.January,
			wantHour:    0,
		},
		{
			name:        "MSK 23:30 → still kicks 9th (same 7-day window)",
			lastTraining: time.Date(2024, 1, 1, 23, 30, 0, 0, moscowLoc),
			tzOffset:    0,
			wantDay:     9,
			wantMonth:   time.January,
			wantHour:    0,
		},
		{
			name:        "UTC+5 user (tzOffset=+2), training noon local → kicks 9th at 00:00 UTC+5",
			lastTraining: time.Date(2024, 1, 1, 12, 0, 0, 0, time.FixedZone("UTC+5", 5*3600)),
			tzOffset:    2, // UTC+5 = MSK+2
			wantDay:     9,
			wantMonth:   time.January,
			wantHour:    0,
		},
		{
			name:        "month boundary: Jan 25 training → kicks Feb 2",
			lastTraining: time.Date(2024, 1, 25, 10, 0, 0, 0, moscowLoc),
			tzOffset:    0,
			wantDay:     2,
			wantMonth:   time.February,
			wantHour:    0,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := removalDeadlineLocal(c.lastTraining, c.tzOffset)
			loc := userLocalLoc(c.tzOffset)
			gotLocal := got.In(loc)

			if gotLocal.Hour() != c.wantHour || gotLocal.Minute() != 0 || gotLocal.Second() != 0 {
				t.Errorf("expected midnight, got %v", gotLocal)
			}
			if gotLocal.Day() != c.wantDay || gotLocal.Month() != c.wantMonth {
				t.Errorf("got %v, want %d %s", gotLocal, c.wantDay, c.wantMonth)
			}
		})
	}
}

// Активный больничный «замораживает» остаток: сколько бы ни шло время болезни,
// до кика остаётся столько же, сколько было на старте больничного (здесь ~3 дня).
func TestInactivityKickDeadline_ActiveSickLeaveFreezesRemaining(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	lastTraining := time.Date(2026, 6, 1, 12, 0, 0, 0, moscow)
	d0 := removalDeadlineLocal(lastTraining, 0)
	sickStart := d0.Add(-3 * 24 * time.Hour) // остаток 3 дня на старте больничного

	ml := &domain.MessageLog{
		TimerStartTime:     strPtr(utils.FormatMoscowTime(lastTraining)),
		HasSickLeave:       true,
		HasHealthy:         false,
		SickLeaveStartTime: strPtr(utils.FormatMoscowTime(sickStart)),
	}
	for _, daysSick := range []int{10, 40, 90} {
		now := sickStart.Add(time.Duration(daysSick) * 24 * time.Hour)
		dl, ok := inactivityKickDeadline(ml, now)
		if !ok {
			t.Fatalf("expected ok at day %d", daysSick)
		}
		remaining := dl.Sub(now)
		if remaining < 2*24*time.Hour || remaining > 4*24*time.Hour {
			t.Errorf("day %d: expected ~3 days frozen remaining, got %v", daysSick, remaining)
		}
	}
}

// Свежевосстановленный (через админку) или вернувшийся юзер: timer_start = NOW, флаги
// больничного сняты → должно быть полноценное недельное окно, а не мгновенный кик.
func TestInactivityKickDeadline_RestoredUserHasFreshWindow(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	now := time.Date(2026, 6, 10, 12, 0, 0, 0, moscow)

	ml := &domain.MessageLog{
		TimerStartTime: strPtr(utils.FormatMoscowTime(now)),
		HasSickLeave:   false,
		HasHealthy:     false,
	}
	dl, ok := inactivityKickDeadline(ml, now)
	if !ok {
		t.Fatal("expected ok=true")
	}
	if remaining := dl.Sub(now); remaining < 6*24*time.Hour {
		t.Errorf("restored user must get a fresh ~week window, got %v", remaining)
	}
}

// Сценарий: тренировка 1 июля, 4 июля больничный (остаток заморожен), 7 июля выход —
// после #healthy остаток такой же, как на момент взятия больничного; дедлайн сдвигается на дни болезни.
func TestInactivityKickDeadline_SickLeaveFrozenThenRecover_JulyScenario(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	lastTraining := time.Date(2026, 7, 1, 12, 0, 0, 0, moscow)
	d0 := removalDeadlineLocal(lastTraining, 0) // 9 июля 00:00 MSK
	sickStart := time.Date(2026, 7, 4, 12, 0, 0, 0, moscow)
	sickEnd := time.Date(2026, 7, 7, 12, 0, 0, 0, moscow) // 4 дня на больничном
	now := sickEnd.Add(time.Minute)

	frozenAtSickStart := d0.Sub(sickStart)

	mlActive := &domain.MessageLog{
		TimerStartTime:     strPtr(utils.FormatMoscowTime(lastTraining)),
		HasSickLeave:       true,
		HasHealthy:         false,
		SickLeaveStartTime: strPtr(utils.FormatMoscowTime(sickStart)),
	}
	dlActive, ok := inactivityKickDeadline(mlActive, sickStart.Add(48*time.Hour))
	if !ok {
		t.Fatal("active sick: expected ok")
	}
	if rem := dlActive.Sub(sickStart.Add(48 * time.Hour)); rem < frozenAtSickStart-time.Hour || rem > frozenAtSickStart+time.Hour {
		t.Errorf("on sick leave remaining should stay frozen ~%v, got %v", frozenAtSickStart, rem)
	}

	mlRecovered := &domain.MessageLog{
		TimerStartTime:       strPtr(utils.FormatMoscowTime(lastTraining)),
		HasSickLeave:         false,
		HasHealthy:           true,
		SickLeaveStartTime:   strPtr(utils.FormatMoscowTime(sickStart)),
		SickLeaveEndTime:     strPtr(utils.FormatMoscowTime(sickEnd)),
		TimezoneOffsetFromMoscow: 0,
	}
	deadline, ok := inactivityKickDeadline(mlRecovered, now)
	if !ok {
		t.Fatal("after recovery: expected ok")
	}
	remainingAfter := deadline.Sub(now)
	if remainingAfter < frozenAtSickStart-time.Hour || remainingAfter > frozenAtSickStart+time.Hour {
		t.Errorf("after recovery want frozen remaining ~%v, got %v", frozenAtSickStart, remainingAfter)
	}
	wantDeadline := sickEnd.Add(frozenAtSickStart)
	if deadline.Sub(wantDeadline) > time.Hour || wantDeadline.Sub(deadline) > time.Hour {
		t.Errorf("deadline %v, want ~%v (выход + замороженный остаток)", deadline, wantDeadline)
	}
}

func TestSickLeaveRemovalNotice(t *testing.T) {
	onSick := sickLeaveRemovalNotice("6 дней 22 ч.", "08.07.2026, 00:00", false)
	if !strings.Contains(onSick, "⏳ До удаления: 6 дней 22 ч.") {
		t.Fatalf("on sick prefix: %q", onSick)
	}
	if !strings.Contains(onSick, "столько останется после выздоровления") {
		t.Fatalf("on sick frozen note: %q", onSick)
	}
	if !strings.Contains(onSick, "📅 Крайний срок: 08.07.2026, 00:00 (твоё время)") {
		t.Fatalf("deadline line: %q", onSick)
	}

	after := sickLeaveRemovalNotice("22 ч.", "", true)
	if !strings.Contains(after, "⏳ До удаления осталось: 22 ч.") {
		t.Fatalf("after recovery: %q", after)
	}
	if strings.Contains(after, "после выздоровления") {
		t.Fatalf("after recovery must not repeat frozen note: %q", after)
	}
	if strings.Contains(after, "Крайний срок") {
		t.Fatalf("empty deadlineLocal should omit calendar line: %q", after)
	}
}

func TestInactivityKickDeadline_NoTimer(t *testing.T) {
	now := time.Date(2026, 7, 1, 12, 0, 0, 0, time.FixedZone("MSK", 3*3600))
	if _, ok := inactivityKickDeadline(nil, now); ok {
		t.Fatal("nil ml")
	}
	if _, ok := inactivityKickDeadline(&domain.MessageLog{}, now); ok {
		t.Fatal("nil timer")
	}
	if _, ok := inactivityKickDeadline(&domain.MessageLog{
		TimerStartTime:      strPtr("2026-07-01T12:00:00+03:00"),
		IsExemptFromDeletion: true,
	}, now); ok {
		t.Fatal("exempt")
	}
}

func TestNextCalendarMidnightAfterMoscow(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	t0 := time.Date(2026, 7, 7, 23, 30, 0, 0, moscow)
	got := nextCalendarMidnightAfterMoscow(t0)
	y, m, d := got.In(moscow).Date()
	if y != 2026 || m != time.July || d != 8 || got.In(moscow).Hour() != 0 {
		t.Fatalf("got %v", got)
	}
}

func TestInactiveWarningAtLocal_NineAMNotMidnight(t *testing.T) {
	moscow := time.FixedZone("MSK", 3*3600)
	removalAt := time.Date(2026, 7, 9, 0, 0, 0, 0, moscow)
	loc := moscow

	for _, hoursBefore := range []int{72, 48, 24} {
		got := inactiveWarningAtLocal(removalAt, hoursBefore, loc)
		if got.Hour() != 9 || got.Minute() != 0 {
			t.Fatalf("hoursBefore=%d: want 9:00 local, got %v", hoursBefore, got.In(loc))
		}
	}
	day5 := inactiveWarningAtLocal(removalAt, 72, loc)
	if day5.Day() != 6 {
		t.Fatalf("day 5 warning: want 6 July, got %v", day5.In(loc))
	}
}
