package database

import (
	"database/sql"
	"fmt"
	"time"
)

// Чтение аналитики из таблицы events для админ-панели (analytics_BT_v1.md §3–§7).
// Все срезы считаются по окну в днях: days <= 0 означает «за всё время».

// EventStatRow — агрегат по одному типу события (обзор «все события»).
type EventStatRow struct {
	Name        string
	Total       int64        // всего событий
	UniqueUsers int64        // уникальных персон (telegram_id/user_id)
	LastAt      sql.NullTime // когда событие случилось в последний раз
}

// ChannelStatRow — атрибуция канала по deep-link source (§8).
type ChannelStatRow struct {
	Source  string
	Started int64 // bot_started с этим source
	Paid    int64 // payment_completed от персон с этим source
}

// analyticsWindowClause возвращает SQL-условие окна и аргументы.
// Для days<=0 окна нет (всё время).
func analyticsWindowClause(column string, days int) (string, []any) {
	if days <= 0 {
		return "", nil
	}
	return fmt.Sprintf(" WHERE %s >= NOW() - make_interval(days => $1)", column), []any{days}
}

// GetEventOverview — сводка по всем типам событий в окне (analytics_BT_v1 §2/§7).
// Это «все таблицы аналитики» в одном читаемом виде: что и сколько трекается.
func (d *Database) GetEventOverview(days int) ([]EventStatRow, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	where, args := analyticsWindowClause("occurred_at", days)
	rows, err := d.db.Query(`
		SELECT event_name,
		       COUNT(*)                                       AS total,
		       COUNT(DISTINCT COALESCE(telegram_id, user_id)) AS uniq,
		       MAX(occurred_at)                               AS last_at
		FROM events`+where+`
		GROUP BY event_name
		ORDER BY total DESC, event_name ASC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("event overview: %w", err)
	}
	defer rows.Close()

	var out []EventStatRow
	for rows.Next() {
		var r EventStatRow
		if err := rows.Scan(&r.Name, &r.Total, &r.UniqueUsers, &r.LastAt); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// EventUniqueCounts — карта event_name → кол-во уникальных персон в окне.
// База для всех воронок (§3–§5): конверсии считаются как отношение уникальных
// пользователей соседних стадий.
func (d *Database) EventUniqueCounts(days int) (map[string]int64, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	where, args := analyticsWindowClause("occurred_at", days)
	rows, err := d.db.Query(`
		SELECT event_name, COUNT(DISTINCT COALESCE(telegram_id, user_id))
		FROM events`+where+`
		GROUP BY event_name
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("event unique counts: %w", err)
	}
	defer rows.Close()

	out := make(map[string]int64)
	for rows.Next() {
		var name string
		var cnt int64
		if err := rows.Scan(&name, &cnt); err != nil {
			continue
		}
		out[name] = cnt
	}
	return out, rows.Err()
}

// GetChannelAttribution — разбивка приобретения по каналам (§8).
// На каждый source считаем стартовавших и доплативших.
func (d *Database) GetChannelAttribution(days int) ([]ChannelStatRow, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	where, args := analyticsWindowClause("occurred_at", days)
	rows, err := d.db.Query(`
		WITH starts AS (
			-- Личные ссылки ?start=ref-<id> сводим в один канал «ref»: иначе строка на каждого пригласившего.
			SELECT CASE WHEN source LIKE 'ref-%' THEN 'ref'
			            ELSE COALESCE(NULLIF(source, ''), 'organic') END AS src,
			       COALESCE(telegram_id, user_id)          AS person
			FROM events`+where+`
			  `+andOrWhere(where)+` event_name = 'bot_started'
		),
		paid AS (
			SELECT DISTINCT COALESCE(telegram_id, user_id) AS person
			FROM events`+where+`
			  `+andOrWhere(where)+` event_name = 'payment_completed'
		)
		SELECT s.src,
		       COUNT(DISTINCT s.person)                              AS started,
		       COUNT(DISTINCT s.person) FILTER (WHERE p.person IS NOT NULL) AS paid
		FROM starts s
		LEFT JOIN paid p ON p.person = s.person
		GROUP BY s.src
		ORDER BY started DESC, s.src ASC
	`, args...)
	if err != nil {
		return nil, fmt.Errorf("channel attribution: %w", err)
	}
	defer rows.Close()

	var out []ChannelStatRow
	for rows.Next() {
		var r ChannelStatRow
		if err := rows.Scan(&r.Source, &r.Started, &r.Paid); err != nil {
			continue
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// andOrWhere — если в строке уже есть WHERE (окно по дате), добавляет AND,
// иначе открывает WHERE. Нужно, чтобы корректно склеить условие окна с фильтром
// event_name в CTE выше.
func andOrWhere(window string) string {
	if window == "" {
		return "WHERE"
	}
	return "AND"
}

// TotalEvents — всего строк в events в окне (для заголовка/контекста).
func (d *Database) TotalEvents(days int) (int64, error) {
	if d == nil || d.db == nil {
		return 0, fmt.Errorf("nil database")
	}
	where, args := analyticsWindowClause("occurred_at", days)
	var n int64
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM events`+where, args...).Scan(&n); err != nil {
		return 0, fmt.Errorf("total events: %w", err)
	}
	return n, nil
}

// AnalyticsLastEventAt — время последнего события вообще (для «данные актуальны на …»).
func (d *Database) AnalyticsLastEventAt() (time.Time, bool) {
	if d == nil || d.db == nil {
		return time.Time{}, false
	}
	var t sql.NullTime
	if err := d.db.QueryRow(`SELECT MAX(occurred_at) FROM events`).Scan(&t); err != nil {
		return time.Time{}, false
	}
	if !t.Valid {
		return time.Time{}, false
	}
	return t.Time, true
}
