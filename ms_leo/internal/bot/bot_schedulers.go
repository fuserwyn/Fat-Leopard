package bot

import (
	"context"
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Проверки прав и фоновые задачи: дневная сводка, мудрость дня, аудит, месячный отчёт.

func (b *Bot) isAdmin(chatID, userID int64) bool {
	// Проверяем, является ли пользователь одним из админов из env.
	if b.config.IsAdminTelegramUser(userID) {
		return true
	}

	if b.api == nil {
		b.logger.Warn("Bot API is nil, cannot verify admin status via Telegram")
		return false
	}

	// Проверяем права администратора
	member, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID,
			UserID: userID,
		},
	})
	if err != nil {
		b.logger.Errorf("Failed to get chat member: %v", err)
		return false
	}

	return member.Status == "administrator" || member.Status == "creator"
}

func (b *Bot) isUserInChat(chatID, userID int64) bool {
	_, err := b.api.GetChatMember(tgbotapi.GetChatMemberConfig{
		ChatConfigWithUser: tgbotapi.ChatConfigWithUser{
			ChatID: chatID,
			UserID: userID,
		},
	})
	return err == nil
}

// formatDurationToDays форматирует время в читаемый вид (дни, часы, минуты)
func (b *Bot) formatDurationToDays(duration time.Duration) string {
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60

	if days > 0 {
		if hours > 0 {
			return fmt.Sprintf("%d %s %d ч.", days, daysWordForm(days), hours)
		}
		return fmt.Sprintf("%d %s", days, daysWordForm(days))
	} else if hours > 0 {
		if minutes > 0 {
			return fmt.Sprintf("%d ч. %d мин.", hours, minutes)
		}
		return fmt.Sprintf("%d ч.", hours)
	} else {
		return fmt.Sprintf("%d мин.", minutes)
	}
}

func (b *Bot) calculateRemainingTime(messageLog *domain.MessageLog) time.Duration {
	b.logger.Infof("DEBUG calculateRemainingTime: HasSickLeave=%t, HasHealthy=%t, SickLeaveStartTime=%v, SickLeaveEndTime=%v",
		messageLog.HasSickLeave, messageLog.HasHealthy,
		messageLog.SickLeaveStartTime != nil, messageLog.SickLeaveEndTime != nil)

	if messageLog.TimerStartTime == nil {
		b.logger.Infof("DEBUG: TimerStartTime is nil, returning full duration")
		return leopardmoney.FullTimerDuration
	}

	moscowNow := utils.GetMoscowTime()
	deadline, ok := inactivityKickDeadline(messageLog, moscowNow)
	if !ok {
		return leopardmoney.FullTimerDuration
	}
	remaining := deadline.Sub(moscowNow)
	if remaining <= 0 {
		return 0
	}
	return remaining
}

// startDailySummaryScheduler запускает планировщик ежемесячных сводок 1-го числа в 16:20
func (b *Bot) startDailySummaryScheduler(ctx context.Context) {
	if b.aiClient == nil {
		b.logger.Warn("AI client not available, monthly summary scheduler disabled")
		return
	}

	// Используем московское время
	loc, _ := time.LoadLocation("Europe/Moscow")
	lastSentMonth := ""
	ticker := time.NewTicker(1 * time.Minute) // Проверяем каждую минуту
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().In(loc)
			day := now.Day()
			hour := now.Hour()
			minute := now.Minute()

			// Проверяем, наступило ли время 16:20 1-го числа месяца
			if day == 1 && hour == 16 && minute == 20 {
				month := now.Format("2006-01")
				// Отправляем сводку только один раз в месяц
				if lastSentMonth != month {
					// Генерируем сводку за прошлый месяц
					lastMonth := now.AddDate(0, -1, 0)
					b.logger.Infof("Generating monthly summary at 16:20 on 1st for month: %s", lastMonth.Format("2006-01"))
					b.generateAndSendMonthlySummary(lastMonth)
					lastSentMonth = month
				}
			}
		}
	}
}

