package bot

import (
	"strings"
	"time"
)

// MiniappCupsHistoryWorkoutLimit — сколько последних тренировок показываем в истории кубков.
const MiniappCupsHistoryWorkoutLimit = 42

// MiniappCupsHistoryWorkout — одна тренировка в истории начислений.
type MiniappCupsHistoryWorkout struct {
	Date        string `json:"date"`
	MessageText string `json:"message_text"`
	Cups        int    `json:"cups"`
	CreatedAt   string `json:"created_at"`
}

// MiniappCupsHistoryWeekly — дополнительные кубки за недельное достижение стаи.
type MiniappCupsHistoryWeekly struct {
	Date      string `json:"date"`
	Cups      int    `json:"cups"`
	CreatedAt string `json:"created_at"`
}

// MiniappCupsHistory — последние тренировки и бонусы недели стаи для шторки профиля.
type MiniappCupsHistory struct {
	Limit      int                         `json:"limit"`
	Workouts   []MiniappCupsHistoryWorkout `json:"workouts"`
	PackWeekly []MiniappCupsHistoryWeekly  `json:"pack_weekly"`
}

// GetMiniappCupsHistoryForAPI — 42 последние тренировки и начисления за недели стаи.
func (b *Bot) GetMiniappCupsHistoryForAPI(userID, packChatID int64) MiniappCupsHistory {
	out := MiniappCupsHistory{
		Limit:      MiniappCupsHistoryWorkoutLimit,
		Workouts:   []MiniappCupsHistoryWorkout{},
		PackWeekly: []MiniappCupsHistoryWeekly{},
	}
	if b == nil || b.db == nil || userID == 0 || packChatID == 0 {
		return out
	}
	chats := []int64{packChatID}
	if userID != packChatID {
		chats = append(chats, userID)
	}
	sessions, err := b.db.ListRecentCupsHistorySessions(userID, chats, MiniappCupsHistoryWorkoutLimit)
	if err != nil {
		b.logger.Warnf("cups history sessions user=%d: %v", userID, err)
	}
	for _, session := range sessions {
		date := strings.TrimSpace(session.SessionDate)
		if len(date) >= 10 {
			date = date[:10]
		}
		created := ""
		if !session.CreatedAt.IsZero() {
			created = session.CreatedAt.UTC().Format(time.RFC3339)
		}
		out.Workouts = append(out.Workouts, MiniappCupsHistoryWorkout{
			Date:        date,
			MessageText: session.MessageText,
			Cups:        session.CupsAdded,
			CreatedAt:   created,
		})
	}
	weekly, err := b.db.ListPackWeeklyGoalMemberCups(userID, packChatID, 200)
	if err != nil {
		b.logger.Warnf("cups history weekly user=%d: %v", userID, err)
	}
	for _, row := range weekly {
		created := ""
		if !row.CreatedAt.IsZero() {
			created = row.CreatedAt.UTC().Format(time.RFC3339)
		}
		out.PackWeekly = append(out.PackWeekly, MiniappCupsHistoryWeekly{
			Date:      moscowCalendarDate(row.CreatedAt),
			Cups:      row.Cups,
			CreatedAt: created,
		})
	}
	return out
}

func moscowCalendarDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.In(time.FixedZone("MSK", 3*60*60)).Format("2006-01-02")
}
