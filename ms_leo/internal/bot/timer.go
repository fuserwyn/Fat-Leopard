package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// normalizeUserDisplayName убирает лишние ведущие '@' и оставляет одно упоминание для @username.
func normalizeUserDisplayName(username string) string {
	clean := strings.TrimLeft(username, "@")
	if clean == "" {
		return username
	}
	if strings.Contains(clean, " ") {
		return clean
	}
	return "@" + clean
}

func (b *Bot) startTimer(userID, chatID int64, username string) {
	// Предупреждение через 6 дней, удаление через 7 дней
	b.startTimerWithDuration(userID, chatID, username, 7*24*time.Hour)
}

func (b *Bot) startTimerWithDuration(userID, chatID int64, username string, duration time.Duration) {
	// Проверяем, не исключен ли пользователь из удаления
	messageLog, err := b.db.GetMessageLog(userID, chatID)
	if err == nil && messageLog.IsExemptFromDeletion {
		b.logger.Infof("User %d (%s) is exempt from deletion, skipping timer", userID, username)
		return
	}

	// Отменяем существующие таймеры
	b.cancelTimer(userID)

	// Создаем новые таймеры
	warningTask := make(chan bool)
	criticalWarningTask := make(chan bool)
	removalTask := make(chan bool)

	timerStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	timerInfo := &domain.TimerInfo{
		UserID:              userID,
		ChatID:              chatID,
		Username:            username,
		WarningTask:         warningTask,
		CriticalWarningTask: criticalWarningTask,
		RemovalTask:         removalTask,
		TimerStartTime:      timerStartTime,
	}

	b.timers[userID] = timerInfo

	// Сохраняем время начала таймера в базу данных
	messageLog, err = b.db.GetMessageLog(userID, chatID)
	if err != nil {
		b.logger.Errorf("Failed to get message log for timer start: %v", err)
	} else {
		// Обновляем время начала таймера
		messageLog.TimerStartTime = &timerStartTime
		if err := b.db.SaveMessageLog(messageLog); err != nil {
			b.logger.Errorf("Failed to save timer start time: %v", err)
		} else {
			b.logger.Infof("Saved timer start time: %s", timerStartTime)
		}
	}

	// Рассчитываем время предупреждения (6 дней до удаления)
	// Если до удаления осталось меньше 24 часов — предупреждение уже было отправлено,
	// не планируем повторное (например, при восстановлении после перезапуска бота)
	warningTime := duration - 24*time.Hour
	if warningTime > 0 {
		go func() {
			time.Sleep(warningTime)
			select {
			case <-warningTask:
				return // Таймер отменен
			default:
				b.sendWarning(userID, chatID, username)
			}
		}()
	}

	// Критическое предупреждение за 3 часа до удаления (Леопард уже готовит, расставляет тарелки)
	criticalWarningTime := duration - 3*time.Hour
	if criticalWarningTime > 0 {
		go func() {
			time.Sleep(criticalWarningTime)
			select {
			case <-criticalWarningTask:
				return // Таймер отменен
			default:
				b.sendCriticalWarning(userID, chatID, username)
			}
		}()
	}

	// Запускаем удаление через указанное время
	go func() {
		time.Sleep(duration)
		select {
		case <-removalTask:
			return // Таймер отменен
		default:
			b.removeUser(userID, chatID, username)
		}
	}()

	if warningTime > 0 && criticalWarningTime > 0 {
		b.logger.Infof("Started timer for user %d (%s) - warning in %v, critical in %v, removal in %v", userID, username, warningTime, criticalWarningTime, duration)
	} else if criticalWarningTime > 0 {
		b.logger.Infof("Started timer for user %d (%s) - warning skipped (<24h left), critical in %v, removal in %v", userID, username, criticalWarningTime, duration)
	} else {
		b.logger.Infof("Started timer for user %d (%s) - warnings skipped (<3h left), removal in %v", userID, username, duration)
	}
}

