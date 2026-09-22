package database

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// PaywallAccessRequest — одна попытка вступления по заявке (связь с payload инвойса pw_<id>).
type PaywallAccessRequest struct {
	ID                       int64
	UserID                   int64
	MonetizedChatID          int64
	Status                   string
	CreatedAt                time.Time
	CompletedAt              sql.NullTime
	AccessExpiresAt          sql.NullTime
	TelegramPaymentChargeID  sql.NullString
	TotalAmountMinor         sql.NullInt64
	Currency                 sql.NullString
	YookassaPaymentID          sql.NullString
	PostPaymentWelcomeSentAt   sql.NullTime
}

func (d *Database) InsertPaywallAccessRequest(userID, monetizedChatID int64) (int64, error) {
	const q = `
		INSERT INTO paywall_access_requests (user_id, monetized_chat_id, status)
		VALUES ($1, $2, 'pending')
		RETURNING id`
	var id int64
	if err := d.db.QueryRow(q, userID, monetizedChatID).Scan(&id); err != nil {
		return 0, fmt.Errorf("insert paywall request: %w", err)
	}
	return id, nil
}

// GetLatestPendingPaywallAccessRequest — последняя pending-заявка: сначала с текущим pack id, иначе любая pending (устаревший monetized_chat_id в строке).
func (d *Database) GetLatestPendingPaywallAccessRequest(userID, monetizedChatID int64) (*PaywallAccessRequest, error) {
	if r, err := d.getLatestPendingPaywallAccessRequestWhere(userID, monetizedChatID, true); err != nil || r != nil {
		return r, err
	}
	return d.getLatestPendingPaywallAccessRequestWhere(userID, monetizedChatID, false)
}

