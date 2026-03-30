package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleSickLeave(msg *tgbotapi.Message) {
	justification := extractSickLeaveJustification(msg)

	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}

	if messageLog.HasSickLeave {
		// Определяем тип чата для адаптации текста
		chatType, err := b.db.GetChatType(msg.Chat.ID)
		if err != nil {
			chatType = "training" // По умолчанию
		}
		var infoText string
		if chatType == "writing" {
			infoText = "✅ У тебя уже активен больничный. Отдыхай и возвращайся к писательству, когда восстановишься."
		} else {
			infoText = "✅ У тебя уже активен больничный. Отдыхай и возвращайся, когда восстановишься."
		}
		response := tgbotapi.NewMessage(msg.Chat.ID, infoText)
		response.ReplyToMessageID = msg.MessageID
		if _, sendErr := b.api.Send(response); sendErr != nil {
			b.logger.Errorf("Failed to send already sick leave info: %v", sendErr)
		}
		return
	}

	if messageLog.SickApprovalPending {
		if justification != "" {
			if b.evaluateSickLeaveJustification(justification, messageLog) {
				b.logger.Infof("Sick leave approved during pending state for user %d (%s)", msg.From.ID, messageLog.Username)
				b.activateSickLeave(msg, messageLog)
			} else {
				b.logger.Infof("Sick leave rejected during pending state for user %d (%s)", msg.From.ID, messageLog.Username)
				b.rejectSickLeave(msg, messageLog, msg.MessageID)
			}
		} else {
			b.sendSickApprovalPendingInfo(msg.Chat.ID, msg.MessageID, messageLog)
		}
		return
	}

	if justification == "" || b.evaluateSickLeaveJustification(justification, messageLog) {
		b.logger.Infof("Sick leave auto-approved for user %d (%s)", msg.From.ID, messageLog.Username)
		b.activateSickLeave(msg, messageLog)
		return
	}

	b.logger.Infof("Sick leave rejected for user %d (%s): justification not convincing", msg.From.ID, messageLog.Username)
	b.rejectSickLeave(msg, messageLog, msg.MessageID)
}

