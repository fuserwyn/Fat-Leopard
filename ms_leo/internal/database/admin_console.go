package database

import (
	"context"
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Консоль админа: список таблиц, страница таблицы и произвольный SQL.
// По умолчанию только чтение — редактор открыт админам мини-аппа, и одна
// опечатка в UPDATE без WHERE стоила бы базы стаи.

// AdminTableInfo — таблица проекта и оценка числа строк из статистики Postgres.
type AdminTableInfo struct {
	Name string `json:"name"`
	Rows int64  `json:"rows"`
}

// AdminQueryResult — результат выборки: колонки и строки в текстовом виде.
type AdminQueryResult struct {
	Columns  []string   `json:"columns"`
	Rows     [][]string `json:"rows"`
	Truncate bool       `json:"truncated"`
	Took     string     `json:"took"`
}

// AdminTableNames — имена таблиц public-схемы.
func (d *Database) AdminTableNames() ([]string, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	rows, err := d.db.Query(`
		SELECT c.relname
		FROM pg_class c
		JOIN pg_namespace n ON n.oid = c.relnamespace
		WHERE n.nspname = 'public' AND c.relkind = 'r'
		ORDER BY c.relname
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]string, 0, 64)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			return nil, err
		}
		out = append(out, name)
	}
	return out, rows.Err()
}

// AdminListTables — таблицы с ТОЧНЫМ числом строк.
//
// reltuples из pg_class врёт: у таблицы, которую ещё не трогал autovacuum, там
// ноль, и в списке «migrations» выглядела бы пустой. База стаи небольшая,
// поэтому считаем честно — одним запросом COUNT(*) по всем таблицам сразу.
func (d *Database) AdminListTables() ([]AdminTableInfo, error) {
	names, err := d.AdminTableNames()
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	parts := make([]string, 0, len(names))
	for _, n := range names {
		if !identRe.MatchString(n) {
			continue
		}
		parts = append(parts, fmt.Sprintf("SELECT %s AS t, COUNT(*) AS c FROM %q", quoteLiteral(n), n))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	rows, err := d.db.QueryContext(ctx, strings.Join(parts, " UNION ALL ")+" ORDER BY t")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminTableInfo, 0, len(names))
	for rows.Next() {
		var t AdminTableInfo
		if err := rows.Scan(&t.Name, &t.Rows); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func quoteLiteral(v string) string {
	return "'" + strings.ReplaceAll(v, "'", "''") + "'"
}

// AdminColumnInfo — колонка таблицы для вкладки «Структура».
type AdminColumnInfo struct {
	Name     string `json:"name"`
	Type     string `json:"type"`
	Nullable bool   `json:"nullable"`
	Default  string `json:"default"`
	PK       bool   `json:"pk"`
}

// AdminTableColumns — структура таблицы: тип, NULL, значение по умолчанию, ключ.
func (d *Database) AdminTableColumns(table string) ([]AdminColumnInfo, error) {
	if d == nil || d.db == nil {
		return nil, fmt.Errorf("nil database")
	}
	if !identRe.MatchString(table) {
		return nil, fmt.Errorf("недопустимое имя таблицы")
	}
	rows, err := d.db.Query(`
		SELECT a.attname,
		       format_type(a.atttypid, a.atttypmod),
		       NOT a.attnotnull,
		       COALESCE(pg_get_expr(ad.adbin, ad.adrelid), ''),
		       COALESCE(i.indisprimary, false)
		FROM pg_attribute a
		JOIN pg_class c ON c.oid = a.attrelid
		JOIN pg_namespace n ON n.oid = c.relnamespace
		LEFT JOIN pg_attrdef ad ON ad.adrelid = c.oid AND ad.adnum = a.attnum
		LEFT JOIN pg_index i ON i.indrelid = c.oid AND a.attnum = ANY(i.indkey) AND i.indisprimary
		WHERE n.nspname = 'public' AND c.relname = $1 AND a.attnum > 0 AND NOT a.attisdropped
		ORDER BY a.attnum
	`, table)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminColumnInfo, 0, 16)
	for rows.Next() {
		var c AdminColumnInfo
		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable, &c.Default, &c.PK); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

var identRe = regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_]*$`)

// AdminTablePage — страница таблицы. Имя проверяем по белому списку из
// pg_class: подставлять его в SQL иначе нельзя, параметром имя не передать.
func (d *Database) AdminTablePage(table string, limit, offset int, orderBy string, desc bool) (AdminQueryResult, error) {
	var out AdminQueryResult
	if !identRe.MatchString(table) {
		return out, fmt.Errorf("недопустимое имя таблицы")
	}
	names, err := d.AdminTableNames()
	if err != nil {
		return out, err
	}
	found := false
	for _, n := range names {
		if n == table {
			found = true
			break
		}
	}
	if !found {
		return out, fmt.Errorf("нет такой таблицы")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	order := ""
	if strings.TrimSpace(orderBy) != "" {
		// Имя колонки в ORDER BY параметром не передать, поэтому сверяем его со
		// структурой таблицы: подставлять пришедшее с клиента нельзя.
		cols, err := d.AdminTableColumns(table)
		if err != nil {
			return out, err
		}
		ok := false
		for _, c := range cols {
			if c.Name == orderBy {
				ok = true
				break
			}
		}
		if !ok {
			return out, fmt.Errorf("нет такой колонки")
		}
		dir := "ASC"
		if desc {
			dir = "DESC"
		}
		order = fmt.Sprintf(" ORDER BY %q %s", orderBy, dir)
	}
	return d.AdminRunQuery(fmt.Sprintf("SELECT * FROM %q%s LIMIT %d OFFSET %d", table, order, limit, offset))
}

var readOnlyPrefixes = []string{"select", "with", "explain", "show", "table", "values"}

// adminStripSQLNoise убирает комментарии и лишние пробелы: без этого
// «-- заметка\nSELECT …» не проходил бы проверку на чтение.
func adminStripSQLNoise(sql string) string {
	s := regexp.MustCompile(`(?s)/\*.*?\*/`).ReplaceAllString(sql, " ")
	lines := make([]string, 0, 8)
	for _, line := range strings.Split(s, "\n") {
		if i := strings.Index(line, "--"); i >= 0 {
			line = line[:i]
		}
		lines = append(lines, line)
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// AdminQueryIsReadOnly — запрос только читает?
func AdminQueryIsReadOnly(sql string) bool {
	s := strings.ToLower(adminStripSQLNoise(sql))
	if s == "" {
		return false
	}
	// Несколько запросов через ';' не пропускаем: второй мог бы быть DELETE.
	if strings.Contains(strings.TrimSuffix(s, ";"), ";") {
		return false
	}
	for _, p := range readOnlyPrefixes {
		if strings.HasPrefix(s, p+" ") || s == p {
			return true
		}
	}
	return false
}

// AdminRunQuery выполняет читающий запрос в read-only транзакции с таймаутом.
// Возвращает не больше 200 строк: результат уезжает в телефон.
func (d *Database) AdminRunQuery(query string) (AdminQueryResult, error) {
	var out AdminQueryResult
	if d == nil || d.db == nil {
		return out, fmt.Errorf("nil database")
	}
	if !AdminQueryIsReadOnly(query) {
		return out, fmt.Errorf("только чтение: SELECT / WITH / EXPLAIN / SHOW / TABLE / VALUES, один запрос без «;»")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	tx, err := d.db.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return out, err
	}
	defer func() { _ = tx.Rollback() }()

	started := time.Now()
	rows, err := tx.QueryContext(ctx, query)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	cols, err := rows.Columns()
	if err != nil {
		return out, err
	}
	out.Columns = cols
	const maxRows = 200
	for rows.Next() {
		if len(out.Rows) >= maxRows {
			out.Truncate = true
			break
		}
		cells := make([]any, len(cols))
		for i := range cells {
			cells[i] = new(sql.NullString)
		}
		if err := rows.Scan(cells...); err != nil {
			return out, err
		}
		row := make([]string, len(cols))
		for i, c := range cells {
			if v, ok := c.(*sql.NullString); ok && v.Valid {
				row[i] = v.String
			} else {
				row[i] = "∅"
			}
		}
		out.Rows = append(out.Rows, row)
	}
	if err := rows.Err(); err != nil {
		return out, err
	}
	out.Took = time.Since(started).Round(time.Millisecond).String()
	return out, nil
}

// AdminPaymentSum — сколько получено в одной валюте за период.
type AdminPaymentSum struct {
	Currency    string `json:"currency"`
	Count       int64  `json:"count"`
	AmountMinor int64  `json:"amount_minor"`
}

// AdminMoneyKindSum — завершённые оплаты: покупка доступа или донат, по валюте.
type AdminMoneyKindSum struct {
	Kind        string // access | donation
	Currency    string
	Count       int64
	AmountMinor int64
}

func adminNormalizeMoneyCurrency(cur string) string {
	c := strings.ToUpper(strings.TrimSpace(cur))
	if c == "XTR" || c == "STARS" {
		return "XTR"
	}
	if c == "" {
		return "RUB"
	}
	return c
}

// AdminSumCompletedPayments — оплаченные заявки доступа с момента since, по валютам.
func (d *Database) AdminSumCompletedPayments(packChatID int64, since time.Time) ([]AdminPaymentSum, error) {
	rows, err := d.adminSumCompletedPaywall(packChatID, since, true)
	if err != nil {
		return nil, err
	}
	out := make([]AdminPaymentSum, 0, len(rows))
	for _, r := range rows {
		out = append(out, AdminPaymentSum{
			Currency:    r.Currency,
			Count:       r.Count,
			AmountMinor: r.AmountMinor,
		})
	}
	return out, nil
}

// AdminSumCompletedMoney — завершённые покупки доступа и донаты, по виду и валюте.
// useSince=false — за всё время; иначе с completed_at/created_at >= since.
func (d *Database) AdminSumCompletedMoney(packChatID int64, since time.Time, useSince bool) ([]AdminMoneyKindSum, error) {
	if d == nil || d.db == nil {
		return nil, nil
	}
	paywall, err := d.adminSumCompletedPaywall(packChatID, since, useSince)
	if err != nil {
		return nil, err
	}
	donations, err := d.adminSumCompletedDonations(since, useSince)
	if err != nil {
		return nil, err
	}
	out := make([]AdminMoneyKindSum, 0, len(paywall)+len(donations))
	for _, r := range paywall {
		out = append(out, AdminMoneyKindSum{Kind: "access", Currency: r.Currency, Count: r.Count, AmountMinor: r.AmountMinor})
	}
	for _, r := range donations {
		out = append(out, AdminMoneyKindSum{Kind: "donation", Currency: r.Currency, Count: r.Count, AmountMinor: r.AmountMinor})
	}
	return out, nil
}

func (d *Database) adminSumCompletedPaywall(packChatID int64, since time.Time, useSince bool) ([]AdminPaymentSum, error) {
	if packChatID == 0 {
		return nil, nil
	}
	q := `
		SELECT COALESCE(NULLIF(BTRIM(currency), ''), 'RUB') AS cur,
		       COUNT(*),
		       COALESCE(SUM(total_amount_minor), 0)
		FROM paywall_access_requests
		WHERE monetized_chat_id = $1
		  AND status = 'completed'`
	args := []any{packChatID}
	if useSince {
		q += `
		  AND COALESCE(completed_at, created_at) >= $2`
		args = append(args, since)
	}
	q += `
		GROUP BY cur
		ORDER BY cur`
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminPaymentSum, 0, 4)
	for rows.Next() {
		var s AdminPaymentSum
		if err := rows.Scan(&s.Currency, &s.Count, &s.AmountMinor); err != nil {
			return nil, err
		}
		s.Currency = adminNormalizeMoneyCurrency(s.Currency)
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *Database) adminSumCompletedDonations(since time.Time, useSince bool) ([]AdminPaymentSum, error) {
	q := `
		SELECT COALESCE(NULLIF(BTRIM(currency), ''), 'RUB') AS cur,
		       COUNT(*),
		       COALESCE(SUM(amount_minor), 0)
		FROM donations
		WHERE status = 'completed'`
	args := []any{}
	if useSince {
		q += `
		  AND COALESCE(completed_at, created_at) >= $1`
		args = append(args, since)
	}
	q += `
		GROUP BY cur
		ORDER BY cur`
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminPaymentSum, 0, 4)
	for rows.Next() {
		var s AdminPaymentSum
		if err := rows.Scan(&s.Currency, &s.Count, &s.AmountMinor); err != nil {
			return nil, err
		}
		s.Currency = adminNormalizeMoneyCurrency(s.Currency)
		out = append(out, s)
	}
	return out, rows.Err()
}

// AdminMoneyPaymentRow — строка списка оплат: доступ или донат.
type AdminMoneyPaymentRow struct {
	Kind         string
	ID           int64
	UserID       int64
	Username     string
	DisplayName  string
	Status       string
	CreatedAt    time.Time
	AmountMinor  sql.NullInt64
	Currency     sql.NullString
	AccessActive bool
}

// AdminMoneyFilter — фильтр списка оплат в админке.
// Kind: "" — всё, "access" — платежи за доступ, "donation" — донаты.
// CompletedOnly — только завершённые (без висящих заявок и отмен).
// Query — ник (с @ или без), имя из профиля или telegram id; ищется вхождение без учёта регистра.
// From/To — период по дате оплаты (completed_at, иначе created_at): From включительно,
// To не включительно; нулевое время — без границы.
// OldestFirst — хронология от старых к новым (по умолчанию новые сверху).
type AdminMoneyFilter struct {
	Kind          string
	CompletedOnly bool
	Query         string
	From          time.Time
	To            time.Time
	OldestFirst   bool
}

// NormalizeAdminMoneyQuery — строка поиска по людям: без пробелов по краям, без @, в нижнем регистре.
func NormalizeAdminMoneyQuery(q string) string {
	q = strings.TrimSpace(q)
	q = strings.TrimLeft(q, "@")
	return strings.ToLower(strings.TrimSpace(q))
}

// sqlArgs — параметры $2…$6 для adminMoneyFilterSQL.
func (f AdminMoneyFilter) sqlArgs() []any {
	from := sql.NullTime{Time: f.From, Valid: !f.From.IsZero()}
	to := sql.NullTime{Time: f.To, Valid: !f.To.IsZero()}
	return []any{NormalizeAdminMoneyKind(f.Kind), f.CompletedOnly, NormalizeAdminMoneyQuery(f.Query), from, to}
}

// NormalizeAdminMoneyKind — допустимый вид оплаты для фильтра, иначе "" (всё).
func NormalizeAdminMoneyKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "access", "payment", "payments":
		return "access"
	case "donation", "donations", "donate":
		return "donation"
	default:
		return ""
	}
}

// adminMoneyUnionSQL — заявки доступа и донаты одним набором строк.
// $1 — чат стаи (monetized_chat_id); ник и имя подтягиваются по нему же.
const adminMoneyUnionSQL = `
			SELECT 'access' AS kind, par.id, par.user_id,
			       COALESCE(NULLIF(BTRIM(ts.username), ''), '') AS username,
			       COALESCE(NULLIF(BTRIM(p.display_name), ''), '') AS display_name,
			       par.status,
			       COALESCE(par.completed_at, par.created_at) AS created_at,
			       par.total_amount_minor AS amount_minor,
			       par.currency,
			       (par.status = 'completed'
			        AND par.access_expires_at IS NOT NULL
			        AND par.access_expires_at > NOW()) AS access_active
			FROM paywall_access_requests par
			LEFT JOIN training_state ts
				ON ts.user_id = par.user_id AND ts.chat_id = par.monetized_chat_id
			LEFT JOIN miniapp_user_profile p
				ON p.user_id = par.user_id AND p.pack_chat_id = par.monetized_chat_id
			WHERE par.monetized_chat_id = $1

			UNION ALL

			SELECT 'donation' AS kind, d.id, d.user_id,
			       COALESCE(NULLIF(BTRIM(ts.username), ''), '') AS username,
			       COALESCE(NULLIF(BTRIM(p.display_name), ''), '') AS display_name,
			       d.status,
			       COALESCE(d.completed_at, d.created_at) AS created_at,
			       d.amount_minor,
			       d.currency,
			       false AS access_active
			FROM donations d
			LEFT JOIN training_state ts
				ON ts.user_id = d.user_id AND ts.chat_id = $1
			LEFT JOIN miniapp_user_profile p
				ON p.user_id = d.user_id AND p.pack_chat_id = $1`

// adminMoneyFilterSQL — условие на строки adminMoneyUnionSQL: $2 — вид, $3 — только завершённые,
// $4 — поиск по нику/имени/id, $5 и $6 — период [с; по). Параметры собирает AdminMoneyFilter.sqlArgs.
const adminMoneyFilterSQL = `
		WHERE ($2::text = '' OR u.kind = $2::text)
		  AND (NOT $3::boolean OR u.status = 'completed')
		  AND ($4::text = ''
		       OR STRPOS(LOWER(u.username), $4::text) > 0
		       OR STRPOS(LOWER(u.display_name), $4::text) > 0
		       OR u.user_id::text = $4::text)
		  AND ($5::timestamptz IS NULL OR u.created_at >= $5::timestamptz)
		  AND ($6::timestamptz IS NULL OR u.created_at < $6::timestamptz)`

// CountMoneyPaymentsForAdmin — все заявки доступа и донаты.
func (d *Database) CountMoneyPaymentsForAdmin(packChatID int64) (int, error) {
	return d.CountMoneyPaymentsForAdminFiltered(packChatID, AdminMoneyFilter{})
}

// CountMoneyPaymentsForAdminFiltered — сколько строк в списке оплат с учётом фильтра.
func (d *Database) CountMoneyPaymentsForAdminFiltered(packChatID int64, f AdminMoneyFilter) (int, error) {
	if d == nil || d.db == nil || packChatID == 0 {
		return 0, nil
	}
	var n int
	q := `SELECT COUNT(*) FROM (` + adminMoneyUnionSQL + `
		) u` + adminMoneyFilterSQL
	args := append([]any{packChatID}, f.sqlArgs()...)
	err := d.db.QueryRow(q, args...).Scan(&n)
	return n, err
}

// ListMoneyPaymentsForAdmin — доступ и донаты одним списком, новые сверху.
func (d *Database) ListMoneyPaymentsForAdmin(packChatID int64, offset, limit int) ([]AdminMoneyPaymentRow, error) {
	return d.ListMoneyPaymentsForAdminFiltered(packChatID, AdminMoneyFilter{}, offset, limit)
}

// ListMoneyPaymentsForAdminFiltered — список оплат с фильтром (вид, статус, человек, период);
// новые сверху, а с OldestFirst — по хронологии от старых.
func (d *Database) ListMoneyPaymentsForAdminFiltered(
	packChatID int64, f AdminMoneyFilter, offset, limit int,
) ([]AdminMoneyPaymentRow, error) {
	if d == nil || d.db == nil || packChatID == 0 {
		return nil, nil
	}
	if limit <= 0 {
		limit = 20
	}
	if offset < 0 {
		offset = 0
	}
	order := "DESC"
	if f.OldestFirst {
		order = "ASC"
	}
	q := `
		SELECT u.kind, u.id, u.user_id, u.username, u.display_name, u.status, u.created_at,
		       u.amount_minor, u.currency, u.access_active
		FROM (` + adminMoneyUnionSQL + `
		) u` + adminMoneyFilterSQL + `
		ORDER BY u.created_at ` + order + `, u.kind ` + order + `, u.id ` + order + `
		OFFSET $7 LIMIT $8`
	args := append([]any{packChatID}, f.sqlArgs()...)
	args = append(args, offset, limit)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminMoneyPaymentRow, 0, limit)
	for rows.Next() {
		var r AdminMoneyPaymentRow
		if err := rows.Scan(
			&r.Kind, &r.ID, &r.UserID, &r.Username, &r.DisplayName, &r.Status, &r.CreatedAt,
			&r.AmountMinor, &r.Currency, &r.AccessActive,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// SumMoneyForAdminFiltered — завершённые оплаты из выборки по виду и валюте:
// сколько и на какую сумму за выбранный период, по выбранному человеку.
func (d *Database) SumMoneyForAdminFiltered(packChatID int64, f AdminMoneyFilter) ([]AdminMoneyKindSum, error) {
	if d == nil || d.db == nil || packChatID == 0 {
		return nil, nil
	}
	q := `
		SELECT u.kind,
		       CASE
		           WHEN UPPER(BTRIM(COALESCE(u.currency, ''))) IN ('XTR', 'STARS') THEN 'XTR'
		           WHEN BTRIM(COALESCE(u.currency, '')) = '' THEN 'RUB'
		           ELSE UPPER(BTRIM(u.currency))
		       END AS cur,
		       COUNT(*),
		       COALESCE(SUM(u.amount_minor), 0)
		FROM (` + adminMoneyUnionSQL + `
		) u` + adminMoneyFilterSQL + `
		GROUP BY u.kind, cur
		ORDER BY u.kind, cur`
	f.CompletedOnly = true
	args := append([]any{packChatID}, f.sqlArgs()...)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminMoneyKindSum, 0, 4)
	for rows.Next() {
		var s AdminMoneyKindSum
		if err := rows.Scan(&s.Kind, &s.Currency, &s.Count, &s.AmountMinor); err != nil {
			return nil, err
		}
		s.Currency = adminNormalizeMoneyCurrency(s.Currency)
		out = append(out, s)
	}
	return out, rows.Err()
}

// AdminMoneyPayerRow — кто платил: итог по человеку за всё время (только завершённые).
type AdminMoneyPayerRow struct {
	UserID        int64
	Username      string
	DisplayName   string
	AccessCount   int64
	DonationCount int64
	StarsTotal    int64 // звёзды штуками
	RubMinorTotal int64 // рубли (и прочие валюты) в копейках
	LastPaidAt    time.Time
}

// ListMoneyPayersForAdmin — люди, которые платили за доступ и/или донатили,
// с количеством и суммами. Свежие плательщики сверху. Учитываются только завершённые
// оплаты; вид, человек и период — из фильтра (CompletedOnly и OldestFirst не важны).
func (d *Database) ListMoneyPayersForAdmin(packChatID int64, f AdminMoneyFilter, limit int) ([]AdminMoneyPayerRow, error) {
	if d == nil || d.db == nil || packChatID == 0 {
		return nil, nil
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT u.user_id,
		       MAX(u.username) AS username,
		       MAX(u.display_name) AS display_name,
		       COUNT(*) FILTER (WHERE u.kind = 'access') AS access_cnt,
		       COUNT(*) FILTER (WHERE u.kind = 'donation') AS donation_cnt,
		       COALESCE(SUM(u.amount_minor) FILTER (
		           WHERE UPPER(BTRIM(COALESCE(u.currency, ''))) IN ('XTR', 'STARS')), 0) AS stars,
		       COALESCE(SUM(u.amount_minor) FILTER (
		           WHERE UPPER(BTRIM(COALESCE(u.currency, ''))) NOT IN ('XTR', 'STARS')), 0) AS rub_minor,
		       MAX(u.created_at) AS last_paid
		FROM (` + adminMoneyUnionSQL + `
		) u` + adminMoneyFilterSQL + `
		GROUP BY u.user_id
		ORDER BY last_paid DESC, u.user_id DESC
		LIMIT $7`
	f.CompletedOnly = true
	args := append([]any{packChatID}, f.sqlArgs()...)
	args = append(args, limit)
	rows, err := d.db.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminMoneyPayerRow, 0, limit)
	for rows.Next() {
		var r AdminMoneyPayerRow
		if err := rows.Scan(
			&r.UserID, &r.Username, &r.DisplayName, &r.AccessCount, &r.DonationCount,
			&r.StarsTotal, &r.RubMinorTotal, &r.LastPaidAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminPersonRow — кто это: ник и отображаемое имя по telegram_id.
type AdminPersonRow struct {
	UserID      int64  `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
}

// AdminPeopleByIDs — имена участников пачкой. Нужно там, где в данных лежит
// только telegram_id (например, автор задачи в трекере), а показать надо ник.
func (d *Database) AdminPeopleByIDs(packChatID int64, ids []int64) ([]AdminPersonRow, error) {
	if d == nil || d.db == nil || len(ids) == 0 {
		return nil, nil
	}
	rows, err := d.db.Query(`
		SELECT u.id,
		       COALESCE(NULLIF(BTRIM(ts.username), ''), ''),
		       COALESCE(NULLIF(BTRIM(p.display_name), ''), '')
		FROM unnest($1::bigint[]) AS u(id)
		LEFT JOIN training_state ts ON ts.user_id = u.id AND ts.chat_id = $2
		LEFT JOIN miniapp_user_profile p ON p.user_id = u.id AND p.pack_chat_id = $2
	`, pq.Array(ids), packChatID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]AdminPersonRow, 0, len(ids))
	for rows.Next() {
		var r AdminPersonRow
		if err := rows.Scan(&r.UserID, &r.Username, &r.DisplayName); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// AdminSetMaxStreak — выставить рекорд стрика напрямую.
//
// UpdateStreak поднимает рекорд через GREATEST и опустить его не может, а
// админу иногда нужно именно исправить цифру (например, накрутили тестами).
func (d *Database) AdminSetMaxStreak(userID, packChatID int64, value int) error {
	if d == nil || d.db == nil || userID == 0 || packChatID == 0 {
		return nil
	}
	if value < 0 {
		value = 0
	}
	_, err := d.db.Exec(`
		UPDATE training_state SET max_streak_days = $3, updated_at = NOW()
		WHERE user_id = $1 AND chat_id = $2
	`, userID, packChatID, value)
	return err
}

// AdminDeleteLatestTrainingSession — снять последнюю зачтённую тренировку.
// Возвращает false, если сессий у человека нет.
func (d *Database) AdminDeleteLatestTrainingSession(userID, packChatID int64) (bool, error) {
	if d == nil || d.db == nil || userID == 0 {
		return false, nil
	}
	res, err := d.db.Exec(`
		DELETE FROM training_sessions
		WHERE id = (
			SELECT id FROM training_sessions
			WHERE user_id = $1 AND chat_id = ANY($2::bigint[])
			ORDER BY session_date DESC, id DESC
			LIMIT 1
		)
	`, userID, pq.Array([]int64{packChatID, userID}))
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}
