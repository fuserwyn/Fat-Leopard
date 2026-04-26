package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/domain"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

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

	ch5 := make(chan bool)
	ch6 := make(chan bool)
	ch7 := make(chan bool)
	ch8 := make(chan bool)

	timerStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	timerInfo := &domain.TimerInfo{
		UserID:          userID,
		ChatID:          chatID,
		Username:        username,
		Day5WarningTask: ch5,
		Day6WarningTask: ch6,
		Day7ZeroXPTask:  ch7,
		RemovalTask:     ch8,
		TimerStartTime:  timerStartTime,
	}
	b.timers[userID] = timerInfo

	messageLog, err = b.db.GetMessageLog(userID, chatID)
	if err != nil {
		b.logger.Errorf("Failed to get message log for timer start: %v", err)
	} else {
		messageLog.TimerStartTime = &timerStartTime
		if err := b.db.SaveMessageLog(messageLog); err != nil {
			b.logger.Errorf("Failed to save timer start time: %v", err)
		}
	}

	timerStart, err := utils.ParseMoscowTime(timerStartTime)
	if err != nil {
		b.logger.Errorf("parse timer start: %v", err)
		return
	}
	b.scheduleLeopardMilestones(userID, chatID, username, timerStart, ch5, ch6, ch7, ch8)
	b.logger.Infof("Started Leopard inactive chain for user %d (%s) from %s", userID, username, timerStartTime)
}

func (b *Bot) restoreTimerWithDuration(userID, chatID int64, username string, remaining time.Duration, existingTimerStartTime string) {
	b.cancelTimer(userID)

	ch5 := make(chan bool)
	ch6 := make(chan bool)
	ch7 := make(chan bool)
	ch8 := make(chan bool)

	timerInfo := &domain.TimerInfo{
		UserID:          userID,
		ChatID:          chatID,
		Username:        username,
		Day5WarningTask: ch5,
		Day6WarningTask: ch6,
		Day7ZeroXPTask:  ch7,
		RemovalTask:     ch8,
		TimerStartTime:  existingTimerStartTime,
	}
	b.timers[userID] = timerInfo

	timerStart, err := utils.ParseMoscowTime(existingTimerStartTime)
	if err != nil {
		b.logger.Errorf("restore timer parse: %v", err)
		return
	}
	b.scheduleLeopardMilestones(userID, chatID, username, timerStart, ch5, ch6, ch7, ch8)
	b.logger.Infof("Restored Leopard inactive chain for user %d (%s), remaining until removal ~ %v", userID, username, remaining)
}

func (b *Bot) scheduleLeopardMilestones(userID, chatID int64, username string, timerStart time.Time, ch5, ch6, ch7, ch8 chan bool) {
	now := utils.GetMoscowTime()
	days := []struct {
		n      int
		ch     chan bool
		action func()
	}{
		{5, ch5, func() { b.sendInactiveWarning(userID, chatID, username, 5) }},
		{6, ch6, func() { b.sendInactiveWarning(userID, chatID, username, 6) }},
		{7, ch7, func() { b.sendInactiveDay7ZeroXP(userID, chatID, username) }},
		{8, ch8, func() { b.removeUser(userID, chatID, username) }},
	}
	for _, d := range days {
		n := d.n
		ch := d.ch
		fn := d.action
		target := timerStart.Add(time.Duration(n) * 24 * time.Hour)
		delay := target.Sub(now)
		removal := n == leopardmoney.InactiveRemovalDays
		go func(delay time.Duration, ch chan bool, fn func(), removal bool) {
			// Дни 5–7 в прошлом — пропускаем предупреждения. День 8 (кик): если дедлайн уже
			// прошёл, нельзя молча выйти — иначе removeUser никогда не вызовется из этой горутины.
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
		}(delay, ch, fn, removal)
	}
}

func (b *Bot) cancelTimer(userID int64) {
	if timer, exists := b.timers[userID]; exists {
		close(timer.Day5WarningTask)
		close(timer.Day6WarningTask)
		close(timer.Day7ZeroXPTask)
		close(timer.RemovalTask)
		delete(b.timers, userID)
		b.logger.Infof("Cancelled timer for user %d", userID)
	}
}

func (b *Bot) sendInactiveWarning(userID, chatID int64, username string, day int) {
	who := normalizeUserDisplayName(username)
	tag := "#training_done"
	messageText := fmt.Sprintf("⚠️ Предупреждение (день %d без отчёта)\n\n%s, прошло уже %d дней без отчёта с хэштегом.\n\n🎯 Отправь %s, чтобы остаться в игре.", day, who, day, tag)

	typingChat := chatID
	if chatID != userID {
		typingChat = userID
	}
	if b.aiClient != nil {
		b.api.Send(tgbotapi.NewChatAction(typingChat, tgbotapi.ChatTyping))
		var ctxBuilder strings.Builder
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\nДень без отчёта: %d\n", username, day))
		if addendum, err := b.aiClient.AnswerUserQuestion(b.config.Prompts.WarningTimerQuestion, ctxBuilder.String()); err == nil {
			addendum = ai.SanitizeTextForUser(addendum)
			if addendum != "" {
				messageText = messageText + "\n\n" + addendum
			}
		}
	}

	if chatID == userID {
		b.api.Send(tgbotapi.NewMessage(userID, messageText))
		return
	}

	_, dmErr := b.api.Send(tgbotapi.NewMessage(userID, messageText))
	if dmErr != nil {
		b.logger.Warnf("send inactive warning DM user=%d: %v", userID, dmErr)
		return
	}
}

func (b *Bot) sendInactiveDay7ZeroXP(userID, chatID int64, username string) {
	log, err := b.db.GetMessageLog(userID, chatID)
	if err == nil {
		log.Calories = 0
		_ = b.db.SaveMessageLog(log)
	}
	who := normalizeUserDisplayName(username)
	txt := fmt.Sprintf("🔴 День 7 без отчёта\n\n%s, твой XP обнулён. Последний шанс: сделай отчёт с #training_done до конца дня 8, иначе удаление из чата.", who)
	if b.aiClient != nil {
		if add, err := b.aiClient.AnswerUserQuestion(b.config.Prompts.CriticalTimerQuestion, "День 7: XP=0, последний шанс.\nПользователь: "+username); err == nil {
			add = ai.SanitizeTextForUser(add)
			if add != "" {
				txt = txt + "\n\n" + add
			}
		}
	}

	if _, dmErr := b.api.Send(tgbotapi.NewMessage(userID, txt)); dmErr != nil {
		b.logger.Warnf("send inactive day7 DM user=%d: %v", userID, dmErr)
	}
}