// startDailyWisdomScheduler отправляет «мудрость дня» ежедневно в 04:20 (МСК)
func (b *Bot) startDailyWisdomScheduler(ctx context.Context) {
	if b.aiClient == nil {
		b.logger.Warn("AI client not available, daily wisdom scheduler disabled")
		return
	}

	loc, _ := time.LoadLocation("Europe/Moscow")
	lastSentDate := ""
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().In(loc)
			hour := now.Hour()
			minute := now.Minute()
			if hour == 4 && minute == 20 {
				today := now.Format("2006-01-02")
				if lastSentDate != today {
					b.logger.Infof("Generating daily wisdom for %s 04:20 MSK", today)
					b.generateAndSendDailyWisdom()
					lastSentDate = today
				}
			}
		}
	}
}

func (b *Bot) generateAndSendDailyWisdom() {
	// Получаем все чаты
	chatIDs, err := b.db.GetAllChatIDs()
	if err != nil {
		b.logger.Errorf("Failed to get chat IDs for daily wisdom: %v", err)
		return
	}
	if len(chatIDs) == 0 {
		return
	}

	wisdom, err := b.aiClient.GenerateDailyWisdom()
	if err != nil {
		b.logger.Errorf("Failed to generate daily wisdom: %v", err)
		candidates := []string{
			"Тишина внутри сильнее шума вокруг. Дисциплина — это форма заботы о себе. Начни с малого и будь верен пути.",
			"Сила духа рождается в простых шагах. Выбери одно действие сегодня — и сделай его спокойно.",
			"Тело слушает разум. Разум слушает дыхание. Ровное дыхание — ровный прогресс.",
			"Пусть тренировка будет краткой, но честной. Постоянство сильнее порывов.",
			"Не ищи идеального момента. Сделай его. Терпение и движение — союзники."}
		idx := int(time.Now().Unix() % int64(len(candidates)))
		wisdom = candidates[idx]
	} else {
		wisdom = strings.ReplaceAll(wisdom, "**", "")
	}

	b.saveDailyWisdomPackFeed(wisdom)

	// Сохраняем мудрость за сегодня, чтобы подписчики (см. startDailyWisdomSubscriptionScheduler)
	// получили её в личку в свой локальный час.
	mskToday := utils.GetMoscowTime().Format("2006-01-02")
	if err := b.db.SaveDailyWisdomOfDay(mskToday, wisdom); err != nil {
		b.logger.Warnf("save daily wisdom of day: %v", err)
	}

	packChatID := b.config.MonetizedChatID
	for _, chatID := range chatIDs {
		if packChatID != 0 && chatID == packChatID {
			continue
		}
		msg := tgbotapi.NewMessage(chatID, wisdom)
		b.logger.Infof("Sending daily wisdom to chat %d", chatID)
		if _, err := b.api.Send(msg); err != nil {
			b.logger.Errorf("Failed to send daily wisdom to chat %d: %v", chatID, err)
		}
	}
}

// auditLast24h проверяет сообщения за последние 24 часа и отправляет пропущенные подтверждения (без повторных начислений)
func (b *Bot) auditLast24h() {
	loc, _ := time.LoadLocation("Europe/Moscow")
	end := time.Now().In(loc)
	start := end.Add(-24 * time.Hour)

	chatIDs, err := b.db.GetAllChatIDs()
	if err != nil {
		b.logger.Errorf("auditLast24h: failed to get chat IDs: %v", err)
		return
	}

	for _, chatID := range chatIDs {
		msgs, err := b.db.GetMessagesInRange(chatID, start, end)
		if err != nil {
			b.logger.Errorf("auditLast24h: failed to get messages for chat %d: %v", chatID, err)
			continue
		}
		for _, um := range msgs {
			switch um.MessageType {
			case "training_done":
				b.auditProcessTrainingDone(um)

			case "sick_leave":
				ml, err := b.db.GetMessageLog(um.UserID, um.ChatID)
				if err != nil {
					continue
				}
				if ml.SickLeaveStartTime != nil {
					continue
				}
				// Отправляем мягкое подтверждение больничного
				text := "🏥 Больничный принят! 🤒\n\n⏸️ Таймер приостановлен на время болезни.\n\n💬 Подтверждение отправлено после перезапуска. Выздоравливай!"
				b.api.Send(tgbotapi.NewMessage(um.ChatID, text))

			case "healthy":
				ml, err := b.db.GetMessageLog(um.UserID, um.ChatID)
				if err != nil {
					continue
				}
				if !ml.HasHealthy {
					text := "💪 Выздоровление принято! 🎉\n\n⏰ Таймер возобновлён.\n\n💬 Подтверждение отправлено после перезапуска."
					b.api.Send(tgbotapi.NewMessage(um.ChatID, text))
				}
			}
		}
	}
}

