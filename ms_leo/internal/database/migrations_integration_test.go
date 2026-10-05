package database

import (
	"database/sql"
	"os"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// openMigratedTestDB — пустая база из LEO_TEST_PG_DSN со всеми миграциями проекта.
// Без переменной тест пропускается.
func openMigratedTestDB(t *testing.T) *Database {
	t.Helper()
	dsn := os.Getenv("LEO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("LEO_TEST_PG_DSN не задан")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.Exec(`DROP SCHEMA public CASCADE; CREATE SCHEMA public`); err != nil {
		t.Fatal(err)
	}
	d := NewForTest(db)
	// Как при старте бота: базовые таблицы и следом все миграции.
	if err := d.CreateTables(); err != nil {
		t.Fatalf("таблицы и миграции: %v", err)
	}
	return d
}

func TestAllMigrationsApplyOnEmptyDB(t *testing.T) {
	d := openMigratedTestDB(t)
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM pack_weekly_goal_settings`).Scan(&n); err != nil {
		t.Fatal(err)
	}
}

// Список оплат и «кто платил» в админке — на настоящей схеме.
func TestAdminMoneyQueriesOnRealSchema(t *testing.T) {
	d := openMigratedTestDB(t)
	const pack = int64(-100500)
	exec := func(q string, args ...any) {
		t.Helper()
		if _, err := d.db.Exec(q, args...); err != nil {
			t.Fatalf("%v\n%s", err, q)
		}
	}
	exec(`INSERT INTO training_state (user_id, chat_id, username, last_message) VALUES (1, $1, 'ivan', NOW()), (2, $1, '', NOW())`, pack)
	exec(`INSERT INTO miniapp_user_profile (user_id, pack_chat_id, display_name) VALUES (2, $1, 'Мария')`, pack)
	// Иван: оплатил доступ рублями и задонатил звёздами; ещё одна заявка висит.
	exec(`INSERT INTO paywall_access_requests (user_id, monetized_chat_id, status, total_amount_minor, currency, completed_at, access_expires_at)
	      VALUES (1, $1, 'completed', 21000, 'RUB', NOW() - INTERVAL '2 days', NOW() + INTERVAL '30 days')`, pack)
	exec(`INSERT INTO paywall_access_requests (user_id, monetized_chat_id, status) VALUES (1, $1, 'pending')`, pack)
	exec(`INSERT INTO donations (user_id, provider, status, amount_minor, currency, completed_at) VALUES (1, 'stars', 'completed', 150, 'XTR', NOW() - INTERVAL '1 day')`)
	// Мария: два доната рублями, один не завершён.
	exec(`INSERT INTO donations (user_id, provider, status, amount_minor, currency, completed_at) VALUES (2, 'yookassa', 'completed', 50000, 'RUB', NOW())`)
	exec(`INSERT INTO donations (user_id, provider, status, amount_minor, currency) VALUES (2, 'yookassa', 'pending', 10000, 'RUB')`)
	// Чужая стая в список не попадает.
	exec(`INSERT INTO paywall_access_requests (user_id, monetized_chat_id, status, total_amount_minor, currency) VALUES (9, -1, 'completed', 100, 'RUB')`)

	count := func(f AdminMoneyFilter) int {
		t.Helper()
		n, err := d.CountMoneyPaymentsForAdminFiltered(pack, f)
		if err != nil {
			t.Fatal(err)
		}
		rows, err := d.ListMoneyPaymentsForAdminFiltered(pack, f, 0, 50)
		if err != nil {
			t.Fatal(err)
		}
		if len(rows) != n {
			t.Fatalf("фильтр %+v: count %d, а строк %d", f, n, len(rows))
		}
		return n
	}
	if n := count(AdminMoneyFilter{}); n != 5 {
		t.Errorf("все оплаты: %d, ждали 5", n)
	}
	if n := count(AdminMoneyFilter{Kind: "access"}); n != 2 {
		t.Errorf("доступ: %d, ждали 2", n)
	}
	if n := count(AdminMoneyFilter{Kind: "donation", CompletedOnly: true}); n != 2 {
		t.Errorf("завершённые донаты: %d, ждали 2", n)
	}
	if n := count(AdminMoneyFilter{CompletedOnly: true}); n != 3 {
		t.Errorf("завершённые: %d, ждали 3", n)
	}

	rows, err := d.ListMoneyPaymentsForAdminFiltered(pack, AdminMoneyFilter{CompletedOnly: true}, 0, 50)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].UserID != 2 || rows[0].DisplayName != "Мария" || rows[0].Kind != "donation" {
		t.Errorf("сверху должен быть свежий донат Марии: %+v", rows[0])
	}
	if last := rows[len(rows)-1]; last.Kind != "access" || last.Username != "ivan" || !last.AccessActive {
		t.Errorf("доступ Ивана: %+v", last)
	}
	if page, err := d.ListMoneyPaymentsForAdminFiltered(pack, AdminMoneyFilter{}, 4, 50); err != nil || len(page) != 1 {
		t.Errorf("пагинация: %d строк, %v", len(page), err)
	}

	payers, err := d.ListMoneyPayersForAdmin(pack, "", 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(payers) != 2 {
		t.Fatalf("плательщиков %d, ждали 2: %+v", len(payers), payers)
	}
	maria, ivan := payers[0], payers[1]
	if maria.UserID != 2 || maria.DonationCount != 1 || maria.AccessCount != 0 || maria.RubMinorTotal != 50000 || maria.StarsTotal != 0 {
		t.Errorf("Мария: %+v", maria)
	}
	if ivan.UserID != 1 || ivan.AccessCount != 1 || ivan.DonationCount != 1 || ivan.RubMinorTotal != 21000 || ivan.StarsTotal != 150 || ivan.Username != "ivan" {
		t.Errorf("Иван: %+v", ivan)
	}
	if only, err := d.ListMoneyPayersForAdmin(pack, "access", 50); err != nil || len(only) != 1 || only[0].UserID != 1 {
		t.Errorf("плательщики за доступ: %+v %v", only, err)
	}

	sums, err := d.AdminSumCompletedMoney(pack, time.Time{}, false)
	if err != nil || len(sums) != 3 {
		t.Errorf("сводка: %+v %v", sums, err)
	}
}
