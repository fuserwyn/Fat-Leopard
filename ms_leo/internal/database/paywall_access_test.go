package database

import (
	"errors"
	"regexp"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
)

// Регрессия: после кика за неактивность из платной группы оплаченный доступ должен «сгореть»,
// иначе при повторном /start paywallPrivateNeedsPayFirst видит активную запись и бот молча шлёт
// инвайт-ссылку, не предлагая оплату.
func TestExpirePaywallAccessForUser_KickFlow(t *testing.T) {
	const userID = int64(1001)
	const chatID = int64(-100500)

	t.Run("expires only completed+active rows for given user/chat", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		d := &Database{db: db}

		// 1) Купленный доступ виден как активный — это исходное состояние «оплатил, но прокрастинировал».
		mock.ExpectQuery(`SELECT EXISTS\s*\(\s*SELECT 1 FROM paywall_access_requests`).
			WithArgs(userID, chatID).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(true))

		ok, err := d.UserHasActivePaywallAccess(userID, chatID)
		if err != nil {
			t.Fatalf("UserHasActivePaywallAccess (before kick): %v", err)
		}
		if !ok {
			t.Fatal("предусловие: до кика доступ должен быть активным")
		}

		// 2) Кик за неактивность: выставляем access_expires_at = NOW() ровно для completed+активных
		// записей этого пользователя в этом чате. Pending/чужих/уже истёкших не трогаем.
		mock.ExpectExec(regexp.QuoteMeta(
			"UPDATE paywall_access_requests\n\t\tSET access_expires_at = NOW()\n\t\tWHERE user_id = $1\n\t\t  AND monetized_chat_id = $2\n\t\t  AND status = 'completed'\n\t\t  AND access_expires_at IS NOT NULL\n\t\t  AND access_expires_at > NOW()",
		)).
			WithArgs(userID, chatID).
			WillReturnResult(sqlmock.NewResult(0, 1))

		if err := d.ExpirePaywallAccessForUser(userID, chatID); err != nil {
			t.Fatalf("ExpirePaywallAccessForUser: %v", err)
		}

		// 3) После кика повторная проверка доступа должна вернуть false:
		// access_expires_at > NOW() уже не выполняется → paywallPrivateNeedsPayFirst вернёт true,
		// а handleStart покажет paywallPrivateUnpaidUserText с кнопками оплаты.
		mock.ExpectQuery(`SELECT EXISTS\s*\(\s*SELECT 1 FROM paywall_access_requests`).
			WithArgs(userID, chatID).
			WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(false))

		ok, err = d.UserHasActivePaywallAccess(userID, chatID)
		if err != nil {
			t.Fatalf("UserHasActivePaywallAccess (after kick): %v", err)
		}
		if ok {
			t.Fatal("после кика активного доступа быть не должно — иначе бот не предложит оплату")
		}

		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sqlmock expectations: %v", err)
		}
	})

	t.Run("noop when user has no completed rows", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		d := &Database{db: db}

		mock.ExpectExec(`UPDATE\s+paywall_access_requests\s+SET\s+access_expires_at\s*=\s*NOW\(\)`).
			WithArgs(userID, chatID).
			WillReturnResult(sqlmock.NewResult(0, 0))

		if err := d.ExpirePaywallAccessForUser(userID, chatID); err != nil {
			t.Fatalf("ExpirePaywallAccessForUser (noop): %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sqlmock expectations: %v", err)
		}
	})

	t.Run("wraps db error", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		d := &Database{db: db}

		boom := errors.New("connection reset")
		mock.ExpectExec(`UPDATE\s+paywall_access_requests\s+SET\s+access_expires_at\s*=\s*NOW\(\)`).
			WithArgs(userID, chatID).
			WillReturnError(boom)

		err = d.ExpirePaywallAccessForUser(userID, chatID)
		if err == nil {
			t.Fatal("expected error from underlying driver")
		}
		if !errors.Is(err, boom) {
			t.Fatalf("expected wrapped boom, got %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sqlmock expectations: %v", err)
		}
	})
}

// Гарантируем, что запрос помечает только записи в нужной группе и только со статусом completed:
// pending-заявки нельзя «истекать» (иначе пользователь не сможет завершить начатую оплату),
// а чужие чаты вообще трогать не должны.
func TestExpirePaywallAccessForUser_QueryShape(t *testing.T) {
	db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New: %v", err)
	}
	defer db.Close()
	d := &Database{db: db}

	mock.ExpectExec(
		`UPDATE\s+paywall_access_requests\s+` +
			`SET\s+access_expires_at\s*=\s*NOW\(\)\s+` +
			`WHERE\s+user_id\s*=\s*\$1\s+` +
			`AND\s+monetized_chat_id\s*=\s*\$2\s+` +
			`AND\s+status\s*=\s*'completed'\s+` +
			`AND\s+access_expires_at\s+IS\s+NOT\s+NULL\s+` +
			`AND\s+access_expires_at\s*>\s*NOW\(\)`,
	).
		WithArgs(int64(42), int64(-100777)).
		WillReturnResult(sqlmock.NewResult(0, 2))

	if err := d.ExpirePaywallAccessForUser(42, -100777); err != nil {
		t.Fatalf("ExpirePaywallAccessForUser: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sqlmock expectations: %v", err)
	}
}

