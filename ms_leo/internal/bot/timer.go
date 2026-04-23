package bot

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func parseLastMessageTime(lastMessage string) (time.Time, error) {
	if ts, err := utils.ParseMoscowTime(lastMessage); err == nil {
		return ts, nil
	}
	// Backward compatibility for legacy rows stored without timezone.
	return time.ParseInLocation("2006-01-02 15:04:05", lastMessage, utils.GetMoscowTime().Location())
}

func removalDMText() string {
	return "Ну что, 7 дней без движения — и стая тебя больше не видит.\nТак тоже бывает. XP сгорел, доступ закрыт.\nЕсли захочешь вторую попытку — леопард не будет делать вид, что ничего не было."
}

func removalDMReplyMarkup() *tgbotapi.InlineKeyboardMarkup {
	return &tgbotapi.InlineKeyboardMarkup{
		InlineKeyboard: [][]tgbotapi.InlineKeyboardButton{
			tgbotapi.NewInlineKeyboardRow(
				tgbotapi.NewInlineKeyboardButtonData("Вернуться в стаю", paywallCallbackReturnToPack),
			),
		},
	}
}

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

func (b *Bot) removeUser(userID, chatID int64, username string) {
	b.logger.Infof("Attempting to remove user %d (%s) from chat %d", userID, username, chatID)
	who := normalizeUserDisplayName(username)

	// КРИТИЧЕСКИ ВАЖНО: Проверяем, не был ли только что отправлен #training_done
	// Если пользователь отправил #training_done, таймер должен был быть перезапущен
	// и этот вызов removeUser не должен был произойти
	messageLog, err := b.db.GetMessageLog(userID, chatID)
	if err == nil {
		// Проверяем, был ли недавно отправлен #training_done
		// Если HasTrainingDone = true и LastMessage недавно обновлен, не удаляем
		if messageLog.HasTrainingDone {
			lastMessageTime, parseErr := parseLastMessageTime(messageLog.LastMessage)
			if parseErr == nil {
				timeSinceLastMessage := utils.GetMoscowTime().Sub(lastMessageTime)
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

	dmText := removalDMText()
	dmDelivered := chatID == userID
	dmStatus := "dm_skipped"
	dmErrorText := ""
	if chatID != userID {
		dmMsg := tgbotapi.NewMessage(userID, dmText)
		dmMsg.ReplyMarkup = removalDMReplyMarkup()
		if _, err := b.api.Send(dmMsg); err != nil {
			b.logger.Warnf("send removal DM user=%d: %v", userID, err)
			dmErrorText = err.Error()
			dmStatus = "dm_failed"
			var tgErr *tgbotapi.Error
			if errors.As(err, &tgErr) && tgErr.Code == 403 {
				dmStatus = "dm_blocked"
			}
		} else {
			dmDelivered = true
			dmStatus = "dm_sent"
		}
	} else {
		dmStatus = "dm_sent"
	}
	if err := b.db.LogDeletionEvent(userID, chatID, dmStatus, dmErrorText); err != nil {
		b.logger.Errorf("log deletion event user=%d chat=%d: %v", userID, chatID, err)
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
		// Отправляем сообщение об удалении в группу.
		message := fmt.Sprintf("🚫 %s удалён из чата за неактивность.\n\n🦁 Ням-ням, вкусненько!\n\n💪 Ты ведь не хочешь стать как я?\n\nТогда тренируйся и отправляй отчёты!", who)
		if chatID != userID && !dmDelivered {
			message += "\n\nℹ️ Уведомление в личные сообщения доставить не удалось. Открой диалог с ботом через /start, чтобы получать личные предупреждения и уведомления."
		}
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
