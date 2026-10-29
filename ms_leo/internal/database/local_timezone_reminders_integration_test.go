package database

import "testing"

// Задача #165: мини-апп сохраняет пояс телефона, а планировщики напоминания и мудрости дня
// должны сразу видеть новый пояс и хранить отметку отправки по локальной дате юзера.
func TestRemindersFollowTimezoneSavedFromMiniapp(t *testing.T) {
	d := openMigratedTestDB(t)
	const pack, user = int64(-100165), int64(165)
	if _, err := d.db.Exec(`INSERT INTO training_state (user_id, chat_id, username, last_message) VALUES ($1, $2, 'stas', NOW())`, user, pack); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveWorkoutReminderSettings(user, pack, WorkoutReminderSettings{Enabled: true, RemindHour: 23}); err != nil {
		t.Fatal(err)
	}
	if err := d.SaveWisdomSubscriptionSettings(user, pack, WisdomSubscriptionSettings{Enabled: true, RemindHour: 4}); err != nil {
		t.Fatal(err)
	}

	// Москва → Белград: мини-апп прислал МСК-2.
	if err := d.PatchTrainingStateTimezoneOffsetIfExists(user, pack, -2); err != nil {
		t.Fatal(err)
	}
	if err := d.PatchTrainingStateTimezoneOffsetIfExists(user, pack, 13); err == nil {
		t.Fatal("offset out of range must be rejected")
	}

	rem, err := d.ListDueWorkoutReminderCandidates(pack)
	if err != nil || len(rem) != 1 {
		t.Fatalf("reminder candidates: %v %+v", err, rem)
	}
	if rem[0].TimezoneOffsetFromMoscow != -2 || rem[0].RemindHour != 23 {
		t.Fatalf("reminder candidate: %+v", rem[0])
	}
	wis, err := d.ListDueWisdomSubscriptionCandidates(pack)
	if err != nil || len(wis) != 1 || wis[0].TimezoneOffsetFromMoscow != -2 {
		t.Fatalf("wisdom candidates: %v %+v", err, wis)
	}

	// Отметка отправки — локальная дата (23:00 по Белграду 28-го = 01:00 МСК 29-го).
	if err := d.MarkWorkoutReminderSent(user, pack, "2026-10-28"); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkWisdomSubscriptionSent(user, pack, "2026-10-28"); err != nil {
		t.Fatal(err)
	}
	rem, err = d.ListDueWorkoutReminderCandidates(pack)
	if err != nil || len(rem) != 1 || rem[0].LastSentMskDate.String != "2026-10-28" {
		t.Fatalf("reminder after mark: %v %+v", err, rem)
	}
	wis, err = d.ListDueWisdomSubscriptionCandidates(pack)
	if err != nil || len(wis) != 1 || wis[0].LastSentMskDate.String != "2026-10-28" {
		t.Fatalf("wisdom after mark: %v %+v", err, wis)
	}
}
