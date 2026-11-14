package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Правка показателей участника из карточки в мини-аппе: кубки, стрик, рекорд
// и зачёт тренировок. Раньше это жило только в чате (/admin → «Найти юзера»).
//
// Уровень отдельно не задаётся: он считается от накопленных кубков, поэтому
// правка кубков сама двигает уровень и попытки спасти стрик.

// MiniappAdminStatField — что правим.
type MiniappAdminStatField string

const (
	StatFieldCups     MiniappAdminStatField = "cups"
	StatFieldStreak   MiniappAdminStatField = "streak"
	StatFieldRecord   MiniappAdminStatField = "record"
	StatFieldWorkouts MiniappAdminStatField = "workouts"
)

// MiniappAdminSetStat — задать или сдвинуть показатель.
// mode: "set" — выставить значение, "add" — прибавить (для тренировок — зачесть
// или снять последнюю).
func (b *Bot) MiniappAdminSetStat(
	viewerUserID int64,
	initD initdata.InitData,
	targetUserID int64,
	field MiniappAdminStatField,
	mode string,
	value int,
) error {
	packChatID, err := b.requireMiniappAdmin(viewerUserID, initD)
	if err != nil {
		return err
	}
	if targetUserID == 0 {
		return ErrAdminActionInvalid
	}
	add := strings.TrimSpace(mode) != "set"

	switch field {
	case StatFieldCups:
		if add {
			if err := b.db.AddCups(targetUserID, packChatID, value); err != nil {
				return err
			}
		} else {
			if err := b.db.SetCupsForUserScope(targetUserID, packChatID, value); err != nil {
				return err
			}
		}
		return nil

	case StatFieldStreak:
		ml, err := b.db.GetMessageLogAnyState(targetUserID, packChatID)
		if err != nil || ml == nil {
			return ErrAdminNotFound
		}
		next := value
		if add {
			next = ml.StreakDays + value
		}
		if next < 0 {
			next = 0
		}
		lastDate := ""
		if ml.LastTrainingDate != nil {
			lastDate = strings.TrimSpace(*ml.LastTrainingDate)
		}
		if lastDate == "" {
			lastDate = time.Now().UTC().Format("2006-01-02")
		}
		if err := b.db.UpdateStreak(targetUserID, packChatID, next, lastDate); err != nil {
			return err
		}
		// Ачивки завязаны на рекорд серии — пересобираем, иначе карточка и
		// достижения разъедутся.
		if after, e := b.db.GetMessageLogAnyState(targetUserID, packChatID); e == nil && after != nil {
			max := next
			if after.MaxStreakDays > max {
				max = after.MaxStreakDays
			}
			_, _ = b.syncAchievementsFromStreak(targetUserID, packChatID, max)
		}
		return nil

	case StatFieldRecord:
		next := value
		if add {
			ml, err := b.db.GetMessageLogAnyState(targetUserID, packChatID)
			if err != nil || ml == nil {
				return ErrAdminNotFound
			}
			next = ml.MaxStreakDays + value
		}
		if next < 0 {
			next = 0
		}
		if err := b.db.AdminSetMaxStreak(targetUserID, packChatID, next); err != nil {
			return err
		}
		_, _ = b.syncAchievementsFromStreak(targetUserID, packChatID, next)
		return nil

	case StatFieldWorkouts:
		// Общее число тренировок — не счётчик, а количество записей о сессиях.
		// Поэтому «+N» добавляет записи задним числом на сегодня, «−N» снимает
		// последние: иначе цифра в карточке разошлась бы с историей.
		if !add {
			return fmt.Errorf("тренировки можно только зачесть или снять")
		}
		if value > 0 {
			today := time.Now().UTC().Format("2006-01-02")
			for i := 0; i < value && i < 50; i++ {
				if err := b.db.SaveTrainingSession(&domain.TrainingSession{
					UserID:         targetUserID,
					ChatID:         packChatID,
					SessionDate:    today,
					MessageText:    "зачтено админом",
					TrainingsCount: 1,
				}); err != nil {
					return err
				}
			}
			return nil
		}
		for i := 0; i < -value && i < 50; i++ {
			ok, err := b.db.AdminDeleteLatestTrainingSession(targetUserID, packChatID)
			if err != nil {
				return err
			}
			if !ok {
				break
			}
		}
		return nil
	}
	return ErrAdminActionInvalid
}
