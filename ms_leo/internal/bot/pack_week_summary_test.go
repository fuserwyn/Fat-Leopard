package bot

import (
	"database/sql"
	"errors"
	"testing"
	"time"
)

func TestNextPackWeeklyGoal(t *testing.T) {
	if got := nextPackWeeklyGoal(75, true); got != 80 {
		t.Errorf("закрыли 75 — следующая 80, got %d", got)
	}
	if got := nextPackWeeklyGoal(75, false); got != 75 {
		t.Errorf("не закрыли — цель та же, got %d", got)
	}
	if got := nextPackWeeklyGoal(maxPackWeeklyWorkoutGoal, true); got != maxPackWeeklyWorkoutGoal {
		t.Errorf("цель не выходит за максимум, got %d", got)
	}
}

func TestPreviousWeekMSK(t *testing.T) {
	cases := []struct {
		now        time.Time
		start, end string
	}{
		// Понедельник 00:30 МСК — это ещё воскресенье по UTC.
		{time.Date(2026, 10, 11, 21, 30, 0, 0, time.UTC), "2026-10-05", "2026-10-11"},
		{time.Date(2026, 10, 18, 20, 0, 0, 0, time.UTC), "2026-10-05", "2026-10-11"},
		{time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC), "2026-09-28", "2026-10-04"},
	}
	for _, c := range cases {
		if s, e := previousWeekMSK(c.now); s != c.start || e != c.end {
			t.Errorf("%v: %s..%s, want %s..%s", c.now, s, e, c.start, c.end)
		}
	}
}

func TestPackWeekSummaryWithoutDB(t *testing.T) {
	var b *Bot
	if s, err := b.EnsurePackWeekSummary(1, time.Now()); s != nil || err != nil {
		t.Fatalf("без базы итогов нет: %v %v", s, err)
	}
	if s, err := b.GetPendingPackWeekSummaryForAPI(1, 1, time.Now()); s != nil || err != nil {
		t.Fatalf("без базы модалки нет: %v %v", s, err)
	}
	if err := b.MarkPackWeekSummarySeenForAPI(1, 1, "2026-10-05"); !errors.Is(err, ErrPackWeekSummaryNotFound) {
		t.Fatalf("без базы отметить нечего: %v", err)
	}
}

// addPackSession — зачтённая тренировка участника в стае на дату.
func addPackSession(t *testing.T, db *sql.DB, userID int64, date string) {
	t.Helper()
	if _, err := db.Exec(`INSERT INTO training_sessions (user_id, chat_id, session_date, trainings_count) VALUES ($1, $2, $3, 1)`,
		userID, itPack, date); err != nil {
		t.Fatal(err)
	}
}

// Понедельник после недели 5–11 октября (МСК).
var summaryMonday = time.Date(2026, 10, 12, 9, 0, 0, 0, time.UTC)

