package database

import (
	"database/sql"
	"fmt"
	"strings"
)

// UserContactOwner — владелец списка контактов, у которого есть указанный контакт.
type UserContactOwner struct {
	OwnerUserID int64
	ContactName string // имя контакта так, как оно записано в карточке у владельца
}

// SaveUserContact — запоминает контакт, присланный пользователем боту (upsert имени).
func (d *Database) SaveUserContact(ownerUserID, contactUserID int64, contactName string) error {
	if d == nil || ownerUserID == 0 || contactUserID == 0 || ownerUserID == contactUserID {
		return nil
	}
	const q = `
		INSERT INTO user_contacts (owner_user_id, contact_user_id, contact_name)
		VALUES ($1, $2, $3)
		ON CONFLICT (owner_user_id, contact_user_id) DO UPDATE SET
			contact_name = EXCLUDED.contact_name`
	if _, err := d.db.Exec(q, ownerUserID, contactUserID, strings.TrimSpace(contactName)); err != nil {
		return fmt.Errorf("save user contact: %w", err)
	}
	return nil
}

// CountUserContacts — сколько контактов пользователь прислал боту.
func (d *Database) CountUserContacts(ownerUserID int64) (int, error) {
	if d == nil || ownerUserID == 0 {
		return 0, nil
	}
	var n int
	if err := d.db.QueryRow(`SELECT COUNT(*) FROM user_contacts WHERE owner_user_id = $1`, ownerUserID).Scan(&n); err != nil {
		return 0, fmt.Errorf("count user contacts: %w", err)
	}
	return n, nil
}

// ListContactOwners — у кого в контактах есть contactUserID.
func (d *Database) ListContactOwners(contactUserID int64) ([]UserContactOwner, error) {
	if d == nil || contactUserID == 0 {
		return nil, nil
	}
	rows, err := d.db.Query(`
		SELECT owner_user_id, contact_name
		FROM user_contacts
		WHERE contact_user_id = $1 AND owner_user_id <> $1
		ORDER BY created_at`, contactUserID)
	if err != nil {
		return nil, fmt.Errorf("list contact owners: %w", err)
	}
	defer rows.Close()
	var out []UserContactOwner
	for rows.Next() {
		var o UserContactOwner
		if err := rows.Scan(&o.OwnerUserID, &o.ContactName); err != nil {
			return nil, fmt.Errorf("scan contact owner: %w", err)
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

// IsContactJoinNotificationEnabled — включено ли уведомление «контакт вступил в стаю». Нет строки — включено.
func (d *Database) IsContactJoinNotificationEnabled(userID, packChatID int64) (bool, error) {
	if d == nil || userID == 0 || packChatID == 0 {
		return true, nil
	}
	var enabled bool
	err := d.db.QueryRow(
		`SELECT enabled FROM miniapp_contact_join_notifications WHERE user_id = $1 AND pack_chat_id = $2`,
		userID, packChatID,
	).Scan(&enabled)
	if err == sql.ErrNoRows {
		return true, nil
	}
	if err != nil {
		return true, fmt.Errorf("get contact join notification: %w", err)
	}
	return enabled, nil
}

// SaveContactJoinNotificationEnabled — upsert переключателя из профиля мини-аппа.
func (d *Database) SaveContactJoinNotificationEnabled(userID, packChatID int64, enabled bool) error {
	if d == nil || userID == 0 || packChatID == 0 {
		return nil
	}
	const q = `
		INSERT INTO miniapp_contact_join_notifications (user_id, pack_chat_id, enabled, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (user_id, pack_chat_id) DO UPDATE SET
			enabled    = EXCLUDED.enabled,
			updated_at = NOW()`
	if _, err := d.db.Exec(q, userID, packChatID, enabled); err != nil {
		return fmt.Errorf("save contact join notification: %w", err)
	}
	return nil
}

// MarkContactJoinNotified — фиксирует отправку; firstTime=false, если по паре уже уведомляли.
func (d *Database) MarkContactJoinNotified(ownerUserID, contactUserID, packChatID int64) (firstTime bool, err error) {
	if d == nil || ownerUserID == 0 || contactUserID == 0 || packChatID == 0 {
		return false, nil
	}
	res, err := d.db.Exec(`
		INSERT INTO contact_join_notify_log (owner_user_id, contact_user_id, pack_chat_id)
		VALUES ($1, $2, $3)
		ON CONFLICT (owner_user_id, contact_user_id, pack_chat_id) DO NOTHING`,
		ownerUserID, contactUserID, packChatID)
	if err != nil {
		return false, fmt.Errorf("mark contact join notified: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, nil
	}
	return n > 0, nil
}
