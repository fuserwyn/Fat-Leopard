package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/domain"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func (b *Bot) handleSickLeave(msg *tgbotapi.Message) {
	justification := extractSickLeaveJustification(msg)

	// Стейт больничного (HasSickLeave / SickLeaveStartTime / approval-флаги) ведём на pack-row,
	// чтобы recoverTimersFromDatabase при рестарте видел активный больничный по той же строке,
	// на которой стоит таймер кика. Иначе pack-row не знает о больничном и кикает спящего юзера.
	messageLog, err := b.db.GetMessageLog(msg.From.ID, b.kickChatIDForMessage(msg))
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}

	if messageLog.HasSickLeave {
		_, remainingText, deadlineLocal := b.formatInactivityRemovalSummary(messageLog)
		infoText := "✅ У тебя уже активен больничный. Отдыхай и возвращайся, когда восстановишься.\n\n⏸️ Таймер остановлен.\n\n" +
			sickLeaveRemovalNotice(remainingText, deadlineLocal, true)
		b.notifyUserText(msg, infoText, "", msg.MessageID)
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

// activateSickLeaveForUser выставляет активный больничный по единой логике самостоятельной
// активации: сбрасывает approval/healthy-флаги, пишет sick_leave_start_time (МСК), поднимает
// HasSickLeave и ставит kick-таймер на паузу. messageLog мутируется на месте. Используется и
// юзерским путём (activateSickLeave), и админским (setSickLeaveForUser) — чтобы состояние
// больничного было идентичным, кто бы его ни выставил. Трекинг — только при успешном сохранении;
// таймер снимаем всегда (как в исходной логике).
func (b *Bot) activateSickLeaveForUser(userID int64, messageLog *domain.MessageLog) error {
	b.cancelSickApprovalWatcher(userID)
	messageLog.SickApprovalPending = false
	messageLog.SickApprovalDeadline = nil
	messageLog.SickApprovalMessageID = nil
	messageLog.SickLeaveEndTime = nil
	messageLog.SickTime = nil

	sickLeaveStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	messageLog.SickLeaveStartTime = &sickLeaveStartTime
	messageLog.HasSickLeave = true
	messageLog.HasHealthy = false

	saveErr := b.db.SaveMessageLog(messageLog)
	if saveErr == nil {
		b.trackSickLeaveStarted(userID) // §5: больничный активирован
	}
	b.cancelTimer(userID)
	return saveErr
}

func (b *Bot) activateSickLeave(msg *tgbotapi.Message, messageLog *domain.MessageLog) {
	if err := b.activateSickLeaveForUser(msg.From.ID, messageLog); err != nil {
		b.logger.Errorf("Failed to update message log: %v", err)
	} else {
		b.logger.Infof("Successfully saved sick leave start time")
	}

	remainingTime, remainingTimeFormatted, deadlineLocal := b.formatInactivityRemovalSummary(messageLog)
	b.logger.Infof("Calculated remaining time at sick leave start (profile logic): %v", remainingTime)

	messageText := fmt.Sprintf(
		"🏥 Больничный принят! 🤒\n\n⏸️ Таймер приостановлен на время болезни.\n\n%s\n\n💪 Выздоравливай и возвращайся к тренировкам!\n\n📝 Когда поправишься — «Выйти с больничного» в профиле или #healthy в чате.",
		sickLeaveRemovalNotice(remainingTimeFormatted, deadlineLocal, false),
	)

	// ИИ‑приписка: пожелание выздоровления (5 предложений)
	if b.aiClient != nil {
		action := tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping)
		b.sendChatActionIfTG(msg, action)
		stopTyping := make(chan struct{})
		defer close(stopTyping)
		go func() {
			ticker := time.NewTicker(4 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					b.sendChatActionIfTG(msg, action)
				case <-stopTyping:
					return
				}
			}
		}()

		totalCups, _ := b.db.GetUserCups(msg.From.ID, messageLog.ChatID)
		question := "Сделай ровно 5 предложений‑приписку после сообщения о взятии больничного: строго, дружелюбно, пожелай скорейшего восстановления и мягко мотивируй вернуться к режиму. Учитывай текущие кубки, упомяни, что я 'ем' только ленивых (без угроз активным). Не повторяй цифры из основного текста. Без Markdown."
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
		ctxBuilder.WriteString(fmt.Sprintf("Всего кубков: %d\n", totalCups))
		if addendum, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
			addendum = ai.SanitizeTextForUser(addendum)
			if addendum != "" {
				messageText = messageText + "\n\n" + addendum
			}
		} else {
			b.logger.Warnf("AI addendum generation (sick_leave) failed: %v", err)
		}
	}

	b.logger.Infof("Sending sick leave message to chat %d (mini-app origin: %v)", msg.Chat.ID, b.isMiniappOriginActive(msg.From.ID))
	b.notifyUserText(msg, messageText, "", 0)
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

	// В мини-апп-флоу пушим только в очередь приложения (TG-message не создаётся → SickApprovalMessageID не пишем).
	// reply-логика на TG-MessageID в мини-аппе не работает (синтетические сообщения без reply_to).
	sentMsgID := b.notifyUserTextByID(messageLog.UserID, chatID, warningText, "", replyTo)
	if sentMsgID == 0 {
		return
	}

	messageID := int64(sentMsgID)
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

	b.notifyUserTextByID(messageLog.UserID, chatID, replyText, "", replyTo)
}

