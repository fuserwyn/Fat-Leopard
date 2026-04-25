package database

import (
	"errors"
	"regexp"
	"testing"

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
