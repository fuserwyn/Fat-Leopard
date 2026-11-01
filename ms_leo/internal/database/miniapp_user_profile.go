package database

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// MiniappUserProfile — опциональные поля мини-аппа (стая / MONETIZED_CHAT_ID).
type MiniappUserProfile struct {
	UserID            int64
	PackChatID        int64
	Gender            string
	DisplayName       string
	AgeYears          sql.NullInt64
	TelegramPhotoURL  string
	Theme             string
	UpdatedAt         time.Time
}

// GetMiniappUserProfile — профиль.
func (d *Database) GetMiniappUserProfile(userID, packChatID int64) (*MiniappUserProfile, error) {
	const q = `
		SELECT user_id, pack_chat_id, gender, display_name, age_years, telegram_photo_url, COALESCE(theme, ''), updated_at
		FROM miniapp_user_profile
		WHERE user_id = $1 AND pack_chat_id = $2`
	var p MiniappUserProfile
	err := d.db.QueryRow(q, userID, packChatID).Scan(
		&p.UserID, &p.PackChatID, &p.Gender, &p.DisplayName, &p.AgeYears, &p.TelegramPhotoURL, &p.Theme, &p.UpdatedAt,
	)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// UpsertMiniappTelegramPhotoURL — URL аватарки из WebApp initData (`user.photo_url`); только колонка photo.
func (d *Database) UpsertMiniappTelegramPhotoURL(userID, packChatID int64, photoURL string) error {
	photoURL = strings.TrimSpace(photoURL)
	if d == nil || packChatID == 0 || userID == 0 || photoURL == "" {
		return nil
	}
	if len(photoURL) > 768 {
		return fmt.Errorf("telegram photo url too long")
	}
	const q = `
		INSERT INTO miniapp_user_profile (user_id, pack_chat_id, gender, display_name, age_years, telegram_photo_url, updated_at)
		VALUES ($1, $2, '', '', NULL, $3, NOW())
		ON CONFLICT (user_id, pack_chat_id) DO UPDATE SET
			telegram_photo_url = EXCLUDED.telegram_photo_url,
			updated_at = EXCLUDED.updated_at`
	_, err := d.db.Exec(q, userID, packChatID, photoURL)
	if err != nil {
		return fmt.Errorf("upsert telegram photo url: %w", err)
	}
	return nil
}

// MiniappTelegramPhotoURLMap — telegram_photo_url по списку user_id (лента / тред / чат стаи).
func (d *Database) MiniappTelegramPhotoURLMap(packChatID int64, userIDs []int64) (map[int64]string, error) {
	out := make(map[int64]string)
	if d == nil || packChatID == 0 || len(userIDs) == 0 {
		return out, nil
	}
	uniq := uniqInt64PreserveOrder(userIDs)
	if len(uniq) == 0 {
		return out, nil
	}
	q := `
		SELECT user_id, telegram_photo_url FROM miniapp_user_profile
		WHERE pack_chat_id = $1 AND user_id = ANY($2)
		  AND NULLIF(BTRIM(telegram_photo_url), '') IS NOT NULL`
	rows, err := d.db.Query(q, packChatID, pq.Array(uniq))
	if err != nil {
		return nil, fmt.Errorf("telegram photo url map: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		var url string
		if err := rows.Scan(&uid, &url); err != nil {
			return nil, err
		}
		url = strings.TrimSpace(url)
		if url != "" {
			out[uid] = url
		}
	}
	return out, rows.Err()
}

func uniqInt64PreserveOrder(ids []int64) []int64 {
	seen := make(map[int64]struct{})
	var out []int64
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
func (d *Database) UpsertMiniappUserProfile(p *MiniappUserProfile) error {
	if p == nil {
		return fmt.Errorf("nil profile")
	}
	if p.PackChatID == 0 {
		return fmt.Errorf("pack chat id")
	}
	const q = `
		INSERT INTO miniapp_user_profile (user_id, pack_chat_id, gender, display_name, age_years, theme, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (user_id, pack_chat_id) DO UPDATE SET
			gender = EXCLUDED.gender,
			display_name = EXCLUDED.display_name,
			age_years = EXCLUDED.age_years,
			theme = CASE WHEN EXCLUDED.theme <> '' THEN EXCLUDED.theme ELSE miniapp_user_profile.theme END,
			updated_at = EXCLUDED.updated_at`
	_, err := d.db.Exec(q, p.UserID, p.PackChatID, p.Gender, p.DisplayName, p.AgeYears, strings.TrimSpace(p.Theme), time.Now().UTC())
	return err
}

// UpsertMiniappTheme — только колонка темы; остальные поля профиля не трогает.
func (d *Database) UpsertMiniappTheme(userID, packChatID int64, theme string) error {
	theme = strings.TrimSpace(theme)
	if d == nil || packChatID == 0 || userID == 0 {
		return nil
	}
	const q = `
		INSERT INTO miniapp_user_profile (user_id, pack_chat_id, gender, display_name, age_years, theme, updated_at)
		VALUES ($1, $2, '', '', NULL, $3, NOW())
		ON CONFLICT (user_id, pack_chat_id) DO UPDATE SET
			theme = EXCLUDED.theme,
			updated_at = EXCLUDED.updated_at`
	_, err := d.db.Exec(q, userID, packChatID, theme)
	if err != nil {
		return fmt.Errorf("upsert miniapp theme: %w", err)
	}
	return nil
}

// PatchTrainingStateGenderIfExists — дублирует пол в training_state (чат стаи).
func (d *Database) PatchTrainingStateGenderIfExists(userID, packChatID int64, gender string) error {
	gender = strings.TrimSpace(strings.ToLower(gender))
	if gender != "m" && gender != "f" && gender != "" {
		return nil
	}
	_, err := d.db.Exec(
		`UPDATE training_state SET gender = $1, updated_at = $2 WHERE user_id = $3 AND chat_id = $4`,
		gender, time.Now().UTC(), userID, packChatID,
	)
	return err
}

// GetTimezoneOffsetFromTrainingState — текущее смещение пользователя относительно МСК (часы) для пары (user, pack).
// 0 если строки нет (или ещё не онбординг) — клиент может это интерпретировать как «по умолчанию МСК».
func (d *Database) GetTimezoneOffsetFromTrainingState(userID, packChatID int64) (int, error) {
	const q = `SELECT COALESCE(timezone_offset_from_moscow, 0)
	             FROM training_state
	             WHERE user_id = $1 AND chat_id = $2`
	var offset int
	if err := d.db.QueryRow(q, userID, packChatID).Scan(&offset); err != nil {
		if err == sql.ErrNoRows {
			return 0, nil
		}
		return 0, err
	}
	return offset, nil
}

// PatchTrainingStateTimezoneOffsetIfExists — точечно меняет TZ в training_state без перезаписи остальных полей.
// Если строки нет — молча no-op (онбординг ещё не прошёл, TZ не нужен).
func (d *Database) PatchTrainingStateTimezoneOffsetIfExists(userID, packChatID int64, offset int) error {
	if offset < -12 || offset > 12 {
		return fmt.Errorf("timezone offset out of range: %d", offset)
	}
	_, err := d.db.Exec(
		`UPDATE training_state SET timezone_offset_from_moscow = $1, updated_at = $2 WHERE user_id = $3 AND chat_id = $4`,
		offset, time.Now().UTC(), userID, packChatID,
	)
	return err
}

// GetTimezoneNameFromTrainingState — сохранённая IANA-зона; пусто, если строки нет или зона ещё не приходила.
func (d *Database) GetTimezoneNameFromTrainingState(userID, packChatID int64) (string, error) {
	var name string
	err := d.db.QueryRow(
		`SELECT timezone_name FROM training_state WHERE user_id = $1 AND chat_id = $2`,
		userID, packChatID,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return name, err
}

// PatchTrainingStateTimezoneNameIfExists — запоминает IANA-зону устройства (Europe/Belgrade).
// Пустое имя не пишем: старый клиент его не шлёт, а зону стирать незачем.
func (d *Database) PatchTrainingStateTimezoneNameIfExists(userID, packChatID int64, name string) error {
	if name == "" {
		return nil
	}
	_, err := d.db.Exec(
		`UPDATE training_state SET timezone_name = $1, updated_at = $2 WHERE user_id = $3 AND chat_id = $4`,
		name, time.Now().UTC(), userID, packChatID,
	)
	return err
}

// TrainingStateTimezone — пользователь с известной IANA-зоной и сохранённым смещением.
type TrainingStateTimezone struct {
	UserID int64
	Name   string
	Offset int
}

// ListTrainingStateTimezones — все строки стаи, у которых известна IANA-зона.
func (d *Database) ListTrainingStateTimezones(packChatID int64) ([]TrainingStateTimezone, error) {
	rows, err := d.db.Query(
		`SELECT user_id, timezone_name, COALESCE(timezone_offset_from_moscow, 0)
		   FROM training_state
		  WHERE chat_id = $1 AND timezone_name <> ''`,
		packChatID,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TrainingStateTimezone
	for rows.Next() {
		var t TrainingStateTimezone
		if err := rows.Scan(&t.UserID, &t.Name, &t.Offset); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}