func (d *Database) getLatestPendingPaywallAccessRequestWhere(userID, monetizedChatID int64, matchChat bool) (*PaywallAccessRequest, error) {
	q := `
		SELECT id, user_id, monetized_chat_id, status, created_at, completed_at, access_expires_at,
		       telegram_payment_charge_id, total_amount_minor, currency, yookassa_payment_id,
		       post_payment_welcome_sent_at
		FROM paywall_access_requests
		WHERE user_id = $1 AND status = 'pending'`
	args := []any{userID}
	if matchChat {
		q += ` AND monetized_chat_id = $2`
		args = append(args, monetizedChatID)
	}
	q += ` ORDER BY id DESC LIMIT 1`
	var r PaywallAccessRequest
	err := d.db.QueryRow(q, args...).Scan(
		&r.ID, &r.UserID, &r.MonetizedChatID, &r.Status, &r.CreatedAt, &r.CompletedAt, &r.AccessExpiresAt,
		&r.TelegramPaymentChargeID, &r.TotalAmountMinor, &r.Currency, &r.YookassaPaymentID,
		&r.PostPaymentWelcomeSentAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// GetLatestCompletedPaywallAccessRequest — последняя успешная оплата (для доставки доступа после вебхука/sync).
func (d *Database) GetLatestCompletedPaywallAccessRequest(userID, monetizedChatID int64) (*PaywallAccessRequest, error) {
	const q = `
		SELECT id, user_id, monetized_chat_id, status, created_at, completed_at, access_expires_at,
		       telegram_payment_charge_id, total_amount_minor, currency, yookassa_payment_id,
		       post_payment_welcome_sent_at
		FROM paywall_access_requests
		WHERE user_id = $1 AND monetized_chat_id = $2 AND status = 'completed'
		ORDER BY id DESC
		LIMIT 1`
	var r PaywallAccessRequest
	err := d.db.QueryRow(q, userID, monetizedChatID).Scan(
		&r.ID, &r.UserID, &r.MonetizedChatID, &r.Status, &r.CreatedAt, &r.CompletedAt, &r.AccessExpiresAt,
		&r.TelegramPaymentChargeID, &r.TotalAmountMinor, &r.Currency, &r.YookassaPaymentID,
		&r.PostPaymentWelcomeSentAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// ListPendingPaywallRequestsWithYookassaPayment — заявки, по которым счёт в ЮKassa уже создан,
// а оплата так и не зачтена. Фоновая сверка (bot/paywall_reconciler.go) опрашивает по ним
// GET /v3/payments/{id}, когда вебхук ms_payments не дошёл. Свежие первыми; maxAge отсекает
// брошенные заявки, по которым юзер просто закрыл форму оплаты.
func (d *Database) ListPendingPaywallRequestsWithYookassaPayment(maxAge time.Duration, limit int) ([]PaywallAccessRequest, error) {
	if limit <= 0 {
		limit = 25
	}
	if maxAge <= 0 {
		maxAge = 72 * time.Hour
	}
	const q = `
		SELECT id, user_id, monetized_chat_id, status, created_at, completed_at, access_expires_at,
		       telegram_payment_charge_id, total_amount_minor, currency, yookassa_payment_id,
		       post_payment_welcome_sent_at
		FROM paywall_access_requests
		WHERE status = 'pending'
		  AND yookassa_payment_id IS NOT NULL
		  AND btrim(yookassa_payment_id) <> ''
		  AND created_at > NOW() - $1::interval
		ORDER BY id DESC
		LIMIT $2`
	rows, err := d.db.Query(q, fmt.Sprintf("%d seconds", int64(maxAge.Seconds())), limit)
	if err != nil {
		return nil, fmt.Errorf("list pending paywall requests: %w", err)
	}
	defer rows.Close()
	var out []PaywallAccessRequest
	for rows.Next() {
		var r PaywallAccessRequest
		if err := rows.Scan(
			&r.ID, &r.UserID, &r.MonetizedChatID, &r.Status, &r.CreatedAt, &r.CompletedAt, &r.AccessExpiresAt,
			&r.TelegramPaymentChargeID, &r.TotalAmountMinor, &r.Currency, &r.YookassaPaymentID,
			&r.PostPaymentWelcomeSentAt,
		); err != nil {
			return nil, fmt.Errorf("scan pending paywall request: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows pending paywall requests: %w", err)
	}
	return out, nil
}

func (d *Database) GetPaywallAccessRequestByID(id int64) (*PaywallAccessRequest, error) {
	const q = `
		SELECT id, user_id, monetized_chat_id, status, created_at, completed_at, access_expires_at,
		       telegram_payment_charge_id, total_amount_minor, currency, yookassa_payment_id,
		       post_payment_welcome_sent_at
		FROM paywall_access_requests WHERE id = $1`
	var r PaywallAccessRequest
	err := d.db.QueryRow(q, id).Scan(
		&r.ID, &r.UserID, &r.MonetizedChatID, &r.Status, &r.CreatedAt, &r.CompletedAt, &r.AccessExpiresAt,
		&r.TelegramPaymentChargeID, &r.TotalAmountMinor, &r.Currency, &r.YookassaPaymentID,
		&r.PostPaymentWelcomeSentAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// UserHasActivePaywallAccess — есть активная (не истекшая) оплата доступа к этой группе.
func (d *Database) UserHasActivePaywallAccess(userID, monetizedChatID int64) (bool, error) {
	const q = `
		SELECT EXISTS (
			SELECT 1 FROM paywall_access_requests
			WHERE user_id = $1
			  AND monetized_chat_id = $2
			  AND status = 'completed'
			  AND access_expires_at IS NOT NULL
			  AND access_expires_at > NOW()
		)`
	var ok bool
	if err := d.db.QueryRow(q, userID, monetizedChatID).Scan(&ok); err != nil {
		return false, fmt.Errorf("paywall active access check: %w", err)
	}
	return ok, nil
}

// PaywallAccessDebugSnapshot — кратко последние заявки user+chat (для логов при отказе во входе).
func (d *Database) PaywallAccessDebugSnapshot(userID, monetizedChatID int64) string {
	const q = `
		SELECT id, status,
		       (access_expires_at IS NOT NULL AND access_expires_at > NOW()) AS access_valid
		FROM paywall_access_requests
		WHERE user_id = $1 AND monetized_chat_id = $2
		ORDER BY id DESC
		LIMIT 5`
	rows, err := d.db.Query(q, userID, monetizedChatID)
	if err != nil {
		return fmt.Sprintf("err=%v", err)
	}
	defer rows.Close()
	var parts []string
	for rows.Next() {
		var id int64
		var status string
		var accessValid bool
		if err := rows.Scan(&id, &status, &accessValid); err != nil {
			return fmt.Sprintf("scan_err=%v", err)
		}
		parts = append(parts, fmt.Sprintf("%d:%s(exp_ok=%v)", id, status, accessValid))
	}
	if err := rows.Err(); err != nil {
		return fmt.Sprintf("rows_err=%v", err)
	}
	if len(parts) == 0 {
		return "no_rows_for_user_chat"
	}
	return strings.Join(parts, ", ")
}

// ExpirePaywallAccessForUser помечает все активные оплаченные доступы пользователя к этой группе
// как истёкшие. Используется при выкидывании за неактивность — повторный вход требует новой оплаты,
// иначе после кика бот продолжает считать доступ активным и выдаёт инвайт без приглашения к оплате.
func (d *Database) ExpirePaywallAccessForUser(userID, monetizedChatID int64) error {
	const q = `
		UPDATE paywall_access_requests
		SET access_expires_at = NOW()
		WHERE user_id = $1
		  AND monetized_chat_id = $2
		  AND status = 'completed'
		  AND access_expires_at IS NOT NULL
		  AND access_expires_at > NOW()`
	if _, err := d.db.Exec(q, userID, monetizedChatID); err != nil {
		return fmt.Errorf("expire paywall access: %w", err)
	}
	return nil
}

// RestorePaywallAccessForUser — возвращает доступ удалённому юзеру (обратно к ExpirePaywallAccessForUser):
// делает доступ у его последней completed-заявки снова бессрочным ('infinity').
// ВАЖНО: активный доступ = access_expires_at IS NOT NULL AND > NOW() (см. UserHasActivePaywallAccess),
// поэтому бессрочный — это 'infinity'::timestamptz, а НЕ NULL (NULL = нет доступа).
// Идемпотентно; возвращает true, если была затронута строка (был платёж и доступ восстановлен).
func (d *Database) RestorePaywallAccessForUser(userID, monetizedChatID int64) (bool, error) {
	const q = `
		UPDATE paywall_access_requests
		SET access_expires_at = 'infinity'::timestamptz
		WHERE id = (
			SELECT id FROM paywall_access_requests
			WHERE user_id = $1 AND monetized_chat_id = $2 AND status = 'completed'
			ORDER BY completed_at DESC NULLS LAST, id DESC
			LIMIT 1
		)`
	res, err := d.db.Exec(q, userID, monetizedChatID)
	if err != nil {
		return false, fmt.Errorf("restore paywall access: %w", err)
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}

// SetPaywallYookassaPaymentID — сохраняет id платежа ЮKassa для опроса API, если вебхук не дошёл.
func (d *Database) SetPaywallYookassaPaymentID(reqID int64, yookassaPaymentID string) error {
	const q = `
		UPDATE paywall_access_requests
		SET yookassa_payment_id = $2
		WHERE id = $1 AND status = 'pending'`
	_, err := d.db.Exec(q, reqID, yookassaPaymentID)
	if err != nil {
		return fmt.Errorf("set yookassa payment id: %w", err)
	}
	return nil
}

type PaywallRestoreOutboxPayload struct {
	RequestID int64 `json:"request_id"`
	UserID    int64 `json:"user_id"`
	ChatID    int64 `json:"chat_id"`
}

// CompletePaywallAccessRequest закрывает pending-заявку. Если enqueueRestore=true, кладёт событие в outbox
// (ЮKassa / фоновая доставка). Для Telegram Payments / Stars после оплаты лучше вызывать с enqueueRestore=false
// и сразу держать paywallDeliverAccessAfterPayment в процессе бота — иначе юзер может долго не увидеть приветствие.
func (d *Database) CompletePaywallAccessRequest(id int64, userID, monetizedChatID int64, telegramChargeID string, amountMinor int, currency string, enqueueRestore bool) (bool, error) {
	tx, err := d.db.Begin()
	if err != nil {
		return false, err
	}
	defer tx.Rollback()

	// Доступ — разовая покупка без подписочного срока: пишем 'infinity'::timestamptz, чтобы
	// UserHasActivePaywallAccess возвращал true пока кик за неактивность не выставит NOW()
	// через ExpirePaywallAccessForUser. Никаких «30 дней» (см. требование пользователя).
	const q = `
		UPDATE paywall_access_requests
		SET status = 'completed',
		    completed_at = NOW(),
		    access_expires_at = 'infinity'::timestamptz,
		    monetized_chat_id = $3,
		    telegram_payment_charge_id = $4,
		    total_amount_minor = $5,
		    currency = $6
		WHERE id = $1 AND user_id = $2 AND status = 'pending'`
	res, err := tx.Exec(q, id, userID, monetizedChatID, telegramChargeID, amountMinor, currency)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	if n != 1 {
		return false, nil
	}

	if enqueueRestore {
		payload := PaywallRestoreOutboxPayload{RequestID: id, UserID: userID, ChatID: monetizedChatID}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return false, err
		}
		if _, err := tx.Exec(`
			INSERT INTO outbox_events (event_type, aggregate_key, payload, status, next_attempt_at)
			VALUES ($1, $2, $3::jsonb, 'pending', NOW())
		`, "paywall_access_restore_requested", fmt.Sprintf("paywall_request:%d", id), string(payloadJSON)); err != nil {
			return false, err
		}
	}

	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func (d *Database) CompletePaywallAccessRequestAndEnqueueRestore(id int64, userID, monetizedChatID int64, telegramChargeID string, amountMinor int, currency string) (bool, error) {
	return d.CompletePaywallAccessRequest(id, userID, monetizedChatID, telegramChargeID, amountMinor, currency, true)
}

// PaywallPostPaymentWelcomeSent — уже отправлено DM-приветствие после оплаты по этой заявке (идемпотентные ретраи).
func (d *Database) PaywallPostPaymentWelcomeSent(requestID int64) (bool, error) {
	var t sql.NullTime
	err := d.db.QueryRow(
		`SELECT post_payment_welcome_sent_at FROM paywall_access_requests WHERE id = $1`,
		requestID,
	).Scan(&t)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return t.Valid, nil
}

// PaywallMarkPostPaymentWelcomeSent — отметить успешную отправку приветственного DM (один раз на заявку).
func (d *Database) PaywallMarkPostPaymentWelcomeSent(requestID int64) error {
	_, err := d.db.Exec(
		`UPDATE paywall_access_requests SET post_payment_welcome_sent_at = NOW() WHERE id = $1 AND post_payment_welcome_sent_at IS NULL`,
		requestID,
	)
	return err
}

// EnqueuePaywallAccessRestoreEvent — событие для outbox-воркера: добить paywallDeliverAccessAfterPayment
// (повторный successful_payment от Telegram, сбой Send до отметки welcome, ЮKassa и т.д.).
func (d *Database) EnqueuePaywallAccessRestoreEvent(requestID, userID, chatID int64) error {
	payload := PaywallRestoreOutboxPayload{RequestID: requestID, UserID: userID, ChatID: chatID}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	_, err = d.db.Exec(`
		INSERT INTO outbox_events (event_type, aggregate_key, payload, status, next_attempt_at)
		VALUES ($1, $2, $3::jsonb, 'pending', NOW())
	`, "paywall_access_restore_requested", fmt.Sprintf("paywall_request:%d", requestID), string(payloadJSON))
	return err
}
