package bot

import (
	"database/sql"
	"strings"
	"testing"

	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type memberState struct {
	Streak, MaxStreak, Cups, Achievements int
	LastTraining                          string
}

func memberStateOf(t *testing.T, db *sql.DB, userID int64) memberState {
	t.Helper()
	var s memberState
	var last sql.NullString
	if err := db.QueryRow(`
		SELECT streak_days, COALESCE(max_streak_days, 0), COALESCE(cups_earned, 0), COALESCE(achievement_count, 0), last_training_date::text
		FROM training_state WHERE user_id = $1 AND chat_id = $2`, userID, itPack).Scan(&s.Streak, &s.MaxStreak, &s.Cups, &s.Achievements, &last); err != nil {
		t.Fatal(err)
	}
	s.LastTraining = last.String
	return s
}

// setTrainingHistory выставляет участнику прошлое: стрик и дату последней
// тренировки daysAgo дней назад (по московскому времени, как считает бот).
func setTrainingHistory(t *testing.T, db *sql.DB, userID int64, streak, maxStreak, daysAgo int) {
	t.Helper()
	last := utils.GetMoscowTime().AddDate(0, 0, -daysAgo).Format("2006-01-02")
	if _, err := db.Exec(`
		UPDATE training_state SET streak_days = $3, max_streak_days = $4, last_training_date = $5
		WHERE user_id = $1 AND chat_id = $2`, userID, itPack, streak, maxStreak, last); err != nil {
		t.Fatal(err)
	}
}

// logWorkout записывает тренировку так, как это делает мини-апп, и возвращает
// сводку, которую видит пользователь.
func logWorkout(b *Bot, userID int64, text string) string {
	reply := make(chan string, 1)
	b.handleLeopardMoneyTrainingDone(&tgbotapi.Message{
		From: &tgbotapi.User{ID: userID, UserName: "leopard"},
		Chat: &tgbotapi.Chat{ID: userID, Type: "private"},
		Text: text,
	}, reply, 0)
	select {
	case s := <-reply:
		return s
	default:
		return ""
	}
}

// Первая тренировка начинает стрик, начисляет кубки и пишется в историю.
func TestWorkoutFirstReportStartsStreak(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)
	const report = "#training_done пробежка"

	summary := logWorkout(b, itUser, report)

	st := memberStateOf(t, db, itUser)
	today := utils.GetMoscowTime().Format("2006-01-02")
	wantCups := leopardmoney.TrainingCupsFromReportText(report)
	if st.Streak != 1 || st.MaxStreak != 1 || st.Cups != wantCups || st.LastTraining != today {
		t.Errorf("состояние после первой тренировки: %+v (кубков ждали %d, дата %s)", st, wantCups, today)
	}
	if !strings.Contains(summary, "Отчёт принят") || !strings.Contains(summary, "Стрик: 1 ") {
		t.Errorf("сводка пользователю: %q", summary)
	}
	var sessions, cups int
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(SUM(cups_added), 0) FROM training_sessions WHERE user_id = $1 AND session_date = $2`, itUser, today).Scan(&sessions, &cups); err != nil {
		t.Fatal(err)
	}
	if sessions != 1 || cups != wantCups {
		t.Errorf("запись в истории тренировок: %d шт., %d кубков", sessions, cups)
	}
}

// Правила стрика на настоящей базе: вчера → +1, пропуск → с единицы,
// вторая тренировка за день стрик не меняет, рекорд не уменьшается.
func TestWorkoutStreakRules(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)

	setTrainingHistory(t, db, itUser, 5, 5, 1)
	logWorkout(b, itUser, "#training_done йога")
	if st := memberStateOf(t, db, itUser); st.Streak != 6 || st.MaxStreak != 6 {
		t.Fatalf("тренировка на следующий день: %+v", st)
	}

	logWorkout(b, itUser, "#training_done ещё раз")
	if st := memberStateOf(t, db, itUser); st.Streak != 6 {
		t.Fatalf("вторая тренировка за день не растит стрик: %+v", st)
	}

	setTrainingHistory(t, db, itUser, 10, 10, 3)
	summary := logWorkout(b, itUser, "#training_done вернулся")
	if st := memberStateOf(t, db, itUser); st.Streak != 1 || st.MaxStreak != 10 {
		t.Fatalf("после пропуска стрик с единицы, рекорд на месте: %+v", st)
	}
	if !strings.Contains(summary, "Стрик: 1 ") {
		t.Errorf("сводка после пропуска: %q", summary)
	}
}

// Стрик в 7 дней даёт первую ачивку — и только один раз.
func TestWorkoutStreakMilestoneGrantsAchievement(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)

	setTrainingHistory(t, db, itUser, 5, 5, 1)
	logWorkout(b, itUser, "#training_done день шестой")
	if st := memberStateOf(t, db, itUser); st.Achievements != 0 {
		t.Fatalf("на шестой день ачивки ещё нет: %+v", st)
	}

	setTrainingHistory(t, db, itUser, 6, 6, 1)
	logWorkout(b, itUser, "#training_done день седьмой")
	st := memberStateOf(t, db, itUser)
	if st.Streak != 7 || st.Achievements != leopardmoney.AchievementsCountForStreak(7) || st.Achievements == 0 {
		t.Fatalf("на седьмой день — первая ачивка: %+v", st)
	}

	// Стрик сгорел и снова дошёл до семи — рекорд тот же, ачивка не дублируется.
	setTrainingHistory(t, db, itUser, 6, 7, 1)
	logWorkout(b, itUser, "#training_done снова седьмой")
	if again := memberStateOf(t, db, itUser); again.Achievements != st.Achievements {
		t.Errorf("ачивка за тот же рубеж выдана повторно: %+v", again)
	}
}

// Тренировка, закрывшая недельную цель стаи, даёт бонус всем — один раз за неделю.
func TestWorkoutClosesPackWeeklyGoal(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)
	seedMember(t, db, itUser+1, "friend", false)
	if err := b.db.SetPackWeeklyWorkoutGoal(itPack, 2, itAdmin); err != nil {
		t.Fatal(err)
	}

	logWorkout(b, itUser, "#training_done раз")
	if p := b.GetMiniappPackWeeklyProgressForAPI(itPack); p.Goal != 2 || p.WorkoutsWeek != 1 || p.GoalReached || p.BonusActive {
		t.Fatalf("до цели одна тренировка: %+v", p)
	}
	friendBefore := memberStateOf(t, db, itUser+1).Cups

	logWorkout(b, itUser+1, "#training_done два")
	p := b.GetMiniappPackWeeklyProgressForAPI(itPack)
	if !p.GoalReached || !p.BonusActive || p.BonusActiveUntil == "" || !b.IsPackBonusThemeActive(itPack) {
		t.Fatalf("цель закрыта — бонусная тема активна: %+v", p)
	}
	workoutCups := leopardmoney.TrainingCupsFromReportText("#training_done два")
	if got := memberStateOf(t, db, itUser+1).Cups - friendBefore; got != workoutCups+PackWeeklyGoalCupsBonus {
		t.Errorf("кубки закрывшему цель: +%d, ждали %d за тренировку и %d бонус", got, workoutCups, PackWeeklyGoalCupsBonus)
	}
	firstCups := memberStateOf(t, db, itUser).Cups

	logWorkout(b, itUser, "#training_done три")
	if got := memberStateOf(t, db, itUser).Cups - firstCups; got != leopardmoney.TrainingCupsFromReportText("#training_done три") {
		t.Errorf("бонус недели выдаётся один раз: +%d", got)
	}
	var posts int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_messages WHERE message_text LIKE 'Цель недели стаи достигнута%'`).Scan(&posts); err != nil || posts != 1 {
		t.Errorf("пост в ленте о закрытой цели: %d %v", posts, err)
	}
}
