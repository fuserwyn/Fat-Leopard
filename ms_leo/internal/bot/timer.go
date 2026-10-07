package bot

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// inactivityKickWatchdogInterval — как часто periodic-страховка добивает кики, которые
// рантайм-таймер мог пропустить (рестарт мс «между» сработками, потерянная горутина и т.п.).
// Источник правды — calculateRemainingTime по той же pack-row (chat_id == MonetizedChatID),
// что и в recoverTimersFromDatabase. Идемпотентно: removeUser помечает is_deleted=true,
// а GetAllUsersWithTimers фильтрует is_deleted=false — повторного кика быть не может.
const inactivityKickWatchdogInterval = 30 * time.Minute

func parseLastMessageTime(lastMessage string) (time.Time, error) {
	if ts, err := utils.ParseMoscowTime(lastMessage); err == nil {
		return ts, nil
	}
	return time.ParseInLocation("2006-01-02 15:04:05", lastMessage, utils.GetMoscowTime().Location())
}

// recentlyReported — true, если по строке training_state (chatID из триггера, обычно MonetizedChatID)
// зафиксирован недавний #training_done. Легаси private-row (userID, userID) — только если MONETIZED_CHAT_ID не задан.
func recentlyReported(b *Bot, userID, chatID int64) bool {
	check := func(ml *domain.MessageLog) bool {
		if ml == nil || !ml.HasTrainingDone || strings.TrimSpace(ml.LastMessage) == "" {
			return false
		}
		ts, err := parseLastMessageTime(ml.LastMessage)
		if err != nil {
			return false
		}
		return utils.GetMoscowTime().Sub(ts) < 1*time.Minute
	}
	if ml, err := b.db.GetMessageLog(userID, chatID); err == nil && check(ml) {
		return true
	}
	if b != nil && b.config != nil && b.config.MonetizedChatID == 0 && chatID != userID {
		if ml, err := b.db.GetMessageLog(userID, userID); err == nil && check(ml) {
			return true
		}
	}
	return false
}