// restoreTimerWithDuration восстанавливает таймер без обновления timer_start_time в БД
func (b *Bot) restoreTimerWithDuration(userID, chatID int64, username string, duration time.Duration, existingTimerStartTime string) {
	// Отменяем существующие таймеры
	b.cancelTimer(userID)

	// Создаем новые таймеры
	warningTask := make(chan bool)
	criticalWarningTask := make(chan bool)
	removalTask := make(chan bool)

	timerInfo := &domain.TimerInfo{
		UserID:              userID,
		ChatID:              chatID,
		Username:            username,
		WarningTask:         warningTask,
		CriticalWarningTask: criticalWarningTask,
		RemovalTask:         removalTask,
		TimerStartTime:      existingTimerStartTime, // Используем существующее время из БД
	}

	b.timers[userID] = timerInfo

	// НЕ обновляем timer_start_time в БД - используем существующее значение

	// Рассчитываем время предупреждения (6 дней до удаления)
	// Если до удаления осталось меньше 24 часов — предупреждение уже было отправлено
	// при срабатывании оригинального таймера. Не планируем повторное предупреждение.
	warningTime := duration - 24*time.Hour
	if warningTime > 0 {
		go func() {
			time.Sleep(warningTime)
			select {
			case <-warningTask:
				return // Таймер отменен
			default:
				b.sendWarning(userID, chatID, username)
			}
		}()
	} else {
		b.logger.Infof("Skipping warning for user %d (%s) - less than 24h until removal, warning already sent", userID, username)
	}

	// Критическое предупреждение за 3 часа до удаления
	// Если осталось меньше 3 часов — критическое предупреждение уже было или скоро удаление
	criticalWarningTime := duration - 3*time.Hour
	if criticalWarningTime > 0 {
		go func() {
			time.Sleep(criticalWarningTime)
			select {
			case <-criticalWarningTask:
				return // Таймер отменен
			default:
				b.sendCriticalWarning(userID, chatID, username)
			}
		}()
	} else {
		b.logger.Infof("Skipping critical warning for user %d (%s) - less than 3h until removal", userID, username)
	}

	// Запускаем удаление через указанное время
	go func() {
		time.Sleep(duration)
		select {
		case <-removalTask:
			return // Таймер отменен
		default:
			b.removeUser(userID, chatID, username)
		}
	}()

	b.logger.Infof("Restored timer for user %d (%s) - warning in %v, critical in %v, removal in %v (timer start time: %s)", userID, username, warningTime, criticalWarningTime, duration, existingTimerStartTime)
}

func (b *Bot) cancelTimer(userID int64) {
	if timer, exists := b.timers[userID]; exists {
		close(timer.WarningTask)
		close(timer.CriticalWarningTask)
		close(timer.RemovalTask)
		delete(b.timers, userID)
		b.logger.Infof("Cancelled timer for user %d", userID)
	}
}

func (b *Bot) sendWarning(userID, chatID int64, username string) {
	// Базовый текст предупреждения
	who := normalizeUserDisplayName(username)
	messageText := fmt.Sprintf("⚠️ Предупреждение!\n\n%s, ты не отправляешь отчет о тренировке уже 6 дней!\n\n⏰ У тебя остался 1 день до удаления из чата!\n\n🎯 Отправь #training_done прямо сейчас!", who)

	// Добавляем короткую ИИ‑приписку к предупреждению
	if b.aiClient != nil {
		action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
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

		// Стараемся собрать немного контекста
		var ctxBuilder strings.Builder
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", username))
		if log, err := b.db.GetMessageLog(userID, chatID); err == nil {
			// Добавляем пол пользователя в контекст
			userGender := strings.TrimSpace(strings.ToLower(log.Gender))
			if userGender != "" {
				var genderText string
				if userGender == "f" {
					genderText = "женский"
				} else if userGender == "m" {
					genderText = "мужской"
				}
				if genderText != "" {
					ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
				}
			}
			ctxBuilder.WriteString(fmt.Sprintf("Серия: %d дней\n", log.StreakDays))
			ctxBuilder.WriteString("Нет отчёта 6 дней, остался 1 день.\n")
			if log.HasSickLeave && !log.HasHealthy {
				ctxBuilder.WriteString("На больничном сейчас.\n")
			}
		}
		if cups, err := b.db.GetUserCups(userID, chatID); err == nil {
			ctxBuilder.WriteString(fmt.Sprintf("Кубков всего: %d\n", cups))
		}

		question := b.config.Prompts.WarningTimerQuestion
		if addendum, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
			addendum = strings.TrimSpace(strings.ReplaceAll(addendum, "**", ""))
			if addendum != "" {
				messageText = messageText + "\n\n" + addendum
			}
		} else {
			b.logger.Warnf("AI addendum generation (warning) failed: %v", err)
		}
	}

	msg := tgbotapi.NewMessage(chatID, messageText)
	b.logger.Infof("Sending warning to user %d (%s)", userID, username)
	_, err := b.api.Send(msg)
	if err != nil {
		b.logger.Errorf("Failed to send warning: %v", err)
	} else {
		b.logger.Infof("Successfully sent warning to user %d (%s)", userID, username)
	}
}

