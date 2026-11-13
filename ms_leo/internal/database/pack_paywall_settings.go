package database

import (
	"database/sql"
	"fmt"
)

// GetPackPaywallAmountMinor — цена access, заданная админом (копейки RUB).
// ok=false, если строки нет или сумма не задана: тогда вызывающий берёт env-дефолт.
func (d *Database) GetPackPaywallAmountMinor(packChatID int64) (amountMinor int, ok bool, err error) {
	if d == nil || packChatID == 0 {
		return 0, false, nil
	}
	err = d.db.QueryRow(
		`SELECT amount_minor FROM pack_paywall_settings WHERE pack_chat_id = $1`,
		packChatID,
	).Scan(&amountMinor)
	if err == sql.ErrNoRows {
		return 0, false, nil
	}
	if err != nil {
		return 0, false, fmt.Errorf("get pack paywall amount: %w", err)
	}
	return amountMinor, amountMinor > 0, nil
}

// SetPackPaywallAmountMinor — админ выставляет цену access (копейки RUB).
func (d *Database) SetPackPaywallAmountMinor(packChatID int64, amountMinor int, updatedBy int64) error {
	if packChatID == 0 {
		return fmt.Errorf("pack not configured")
	}
	if amountMinor <= 0 || amountMinor > 10_000_000 {
		return fmt.Errorf("invalid paywall amount")
	}
	_, err := d.db.Exec(`
		INSERT INTO pack_paywall_settings (pack_chat_id, amount_minor, updated_by, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (pack_chat_id) DO UPDATE
		  SET amount_minor = EXCLUDED.amount_minor,
		      updated_by = EXCLUDED.updated_by,
		      updated_at = NOW()
	`, packChatID, amountMinor, updatedBy)
	if err != nil {
		return fmt.Errorf("set pack paywall amount: %w", err)
	}
	return nil
}

// ClearPackPaywallAmountMinor — вернуть цену к значению из настроек сервера.
func (d *Database) ClearPackPaywallAmountMinor(packChatID int64) error {
	if packChatID == 0 {
		return fmt.Errorf("pack not configured")
	}
	_, err := d.db.Exec(`DELETE FROM pack_paywall_settings WHERE pack_chat_id = $1`, packChatID)
	if err != nil {
		return fmt.Errorf("clear pack paywall amount: %w", err)
	}
	return nil
}
