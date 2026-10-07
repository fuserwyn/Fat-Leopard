package database

import (
	"database/sql"
	"fmt"
)

// SaveReleaseNotesLog — фиксируем опубликованные Release Notes за период (period_end — дата конца окна, МСК).
func (d *Database) SaveReleaseNotesLog(periodEnd, text string) error {
	if d == nil || periodEnd == "" || text == "" {
		return nil
	}
	const q = `
		INSERT INTO release_notes_log (period_end, text)
		VALUES ($1, $2)
		ON CONFLICT (period_end) DO UPDATE SET text = EXCLUDED.text`
	if _, err := d.db.Exec(q, periodEnd, text); err != nil {
		return fmt.Errorf("save release notes log: %w", err)
	}
	return nil
}

// ClaimReleaseNotesLog — атомарно занять выпуск за период перед публикацией.
// false — запись уже есть: другой экземпляр бота (при деплое старый и новый
// живут вместе) публикует или уже опубликовал этот выпуск.
func (d *Database) ClaimReleaseNotesLog(periodEnd, text string) (bool, error) {
	if d == nil || periodEnd == "" || text == "" {
		return false, nil
	}
	res, err := d.db.Exec(`
		INSERT INTO release_notes_log (period_end, text)
		VALUES ($1, $2)
		ON CONFLICT (period_end) DO NOTHING`, periodEnd, text)
	if err != nil {
		return false, fmt.Errorf("claim release notes log: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ReleaseReleaseNotesLog — снять занятый выпуск, если публикация не удалась,
// чтобы следующая попытка могла пройти.
func (d *Database) ReleaseReleaseNotesLog(periodEnd string) error {
	if d == nil || periodEnd == "" {
		return nil
	}
	if _, err := d.db.Exec(`DELETE FROM release_notes_log WHERE period_end = $1`, periodEnd); err != nil {
		return fmt.Errorf("release release notes log: %w", err)
	}
	return nil
}

// HasReleaseNotesLog — уже публиковали Release Notes с таким period_end.
func (d *Database) HasReleaseNotesLog(periodEnd string) (bool, error) {
	if d == nil || periodEnd == "" {
		return false, nil
	}
	var n int
	err := d.db.QueryRow(`SELECT COUNT(*) FROM release_notes_log WHERE period_end = $1`, periodEnd).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("has release notes log: %w", err)
	}
	return n > 0, nil
}

// GetLatestReleaseNotesPeriodEnd — дата последней публикации (YYYY-MM-DD) или пусто.
func (d *Database) GetLatestReleaseNotesPeriodEnd() (string, error) {
	if d == nil {
		return "", nil
	}
	var s sql.NullString
	err := d.db.QueryRow(`SELECT to_char(period_end, 'YYYY-MM-DD') FROM release_notes_log ORDER BY period_end DESC LIMIT 1`).Scan(&s)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("get latest release notes period: %w", err)
	}
	return s.String, nil
}
