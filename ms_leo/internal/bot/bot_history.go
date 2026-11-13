package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/game/leopardmoney"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Сканирование истории чата, память ИИ и обработка нажатий кнопок.

// scanChatHistory сканирует историю сообщений за указанный период и сохраняет в БД
func (b *Bot) scanChatHistory(ctx context.Context, daysBack int) {
	b.logger.Infof("Starting chat history scan for last %d days", daysBack)

	// Вычисляем время, с которого начинать сканирование
	cutoffTime := time.Now().AddDate(0, 0, -daysBack)

	// Получаем все чаты из БД
	chatIDs, err := b.db.GetAllChatIDs()
	if err != nil {
		b.logger.Errorf("Failed to get chat IDs for history scan: %v", err)
		return
	}

	if len(chatIDs) == 0 {
		b.logger.Info("No chats found to scan")
		return
	}

	b.logger.Infof("Found %d chats to scan", len(chatIDs))

	// Получаем доступные обновления через getUpdates
	// ВАЖНО: Telegram Bot API ограничен - можно получить максимум последние 100 обновлений
	// Это НЕ покроет всю историю за 2 месяца, только последние доступные обновления
	// Для полной истории нужно использовать экспорт данных или Telegram Client API (MTProto)
	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60
	u.Limit = 100 // Максимум доступных обновлений

	b.logger.Warnf("Telegram Bot API limitation: can only get last ~100 updates, not full history. This won't cover 2 months of messages.")

	updates, err := b.api.GetUpdates(u)
	if err != nil {
		b.logger.Errorf("Failed to get updates for history scan: %v", err)
		return
	}

	b.logger.Infof("Got %d updates from Telegram API (limited by Bot API)", len(updates))

	processedCount := 0
	savedCount := 0
	skippedTooOld := 0
	skippedNotTargetChat := 0
	skippedAlreadyExists := 0

	for _, update := range updates {
		select {
		case <-ctx.Done():
			b.logger.Info("History scan cancelled")
			return
		default:
		}

		if update.Message == nil {
			continue
		}

		msg := update.Message

		// Проверяем, что сообщение в нужном периоде
		msgTime := time.Unix(int64(msg.Date), 0)
		if msgTime.Before(cutoffTime) {
			skippedTooOld++
			continue // Слишком старое сообщение
		}

		// Проверяем, что это наш чат
		isTargetChat := false
		for _, chatID := range chatIDs {
			if msg.Chat.ID == chatID {
				isTargetChat = true
				break
			}
		}

		if !isTargetChat {
			skippedNotTargetChat++
			continue // Не наш чат
		}

		// Проверяем, не сохранено ли уже это сообщение
		existingMessages, err := b.db.GetUserMessages(msg.From.ID, msg.Chat.ID, msgTime.Add(-1*time.Hour), msgTime.Add(time.Hour))
		if err == nil {
			alreadyExists := false
			for _, existing := range existingMessages {
				if existing.MessageText == msg.Text && existing.CreatedAt.Unix() == int64(msg.Date) {
					alreadyExists = true
					break
				}
			}
			if alreadyExists {
				skippedAlreadyExists++
				continue // Уже сохранено
			}
		}

		// Определяем тип сообщения
		text := msg.Text
		if text == "" && msg.Caption != "" {
			text = msg.Caption
		}

		messageType := "general"
		textLower := strings.ToLower(text)
		if leopardmoney.IsTrainingReportLine(text) {
			messageType = "training_done"
		} else if strings.Contains(textLower, "#sick_leave") {
			messageType = "sick_leave"
		} else if strings.Contains(textLower, "#healthy") {
			messageType = "healthy"
		} else if msg.IsCommand() {
			messageType = "command"
		}

		// Получаем username
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

		// Сохраняем сообщение
		userMsg := &domain.UserMessage{
			UserID:      msg.From.ID,
			ChatID:      msg.Chat.ID,
			Username:    username,
			MessageText: text,
			MessageType: messageType,
			CreatedAt:   msgTime,
		}

		if err := b.db.SaveUserMessage(userMsg); err != nil {
			b.logger.Errorf("Failed to save scanned message: %v", err)
		} else {
			savedCount++
		}

		processedCount++
	}

	b.logger.Infof("History scan completed:")
	b.logger.Infof("  - Processed: %d messages", processedCount)
	b.logger.Infof("  - Saved: %d new messages", savedCount)
	b.logger.Infof("  - Skipped (too old): %d", skippedTooOld)
	b.logger.Infof("  - Skipped (not target chat): %d", skippedNotTargetChat)
	b.logger.Infof("  - Skipped (already exists): %d", skippedAlreadyExists)
	b.logger.Warnf("NOTE: Telegram Bot API only provides last ~100 updates. Full history requires data export or MTProto client.")
}

