package database

import (
	"database/sql"
	"errors"
	"fmt"
)

// Приглашение друга: ссылка t.me/<бот>?start=ref-<id пригласившего>.
// Засчитывается только новичок — тот, кого бот раньше не видел, — и только
// за первым пригласившим. Награда считается по тем, кто записал первую тренировку.

// RecordReferral запоминает, что inviteeID пришёл по ссылке inviterID.
// false — не засчитано: себя, неизвестный пригласивший, новичок уже бывал в боте
// или уже пришёл по чужой ссылке.
func (d *Database) RecordReferral(inviterID, inviteeID int64) (bool, error) {
	if inviterID == 0 || inviteeID == 0 || inviterID == inviteeID {
		return false, nil
	}
	res, err := d.db.Exec(`
		INSERT INTO referrals (invitee_user_id, inviter_user_id)
		SELECT $2, $1
		WHERE EXISTS (SELECT 1 FROM training_state WHERE user_id = $1)
		  AND NOT EXISTS (SELECT 1 FROM training_state WHERE user_id = $2)
		  AND NOT EXISTS (SELECT 1 FROM bot_visits WHERE user_id = $2)
		  AND NOT EXISTS (SELECT 1 FROM events WHERE telegram_id = $2 OR user_id = $2)
		ON CONFLICT (invitee_user_id) DO NOTHING`, inviterID, inviteeID)
	if err != nil {
		return false, fmt.Errorf("record referral: %w", err)
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// MarkReferralFirstWorkout отмечает первую тренировку приглашённого. Возвращает
// пригласившего и сколько его друзей уже записали тренировку — только если отметка
// поставлена сейчас; иначе inviterID = 0 (не приглашён или уже отмечен).
func (d *Database) MarkReferralFirstWorkout(inviteeID int64) (inviterID int64, qualified int, err error) {
	if inviteeID == 0 {
		return 0, 0, nil
	}
	err = d.db.QueryRow(`
		UPDATE referrals SET first_workout_at = NOW()
		WHERE invitee_user_id = $1 AND first_workout_at IS NULL
		RETURNING inviter_user_id`, inviteeID).Scan(&inviterID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, 0, nil
	}
	if err != nil {
		return 0, 0, fmt.Errorf("mark referral first workout: %w", err)
	}
	_, qualified, err = d.GetReferralCounts(inviterID)
	if err != nil {
		return inviterID, 0, err
	}
	return inviterID, qualified, nil
}

// GetReferralCounts — сколько пришло по ссылке и сколько из них записали тренировку.
func (d *Database) GetReferralCounts(inviterID int64) (joined, qualified int, err error) {
	err = d.db.QueryRow(`
		SELECT COUNT(*), COUNT(first_workout_at)
		FROM referrals WHERE inviter_user_id = $1`, inviterID).Scan(&joined, &qualified)
	if err != nil {
		return 0, 0, fmt.Errorf("referral counts: %w", err)
	}
	return joined, qualified, nil
}
