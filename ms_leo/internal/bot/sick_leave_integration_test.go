package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"

	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func privateMsg(userID int64, text string) *tgbotapi.Message {
	return &tgbotapi.Message{
		MessageID: 5,
		From:      &tgbotapi.User{ID: userID, UserName: "leopard"},
		Chat:      &tgbotapi.Chat{ID: userID, Type: "private"},
		Text:      text,
	}
}

func sickState(t *testing.T, db *sql.DB, userID int64) (sick, healthy bool) {
	t.Helper()
	if err := db.QueryRow(`SELECT COALESCE(has_sick_leave, FALSE), COALESCE(has_healthy, FALSE) FROM training_state WHERE user_id = $1 AND chat_id = $2`, userID, itPack).Scan(&sick, &healthy); err != nil {
		t.Fatal(err)
	}
	return
}

func lastTextTo(tg *fakeTelegram, userID int64) string {
	texts := tg.textsTo(userID)
	if len(texts) == 0 {
		return ""
	}
	return texts[len(texts)-1]
}

// Больничный ставит удаление на паузу; выход с больничного возвращает отсчёт.
func TestSickLeavePausesRemovalUntilRecovery(t *testing.T) {
	b, tg, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)
	setTimerStarted(t, db, itUser, 3)

	b.handleSickLeave(privateMsg(itUser, "#sick_leave"))
	if sick, _ := sickState(t, db, itUser); !sick {
		t.Fatal("больничный без пояснения принимается сразу")
	}
	if text := lastTextTo(tg, itUser); !strings.Contains(text, "Больничный принят") || !strings.Contains(text, "Таймер приостановлен") {
		t.Errorf("ответ на больничный: %q", text)
	}

	tg.reset()
	b.handleSickLeave(privateMsg(itUser, "#sick_leave"))
	if text := lastTextTo(tg, itUser); !strings.Contains(text, "уже активен больничный") {
		t.Errorf("повторный запрос: %q", text)
	}

	// Пока болеет, срок может выйти — удалять нельзя.
	setTimerStarted(t, db, itUser, 30)
	b.runInactivityKickSweep()
	if isKicked(t, db, itUser) {
		t.Fatal("на больничном не удаляют")
	}

	// Выздоровел, пока срок не вышел.
	setTimerStarted(t, db, itUser, 3)
	tg.reset()
	b.handleHealthy(privateMsg(itUser, "#healthy"))
	sick, healthy := sickState(t, db, itUser)
	if sick || !healthy {
		t.Fatalf("после выздоровления: sick=%v healthy=%v", sick, healthy)
	}
	if text := lastTextTo(tg, itUser); !strings.Contains(text, "Выздоровление принято") {
		t.Errorf("ответ на выздоровление: %q", text)
	}
	if isKicked(t, db, itUser) {
		t.Fatal("выздоровевшего в срок не удаляют")
	}

	tg.reset()
	b.handleHealthy(privateMsg(itUser, "#healthy"))
	if text := lastTextTo(tg, itUser); !strings.Contains(text, "активного больничного сейчас нет") {
		t.Errorf("выздоровление без больничного: %q", text)
	}
}

// Отговорка вместо болезни — больничный не даётся; описание болезни — даётся.
func TestSickLeaveJustificationIsChecked(t *testing.T) {
	b, tg, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "excuse", false)
	seedMember(t, db, itUser+1, "ill", false)

	b.handleSickLeave(privateMsg(itUser, "#sick_leave много работы, некогда"))
	if sick, _ := sickState(t, db, itUser); sick {
		t.Fatal("работа — не причина для больничного")
	}
	if text := lastTextTo(tg, itUser); !strings.Contains(text, "Больничный не принят") {
		t.Errorf("ответ на отговорку: %q", text)
	}

	b.handleSickLeave(privateMsg(itUser+1, "#sick_leave температура 38 и кашель"))
	if sick, _ := sickState(t, db, itUser+1); !sick {
		t.Fatal("болезнь — причина для больничного")
	}
}

// Дни болезни стрик не жгут: вернулся с больничного — серия продолжается.
func TestSickDaysDoNotBreakStreak(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)
	seedMember(t, db, itUser+1, "lazy", false)
	setTrainingHistory(t, db, itUser, 5, 5, 4)
	setTrainingHistory(t, db, itUser+1, 5, 5, 4)

	// Первый заболел на следующий день после тренировки и болеет до сих пор.
	sickStart := utils.FormatMoscowTime(utils.GetMoscowTime().AddDate(0, 0, -3))
	if _, err := db.Exec(`UPDATE training_state SET has_sick_leave = TRUE, has_healthy = FALSE, sick_leave_start_time = $2 WHERE user_id = $1`, itUser, sickStart); err != nil {
		t.Fatal(err)
	}

	logWorkout(b, itUser, "#training_done после болезни")
	st := memberStateOf(t, db, itUser)
	if st.Streak != 6 {
		t.Errorf("стрик после больничного продолжается: %+v", st)
	}
	if sick, _ := sickState(t, db, itUser); sick {
		t.Error("тренировка снимает больничный")
	}

	// Второй просто пропустил те же дни — серия с единицы.
	logWorkout(b, itUser+1, "#training_done после прогула")
	if st := memberStateOf(t, db, itUser+1); st.Streak != 1 {
		t.Errorf("прогул без больничного обнуляет серию: %+v", st)
	}
}

// Вышел с больничного, когда прежний срок уже истёк: сразу не удаляют —
// есть время до конца суток, чтобы потренироваться (inactivityKickDeadline).
func TestLateRecoveryGivesGraceUntilMidnight(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "leopard", false)
	setTimerStarted(t, db, itUser, 3)
	b.handleSickLeave(privateMsg(itUser, "#sick_leave"))

	// Отсчёт давно истёк, больничный только что закрыт.
	setTimerStarted(t, db, itUser, 40)
	tg.reset()
	b.handleHealthy(privateMsg(itUser, "#healthy"))

	if isKicked(t, db, itUser) {
		t.Fatal("после выздоровления дают время до конца суток")
	}
	if text := lastTextTo(tg, itUser); !strings.Contains(text, "Выздоровление принято") {
		t.Errorf("ответ на выздоровление: %q", text)
	}
	b.runInactivityKickSweep()
	if isKicked(t, db, itUser) {
		t.Fatal("сторож тоже не удаляет до конца суток")
	}
	ml, err := b.db.GetMessageLog(itUser, itPack)
	if err != nil {
		t.Fatal(err)
	}
	now := utils.GetMoscowTime()
	deadline, ok := inactivityKickDeadline(ml, now)
	if !ok || !deadline.After(now) || deadline.Sub(now) > 24*time.Hour {
		t.Errorf("срок — ближайшая полночь: %v (сейчас %v)", deadline, now)
	}
}
