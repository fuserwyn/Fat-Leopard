package bot

import (
	"fmt"
	"strings"

	"leo-bot/internal/domain"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) generateShortLeopardChatAck(username, text string, streak, totalXP, ach int) string {
	fallback := "🦁 Красавчег, сегодня не съем тебя."
	if b.aiClient == nil {
		return fallback
	}

	question := "Сгенерируй ОДНО короткое предложение в стиле Лео: 5-7 слов, по-доброму хищно, с посылом 'сегодня не съем тебя'. Без списков, без Markdown, без эмодзи, без пояснений."
	var ctxBuilder strings.Builder
	ctxBuilder.WriteString("Контекст отчёта тренировки.\n")
	ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", username))
	ctxBuilder.WriteString(fmt.Sprintf("Серия: %d дней\n", streak))
	ctxBuilder.WriteString(fmt.Sprintf("XP: %d\n", totalXP))
	ctxBuilder.WriteString(fmt.Sprintf("Ачивки: %d\n", ach))
	ctxBuilder.WriteString(fmt.Sprintf("Текст отчёта: %s\n", text))

	ack, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String())
	if err != nil {
		b.logger.Warnf("generate short leopard ack: %v", err)
		return fallback
	}
	ack = strings.TrimSpace(strings.ReplaceAll(ack, "**", ""))
	if ack == "" {
		return fallback
	}
	words := len(strings.Fields(ack))
	if words < 3 || words > 12 {
		// Страхуем длину, если модель нарушила ограничение.
		return fallback
	}
	return "🦁 " + ack
}