func (b *Bot) sendCriticalWarning(userID, chatID int64, username string) {
	// Критическое предупреждение за 3 часа до удаления — Леопард уже готовит
	who := normalizeUserDisplayName(username)
	messageText := fmt.Sprintf("🚨 КРИТИЧЕСКОЕ ПРЕДУПРЕЖДЕНИЕ! 🚨\n\n%s, я уже готовлюсь к обеду! Расставляю тарелки, накрываю на стол... Осталось всего 3 ЧАСА до удаления из чата!\n\n⏰ Это твой последний шанс!\n\n🎯 Отправь #training_done ПРЯМО СЕЙЧАС — или станешь главным блюдом! 😬", who)

	// Добавляем короткую ИИ‑приписку в духе «Леопард уже ест»
	if b.aiClient != nil {
		action := tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping)
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

		var ctxBuilder strings.Builder
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", username))
		if log, err := b.db.GetMessageLog(userID, chatID); err == nil {
			userGender := strings.TrimSpace(strings.ToLower(log.Gender))
			if userGender != "" {
				var genderText string
				if userGender == "f" {
					genderText = "женский"
				} else if userGender == "m" {
					genderText = "мужской"
				}
				if genderText != "" {
					ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
				}
			}
			ctxBuilder.WriteString(fmt.Sprintf("Серия: %d дней\n", log.StreakDays))
			ctxBuilder.WriteString("Осталось 3 часа до удаления. Леопард уже готовит, расставляет тарелки.\n")
		}

		question := b.config.Prompts.CriticalTimerQuestion
		if addendum, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
			addendum = strings.TrimSpace(strings.ReplaceAll(addendum, "**", ""))
			if addendum != "" {
				messageText = messageText + "\n\n" + addendum
			}
		} else {
			b.logger.Warnf("AI addendum generation (critical warning) failed: %v", err)
		}
	}

	msg := tgbotapi.NewMessage(chatID, messageText)
	b.logger.Infof("Sending critical warning to user %d (%s)", userID, username)
	_, err := b.api.Send(msg)
	if err != nil {
		b.logger.Errorf("Failed to send critical warning: %v", err)
	} else {
		b.logger.Infof("Successfully sent critical warning to user %d (%s)", userID, username)
	}
}