// handleScanHistory обрабатывает команду /scan_history для ручного запуска сканирования
func (b *Bot) handleScanHistory(msg *tgbotapi.Message) {
	// Проверяем, что команда от одного из админов из env.
	if !b.config.IsAdminTelegramUser(msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ This command is for bot admins only")
		b.api.Send(reply)
		return
	}

	// Парсим количество дней (по умолчанию 60)
	args := msg.CommandArguments()
	daysBack := 60
	if args != "" {
		if parsedDays, err := strconv.Atoi(strings.TrimSpace(args)); err == nil && parsedDays > 0 {
			daysBack = parsedDays
		}
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🔄 Scanning chat history for the last %d days...\n\n⚠️ IMPORTANT: Telegram Bot API can only fetch the last ~100 available updates, not full history.\n\nFor ~2 months of history:\n1. Export data from Telegram (Settings → Privacy → Export Telegram data)\n2. Or use Telegram Client API (MTProto) — a heavier integration\n\nThe bot will try available updates, but that won't cover all history.", daysBack))
	b.api.Send(reply)

	// Запускаем сканирование в отдельной горутине
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		b.scanChatHistory(ctx, daysBack)

		// Отправляем отчет
		finalReply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ History scan finished (last %d days)", daysBack))
		b.api.Send(finalReply)
	}()
}

// handleAIMemory обрабатывает команду /ai_memory или /memory для показа информации о долгосрочной памяти AI
func (b *Bot) handleAIMemory(msg *tgbotapi.Message) {
	text := `🧠 AI long-term memory

❌ AI doesn't know anything about you yet.

💡 How it works:
1️⃣ Open a chat with AI: 🤖 Neural nets → 🧠 Text LLMs
2️⃣ Tell the model about yourself
3️⃣ AI saves important facts automatically
4️⃣ Memory is reused in future chats

📝 Example:
"Hi! I'm Alex, a Python developer. I'm building a FastAPI e-commerce project."

✅ AI may remember: name, role, project, tech stack

⚠️ Facts are saved during AI chats, not in this screen`

	// Создаем inline клавиатуру с кнопкой "Назад"
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Back", "back_to_menu"),
		),
	)

	reply := tgbotapi.NewMessage(msg.Chat.ID, text)
	reply.ReplyMarkup = keyboard
	b.api.Send(reply)
}

// handleCallbackQuery обрабатывает нажатия на inline кнопки
func (b *Bot) handleCallbackQuery(callback *tgbotapi.CallbackQuery) {
	data := callback.Data
	msg := callback.Message

	if strings.HasPrefix(data, "admin_") {
		b.handleAdminCallbackQuery(callback)
		return
	}

	// Вход в десктопное приложение — подтверждение из чата.
	if b.handleDesktopLoginCallback(callback) {
		return
	}

	switch data {
	case paywallCallbackResendInvoice:
		b.handlePaywallResendInvoiceCallback(callback)
		return
	case paywallCallbackReturnToPack:
		b.handlePaywallReturnToPackCallback(callback)
		return
	case paywallCallbackPayStars:
		b.handlePaywallPayStarsCallback(callback)
		return
	case paywallCallbackPayYookassa:
		b.handlePaywallPayYookassaCallback(callback)
		return
	case paywallCallbackPayProvider:
		b.handlePaywallPayProviderCallback(callback)
		return
	case paywallCallbackBackToMethods:
		b.handlePaywallBackToMethodsCallback(callback)
		return
	case botSupportCallbackStart, botSupportCallbackCancel:
		b.handleBotSupportCallback(callback)
		return
	case "back_to_menu":
		// Удаляем сообщение и возвращаемся в меню
		deleteMsg := tgbotapi.NewDeleteMessage(msg.Chat.ID, msg.MessageID)
		b.api.Send(deleteMsg)

		// Отправляем главное меню (можно настроить по своему усмотрению)
		menuText := `🦁 Main menu

Commands:
/help - Help
/top - Top members by cups
/cups - Your cup stats

💪 Log workouts in the Fat Leopard mini app (the «+» button)`

		reply := tgbotapi.NewMessage(msg.Chat.ID, menuText)
		b.api.Send(reply)

		// Отвечаем на callback, чтобы убрать загрузку на кнопке
		callbackConfig := tgbotapi.NewCallback(callback.ID, "")
		b.api.Request(callbackConfig)
	default:
		// Неизвестный callback
		b.logger.Warnf("Unknown callback data: %s", data)
		callbackConfig := tgbotapi.NewCallback(callback.ID, "")
		b.api.Request(callbackConfig)
	}
}