// handleLeopardMoneyTrainingDone — отчёт #training_done по модели Leopard Money (XP, ачивки, таймер 8 дней).
func (b *Bot) handleLeopardMoneyTrainingDone(msg *tgbotapi.Message) {
	username := ""
	if msg.From.UserName != "" {
		username = "@" + msg.From.UserName
	} else if msg.From.FirstName != "" {
		username = msg.From.FirstName
		if msg.From.LastName != "" {
			username += " " + msg.From.LastName
		}
	} else {
		username = fmt.Sprintf("User%d", msg.From.ID)
	}

	b.startTimer(msg.From.ID, msg.Chat.ID, username)

	trainingLog := &domain.TrainingLog{
		UserID:     msg.From.ID,
		ChatID:     msg.Chat.ID,
		Username:   username,
		LastReport: utils.FormatMoscowTime(utils.GetMoscowTime()),
	}
	if err := b.db.SaveTrainingLog(trainingLog); err != nil {
		b.logger.Errorf("Failed to save training log: %v", err)
	}

	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}

	if messageLog.SickApprovalPending {
		b.cancelSickApprovalWatcher(msg.From.ID)
		messageLog.SickApprovalPending = false
		messageLog.SickApprovalDeadline = nil
		messageLog.SickApprovalMessageID = nil
		if err := b.db.SaveMessageLog(messageLog); err != nil {
			b.logger.Errorf("Failed to clear sick approval flags after training: %v", err)
		}
	}

	userGender := strings.TrimSpace(strings.ToLower(messageLog.Gender))
	if userGender == "" {
		userGender = b.detectGenderFromName(msg.From.FirstName)
		if userGender != "" {
			if err := b.updateUserGender(msg.From.ID, msg.Chat.ID, userGender); err != nil {
				b.logger.Warnf("Failed to update user gender: %v", err)
			}
		}
	}

	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}

	localNow := b.getUserLocalNow(messageLog.TimezoneOffsetFromMoscow)
	today := localNow.Format("2006-01-02")

	sessionsToday, err := b.db.CountTrainingSessionsInDateRange(msg.From.ID, msg.Chat.ID, today, today)
	if err != nil {
		b.logger.Errorf("sessions today: %v", err)
		sessionsToday = 0
	}
	firstReportToday := sessionsToday == 0

	xpAdd := 0
	if firstReportToday {
		xpAdd = leopardmoney.XPPerActiveDay
	}

	newStreak := 1
	if messageLog.LastTrainingDate != nil && *messageLog.LastTrainingDate == today {
		newStreak = messageLog.StreakDays
	} else if messageLog.LastTrainingDate != nil {
		yesterdayStr := localNow.AddDate(0, 0, -1).Format("2006-01-02")
		if *messageLog.LastTrainingDate == yesterdayStr {
			newStreak = messageLog.StreakDays + 1
		} else {
			newStreak = 1
		}
	}

	newCalStreak := 1
	if messageLog.LastTrainingDate != nil && *messageLog.LastTrainingDate == today {
		newCalStreak = messageLog.CalorieStreakDays
	} else if messageLog.LastTrainingDate != nil {
		yesterdayStr := localNow.AddDate(0, 0, -1).Format("2006-01-02")
		if *messageLog.LastTrainingDate == yesterdayStr {
			newCalStreak = messageLog.CalorieStreakDays + 1
		}
	}

	if xpAdd > 0 {
		if err := b.db.AddCalories(msg.From.ID, msg.Chat.ID, xpAdd); err != nil {
			b.logger.Errorf("add XP: %v", err)
		}
	}

	if err := b.db.UpdateStreak(msg.From.ID, msg.Chat.ID, newStreak, today); err != nil {
		b.logger.Errorf("update streak: %v", err)
	}
	if err := b.db.UpdateCalorieStreakWithDate(msg.From.ID, msg.Chat.ID, newCalStreak, today); err != nil {
		b.logger.Errorf("update cal streak: %v", err)
	}

	msgLog2, _ := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if msgLog2 != nil && newStreak > 0 && newStreak%7 == 0 && newStreak <= 28 {
		want := newStreak / 7
		if msgLog2.AchievementCount < want && want <= leopardmoney.MaxAchievements {
			msgLog2.AchievementCount = want
			msgLog2.LastAchievementStreakLevel = newStreak
			_ = b.db.SaveMessageLog(msgLog2)
		}
	}

	totalXP, _ := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	ach := 0
	if ml, e := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID); e == nil {
		ach = ml.AchievementCount
	}

	tag := "#training_done"

	session := &domain.TrainingSession{
		UserID:         msg.From.ID,
		ChatID:         msg.Chat.ID,
		SessionDate:    today,
		MessageText:    text,
		TrainingsCount: 1,
		CupsAdded:      0,
		IsBonus:        false,
	}
	if err := b.db.SaveTrainingSession(session); err != nil {
		b.logger.Errorf("SaveTrainingSession: %v", err)
	}

	wasOnSickLeave := messageLog.HasSickLeave && !messageLog.HasHealthy

	chatAckText := b.generateShortLeopardChatAck(username, text, newStreak, totalXP, ach)
	chatAck := tgbotapi.NewMessage(msg.Chat.ID, chatAckText)
	if _, err := b.api.Send(chatAck); err != nil {
		b.logger.Errorf("send training chat ack: %v", err)
	}

	messageText := fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Серия: %d дн.\n⚡ +%d XP (всего XP: %d)\n🏆 Ачивок: %d/%d\n\n⏰ Таймер неактивности: %d дней (день 8 — удаление)\n\n🎯 Отчёт с %s", newStreak, xpAdd, totalXP, ach, leopardmoney.MaxAchievements, leopardmoney.InactiveRemovalDays, tag)

	privateReply := tgbotapi.NewMessage(msg.From.ID, messageText)
	if _, err := b.api.Send(privateReply); err != nil {
		b.logger.Warnf("send training private summary: %v", err)
	}

	if b.aiClient != nil && xpAdd > 0 {
		question := b.getUnifiedTrainingPrompt(newStreak, totalXP, ach, wasOnSickLeave)
		var ctxBuilder strings.Builder
		ctxBuilder.WriteString("КРИТИЧЕСКИ ВАЖНО: Отвечай ТОЛЬКО на этот отчёт.\n\n")
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", username))
		if userGender != "" {
			gt := "мужской"
			if userGender == "f" {
				gt = "женский"
			}
			ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", gt))
		}
		ctxBuilder.WriteString(fmt.Sprintf("Серия: %d дней\n", newStreak))
		ctxBuilder.WriteString(fmt.Sprintf("XP: %d, ачивки: %d\n", totalXP, ach))
		ctxBuilder.WriteString(fmt.Sprintf("Текст отчёта:\n%s\n", text))
		if add, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
			add = strings.TrimSpace(strings.ReplaceAll(add, "**", ""))
			if add != "" {
				if _, err := b.api.Send(tgbotapi.NewMessage(msg.From.ID, add)); err != nil {
					b.logger.Warnf("send training private ai note: %v", err)
				}
			}
		}
	}
}