func (b *Bot) removeUser(userID, chatID int64, username string) {
	b.logger.Infof("Attempting to remove user %d (%s) from chat %d", userID, username, chatID)

	// КРИТИЧЕСКИ ВАЖНО: Проверяем, не был ли только что отправлен #training_done
	// Если пользователь отправил #training_done, таймер должен был быть перезапущен
	// и этот вызов removeUser не должен был произойти
	messageLog, err := b.db.GetMessageLog(userID, chatID)
	if err == nil {
		// Проверяем, был ли недавно отправлен #training_done
		// Если HasTrainingDone = true и LastMessage недавно обновлен, не удаляем
		if messageLog.HasTrainingDone {
			lastMessageTime, parseErr := time.Parse("2006-01-02 15:04:05", messageLog.LastMessage)
			if parseErr == nil {
				timeSinceLastMessage := time.Since(lastMessageTime)
				// Если последнее сообщение было менее 1 минуты назад и содержит #training_done, не удаляем
				if timeSinceLastMessage < 1*time.Minute {
					b.logger.Infof("User %d (%s) just sent #training_done (%v ago), cancelling removal", userID, username, timeSinceLastMessage)
					// Отменяем таймер, если он еще существует
					b.cancelTimer(userID)
					return
				}
			}
		}
	}

	// Пытаемся удалить пользователя из чата
	_, err = b.api.Request(tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		UntilDate: time.Now().Add(30 * 24 * time.Hour).Unix(), // Бан на 30 дней
	})

	if err != nil {
		b.logger.Errorf("Failed to remove user %d: %v", userID, err)
		// Отправляем сообщение об ошибке
		errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось удалить пользователя %s из чата", normalizeUserDisplayName(username)))
		b.api.Send(errorMsg)
	} else {
		// Отправляем сообщение об удалении (одна строка про удаление — без дублирования заголовка и тела)
		who := normalizeUserDisplayName(username)
		message := fmt.Sprintf("🚫 %s удалён из чата за неактивность.\n\n🦁 Ням-ням, вкусненько!\n\n💪 Ты ведь не хочешь стать как я?\n\nТогда тренируйся и отправляй отчёты!", who)
		msg := tgbotapi.NewMessage(chatID, message)
		b.logger.Infof("Sending removal message for user %d (%s)", userID, username)
		_, sendErr := b.api.Send(msg)
		if sendErr != nil {
			b.logger.Errorf("Failed to send removal message: %v", sendErr)
		} else {
			b.logger.Infof("Successfully sent removal message for user %d (%s)", userID, username)
		}

		b.logger.Infof("Removed user %d (%s) from chat", userID, username)
	}

	// Помечаем пользователя как удаленного в базе данных
	if err := b.db.MarkUserAsDeleted(userID, chatID); err != nil {
		b.logger.Errorf("Failed to mark user as deleted: %v", err)
	}

	// Удаляем таймер
	delete(b.timers, userID)
	b.logger.Infof("Timer removed for user %d", userID)
}

// recoverTimersFromDatabase восстанавливает таймеры из базы данных при запуске бота
func (b *Bot) recoverTimersFromDatabase() error {
	b.logger.Info("Recovering timers from database...")

	// Получаем всех пользователей с активными таймерами
	users, err := b.db.GetAllUsersWithTimers()
	if err != nil {
		return fmt.Errorf("failed to get users with timers: %w", err)
	}

	recoveredCount := 0
	for _, user := range users {
		// Дополнительное логирование для диагностики проблем с короткими ID
		b.logger.Infof("Processing user: ID=%d, Username='%s', ChatID=%d, HasSickLeave=%t, HasHealthy=%t, IsDeleted=%t, IsExemptFromDeletion=%t",
			user.UserID, user.Username, user.ChatID, user.HasSickLeave, user.HasHealthy, user.IsDeleted, user.IsExemptFromDeletion)

		// Пропускаем пользователей на больничном
		if user.HasSickLeave && !user.HasHealthy {
			b.logger.Infof("Skipping user %d (%s) - on sick leave", user.UserID, user.Username)
			continue
		}

		// Пропускаем удаленных пользователей
		if user.IsDeleted {
			b.logger.Infof("Skipping user %d (%s) - deleted", user.UserID, user.Username)
			continue
		}

		// Пропускаем пользователей, исключенных из удаления
		if user.IsExemptFromDeletion {
			b.logger.Infof("Skipping user %d (%s) - exempt from deletion", user.UserID, user.Username)
			continue
		}

		// Рассчитываем оставшееся время
		remainingTime := b.calculateRemainingTime(user)
		if remainingTime <= 0 {
			// Время истекло - удаляем пользователя
			b.logger.Infof("Timer expired for user %d (%s), removing from chat", user.UserID, user.Username)
			b.removeUser(user.UserID, user.ChatID, user.Username)
			continue
		}

		// Восстанавливаем таймер без обновления timer_start_time в БД
		if user.TimerStartTime != nil {
			b.restoreTimerWithDuration(user.UserID, user.ChatID, user.Username, remainingTime, *user.TimerStartTime)
		} else {
			// Fallback - если timer_start_time отсутствует, используем обычный старт
			b.startTimerWithDuration(user.UserID, user.ChatID, user.Username, remainingTime)
		}
		recoveredCount++

		b.logger.Infof("Recovered timer for user %d (%s) - remaining time: %v", user.UserID, user.Username, remainingTime)
	}

	b.logger.Infof("Successfully recovered %d timers from database", recoveredCount)
	return nil
}