func removalDMText() string {
	return "Как хорошо, что ты не занимался.\nЛео удалил тебя из стаи: 7 дней без движения, и стая тебя больше не видит.\nПрогресс в стае сброшен, доступ к мини-аппу закрыт.\nЕсли захочешь вернуться — нажми кнопку ниже и пройди повторный вход."
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

// kickChatIDForMessage — chat_id строки в training_state, на которой стоит таймер кика.
// В мини-апп-архитектуре всегда работаем по pack-row (pair: user, MonetizedChatID), независимо от того,
// откуда пришло сообщение — из приватного чата с ботом или из (легаси) группы стаи.
// Если MonetizedChatID не настроен — fallback к msg.Chat.ID, чтобы не сломать локальные тесты/окружения без оплаты.
func (b *Bot) kickChatIDForMessage(msg *tgbotapi.Message) int64 {
	if b != nil && b.config != nil && b.config.MonetizedChatID != 0 {
		return b.config.MonetizedChatID
	}
	if msg != nil && msg.Chat != nil {
		return msg.Chat.ID
	}
	return 0
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

// removeUser — «кик» из стаи после 7 дней без движения. Физически из TG-группы никого не выкидываем
// (TG-группы как сущности нет, миграция на мини-апп):
//  1. шлём DM с предложением вернуться;
//  2. аннулируем купленный paywall-доступ — повторный вход только через новую оплату;
//  3. кладём карточку pack_removed в ленту мини-аппа;
//  4. помечаем training_state.is_deleted = true.
func (b *Bot) removeUser(userID, chatID int64, username string) {
	b.logger.Infof("Attempting to remove user %d (%s) from chat %d", userID, username, chatID)

	// Защита от гонки: если #training_done только что был — отменяем удаление.
	// Проверяем обе строки: pack-row (на ней живёт таймер) и private-row (туда пишет
	// last_message handleMessage из мини-аппа/лички). Без второй проверки anti-trigger
	// никогда не ловит гонку: в pack-row last_message обновлялся только сообщениями
	// из группы стаи, а группы мы теперь игнорируем.
	if recentlyReported(b, userID, chatID) {
		b.logger.Infof("User %d (%s) just sent #training_done, cancelling removal", userID, username)
		b.cancelTimer(userID)
		return
	}

	// Защита от устаревшего removal-таймера: если дедлайн уже отодвинут (тренировка,
	// возврат через админку, выход с больничного — всё сбрасывает timer_start = NOW),
	// НЕ удаляем. Иначе ранее запланированный goroutine, не успевший отмениться через
	// cancelTimer, добивал бы только что восстановленного/вернувшегося юзера.
	if ml, mlErr := b.db.GetMessageLog(userID, chatID); mlErr == nil && ml != nil {
		if !ml.IsDeleted && b.calculateRemainingTime(ml) > 0 {
			b.logger.Infof("User %d (%s) not overdue anymore (timer was reset), cancelling removal", userID, username)
			b.cancelTimer(userID)
			return
		}
	}

	b.removeUserNow(userID, chatID, username)
}

// removeUserNow удаляет участника без проверок срока: так исключает админ.
// Проверки в removeUser защищают от устаревшего таймера, а решение человека
// они отменять не должны — иначе «Исключить» отвечало «удалён», а участник с
// невышедшим сроком оставался в стае.
func (b *Bot) removeUserNow(userID, chatID int64, username string) {
	dmStatus := "dm_skipped"
	dmErrorText := ""
	if chatID != userID {
		dmMsg := tgbotapi.NewMessage(userID, removalDMText())
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
			dmStatus = "dm_sent"
		}
	} else {
		dmStatus = "dm_sent"
	}
	if err := b.db.LogDeletionEvent(userID, chatID, dmStatus, dmErrorText); err != nil {
		b.logger.Errorf("log deletion event user=%d chat=%d: %v", userID, chatID, err)
	}

	if chatID == b.config.MonetizedChatID {
		if expErr := b.db.ExpirePaywallAccessForUser(userID, chatID); expErr != nil {
			b.logger.Errorf("Failed to expire paywall access for inactive user %d: %v", userID, expErr)
		}
		b.savePackRemovedMiniappFeed(chatID, userID, username)
		// Кикнутый юзер не должен видеть синюю web_app-кнопку LeopardMiniApp в ЛС.
		// Сбрасываем кеш, чтобы applyMiniappMenuButtonForUser форсированно отправил commands.
		invalidateMiniappMenuButtonCache(userID)
		b.applyMiniappMenuButtonForUser(userID)
		b.logger.Infof("Removed user %d (%s) from pack: paywall access expired, miniapp feed updated", userID, username)
	} else {
		// chatID != MonetizedChatID — это приватный «message_log в личке» (chatID == userID).
		// Никаких физических действий, только пометка is_deleted (см. ниже) — таймер выключен в DM выше.
		b.logger.Infof("Removed user %d (%s) from non-pack chat %d (private flow only)", userID, username, chatID)
	}

	if err := b.db.MarkUserAsDeleted(userID, chatID); err != nil {
		b.logger.Errorf("Failed to mark user as deleted: %v", err)
	}

	// PII-анонимизация аналитики при удалении аккаунта (§9.3, 152-ФЗ): telegram_id
	// в events → псевдоним. Соль — бот-токен (секрет деплоя). Идемпотентно.
	if err := b.db.AnonymizeUserAnalytics(userID, b.config.APIToken); err != nil {
		b.logger.Warnf("anonymize analytics user=%d: %v", userID, err)
	}

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

		// В paywall-режиме источник правды для кика — pack-row (chat_id = MonetizedChatID).
		// Private-row (chat_id = userID) хранит только bookkeeping (last_message, XP, streak)
		// и не должна порождать рантайм-таймер кика: иначе она перетирает b.timers[userID]
		// (мапа без учёта chatID) и слепо рассылает «тебя кикнули» DM активным платящим юзерам,
		// у которых private-row.timer_start_time протух с момента первого открытия мини-аппа.
		if b.config != nil && b.config.MonetizedChatID != 0 && user.ChatID != b.config.MonetizedChatID {
			b.logger.Infof("Skipping user %d (%s) - non-pack row chat_id=%d (kick lives only on pack-row)", user.UserID, user.Username, user.ChatID)
			continue
		}

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
			b.trackAccountDeletedInactivity(user.UserID, user.ChatID)
			b.removeUser(user.UserID, user.ChatID, user.Username)
			continue
		}

		// Восстанавливаем таймер без обновления timer_start_time в БД
		if user.TimerStartTime != nil {
			b.restoreTimerWithDuration(user.UserID, user.ChatID, user.Username, remainingTime, *user.TimerStartTime, user.TimezoneOffsetFromMoscow)
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

// startInactivityKickWatchdog — periodic-страховка от «забытых» киков: если рантайм-таймер
// не сработал (рестарт мс ровно между моментом старта таймера и его deadline, потерянная
// горутина после паники, невидимый дрейф расчёта), watchdog раз в inactivityKickWatchdogInterval
// проходит по pack-row training_state и зовёт removeUser для всех, у кого calculateRemainingTime <= 0.
//
// Без watchdog'а такие юзеры висели бы как «заблудившиеся в лесу»: timer_start_time старый,
// is_deleted=false, доступ в мини-аппе формально активен. С watchdog'ом «всех найдём».
//
// Идемпотентность: GetAllUsersWithTimers исключает is_deleted=false, MarkUserAsDeleted в
// removeUser ставит is_deleted=true — повторного DM/feed-карточки/expirePaywall не будет.
func (b *Bot) startInactivityKickWatchdog(ctx context.Context) {
	if b == nil {
		return
	}
	if b.config == nil || b.config.MonetizedChatID == 0 {
		b.logger.Info("inactivity kick watchdog: skipped, MONETIZED_CHAT_ID не настроен")
		return
	}
	b.logger.Infof("inactivity kick watchdog: started, interval=%s", inactivityKickWatchdogInterval)
	ticker := time.NewTicker(inactivityKickWatchdogInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			b.logger.Info("inactivity kick watchdog: stopped")
			return
		case <-ticker.C:
			b.runInactivityKickSweep()
		}
	}
}

// runInactivityKickSweep — одна итерация watchdog'а. Логика — копия фильтров из
// recoverTimersFromDatabase, минус восстановление работающих таймеров: нас интересуют
// только те, кому уже пора «съесть».
func (b *Bot) runInactivityKickSweep() {
	users, err := b.db.GetAllUsersWithTimers()
	if err != nil {
		b.logger.Errorf("inactivity kick watchdog: get users with timers: %v", err)
		return
	}
	checked := 0
	expired := 0
	for _, user := range users {
		if user == nil {
			continue
		}
		// Только pack-row — таймер кика «живёт» именно на ней (см. recoverTimersFromDatabase).
		if b.config.MonetizedChatID != 0 && user.ChatID != b.config.MonetizedChatID {
			continue
		}
		if user.IsDeleted || user.IsExemptFromDeletion {
			continue
		}
		if user.HasSickLeave && !user.HasHealthy {
			continue
		}
		if user.TimerStartTime == nil {
			continue
		}
		checked++
		if b.calculateRemainingTime(user) > 0 {
			continue
		}
		b.logger.Warnf("inactivity kick watchdog: overdue user %d (%s) chat=%d — removing",
			user.UserID, user.Username, user.ChatID)
		b.removeUser(user.UserID, user.ChatID, user.Username)
		expired++
	}
	if expired > 0 {
		b.logger.Infof("inactivity kick watchdog: sweep done, checked=%d, removed=%d", checked, expired)
	} else {
		b.logger.Debugf("inactivity kick watchdog: sweep done, checked=%d, no overdue users", checked)
	}
}
