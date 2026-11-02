package database

import (
	"fmt"
	"strings"
)

// ClaimServerPaymentReminder занимает напоминание об оплате сервера за месяц (YYYY-MM).
// true — строки не было и вызывающий должен отправить напоминание; false — уже отправлено.
func (d *Database) ClaimServerPaymentReminder(month string, adminUserID int64) (bool, error) {
	month = strings.TrimSpace(month)
	if d == nil || month == "" || adminUserID == 0 {
		return false, nil
	}
	res, err := d.db.Exec(`
		INSERT INTO server_payment_reminders (month, admin_user_id, sent_at)
		VALUES ($1, $2, NOW())
		ON CONFLICT (month) DO NOTHING
	`, month, adminUserID)
	if err != nil {
		return false, fmt.Errorf("claim server payment reminder: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, fmt.Errorf("claim server payment reminder: %w", err)
	}
	return n > 0, nil
}

// ReleaseServerPaymentReminder снимает отметку за месяц: отправка не удалась, повторим позже.
func (d *Database) ReleaseServerPaymentReminder(month string) error {
	if d == nil || strings.TrimSpace(month) == "" {
		return nil
	}
	if _, err := d.db.Exec(`DELETE FROM server_payment_reminders WHERE month = $1`, strings.TrimSpace(month)); err != nil {
		return fmt.Errorf("release server payment reminder: %w", err)
	}
	return nil
}
