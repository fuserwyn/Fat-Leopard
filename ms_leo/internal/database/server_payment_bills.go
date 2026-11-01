package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// ServerPaymentBill — счёт за сервер за месяц (YYYY-MM). Оплата звёздами и картой идёт на один и тот же счёт.
type ServerPaymentBill struct {
	Month             string
	UserID            int64
	Stars             int // 0 — оплаты звёздами в этом счёте нет
	Rub               int // 0 — оплаты картой в этом счёте нет
	SentAt            time.Time
	PaidAt            sql.NullTime
	YookassaPaymentID sql.NullString
	CardConfirmURL    sql.NullString
}

const serverPaymentBillColumns = `month, user_id, stars, rub, sent_at, paid_at, yookassa_payment_id, card_confirm_url`

func scanServerPaymentBill(row interface{ Scan(...any) error }) (*ServerPaymentBill, error) {
	var r ServerPaymentBill
	err := row.Scan(&r.Month, &r.UserID, &r.Stars, &r.Rub, &r.SentAt, &r.PaidAt, &r.YookassaPaymentID, &r.CardConfirmURL)
	if err != nil {
		return nil, err
	}
	return &r, nil
}

// InsertServerPaymentBill сохраняет счёт; false — за этот месяц счёт уже есть.
func (d *Database) InsertServerPaymentBill(bill ServerPaymentBill) (bool, error) {
	res, err := d.db.Exec(`
		INSERT INTO server_payment_bills (month, user_id, stars, rub, sent_at)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (month) DO NOTHING
	`, bill.Month, bill.UserID, bill.Stars, bill.Rub, bill.SentAt)
	if err != nil {
		return false, fmt.Errorf("insert server payment bill: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("insert server payment bill: %w", err)
	}
	return n > 0, nil
}

// DeleteServerPaymentBill снимает счёт, который не удалось отправить: следующий тик выставит его заново.
func (d *Database) DeleteServerPaymentBill(month string) error {
	if _, err := d.db.Exec(`DELETE FROM server_payment_bills WHERE month = $1`, month); err != nil {
		return fmt.Errorf("delete server payment bill: %w", err)
	}
	return nil
}

// GetServerPaymentBill — счёт за месяц; nil без ошибки, если счёта нет.
func (d *Database) GetServerPaymentBill(month string) (*ServerPaymentBill, error) {
	bill, err := scanServerPaymentBill(d.db.QueryRow(
		`SELECT `+serverPaymentBillColumns+` FROM server_payment_bills WHERE month = $1`, strings.TrimSpace(month)))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get server payment bill: %w", err)
	}
	return bill, nil
}

// SetServerPaymentBillCardPayment запоминает текущий платёж ЮKassa и ссылку на него.
func (d *Database) SetServerPaymentBillCardPayment(month, paymentID, confirmURL string) error {
	if _, err := d.db.Exec(`
		UPDATE server_payment_bills SET yookassa_payment_id = $2, card_confirm_url = $3 WHERE month = $1
	`, month, paymentID, confirmURL); err != nil {
		return fmt.Errorf("set server payment card: %w", err)
	}
	return nil
}

// MarkServerPaymentBillPaid закрывает счёт; false без ошибки — уже закрыт (повторный апдейт о той же оплате).
func (d *Database) MarkServerPaymentBillPaid(month, via, chargeID string, at time.Time) (bool, error) {
	res, err := d.db.Exec(`
		UPDATE server_payment_bills
		SET paid_at = $2, paid_via = $3, telegram_payment_charge_id = NULLIF($4, '')
		WHERE month = $1 AND paid_at IS NULL
	`, month, at, via, chargeID)
	if err != nil {
		return false, fmt.Errorf("mark server payment bill paid: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("mark server payment bill paid: %w", err)
	}
	return n > 0, nil
}

// UnpaidServerPaymentBillsWithCard — неоплаченные счета с платежом ЮKassa: их статус доопрашиваем сами.
func (d *Database) UnpaidServerPaymentBillsWithCard() ([]*ServerPaymentBill, error) {
	rows, err := d.db.Query(`SELECT ` + serverPaymentBillColumns + ` FROM server_payment_bills
		WHERE paid_at IS NULL AND yookassa_payment_id IS NOT NULL ORDER BY month`)
	if err != nil {
		return nil, fmt.Errorf("unpaid server payment bills: %w", err)
	}
	defer rows.Close()
	var out []*ServerPaymentBill
	for rows.Next() {
		bill, err := scanServerPaymentBill(rows)
		if err != nil {
			return nil, fmt.Errorf("scan server payment bill: %w", err)
		}
		out = append(out, bill)
	}
	return out, rows.Err()
}

// OldestOverdueServerPaymentBill — самый старый счёт, выставленный раньше before и не оплаченный; nil — таких нет.
func (d *Database) OldestOverdueServerPaymentBill(before time.Time) (*ServerPaymentBill, error) {
	bill, err := scanServerPaymentBill(d.db.QueryRow(`SELECT `+serverPaymentBillColumns+` FROM server_payment_bills
		WHERE paid_at IS NULL AND sent_at < $1 ORDER BY month LIMIT 1`, before))
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("overdue server payment bill: %w", err)
	}
	return bill, nil
}

// ClaimServerPaymentSuspendNotice — true один раз на счёт: рестарт не повторяет уведомление о паузе.
func (d *Database) ClaimServerPaymentSuspendNotice(month string) (bool, error) {
	res, err := d.db.Exec(`
		UPDATE server_payment_bills SET suspend_notified_at = NOW()
		WHERE month = $1 AND suspend_notified_at IS NULL
	`, month)
	if err != nil {
		return false, fmt.Errorf("claim server payment suspend notice: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim server payment suspend notice: %w", err)
	}
	return n > 0, nil
}
