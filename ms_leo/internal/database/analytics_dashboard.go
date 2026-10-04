package database

import (
	"fmt"
	"strings"
)

// Запросы для дашбордов админки: окна со сдвигом (сравнение с прошлым
// периодом), последовательные воронки, ряды по дням и когорты.

// analyticsNowSQL — «сейчас» в том же виде, в каком DEFAULT пишет occurred_at
// (московский wall-clock). Сравниваем с сырой колонкой, чтобы работал индекс
// (event_name, occurred_at).
const analyticsNowSQL = "(NOW() AT TIME ZONE 'Europe/Moscow')"

// analyticsLocalSQL — московское время события для разбивки по дням и неделям:
// обратное преобразование к тому, что записал DEFAULT.
const analyticsLocalSQL = "(occurred_at AT TIME ZONE current_setting('TimeZone'))"

const analyticsPersonSQL = "COALESCE(telegram_id, user_id)"

// analyticsRangeClause — условие окна [сейчас−offset−days, сейчас−offset)
// для колонки column. days<=0 — окна нет (всё время). Номера аргументов
// начинаются с argStart.
func analyticsRangeClause(column string, days, offsetDays, argStart int) (string, []any) {
	if days <= 0 {
		return "", nil
	}
	if offsetDays < 0 {
		offsetDays = 0
	}
	return fmt.Sprintf(
		" AND %[1]s >= %[2]s - make_interval(days => $%[3]d) AND %[1]s < %[2]s - make_interval(days => $%[4]d)",
		column, analyticsNowSQL, argStart, argStart+1,
	), []any{days + offsetDays, offsetDays}
}

// EventWindowCount — сколько событий и уникальных персон в окне.
type EventWindowCount struct {
	Total  int64
	Unique int64
}