func (b *Bot) activateSickLeave(msg *tgbotapi.Message, messageLog *domain.MessageLog) {
	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
	}

	b.cancelSickApprovalWatcher(msg.From.ID)
	messageLog.SickApprovalPending = false
	messageLog.SickApprovalDeadline = nil
	messageLog.SickApprovalMessageID = nil
	messageLog.SickLeaveEndTime = nil
	messageLog.SickTime = nil

	// Записываем время начала больничного
	sickLeaveStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	messageLog.SickLeaveStartTime = &sickLeaveStartTime
	b.logger.Infof("Set sick leave start time: %s", sickLeaveStartTime)

	// Рассчитываем оставшееся время до удаления
	fullTimerDuration := 7 * 24 * time.Hour // 7 дней
	var remainingTime time.Duration

	if messageLog.TimerStartTime != nil {
		timerStart, err := utils.ParseMoscowTime(*messageLog.TimerStartTime)
		if err == nil {
			sickStart, err := utils.ParseMoscowTime(sickLeaveStartTime)
			if err == nil {
				// Время с тренировки до начала болезни
				timeFromTrainingToSick := sickStart.Sub(timerStart)
				// Оставшееся время = полное время - время с тренировки до болезни
				remainingTime = fullTimerDuration - timeFromTrainingToSick
				if remainingTime <= 0 {
					remainingTime = 0
				}
				b.logger.Infof("Timer start: %v, sick start: %v, time from training to sick: %v, remaining time: %v", timerStart, sickStart, timeFromTrainingToSick, remainingTime)
			} else {
				remainingTime = fullTimerDuration
				b.logger.Errorf("Failed to parse sick start time: %v", err)
			}
		} else {
			remainingTime = fullTimerDuration
			b.logger.Errorf("Failed to parse timer start time: %v", err)
		}
	} else {
		remainingTime = fullTimerDuration
		b.logger.Warnf("Timer start time is nil, using full duration")
	}

	// Логируем рассчитанное время
	b.logger.Infof("Calculated remaining time at sick leave start: %v", remainingTime)

	// Обновляем флаги больничного
	messageLog.HasSickLeave = true
	messageLog.HasHealthy = false

	// Добавляем подробное логирование перед сохранением
	b.logger.Infof("Saving message log with fields:")
	b.logger.Infof("  UserID: %d", messageLog.UserID)
	b.logger.Infof("  ChatID: %d", messageLog.ChatID)
	b.logger.Infof("  HasSickLeave: %t", messageLog.HasSickLeave)
	b.logger.Infof("  HasHealthy: %t", messageLog.HasHealthy)
	b.logger.Infof("  SickLeaveStartTime: %s", func() string {
		if messageLog.SickLeaveStartTime != nil {
			return *messageLog.SickLeaveStartTime
		}
		return "nil"
	}())
	b.logger.Infof("  RestTimeTillDel: %s", func() string {
		if messageLog.RestTimeTillDel != nil {
			return *messageLog.RestTimeTillDel
		}
		return "nil"
	}())

	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to update message log: %v", err)
	} else {
		b.logger.Infof("Successfully saved sick leave start time")
	}

	// Отменяем существующие таймеры
	b.cancelTimer(msg.From.ID)

	// Форматируем оставшееся время
	remainingTimeFormatted := b.formatDurationToDays(remainingTime)

	// Отправляем подтверждение с информацией о времени после разморозки
	var messageText string
	if chatType == "writing" {
		messageText = fmt.Sprintf("🏥 Больничный принят! 🤒\n\n⏸️ Таймер приостановлен на время болезни\n\n❄️ После выздоровления останется: %s до удаления\n\n💪 Выздоравливай и возвращайся к писательству!\n\n📝 Когда поправишься, отправь #healthy для возобновления таймера", remainingTimeFormatted)
	} else {
		messageText = fmt.Sprintf("🏥 Больничный принят! 🤒\n\n⏸️ Таймер приостановлен на время болезни\n\n❄️ После выздоровления останется: %s до удаления\n\n💪 Выздоравливай и возвращайся к тренировкам!\n\n📝 Когда поправишься, отправь #healthy для возобновления таймера", remainingTimeFormatted)
	}

	// ИИ‑приписка: пожелание выздоровления (5 предложений)
	if b.aiClient != nil {
		action := tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping)
		b.api.Send(action)
		stopTyping := make(chan struct{})
		defer close(stopTyping)
		go func() {
			ticker := time.NewTicker(4 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					b.api.Send(action)
				case <-stopTyping:
					return
				}
			}
		}()

		totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
		var question string
		if chatType == "writing" {
			question = "Сделай ровно 5 предложений‑приписку после сообщения о взятии больничного: строго, дружелюбно, пожелай скорейшего восстановления и мягко мотивируй вернуться к писательству. НЕ упоминай тренировки, спорт или физическую активность. Учитывай текущие слова и кубки, упомяни, что я 'ем' только ленивых (без угроз активным). Не повторяй цифры из основного текста. Без Markdown."
		} else {
			question = "Сделай ровно 5 предложений‑приписку после сообщения о взятии больничного: строго, дружелюбно, пожелай скорейшего восстановления и мягко мотивируй вернуться к режиму. Учитывай текущие калории и кубки, упомяни, что я 'ем' только ленивых (без угроз активным). Не повторяй цифры из основного текста. Без Markdown."
		}
		var ctxBuilder strings.Builder
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", messageLog.Username))
		// Добавляем пол пользователя в контекст
		userGender := strings.TrimSpace(strings.ToLower(messageLog.Gender))
		if userGender != "" {
			var genderText string
			switch userGender {
			case "f":
				genderText = "женский"
			case "m":
				genderText = "мужской"
			}
			if genderText != "" {
				ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
			}
		}
		ctxBuilder.WriteString("Событие: взят больничный (таймер приостановлен).\n")
		ctxBuilder.WriteString(fmt.Sprintf("После выздоровления останется: %s\n", remainingTimeFormatted))
		if chatType == "writing" {
			ctxBuilder.WriteString(fmt.Sprintf("Всего %s: %d\n", getWordForm(messageLog.Calories), messageLog.Calories))
		} else {
			ctxBuilder.WriteString(fmt.Sprintf("Всего калорий: %d\n", messageLog.Calories))
		}
		ctxBuilder.WriteString(fmt.Sprintf("Всего кубков: %d\n", totalCups))
		if addendum, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
			addendum = strings.TrimSpace(strings.ReplaceAll(addendum, "**", ""))
			if addendum != "" {
				messageText = messageText + "\n\n" + addendum
			}
		} else {
			b.logger.Warnf("AI addendum generation (sick_leave) failed: %v", err)
		}
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)

	b.logger.Infof("Sending sick leave message to chat %d", msg.Chat.ID)
	if _, err := b.api.Send(reply); err != nil {
		b.logger.Errorf("Failed to send sick leave message: %v", err)
	} else {
		b.logger.Infof("Successfully sent sick leave message to chat %d", msg.Chat.ID)
	}
}

