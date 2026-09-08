package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/database"
	"leo-bot/internal/domain"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func formatRemovalAtLocalHuman(removalAt time.Time, loc *time.Location) string {
	d := removalAt.In(loc)
	months := []string{
		"", "января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря",
	}
	return fmt.Sprintf("%d %s, 00:00", d.Day(), months[int(d.Month())])
}

func (b *Bot) startTimer(userID, chatID int64, username string) {
	b.startTimerWithDuration(userID, chatID, username, leopardmoney.FullTimerDuration)
}

func (b *Bot) startTimerWithDuration(userID, chatID int64, username string, _ time.Duration) {
	messageLog, err := b.db.GetMessageLog(userID, chatID)
	if err == nil && messageLog.IsExemptFromDeletion {
		b.logger.Infof("User %d (%s) is exempt from deletion, skipping timer", userID, username)
		return
	}

	b.cancelTimer(userID)

	ch72 := make(chan bool)
	ch48 := make(chan bool)
	ch24 := make(chan bool)
	chRem := make(chan bool)

	timerStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	timerInfo := &domain.TimerInfo{
		UserID:         userID,
		ChatID:         chatID,
		Username:       username,
		Warning72hTask: ch72,
		Warning48hTask: ch48,
		Warning24hTask: ch24,
		RemovalTask:    chRem,
		TimerStartTime: timerStartTime,
	}
	b.timers[userID] = timerInfo

	tzOffset := 0
	messageLog, err = b.db.GetMessageLog(userID, chatID)
	if err != nil {
		b.logger.Errorf("Failed to get message log for timer start: %v", err)
	} else {
		tzOffset = messageLog.TimezoneOffsetFromMoscow
		// Только timer_start_time: полный SaveMessageLog здесь гонялся с UpdateStreak
		// и мог откатить только что пересчитанный streak_days.
		if err := b.db.SetTimerStartTime(userID, chatID, timerStartTime); err != nil {
			b.logger.Errorf("Failed to save timer start time: %v", err)
		}
	}

	timerStart, err := utils.ParseMoscowTime(timerStartTime)
	if err != nil {
		b.logger.Errorf("parse timer start: %v", err)
		return
	}
	removalAt := removalDeadlineLocal(timerStart, tzOffset)
	b.scheduleLeopardMilestones(userID, chatID, username, removalAt, tzOffset, ch72, ch48, ch24, chRem)
	b.logger.Infof("Started Leopard inactive chain for user %d (%s) from %s", userID, username, timerStartTime)
}

func (b *Bot) restoreTimerWithDuration(userID, chatID int64, username string, remaining time.Duration, existingTimerStartTime string, tzOffsetFromMoscow int) {
	b.cancelTimer(userID)

	ch72 := make(chan bool)
	ch48 := make(chan bool)
	ch24 := make(chan bool)
	chRem := make(chan bool)

	timerInfo := &domain.TimerInfo{
		UserID:         userID,
		ChatID:         chatID,
		Username:       username,
		Warning72hTask: ch72,
		Warning48hTask: ch48,
		Warning24hTask: ch24,
		RemovalTask:    chRem,
		TimerStartTime: existingTimerStartTime,
	}
	b.timers[userID] = timerInfo

	timerStart, err := utils.ParseMoscowTime(existingTimerStartTime)
	if err != nil {
		b.logger.Errorf("restore timer parse: %v", err)
		return
	}
	// Абсолютный дедлайн кика: now + remaining (учитывает заморозку на больничном и сдвиг после #healthy).
	// Нельзя брать removalDeadlineLocal(timerStart) — это старый D₀ без паузы болезни.
	now := utils.GetMoscowTime()
	removalAt := now.Add(remaining)
	b.scheduleLeopardMilestones(userID, chatID, username, removalAt, tzOffsetFromMoscow, ch72, ch48, ch24, chRem)
	b.logger.Infof("Restored Leopard inactive chain for user %d (%s), removal at %s (~%v left, training anchor %s)", userID, username, removalAt.Format(time.RFC3339), remaining, timerStart.Format(time.RFC3339))
}