// auditProcessTrainingDone выполняет учет и отправку подтверждения по записи user_messages (после рестарта)
func (b *Bot) auditProcessTrainingDone(um *domain.UserMessage) {
	loc, _ := time.LoadLocation("Europe/Moscow")
	dateStr := um.CreatedAt.In(loc).Format("2006-01-02")

	messageLog, err := b.db.GetMessageLog(um.UserID, um.ChatID)
	if err != nil {
		b.logger.Errorf("auditProcessTrainingDone: failed to get message log: %v", err)
		return
	}

	username := um.Username
	if username == "" {
		username = fmt.Sprintf("User%d", um.UserID)
	}

	already := messageLog.LastTrainingDate != nil && *messageLog.LastTrainingDate == dateStr
	if already {
		// ДЕНЬ УЖЕ УЧТЕН: не начисляем ничего, отправляем только подтверждение, если его могло не быть
		text := fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Я вижу твою тренировку за %s.\n\n⏰ Бот был перезапущен — отправляю подтверждение сейчас.", um.CreatedAt.In(loc).Format("02.01 15:04"))
		b.api.Send(tgbotapi.NewMessage(um.ChatID, text))
		return
	}

	outcome := b.calculateTrainingDayOutcome(messageLog)

	if outcome.EarnRewards {
		_ = b.db.UpdateStreak(um.UserID, um.ChatID, outcome.NewStreakDays, dateStr)
		_ = b.db.AddCups(um.UserID, um.ChatID, 1)
		if bonus := outcome.MilestoneCups(); bonus > 0 {
			_ = b.db.AddCups(um.UserID, um.ChatID, bonus)
		}

		currentCups, _ := b.db.GetUserCups(um.UserID, um.ChatID)
		text := fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Ты тренируешься %d %s подряд\n🏆 +1 кубок за тренировку!\n🏆 Всего кубков: %d\n\n⏰ Таймер перезапускается на 7 %s", outcome.NewStreakDays, daysWordForm(outcome.NewStreakDays), currentCups, daysWordForm(7))
		b.api.Send(tgbotapi.NewMessage(um.ChatID, text))
	} else {
		_ = b.db.AddCups(um.UserID, um.ChatID, 1)
		currentCups, _ := b.db.GetUserCups(um.UserID, um.ChatID)
		text := fmt.Sprintf("🦁 Какой мотивированный леопард! Еще одна тренировка сегодня! 💪\n\n🏆 +1 кубок за дополнительную тренировку!\n🏆 Всего кубков: %d", currentCups)
		b.api.Send(tgbotapi.NewMessage(um.ChatID, text))
	}

	b.startTimer(um.UserID, um.ChatID, username)
}

// generateAndSendMonthlySummary генерирует и отправляет ежемесячную сводку
func (b *Bot) generateAndSendMonthlySummary(month time.Time) {
	if b.aiClient == nil {
		return
	}

	// Получаем все чаты из базы данных
	chatIDs, err := b.db.GetAllChatIDs()
	if err != nil {
		b.logger.Errorf("Failed to get chat IDs: %v", err)
		return
	}

	// Для каждого чата генерируем сводку
	for _, chatID := range chatIDs {
		b.generateMonthlySummaryForChat(chatID, month)
	}
}

// monthlyReportUser данные пользователя для месячного отчёта
type monthlyReportUser struct {
	UserID        int64
	Username      string
	TrainingCount int
	HasSickLeave  bool
	HasHealthy    bool
	StreakDays    int
	Cups          int
}