// Закрытая неделя: итоги один раз, цель +5, модалка только участникам и только до просмотра.
func TestPackWeekSummaryClosedWeekRaisesGoal(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	if err := b.db.SetPackWeeklyWorkoutGoal(itPack, 3, itAdmin); err != nil {
		t.Fatal(err)
	}
	addPackSession(t, db, itUser, "2026-10-05")
	addPackSession(t, db, itUser, "2026-10-07")
	addPackSession(t, db, itUser+1, "2026-10-11")
	addPackSession(t, db, itUser+2, "2026-10-04") // прошлая неделя — не в зачёт
	addPackSession(t, db, itUser+2, "2026-10-12") // уже новая неделя — не в зачёт

	s, err := b.EnsurePackWeekSummary(itPack, summaryMonday)
	if err != nil || s == nil {
		t.Fatalf("итоги: %v %v", s, err)
	}
	if s.WeekStart != "2026-10-05" || s.Workouts != 3 || s.Goal != 3 || !s.GoalReached || s.NextGoal != 8 || s.Participants != 2 {
		t.Fatalf("итоги недели: %+v", s)
	}
	if got := b.packWeeklyWorkoutGoal(itPack, "2026-10-12"); got != 8 {
		t.Fatalf("цель новой недели: %d, ждали 8", got)
	}

	// Повторный сбор ничего не меняет: цель растёт один раз.
	if again, err := b.EnsurePackWeekSummary(itPack, summaryMonday.Add(time.Hour)); err != nil || again.NextGoal != 8 {
		t.Fatalf("повтор: %+v %v", again, err)
	}
	if got := b.packWeeklyWorkoutGoal(itPack, "2026-10-12"); got != 8 {
		t.Fatalf("цель после повтора: %d", got)
	}

	p, err := b.GetPendingPackWeekSummaryForAPI(itUser, itPack, summaryMonday)
	if err != nil || p == nil {
		t.Fatalf("модалка участнику: %v %v", p, err)
	}
	if p.MyWorkouts != 2 || p.WeekEnd != "2026-10-11" || p.Workouts != 3 || !p.GoalReached || p.NextGoal != 8 || p.Participants != 2 {
		t.Fatalf("модалка: %+v", p)
	}
	if p, err := b.GetPendingPackWeekSummaryForAPI(itUser+2, itPack, summaryMonday); err != nil || p != nil {
		t.Fatalf("не участвовал в неделе — модалки нет: %+v %v", p, err)
	}

	if err := b.MarkPackWeekSummarySeenForAPI(itUser, itPack, "2026-10-05"); err != nil {
		t.Fatal(err)
	}
	if p, err := b.GetPendingPackWeekSummaryForAPI(itUser, itPack, summaryMonday); err != nil || p != nil {
		t.Fatalf("видел — больше не показываем: %+v %v", p, err)
	}
	if p, _ := b.GetPendingPackWeekSummaryForAPI(itUser+1, itPack, summaryMonday); p == nil || p.MyWorkouts != 1 {
		t.Fatalf("второй участник модалку видит: %+v", p)
	}
	if err := b.MarkPackWeekSummarySeenForAPI(itUser, itPack, "2026-09-28"); !errors.Is(err, ErrPackWeekSummaryNotFound) {
		t.Errorf("неделя без итогов: %v", err)
	}
	if err := b.MarkPackWeekSummarySeenForAPI(itUser, itPack, "вчера"); !errors.Is(err, ErrPackWeekSummaryNotFound) {
		t.Errorf("кривая дата: %v", err)
	}

	// В дашборде админа прошлая неделя показывается со своей целью.
	weeks := b.dashPackWeeks(summaryMonday.In(moscowLocation()), "2026-10-12")
	if len(weeks) < 2 || weeks[1].WeekStart != "2026-10-05" || weeks[1].Goal != 3 || !weeks[1].Reached {
		t.Errorf("дашборд: %+v", weeks)
	}
}

// Неделя не закрыта: цель та же, настройки не трогаем.
func TestPackWeekSummaryOpenWeekKeepsGoal(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	addPackSession(t, db, itUser, "2026-10-06")

	s, err := b.EnsurePackWeekSummary(itPack, summaryMonday)
	if err != nil || s == nil || s.GoalReached || s.Goal != PackWeeklyWorkoutGoal || s.NextGoal != PackWeeklyWorkoutGoal {
		t.Fatalf("итоги: %+v %v", s, err)
	}
	if _, ok, _ := b.db.GetPackWeeklyWorkoutGoal(itPack); ok {
		t.Error("незакрытая неделя не задаёт цель")
	}
	if p, _ := b.GetPendingPackWeekSummaryForAPI(itUser, itPack, summaryMonday); p == nil || p.GoalReached || p.MyWorkouts != 1 {
		t.Fatalf("модалка о незакрытой неделе: %+v", p)
	}
}

// Недели до запуска итогов не подводятся: старая цель не должна поднимать новую.
func TestPackWeekSummarySkipsWeeksBeforeLaunch(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	addPackSession(t, db, itUser, "2026-09-29")
	s, err := b.EnsurePackWeekSummary(itPack, time.Date(2026, 10, 6, 9, 0, 0, 0, time.UTC))
	if err != nil || s != nil {
		t.Fatalf("неделя до запуска: %+v %v", s, err)
	}
}
