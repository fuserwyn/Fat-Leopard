package bot

import (
	"time"

	"leo-bot/internal/game/leopardmoney"
)

// GetSuggestedWorkoutTypesForAPI — персональный порядок типов тренировки для формы отчёта.
func (b *Bot) GetSuggestedWorkoutTypesForAPI(userID, packChatID int64, tzOffsetHours, daysSinceLastTraining int) []string {
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return append([]string(nil), leopardmoney.DefaultWorkoutTypeOrder...)
	}
	chatIDs := []int64{packChatID}
	if userID != packChatID {
		chatIDs = append(chatIDs, userID)
	}
	rows, err := b.db.ListRecentTrainingSessions(userID, chatIDs, 100)
	if err != nil {
		b.logger.Warnf("suggested workout types user=%d pack=%d: %v", userID, packChatID, err)
		return append([]string(nil), leopardmoney.DefaultWorkoutTypeOrder...)
	}
	hints := make([]leopardmoney.WorkoutSessionHint, 0, len(rows))
	for _, r := range rows {
		hints = append(hints, leopardmoney.WorkoutSessionHint{
			MessageText: r.MessageText,
			CreatedAt:   r.CreatedAt,
		})
	}
	return leopardmoney.SuggestWorkoutTypeOrder(hints, time.Now().UTC(), tzOffsetHours, daysSinceLastTraining)
}