// Фоновая сверка с ЮKassa (bot/paywall_reconciler.go) берёт заявки этим запросом. Кейс redraych
// 2026-09-22: вебхук ms_payments не дошёл, и оплата висела в pending, пока доступ не выдали руками.
// Здесь фиксируем форму запроса — брать только незакрытые заявки с уже созданным счётом и только
// свежие, — и то, что строки раскладываются в структуру без сдвига колонок.
func TestListPendingPaywallRequestsWithYookassaPayment(t *testing.T) {
	const listQueryShape = `SELECT\s+id,\s*user_id,\s*monetized_chat_id.*FROM\s+paywall_access_requests\s+` +
		`WHERE\s+status\s*=\s*'pending'\s+` +
		`AND\s+yookassa_payment_id\s+IS\s+NOT\s+NULL\s+` +
		`AND\s+btrim\(yookassa_payment_id\)\s*<>\s*''\s+` +
		`AND\s+created_at\s*>\s*NOW\(\)\s*-\s*\$1::interval`

	newRows := func() *sqlmock.Rows {
		return sqlmock.NewRows([]string{
			"id", "user_id", "monetized_chat_id", "status", "created_at", "completed_at", "access_expires_at",
			"telegram_payment_charge_id", "total_amount_minor", "currency", "yookassa_payment_id",
			"post_payment_welcome_sent_at",
		})
	}

	t.Run("maps rows and passes age window as interval seconds", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		d := &Database{db: db}

		created := time.Now().Add(-2 * time.Hour)
		mock.ExpectQuery(listQueryShape).
			WithArgs("7200 seconds", 10).
			WillReturnRows(newRows().
				AddRow(int64(77), int64(1001), int64(-100500), "pending", created, nil, nil, nil, nil, nil, "2f0a-succeeded", nil).
				AddRow(int64(76), int64(1002), int64(-100500), "pending", created, nil, nil, nil, nil, nil, "2f0a-canceled", nil))

		rows, err := d.ListPendingPaywallRequestsWithYookassaPayment(2*time.Hour, 10)
		if err != nil {
			t.Fatalf("ListPendingPaywallRequestsWithYookassaPayment: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("ожидали 2 заявки, получили %d", len(rows))
		}
		if rows[0].ID != 77 || rows[0].UserID != 1001 {
			t.Fatalf("первая заявка разложилась неверно: %+v", rows[0])
		}
		if !rows[0].YookassaPaymentID.Valid || rows[0].YookassaPaymentID.String != "2f0a-succeeded" {
			t.Fatalf("payment id потерян: %+v", rows[0].YookassaPaymentID)
		}
		if rows[0].Status != "pending" {
			t.Fatalf("сверщик должен получать только pending, got %q", rows[0].Status)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sqlmock expectations: %v", err)
		}
	})

	t.Run("no pending payments is not an error", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		d := &Database{db: db}

		mock.ExpectQuery(listQueryShape).WillReturnRows(newRows())

		rows, err := d.ListPendingPaywallRequestsWithYookassaPayment(time.Hour, 5)
		if err != nil {
			t.Fatalf("пустая очередь — штатный тик сверщика: %v", err)
		}
		if len(rows) != 0 {
			t.Fatalf("ожидали пусто, получили %d", len(rows))
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sqlmock expectations: %v", err)
		}
	})

	t.Run("falls back to defaults on non-positive limit and age", func(t *testing.T) {
		db, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
		if err != nil {
			t.Fatalf("sqlmock.New: %v", err)
		}
		defer db.Close()
		d := &Database{db: db}

		mock.ExpectQuery(listQueryShape).
			WithArgs("259200 seconds", 25).
			WillReturnRows(newRows())

		if _, err := d.ListPendingPaywallRequestsWithYookassaPayment(0, 0); err != nil {
			t.Fatalf("ListPendingPaywallRequestsWithYookassaPayment(0,0): %v", err)
		}
		if err := mock.ExpectationsWereMet(); err != nil {
			t.Fatalf("sqlmock expectations: %v", err)
		}
	})
}