// EventWindowCounts — счётчики по каждому событию в окне длиной days,
// сдвинутом на offsetDays назад (offsetDays=days — предыдущий период).
func (d *Database) EventWindowCounts(days, offsetDays int) (map[string]EventWindowCount, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	window, args := analyticsRangeClause("occurred_at", days, offsetDays, 1)
	rows, err := d.db.Query(`
		SELECT event_name, COUNT(*), COUNT(DISTINCT `+analyticsPersonSQL+`)
		FROM events
		WHERE TRUE`+window+`
		GROUP BY event_name
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("event window counts: %w", err)
	}
	defer rows.Close()
	out := make(map[string]EventWindowCount)
	for rows.Next() {
		var name string
		var c EventWindowCount
		if err := rows.Scan(&name, &c.Total, &c.Unique); err != nil {
			return nil, err
		}
		out[name] = c
	}
	return out, rows.Err()
}

// sequentialFunnelSQL — запрос последовательной воронки.
//
// Когорта (стадия 0) — персоны с первым событием stages[0]: за всю историю
// (firstEver) или внутри окна. В стадию N попадает тот, кто после стадии N−1
// сделал событие стадии N или любой более поздней: пропущенный необязательный
// шаг (например, выбор способа оплаты) не выбрасывает человека из воронки.
// Поэтому числа не растут от стадии к стадии, а конверсия не больше 100%.
func sequentialFunnelSQL(stages []string, days, offsetDays int, firstEver bool) (string, []any) {
	args := make([]any, 0, len(stages)+2)
	for _, s := range stages {
		args = append(args, s)
	}
	window, wargs := analyticsRangeClause("occurred_at", days, offsetDays, len(stages)+1)
	args = append(args, wargs...)

	var b strings.Builder
	b.WriteString("WITH s0 AS (\n")
	if firstEver {
		firstWindow, _ := analyticsRangeClause("t", days, offsetDays, len(stages)+1)
		b.WriteString("  SELECT person, t FROM (\n")
		b.WriteString("    SELECT " + analyticsPersonSQL + " AS person, MIN(occurred_at) AS t\n")
		b.WriteString("    FROM events WHERE event_name = $1 GROUP BY 1\n")
		b.WriteString("  ) first WHERE person IS NOT NULL" + firstWindow + "\n")
	} else {
		b.WriteString("  SELECT " + analyticsPersonSQL + " AS person, MIN(occurred_at) AS t\n")
		b.WriteString("  FROM events WHERE event_name = $1 AND " + analyticsPersonSQL + " IS NOT NULL" + window + "\n")
		b.WriteString("  GROUP BY 1\n")
	}
	b.WriteString(")")
	// Верхняя граница окна действует и на следующие стадии — иначе у прошлого
	// периода было бы больше времени на конверсию, чем у текущего.
	upper := ""
	if days > 0 {
		upper = fmt.Sprintf(" AND e.occurred_at < %s - make_interval(days => $%d)", analyticsNowSQL, len(stages)+2)
	}
	for i := 1; i < len(stages); i++ {
		later := make([]string, 0, len(stages)-i)
		for j := i; j < len(stages); j++ {
			later = append(later, fmt.Sprintf("$%d", j+1))
		}
		fmt.Fprintf(&b, ",\ns%d AS (\n", i)
		b.WriteString("  SELECT p.person, MIN(e.occurred_at) AS t\n")
		fmt.Fprintf(&b, "  FROM s%d p\n", i-1)
		b.WriteString("  JOIN events e ON e.event_name IN (" + strings.Join(later, ", ") + ")\n")
		b.WriteString("   AND COALESCE(e.telegram_id, e.user_id) = p.person\n")
		b.WriteString("   AND e.occurred_at >= p.t" + upper + "\n")
		b.WriteString("  GROUP BY p.person\n)")
	}
	b.WriteString("\nSELECT ")
	for i := range stages {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "(SELECT COUNT(*) FROM s%d)", i)
	}
	return b.String(), args
}

// SequentialFunnelCounts — сколько персон дошло до каждой стадии воронки.
func (d *Database) SequentialFunnelCounts(stages []string, days, offsetDays int, firstEver bool) ([]int64, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	if len(stages) == 0 {
		return nil, nil
	}
	query, args := sequentialFunnelSQL(stages, days, offsetDays, firstEver)
	out := make([]int64, len(stages))
	dest := make([]any, len(stages))
	for i := range out {
		dest[i] = &out[i]
	}
	if err := d.db.QueryRow(query, args...).Scan(dest...); err != nil {
		return nil, fmt.Errorf("sequential funnel: %w", err)
	}
	return out, nil
}

// EventSeriesRow — одна корзина (день или неделя, YYYY-MM-DD её начала).
type EventSeriesRow struct {
	Bucket      string
	ActiveUsers int64 // уникальные персоны с тренировкой
	Workouts    int64
	Starts      int64 // уникальные персоны, запустившие бота
	Payments    int64
}

// EventSeries — ряды по дням или неделям начиная с fromDate (YYYY-MM-DD, МСК).
// Пустые корзины не возвращаются — их дополняет вызывающий.
func (d *Database) EventSeries(fromDate string, weekly bool) ([]EventSeriesRow, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	unit := "day"
	if weekly {
		unit = "week"
	}
	rows, err := d.db.Query(`
		SELECT to_char(date_trunc('`+unit+`', `+analyticsLocalSQL+`), 'YYYY-MM-DD') AS bucket,
		       COUNT(DISTINCT `+analyticsPersonSQL+`) FILTER (WHERE event_name = $2),
		       COUNT(*) FILTER (WHERE event_name = $2),
		       COUNT(DISTINCT `+analyticsPersonSQL+`) FILTER (WHERE event_name = $3),
		       COUNT(*) FILTER (WHERE event_name = $4)
		FROM events
		WHERE event_name IN ($2, $3, $4)
		  AND occurred_at >= $1::timestamp
		GROUP BY 1
		ORDER BY 1
	`, fromDate, EventWorkoutLogged, EventBotStarted, EventPaymentCompleted)
	if err != nil {
		return nil, fmt.Errorf("event series: %w", err)
	}
	defer rows.Close()
	var out []EventSeriesRow
	for rows.Next() {
		var r EventSeriesRow
		if err := rows.Scan(&r.Bucket, &r.ActiveUsers, &r.Workouts, &r.Starts, &r.Payments); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ActiveUserCounts — сколько персон тренировалось за последние сутки, 7 и 30 дней.
func (d *Database) ActiveUserCounts() (day, week, month int64, err error) {
	if d == nil || d.db == nil {
		return 0, 0, 0, fmt.Errorf("nil database")
	}
	err = d.db.QueryRow(`
		SELECT COUNT(DISTINCT person) FILTER (WHERE occurred_at >= `+analyticsNowSQL+` - INTERVAL '1 day'),
		       COUNT(DISTINCT person) FILTER (WHERE occurred_at >= `+analyticsNowSQL+` - INTERVAL '7 days'),
		       COUNT(DISTINCT person)
		FROM (
			SELECT `+analyticsPersonSQL+` AS person, occurred_at
			FROM events
			WHERE event_name = $1
			  AND occurred_at >= `+analyticsNowSQL+` - INTERVAL '30 days'
		) w
	`, EventWorkoutLogged).Scan(&day, &week, &month)
	if err != nil {
		return 0, 0, 0, fmt.Errorf("active user counts: %w", err)
	}
	return day, week, month, nil
}

// PaidCohortRow — недельная когорта оплативших и сколько из них тренировалось
// спустя 1, 7 и 30 дней после оплаты (или позже).
type PaidCohortRow struct {
	WeekStart string // понедельник, YYYY-MM-DD
	Size      int64
	D1        int64
	D7        int64
	D30       int64
}

// PaidCohortRetention — когорты по неделе первой оплаты начиная с fromDate
// (понедельник, YYYY-MM-DD, МСК), от новых к старым.
func (d *Database) PaidCohortRetention(fromDate string) ([]PaidCohortRow, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	rows, err := d.db.Query(`
		WITH paid AS (
			SELECT `+analyticsPersonSQL+` AS person, MIN(occurred_at) AS t
			FROM events
			WHERE event_name = $2
			GROUP BY 1
		),
		cohort AS (
			SELECT person, t,
			       date_trunc('week', t AT TIME ZONE current_setting('TimeZone')) AS wk
			FROM paid
			WHERE person IS NOT NULL AND t >= $1::timestamp
		),
		span AS (
			SELECT c.person, c.wk, MAX(e.occurred_at - c.t) AS last_gap
			FROM cohort c
			LEFT JOIN events e ON e.event_name = $3
			 AND COALESCE(e.telegram_id, e.user_id) = c.person
			 AND e.occurred_at >= c.t
			GROUP BY c.person, c.wk
		)
		SELECT to_char(wk, 'YYYY-MM-DD'),
		       COUNT(*),
		       COUNT(*) FILTER (WHERE last_gap >= INTERVAL '1 day'),
		       COUNT(*) FILTER (WHERE last_gap >= INTERVAL '7 days'),
		       COUNT(*) FILTER (WHERE last_gap >= INTERVAL '30 days')
		FROM span
		GROUP BY wk
		ORDER BY wk DESC
	`, fromDate, EventPaymentCompleted, EventWorkoutLogged)
	if err != nil {
		return nil, fmt.Errorf("paid cohort retention: %w", err)
	}
	defer rows.Close()
	var out []PaidCohortRow
	for rows.Next() {
		var r PaidCohortRow
		if err := rows.Scan(&r.WeekStart, &r.Size, &r.D1, &r.D7, &r.D30); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// PackWeeklyWorkoutHistory — тренировки стаи по неделям начиная с fromDate
// (понедельник, YYYY-MM-DD): ключ — понедельник недели.
func (d *Database) PackWeeklyWorkoutHistory(packChatID int64, fromDate string) (map[string]int, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	rows, err := d.db.Query(`
		SELECT to_char(date_trunc('week', session_date::timestamp), 'YYYY-MM-DD'), COUNT(*)
		FROM training_sessions
		WHERE chat_id = $1
		  AND session_date >= $2::date
		  AND is_bonus = FALSE
		  AND trainings_count > 0
		GROUP BY 1
	`, packChatID, fromDate)
	if err != nil {
		return nil, fmt.Errorf("pack weekly history: %w", err)
	}
	defer rows.Close()
	out := make(map[string]int)
	for rows.Next() {
		var wk string
		var n int
		if err := rows.Scan(&wk, &n); err != nil {
			return nil, err
		}
		out[wk] = n
	}
	return out, rows.Err()
}

// PackWeeklyGoalBonusWeeks — недели (понедельник, YYYY-MM-DD), за которые стае
// уже выдан бонус недельной цели.
func (d *Database) PackWeeklyGoalBonusWeeks(packChatID int64, fromDate string) (map[string]bool, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	rows, err := d.db.Query(`
		SELECT to_char(week_start_date, 'YYYY-MM-DD')
		FROM pack_weekly_goal_bonus
		WHERE pack_chat_id = $1 AND week_start_date >= $2::date
	`, packChatID, fromDate)
	if err != nil {
		return nil, fmt.Errorf("pack weekly bonus weeks: %w", err)
	}
	defer rows.Close()
	out := make(map[string]bool)
	for rows.Next() {
		var wk string
		if err := rows.Scan(&wk); err != nil {
			return nil, err
		}
		out[wk] = true
	}
	return out, rows.Err()
}

// BotVisitWindowStats — визиты и уникальные посетители бота за days дней
// (days<=0 — за всё время).
func (d *Database) BotVisitWindowStats(days int) (visits, unique int64, err error) {
	if d == nil || d.db == nil {
		return 0, 0, fmt.Errorf("nil database")
	}
	where, args := analyticsWindowClause("visited_at", days)
	err = d.db.QueryRow(`SELECT COUNT(*), COUNT(DISTINCT user_id) FROM bot_visits`+where, args...).Scan(&visits, &unique)
	if err != nil {
		return 0, 0, fmt.Errorf("bot visit window: %w", err)
	}
	return visits, unique, nil
}
