package database

import (
	"database/sql"
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
	YookassaPaymentID        sql.NullString
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

// GetLatestPendingPaywallAccessRequest — последняя неоплаченная заявка пользователя на этот чат (для повторной отправки счёта).
func (d *Database) GetLatestPendingPaywallAccessRequest(userID, monetizedChatID int64) (*PaywallAccessRequest, error) {
	const q = `
		SELECT id, user_id, monetized_chat_id, status, created_at, completed_at, access_expires_at,
		       telegram_payment_charge_id, total_amount_minor, currency, yookassa_payment_id
		FROM paywall_access_requests
		WHERE user_id = $1 AND monetized_chat_id = $2 AND status = 'pending'
		ORDER BY id DESC
		LIMIT 1`
	var r PaywallAccessRequest
	err := d.db.QueryRow(q, userID, monetizedChatID).Scan(
		&r.ID, &r.UserID, &r.MonetizedChatID, &r.Status, &r.CreatedAt, &r.CompletedAt, &r.AccessExpiresAt,
		&r.TelegramPaymentChargeID, &r.TotalAmountMinor, &r.Currency, &r.YookassaPaymentID,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &r, nil
}

func (d *Database) GetPaywallAccessRequestByID(id int64) (*PaywallAccessRequest, error) {
	const q = `
		SELECT id, user_id, monetized_chat_id, status, created_at, completed_at, access_expires_at,
		       telegram_payment_charge_id, total_amount_minor, currency, yookassa_payment_id
		FROM paywall_access_requests WHERE id = $1`
	var r PaywallAccessRequest
	err := d.db.QueryRow(q, id).Scan(
		&r.ID, &r.UserID, &r.MonetizedChatID, &r.Status, &r.CreatedAt, &r.CompletedAt, &r.AccessExpiresAt,
		&r.TelegramPaymentChargeID, &r.TotalAmountMinor, &r.Currency, &r.YookassaPaymentID,
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

func (d *Database) CompletePaywallAccessRequest(id int64, userID, monetizedChatID int64, telegramChargeID string, amountMinor int, currency string) (bool, error) {
	const q = `
		UPDATE paywall_access_requests
		SET status = 'completed',
		    completed_at = NOW(),
		    access_expires_at = NOW() + INTERVAL '30 days',
		    telegram_payment_charge_id = $4,
		    total_amount_minor = $5,
		    currency = $6
		WHERE id = $1 AND user_id = $2 AND monetized_chat_id = $3 AND status = 'pending'`
	res, err := d.db.Exec(q, id, userID, monetizedChatID, telegramChargeID, amountMinor, currency)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n == 1, nil
}