func (b *Bot) scheduleLeopardMilestones(userID, chatID int64, username string, removalAt time.Time, tzOffsetFromMoscow int, ch72, ch48, ch24, chRem chan bool) {
	now := utils.GetMoscowTime()
	loc := userLocalLoc(tzOffsetFromMoscow)
	type milestone struct {
		at      time.Time
		ch      chan bool
		fn      func()
		removal bool
	}
	ms := []milestone{
		{
			at: removalAt.Add(-72 * time.Hour),
			ch: ch72,
			fn: func() {
				b.sendInactiveRemovalWarning(userID, chatID, username, 72, removalAt, loc)
			},
			removal: false,
		},
		{
			at: removalAt.Add(-48 * time.Hour),
			ch: ch48,
			fn: func() {
				b.sendInactiveRemovalWarning(userID, chatID, username, 48, removalAt, loc)
			},
			removal: false,
		},
		{
			at: removalAt.Add(-24 * time.Hour),
			ch: ch24,
			fn: func() {
				b.sendInactiveRemovalWarning(userID, chatID, username, 24, removalAt, loc)
			},
			removal: false,
		},
		{
			at:      removalAt,
			ch:      chRem,
			fn:      func() { b.removeUser(userID, chatID, username) },
			removal: true,
		},
	}
	for _, m := range ms {
		delay := m.at.Sub(now)
		removal := m.removal
		go func(delay time.Duration, ch chan bool, fn func(), removal bool) {
			// Предупреждения в прошлом — не шлём задним числом. Кик: если дедлайн уже прошёл — выполнить.
			if delay <= 0 {
				if !removal {
					return
				}
				select {
				case <-ch:
					return
				default:
					fn()
				}
				return
			}
			t := time.NewTimer(delay)
			defer t.Stop()
			select {
			case <-ch:
				return
			case <-t.C:
				select {
				case <-ch:
					return
				default:
					fn()
				}
			}
		}(delay, m.ch, m.fn, removal)
	}
}

func (b *Bot) cancelTimer(userID int64) {
	if timer, exists := b.timers[userID]; exists {
		close(timer.Warning72hTask)
		close(timer.Warning48hTask)
		close(timer.Warning24hTask)
		close(timer.RemovalTask)
		delete(b.timers, userID)
		b.logger.Infof("Cancelled timer for user %d", userID)
	}
}

// sendInactiveRemovalWarning — предупреждение за 72 ч (день 5), 48 ч (день 6) или 24 ч (день 7) до кика в 00:00 локального TZ юзера.
func (b *Bot) sendInactiveRemovalWarning(userID, chatID int64, username string, hoursBefore int, removalAt time.Time, loc *time.Location) {
	who := normalizeUserDisplayName(username)
	deadlineHuman := formatRemovalAtLocalHuman(removalAt, loc)
	messageText := fmt.Sprintf(
		"👀 Ты давно не занимался\n\n"+
			"%s, если не отметишь тренировку в мини-аппе, Леопард тебя съест 🍴 — %s.\n\n"+
			"Помни, в стае любое движение считается.",
		who,
		deadlineHuman,
	)

	typingChat := chatID
	if chatID != userID {
		typingChat = userID
	}
	if b.aiClient != nil {
		b.api.Send(tgbotapi.NewChatAction(typingChat, tgbotapi.ChatTyping))
		stage := "day_5"
		switch hoursBefore {
		case 48:
			stage = "day_6"
		case 24:
			stage = "day_7"
		}
		var ctxBuilder strings.Builder
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\nstage: %s\nДо удаления за неактивность осталось около %d ч.\nДедлайн: %s\n", username, stage, hoursBefore, deadlineHuman))
		if addendum, err := b.aiClient.AnswerUserQuestion(b.livePrompts().WarningTimerQuestion, ctxBuilder.String()); err == nil {
			addendum = ai.SanitizeTextForUser(addendum)
			if addendum != "" {
				messageText = messageText + "\n\n" + addendum
			}
		}
	}

	// §5: burn_warning_sent — предупреждение о сгорании стрика/удалении (day_5/6/7).
	// Идемпотентность по user+stage+дата кика, чтобы повтор планировщика не дублировал.
	burnStage := "day_5"
	switch hoursBefore {
	case 48:
		burnStage = "day_6"
	case 24:
		burnStage = "day_7"
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name:           database.EventBurnWarningSent,
		UserID:         userID,
		TelegramID:     userID,
		Payload:        map[string]any{"stage": burnStage},
		IdempotencyKey: fmt.Sprintf("burn_warning_sent:%d:%s:%s", userID, burnStage, removalAt.Format("2006-01-02")),
	})

	b.miniappPersonalPush(userID, messageText)

	if chatID == userID {
		b.api.Send(tgbotapi.NewMessage(userID, messageText))
		return
	}

	_, dmErr := b.api.Send(tgbotapi.NewMessage(userID, messageText))
	if dmErr != nil {
		b.logger.Warnf("send inactive removal warning DM user=%d: %v", userID, dmErr)
		return
	}
}