func extractSickLeaveJustification(msg *tgbotapi.Message) string {
	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}

	lower := strings.ToLower(text)
	lower = strings.ReplaceAll(lower, "#sick_leave", "")
	lower = strings.ReplaceAll(lower, "#sickleave", "")
	lower = strings.ReplaceAll(lower, "#healthy", "")
	lower = strings.ReplaceAll(lower, "#здоров", "")
	return strings.TrimSpace(lower)
}

func (b *Bot) cancelSickApprovalWatcher(userID int64) {
	b.sickApprovalMutex.Lock()
	defer b.sickApprovalMutex.Unlock()

	if ch, ok := b.sickApprovalWatchers[userID]; ok {
		close(ch)
		delete(b.sickApprovalWatchers, userID)
	}
}

func (b *Bot) startSickApprovalWatcher(userID, chatID int64, deadline time.Time) {
	wait := time.Until(deadline)
	if wait < 0 {
		wait = 0
	}

	cancelChan := make(chan struct{})

	b.sickApprovalMutex.Lock()
	if existing, ok := b.sickApprovalWatchers[userID]; ok {
		close(existing)
	}
	b.sickApprovalWatchers[userID] = cancelChan
	b.sickApprovalMutex.Unlock()

	go func() {
		timer := time.NewTimer(wait)
		defer timer.Stop()
		select {
		case <-timer.C:
			b.forceCancelSickLeave(userID, chatID)
		case <-cancelChan:
			return
		}
	}()
}

func (b *Bot) sickApprovalTimeLeft(messageLog *domain.MessageLog) string {
	if messageLog.SickApprovalDeadline != nil {
		remaining := time.Until(*messageLog.SickApprovalDeadline)
		if remaining < 0 {
			remaining = 0
		}
		return b.formatDurationToDays(remaining)
	}
	return ""
}

func (b *Bot) sendSickApprovalWarning(chatID int64, replyTo int, messageLog *domain.MessageLog) {
	remainingText := b.sickApprovalTimeLeft(messageLog)
	warningText := "⚠️ Я всё ещё не вижу доказательств болезни. "
	if remainingText != "" {
		warningText += fmt.Sprintf("У тебя осталось %s, чтобы меня убедить. ", remainingText)
	}
	warningText += "Если проигнорируешь — отменю больничный и запущу таймер."

	msg := tgbotapi.NewMessage(chatID, warningText)
	if replyTo != 0 {
		msg.ReplyToMessageID = replyTo
	}
	sent, err := b.api.Send(msg)
	if err != nil {
		b.logger.Errorf("Failed to send sick approval warning: %v", err)
		return
	}

	messageID := int64(sent.MessageID)
	messageLog.SickApprovalMessageID = &messageID
	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to update sick approval message id: %v", err)
	}
}