// generateMonthlySummaryForChat генерирует месячную сводку для конкретного чата
func (b *Bot) generateMonthlySummaryForChat(chatID int64, month time.Time) {
	// Получаем сообщения за месяц
	messages, err := b.db.GetMonthlyMessages(chatID, month)
	if err != nil {
		b.logger.Errorf("Failed to get monthly messages for chat %d: %v", chatID, err)
		return
	}

	if len(messages) == 0 {
		return // Нет сообщений за месяц
	}

	// Группируем и считаем по пользователям
	userMap := make(map[int64]*monthlyReportUser)
	for _, msg := range messages {
		if userMap[msg.UserID] == nil {
			userLog, err := b.db.GetMessageLog(msg.UserID, msg.ChatID)
			if err != nil {
				continue
			}
			cups, _ := b.db.GetUserCups(msg.UserID, msg.ChatID)
			userMap[msg.UserID] = &monthlyReportUser{
				UserID:        msg.UserID,
				Username:      msg.Username,
				TrainingCount: 0,
				HasSickLeave:  false,
				HasHealthy:    false,
				StreakDays:    userLog.StreakDays,
				Cups:          cups,
			}
		}

		u := userMap[msg.UserID]
		switch msg.MessageType {
		case "training_done":
			u.TrainingCount++
		case "sick_leave":
			u.HasSickLeave = true
		case "healthy":
			u.HasHealthy = true
		}
	}

	// Преобразуем в slice и сортируем по количеству тренировок (убыв.)
	var usersData []*monthlyReportUser
	for _, u := range userMap {
		usersData = append(usersData, u)
	}
	for i := 0; i < len(usersData)-1; i++ {
		for j := i + 1; j < len(usersData); j++ {
			if usersData[j].TrainingCount > usersData[i].TrainingCount {
				usersData[i], usersData[j] = usersData[j], usersData[i]
			}
		}
	}

	if len(usersData) == 0 {
		return
	}

	// Заголовок «за март 2026» — винительный падеж; родительный (марта) не ставим после «за» в такой конструкции.
	monthNamesAccusative := []string{"январь", "февраль", "март", "апрель", "май", "июнь",
		"июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}
	monthTitle := monthNamesAccusative[month.Month()-1]
	year := month.Year()

	// Формируем отчёт в стиле Fat Leopard
	var sb strings.Builder

	sb.WriteString("📊 Отчёт Fat Leopard за ")
	sb.WriteString(monthTitle)
	sb.WriteString(fmt.Sprintf(" %d\n\n", year))
	sb.WriteString("Привет, стая! 🦁\n\n")

	// Максимум в месяце
	maxTrainings := 0
	for _, u := range usersData {
		if u.TrainingCount > maxTrainings {
			maxTrainings = u.TrainingCount
		}
	}
	if maxTrainings > 0 {
		maxLabel := trainingsWordForm(maxTrainings)
		sb.WriteString(fmt.Sprintf("Максимум в месяце: %d %s\n\n", maxTrainings, maxLabel))
	}

	// Сводка по каждому: пользователь, сколько тренировок, стрик на момент отчёта
	for _, u := range usersData {
		name := u.Username
		if name == "" {
			name = fmt.Sprintf("User%d", u.UserID)
		}

		lineWorkLabel := trainingsWordForm(u.TrainingCount)
		sb.WriteString(fmt.Sprintf("• %s: %d %s", name, u.TrainingCount, lineWorkLabel))
		sb.WriteString(fmt.Sprintf(", стрик на момент отчёта: %d %s", u.StreakDays, daysWordForm(u.StreakDays)))
		sb.WriteString(fmt.Sprintf(", %d %s", u.Cups, cupsWordForm(u.Cups)))

		var flags []string
		if u.HasSickLeave {
			flags = append(flags, "больничный")
		}
		if u.HasHealthy {
			flags = append(flags, "выздоровел(а)")
		}
		if len(flags) > 0 {
			sb.WriteString(" (" + strings.Join(flags, ", ") + ")")
		}
		sb.WriteString("\n")
	}

	// Заключение от Fat Leopard
	sb.WriteString("\n")
	anyTraining := false
	for _, u := range usersData {
		if u.TrainingCount > 0 {
			anyTraining = true
			break
		}
	}
	if anyTraining {
		sb.WriteString("Я бы съел пиццу. Вы — тренировки. Продолжаем в том же духе! 💪🦁")
	} else {
		sb.WriteString("Новый месяц — новый шанс. Не дайте мне превратить вас в обед! 🦁💪")
	}

	summary := sb.String()

	reply := tgbotapi.NewMessage(chatID, summary)
	b.logger.Infof("Sending monthly report to chat %d", chatID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send monthly report: %v", err)
	} else {
		b.logger.Infof("Successfully sent monthly report to chat %d", chatID)
	}
}
