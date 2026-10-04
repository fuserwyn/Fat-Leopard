package database

import (
	"database/sql"
	"os"
	"reflect"
	"strings"
	"testing"

	_ "github.com/lib/pq"
)

func TestSequentialFunnelSQLShape(t *testing.T) {
	q, args := sequentialFunnelSQL([]string{"a", "b", "c"}, 7, 7, true)
	if !reflect.DeepEqual(args, []any{"a", "b", "c", 14, 7}) {
		t.Fatalf("args: %v", args)
	}
	for _, want := range []string{"s0 AS", "s2 AS", "IN ($2, $3)", "IN ($3)", "make_interval(days => $4)", "make_interval(days => $5)"} {
		if !strings.Contains(q, want) {
			t.Errorf("нет %q в запросе:\n%s", want, q)
		}
	}
	q, args = sequentialFunnelSQL([]string{"a", "b"}, 0, 0, true)
	if len(args) != 2 || strings.Contains(q, "make_interval") {
		t.Fatalf("всё время — без окна: %v\n%s", args, q)
	}
}

// Интеграционный тест на настоящем Postgres: LEO_TEST_PG_DSN=postgres://… go test.
func TestAnalyticsDashboardQueries(t *testing.T) {
	dsn := os.Getenv("LEO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("LEO_TEST_PG_DSN не задан")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := db.Exec(q, args...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	exec(`DROP TABLE IF EXISTS events, bot_visits, training_sessions, pack_weekly_goal_bonus`)
	exec(`CREATE TABLE events (
		id BIGSERIAL PRIMARY KEY, event_name VARCHAR(64) NOT NULL, user_id BIGINT, telegram_id BIGINT,
		occurred_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT (NOW() AT TIME ZONE 'Europe/Moscow'),
		source VARCHAR(32))`)
	exec(`CREATE TABLE bot_visits (id BIGSERIAL PRIMARY KEY, user_id BIGINT NOT NULL, visited_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW())`)
	exec(`CREATE TABLE training_sessions (chat_id BIGINT, session_date DATE, is_bonus BOOLEAN DEFAULT FALSE, trainings_count INT DEFAULT 1)`)
	exec(`CREATE TABLE pack_weekly_goal_bonus (pack_chat_id BIGINT, week_start_date DATE)`)

	ev := func(person int64, name string, hoursAgo int) {
		t.Helper()
		exec(`INSERT INTO events (event_name, telegram_id, occurred_at)
		      VALUES ($1, $2, (NOW() AT TIME ZONE 'Europe/Moscow') - make_interval(hours => $3))`, name, person, hoursAgo)
	}
	// 1: полный путь без «выбора способа» и «к оплате».
	ev(1, EventBotStarted, 72)
	ev(1, EventPaywallViewed, 71)
	ev(1, EventPaymentCompleted, 50)
	ev(1, EventMiniappOpened, 49)
	ev(1, EventWorkoutLogged, 20)
	ev(1, EventWorkoutLogged, 2)
	// 2: только стартовал.
	ev(2, EventBotStarted, 70)
	// 3: оплатил давно, мини-апп открыл на этой неделе — в воронку недели не входит.
	ev(3, EventBotStarted, 24*40)
	ev(3, EventPaymentCompleted, 24*40-1)
	ev(3, EventMiniappOpened, 30)
	ev(3, EventWorkoutLogged, 24*3)
	// 4: стартовал в прошлом периоде.
	ev(4, EventBotStarted, 24*10)

	d := &Database{db: db}
	stages := []string{EventBotStarted, EventPaywallViewed, EventPaymentMethodSelected, EventPaymentInitiated, EventPaymentCompleted, EventMiniappOpened}

	got, err := d.SequentialFunnelCounts(stages, 7, 0, true)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int64{2, 1, 1, 1, 1, 1}; !reflect.DeepEqual(got, want) {
		t.Errorf("воронка за 7 дней: %v, ждали %v", got, want)
	}
	if got, err = d.SequentialFunnelCounts(stages, 7, 7, true); err != nil || !reflect.DeepEqual(got, []int64{1, 0, 0, 0, 0, 0}) {
		t.Errorf("воронка прошлого периода: %v %v", got, err)
	}
	if got, err = d.SequentialFunnelCounts(stages, 0, 0, true); err != nil || !reflect.DeepEqual(got, []int64{4, 2, 2, 2, 2, 2}) {
		t.Errorf("воронка за всё время: %v %v", got, err)
	}
	// Не первое за всю историю, а первое в окне: 3-й открыл мини-апп на этой неделе.
	if got, err = d.SequentialFunnelCounts([]string{EventMiniappOpened, EventWorkoutLogged}, 7, 0, false); err != nil || !reflect.DeepEqual(got, []int64{2, 1}) {
		t.Errorf("воронка по окну: %v %v", got, err)
	}

	counts, err := d.EventWindowCounts(7, 0)
	if err != nil {
		t.Fatal(err)
	}
	if c := counts[EventWorkoutLogged]; c.Total != 3 || c.Unique != 2 {
		t.Errorf("тренировки за 7 дней: %+v", c)
	}
	if c := counts[EventBotStarted]; c.Unique != 2 {
		t.Errorf("старты за 7 дней: %+v", c)
	}
	if prev, err := d.EventWindowCounts(7, 7); err != nil || prev[EventBotStarted].Unique != 1 {
		t.Errorf("старты прошлого периода: %+v %v", prev, err)
	}
	if all, err := d.EventWindowCounts(0, 0); err != nil || all[EventBotStarted].Unique != 4 {
		t.Errorf("старты за всё время: %+v %v", all, err)
	}

	var from string
	if err := db.QueryRow(`SELECT to_char((NOW() AT TIME ZONE 'Europe/Moscow') - INTERVAL '6 days', 'YYYY-MM-DD')`).Scan(&from); err != nil {
		t.Fatal(err)
	}
	series, err := d.EventSeries(from, false)
	if err != nil {
		t.Fatal(err)
	}
	var workouts, starts, payments int64
	for _, r := range series {
		workouts += r.Workouts
		starts += r.Starts
		payments += r.Payments
	}
	if workouts != 3 || starts != 2 || payments != 1 {
		t.Errorf("ряды: тренировок %d, стартов %d, оплат %d (%+v)", workouts, starts, payments, series)
	}
	if _, err := d.EventSeries(from, true); err != nil {
		t.Errorf("ряды по неделям: %v", err)
	}

	day, week, month, err := d.ActiveUserCounts()
	if err != nil || day != 1 || week != 2 || month != 2 {
		t.Errorf("активные: %d/%d/%d %v", day, week, month, err)
	}

	cohorts, err := d.PaidCohortRetention("2000-01-03")
	if err != nil {
		t.Fatal(err)
	}
	var size, d1, d30 int64
	for _, c := range cohorts {
		size += c.Size
		d1 += c.D1
		d30 += c.D30
	}
	// 1-й тренировался спустя 30 и 48 часов после оплаты, 3-й — спустя 36 дней.
	if size != 2 || d1 != 2 || d30 != 1 {
		t.Errorf("когорты: размер %d, D1 %d, D30 %d (%+v)", size, d1, d30, cohorts)
	}

	exec(`INSERT INTO bot_visits (user_id, visited_at) VALUES (1, NOW()), (1, NOW()), (2, NOW() - INTERVAL '20 days')`)
	if v, u, err := d.BotVisitWindowStats(7); err != nil || v != 2 || u != 1 {
		t.Errorf("визиты за 7 дней: %d/%d %v", v, u, err)
	}
	if v, u, err := d.BotVisitWindowStats(0); err != nil || v != 3 || u != 2 {
		t.Errorf("визиты за всё время: %d/%d %v", v, u, err)
	}

	exec(`INSERT INTO training_sessions (chat_id, session_date) VALUES (5, '2026-09-28'), (5, '2026-09-30'), (5, '2026-10-05'), (6, '2026-09-29')`)
	exec(`INSERT INTO training_sessions (chat_id, session_date, is_bonus) VALUES (5, '2026-09-29', TRUE)`)
	exec(`INSERT INTO pack_weekly_goal_bonus VALUES (5, '2026-09-28')`)
	hist, err := d.PackWeeklyWorkoutHistory(5, "2026-09-28")
	if err != nil || hist["2026-09-28"] != 2 || hist["2026-10-05"] != 1 {
		t.Errorf("недели стаи: %v %v", hist, err)
	}
	if bonus, err := d.PackWeeklyGoalBonusWeeks(5, "2026-09-28"); err != nil || !bonus["2026-09-28"] || len(bonus) != 1 {
		t.Errorf("бонусные недели: %v %v", bonus, err)
	}
}