func (b *Bot) sendSickApprovalPendingInfo(chatID int64, replyTo int, messageLog *domain.MessageLog) {
	remainingText := b.sickApprovalTimeLeft(messageLog)
	replyText := "⚠️ Заявка на больничный уже рассматривается."
	if remainingText != "" {
		replyText += fmt.Sprintf(" Осталось %s, чтобы меня убедить.", remainingText)
	} else {
		replyText += " Ответь быстрее, иначе отменю запрос."
	}

	msg := tgbotapi.NewMessage(chatID, replyText)
	if replyTo != 0 {
		msg.ReplyToMessageID = replyTo
	}
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Errorf("Failed to send pending sick leave message: %v", err)
	}
}

func (b *Bot) sendSickLeaveRejection(chatID int64, replyTo int) {
	text := "❌ Больничный не принят.\n\nНе вижу признаков болезни. Если это работа или другие дела — тренируйся честно. " +
		"Когда действительно заболеешь, дай знать, и я приостановлю таймер."
	msg := tgbotapi.NewMessage(chatID, text)
	if replyTo != 0 {
		msg.ReplyToMessageID = replyTo
	}
	if _, err := b.api.Send(msg); err != nil {
		b.logger.Errorf("Failed to send sick leave rejection message: %v", err)
	}
}

func (b *Bot) rejectSickLeave(msg *tgbotapi.Message, messageLog *domain.MessageLog, replyTo int) {
	b.cancelSickApprovalWatcher(msg.From.ID)
	messageLog.SickApprovalPending = false
	messageLog.SickApprovalDeadline = nil
	messageLog.SickApprovalMessageID = nil
	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to clear sick approval flags after rejection: %v", err)
	}
	b.sendSickLeaveRejection(msg.Chat.ID, replyTo)
}

func (b *Bot) restoreSickApprovalWatchers() {
	pending, err := b.db.GetPendingSickApprovals()
	if err != nil {
		b.logger.Errorf("Failed to load pending sick approvals: %v", err)
		return
	}

	for _, p := range pending {
		if p.SickApprovalDeadline != nil {
			b.startSickApprovalWatcher(p.UserID, p.ChatID, *p.SickApprovalDeadline)
		} else {
			b.forceCancelSickLeave(p.UserID, p.ChatID)
		}
	}
}

func (b *Bot) tryHandleSickApprovalReply(msg *tgbotapi.Message, text string) {
	if msg.ReplyToMessage == nil || msg.ReplyToMessage.From == nil || !msg.ReplyToMessage.From.IsBot || msg.ReplyToMessage.From.ID != b.api.Self.ID {
		return
	}

	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log for sick approval reply: %v", err)
		return
	}

	if !messageLog.SickApprovalPending || messageLog.SickApprovalMessageID == nil {
		return
	}

	if int64(msg.ReplyToMessage.MessageID) != *messageLog.SickApprovalMessageID {
		return
	}

	if b.evaluateSickLeaveJustification(text, messageLog) {
		b.logger.Infof("Sick leave approved after reply for user %d", msg.From.ID)
		b.activateSickLeave(msg, messageLog)
		return
	}
	b.logger.Infof("Sick leave rejected after reply for user %d", msg.From.ID)
	b.rejectSickLeave(msg, messageLog, msg.MessageID)
}

func (b *Bot) forceCancelSickLeave(userID, chatID int64) {
	b.logger.Infof("Force cancelling sick leave request for user %d", userID)
	b.cancelSickApprovalWatcher(userID)

	messageLog, err := b.db.GetMessageLog(userID, chatID)
	if err != nil {
		b.logger.Errorf("Failed to get message log for force cancel: %v", err)
		return
	}

	if !messageLog.SickApprovalPending {
		return
	}

	replyMessageID := messageLog.SickApprovalMessageID
	messageLog.SickApprovalPending = false
	messageLog.SickApprovalDeadline = nil
	messageLog.SickApprovalMessageID = nil

	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to clear sick approval flags on force cancel: %v", err)
	}

	text := "⛔️ Я не получил подтверждений болезни и отменил больничный. Таймер продолжает тикать. " +
		"Если действительно болен — предоставь доказательства и попроси снова."
	alert := tgbotapi.NewMessage(chatID, text)
	if replyMessageID != nil {
		alert.ReplyToMessageID = int(*replyMessageID)
	}
	if _, err := b.api.Send(alert); err != nil {
		b.logger.Errorf("Failed to send force cancel message: %v", err)
	}
}