func (b *Bot) sendSickLeaveRejection(msg *tgbotapi.Message, replyTo int) {
	text := "❌ Больничный не принят.\n\nНе вижу признаков болезни. Если это работа или другие дела — тренируйся честно. " +
		"Когда действительно заболеешь, дай знать, и я приостановлю таймер."
	b.notifyUserText(msg, text, "", replyTo)
}

func (b *Bot) rejectSickLeave(msg *tgbotapi.Message, messageLog *domain.MessageLog, replyTo int) {
	b.cancelSickApprovalWatcher(msg.From.ID)
	messageLog.SickApprovalPending = false
	messageLog.SickApprovalDeadline = nil
	messageLog.SickApprovalMessageID = nil
	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to clear sick approval flags after rejection: %v", err)
	}
	b.sendSickLeaveRejection(msg, replyTo)
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

	// Approval-стейт лежит на pack-row (см. handleSickLeave) — грузим оттуда же.
	messageLog, err := b.db.GetMessageLog(msg.From.ID, b.kickChatIDForMessage(msg))
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
	replyTo := 0
	if replyMessageID != nil {
		replyTo = int(*replyMessageID)
	}
	b.notifyUserTextByID(userID, chatID, text, "", replyTo)
}

func (b *Bot) handleHealthy(msg *tgbotapi.Message) {
	// Стейт больничного и таймер живут на pack-row — выздоровление сбрасываем там же.
	messageLog, err := b.db.GetMessageLog(msg.From.ID, b.kickChatIDForMessage(msg))
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}
	if !messageLog.HasSickLeave {
		infoText := "ℹ️ Ты уже здоров(а) — активного больничного сейчас нет."
		b.notifyUserText(msg, infoText, "", msg.MessageID)
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
		b.trackSickLeaveEnded(msg.From.ID, "manual") // §5: выздоровление по #healthy
	}

	// Рассчитываем оставшееся время (та же логика, что в профиле мини-аппа).
	remainingTime, remainingTimeFormatted, deadlineLocal := b.formatInactivityRemovalSummary(messageLog)
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

		replyText := "⏰ Время истекло! 🚫\n\n💪 Выздоровление принято, но время таймера уже истекло.\n\n🦁 Ням-ням, вкусненько! Я питаюсь ленивыми леопардами и становлюсь жирнее!\n\n💪 Ты ведь не хочешь стать как я?\n\nТогда тренируйся и отправляй отчёты!"
		b.notifyUserText(msg, replyText, "", 0)

		// Удаляем пользователя — кик идёт по pack-row, чтобы корректно сработал ExpirePaywallAccessForUser.
		b.removeUser(msg.From.ID, b.kickChatIDForMessage(msg), username)
		return
	}

	// Восстанавливаем цепочку таймеров, не перетирая timer_start последней тренировкой (иначе «окно» перезапускается с #healthy).
	if messageLog.TimerStartTime == nil || strings.TrimSpace(*messageLog.TimerStartTime) == "" {
		b.startTimerWithDuration(msg.From.ID, b.kickChatIDForMessage(msg), messageLog.Username, remainingTime)
	} else {
		ts := strings.TrimSpace(*messageLog.TimerStartTime)
		b.restoreTimerWithDuration(msg.From.ID, b.kickChatIDForMessage(msg), messageLog.Username, remainingTime, ts, messageLog.TimezoneOffsetFromMoscow)
	}

	// Отправляем подтверждение с информацией о времени до удаления
	messageText := fmt.Sprintf(
		"💪 Выздоровление принято! 🎉\n\n⏰ Таймер возобновлён с места остановки!\n\n%s",
		sickLeaveRemovalNotice(remainingTimeFormatted, deadlineLocal, true),
	)

	// ИИ‑приписка: поздравление с выздоровлением (5 предложений)
	if b.aiClient != nil {
		action := tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping)
		b.sendChatActionIfTG(msg, action)
		stopTyping := make(chan struct{})
		defer close(stopTyping)
		go func() {
			ticker := time.NewTicker(4 * time.Second)
			defer ticker.Stop()
			for {
				select {
				case <-ticker.C:
					b.sendChatActionIfTG(msg, action)
				case <-stopTyping:
					return
				}
			}
		}()

		totalCups, _ := b.db.GetUserCups(msg.From.ID, messageLog.ChatID)
		question := "Сделай ровно 5 предложений‑приписку после сообщения о выздоровлении: поздравь, напомни о дисциплине, похвали за честность и предупреди о контроле таймера. Учитывай текущие кубки. Не повторяй цифры из основного текста. Без Markdown."
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
		ctxBuilder.WriteString(fmt.Sprintf("Всего кубков: %d\n", totalCups))
		if addendum, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
			addendum = ai.SanitizeTextForUser(addendum)
			if addendum != "" {
				messageText = messageText + "\n\n" + addendum
			}
		} else {
			b.logger.Warnf("AI addendum generation (healthy) failed: %v", err)
		}
	}

	b.logger.Infof("Sending healthy confirmation message to chat %d (mini-app origin: %v)", msg.Chat.ID, b.isMiniappOriginActive(msg.From.ID))
	b.notifyUserText(msg, messageText, "", 0)
}