func (b *Bot) handleHealthy(msg *tgbotapi.Message) {
	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
	}

	// Получаем данные о времени таймера и больничного
	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}

	b.cancelSickApprovalWatcher(msg.From.ID)
	messageLog.SickApprovalPending = false
	messageLog.SickApprovalDeadline = nil
	messageLog.SickApprovalMessageID = nil

	// Записываем время окончания больничного
	sickLeaveEndTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	messageLog.SickLeaveEndTime = &sickLeaveEndTime
	b.logger.Infof("Set sick leave end time: %s", sickLeaveEndTime)

	// Рассчитываем время болезни
	if messageLog.SickLeaveStartTime != nil {
		b.logger.Infof("Sick leave start time: %s", *messageLog.SickLeaveStartTime)
		sickStart, err := utils.ParseMoscowTime(*messageLog.SickLeaveStartTime)
		if err == nil {
			sickEnd, err := utils.ParseMoscowTime(sickLeaveEndTime)
			if err == nil {
				sickDuration := sickEnd.Sub(sickStart)
				sickTimeStr := sickDuration.String()
				messageLog.SickTime = &sickTimeStr
				b.logger.Infof("Calculated sick time: %v (%s)", sickDuration, sickTimeStr)
			} else {
				b.logger.Errorf("Failed to parse sick end time: %v", err)
			}
		} else {
			b.logger.Errorf("Failed to parse sick start time: %v", err)
		}
	} else {
		b.logger.Warnf("Sick leave start time is nil")
	}

	// Обновляем флаг выздоровления
	messageLog.HasHealthy = true
	messageLog.HasSickLeave = false

	// Добавляем подробное логирование перед сохранением
	b.logger.Infof("Saving message log with fields:")
	b.logger.Infof("  UserID: %d", messageLog.UserID)
	b.logger.Infof("  ChatID: %d", messageLog.ChatID)
	b.logger.Infof("  HasSickLeave: %t", messageLog.HasSickLeave)
	b.logger.Infof("  HasHealthy: %t", messageLog.HasHealthy)
	b.logger.Infof("  SickLeaveStartTime: %s", func() string {
		if messageLog.SickLeaveStartTime != nil {
			return *messageLog.SickLeaveStartTime
		}
		return "nil"
	}())
	b.logger.Infof("  SickLeaveEndTime: %s", func() string {
		if messageLog.SickLeaveEndTime != nil {
			return *messageLog.SickLeaveEndTime
		}
		return "nil"
	}())
	b.logger.Infof("  SickTime: %s", func() string {
		if messageLog.SickTime != nil {
			return *messageLog.SickTime
		}
		return "nil"
	}())

	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to update message log: %v", err)
	} else {
		b.logger.Infof("Successfully saved message log with sick leave data")
	}

	// Рассчитываем оставшееся время используя исправленную функцию
	remainingTime := b.calculateRemainingTime(messageLog)
	b.logger.Infof("Calculated remaining time after recovery: %v", remainingTime)

	// Проверяем, не истекло ли время
	if remainingTime <= 0 {
		// Время истекло - удаляем пользователя
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

		// Отправляем сообщение об истечении времени
		var replyText string
		if chatType == "writing" {
			replyText = "⏰ Время истекло! 🚫\n\n💪 Выздоровление принято, но время таймера уже истекло.\n\n🦁 Ням-ням, вкусненько! Я питаюсь ленивыми леопардами и становлюсь жирнее!\n\n💪 Ты ведь не хочешь стать как я?\n\nТогда пиши и отправляй отчёты!"
		} else {
			replyText = "⏰ Время истекло! 🚫\n\n💪 Выздоровление принято, но время таймера уже истекло.\n\n🦁 Ням-ням, вкусненько! Я питаюсь ленивыми леопардами и становлюсь жирнее!\n\n💪 Ты ведь не хочешь стать как я?\n\nТогда тренируйся и отправляй отчёты!"
		}
		reply := tgbotapi.NewMessage(msg.Chat.ID, replyText)
		b.api.Send(reply)

		// Удаляем пользователя
		b.removeUser(msg.From.ID, msg.Chat.ID, username)
		return
	}

	// Запускаем таймер с оставшимся временем
	b.startTimerWithDuration(msg.From.ID, msg.Chat.ID, messageLog.Username, remainingTime)

	// Форматируем оставшееся время
	remainingTimeFormatted := b.formatDurationToDays(remainingTime)

	// Отправляем подтверждение с информацией о времени до удаления
	messageText := fmt.Sprintf("💪 Выздоровление принято! 🎉\n\n⏰ Таймер возобновлён с места остановки!\n\n⏳ До удаления осталось: %s", remainingTimeFormatted)

	// ИИ‑приписка: поздравление с выздоровлением (5 предложений)
	if b.aiClient != nil {
		action := tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping)
		b.api.Send(action)
		stopTyping := make(chan struct{})
		defer close(stopTyping)
		go func() {
			ticker := time.NewTicker(4 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					b.api.Send(action)
				case <-stopTyping:
					return
				}
			}
		}()

		totalCups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
		var question string
		if chatType == "writing" {
			question = "Сделай ровно 5 предложений‑приписку после сообщения о выздоровлении: поздравь, напомни о дисциплине писательства, похвали за честность и предупреди о контроле таймера. НЕ упоминай тренировки, спорт или физическую активность. Учитывай текущие слова и кубки. Не повторяй цифры из основного текста. Без Markdown."
		} else {
			question = "Сделай ровно 5 предложений‑приписку после сообщения о выздоровлении: поздравь, напомни о дисциплине, похвали за честность и предупреди о контроле таймера. Учитывай текущие калории и кубки. Не повторяй цифры из основного текста. Без Markdown."
		}
		var ctxBuilder strings.Builder
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", messageLog.Username))
		userGender := strings.TrimSpace(strings.ToLower(messageLog.Gender))
		if userGender != "" {
			var genderText string
			switch userGender {
			case "f":
				genderText = "женский"
			case "m":
				genderText = "мужской"
			}
			if genderText != "" {
				ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
			}
		}
		ctxBuilder.WriteString(fmt.Sprintf("После выздоровления осталось: %s\n", remainingTimeFormatted))
		if chatType == "writing" {
			ctxBuilder.WriteString(fmt.Sprintf("Всего %s: %d\n", getWordForm(messageLog.Calories), messageLog.Calories))
		} else {
			ctxBuilder.WriteString(fmt.Sprintf("Всего калорий: %d\n", messageLog.Calories))
		}
		ctxBuilder.WriteString(fmt.Sprintf("Всего кубков: %d\n", totalCups))
		if addendum, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
			addendum = strings.TrimSpace(strings.ReplaceAll(addendum, "**", ""))
			if addendum != "" {
				messageText = messageText + "\n\n" + addendum
			}
		} else {
			b.logger.Warnf("AI addendum generation (healthy) failed: %v", err)
		}
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)

	b.logger.Infof("Sending healthy confirmation message to chat %d", msg.Chat.ID)
	if _, err := b.api.Send(reply); err != nil {
		b.logger.Errorf("Failed to send healthy confirmation message: %v", err)
	} else {
		b.logger.Infof("Successfully sent healthy confirmation message to chat %d", msg.Chat.ID)
	}
}
