package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/domain"
	"leo-bot/internal/logger"
	"leo-bot/internal/usecase/sickleave"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api                  *tgbotapi.BotAPI
	db                   *database.Database
	logger               logger.Logger
	config               *config.Config
	timers               map[int64]*domain.TimerInfo
	aiClient             *ai.OpenRouterClient
	sickApprovalWatchers map[int64]chan struct{}
	sickApprovalMutex    sync.Mutex
	sickLeaveEvaluator   *sickleave.Evaluator
}

func New(cfg *config.Config, db *database.Database, log logger.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.APIToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	// Создаем таблицы в базе данных
	if err := db.CreateTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}

	// Создаем клиент OpenRouter для ИИ
	var aiClient *ai.OpenRouterClient
	if cfg.OpenRouterAPIKey != "" {
		aiClient = ai.NewOpenRouterClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel, log)
		log.Infof("OpenRouter AI client initialized with model: %s", cfg.OpenRouterModel)
	} else {
		log.Warn("OpenRouter API key not provided, AI features will be disabled")
	}

	return &Bot{
		api:                  api,
		db:                   db,
		logger:               log,
		config:               cfg,
		timers:               make(map[int64]*domain.TimerInfo),
		aiClient:             aiClient,
		sickApprovalWatchers: make(map[int64]chan struct{}),
		sickLeaveEvaluator:   sickleave.NewEvaluator(aiClient, log),
	}, nil
}

func (b *Bot) Start(ctx context.Context) error {
	b.logger.Info("Starting bot...")

	// Восстанавливаем таймеры из базы данных
	if err := b.recoverTimersFromDatabase(); err != nil {
		b.logger.Errorf("Failed to recover timers from database: %v", err)
		// Не останавливаем бота, просто логируем ошибку
	}

	b.restoreSickApprovalWatchers()

	// Сканируем историю сообщений при старте, если включено в конфиге
	if b.config.ScanHistoryOnStart {
		hasMessages, err := b.db.HasAnyMessages()
		if err == nil && !hasMessages {
			b.logger.Info("SCAN_HISTORY_ON_START=true and database is empty, starting initial history scan...")
			go b.scanChatHistory(ctx, 60) // Сканируем за последние 60 дней
		} else if hasMessages {
			b.logger.Info("Messages already exist in database, skipping history scan. New messages will be saved automatically.")
		}
	} else {
		b.logger.Info("SCAN_HISTORY_ON_START=false, skipping history scan. New messages will be saved automatically.")
	}

	// Запускаем ежемесячную сводку (1-го числа 16:20) и «мудрость дня» (ежедневно 04:20)
	go b.startDailySummaryScheduler(ctx)
	go b.startDailyWisdomScheduler(ctx)

	u := tgbotapi.NewUpdate(0)
	u.Timeout = 60

	updates := b.api.GetUpdatesChan(u)

	for {
		select {
		case update := <-updates:
			go b.handleUpdate(update)
		case <-ctx.Done():
			b.logger.Info("Bot stopped")
			return nil
		}
	}
}

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	// Обрабатываем callback queries (нажатия на inline кнопки)
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(update.CallbackQuery)
		return
	}

	// Обрабатываем реакции на сообщения (смайлики)
	// Примечание: telegram-bot-api v5.5.1 пока не поддерживает MessageReaction в Update
	// Если библиотека обновится с поддержкой реакций, можно будет обработать здесь
	// if update.MessageReaction != nil {
	// 	b.handleMessageReaction(update.MessageReaction)
	// 	return
	// }

	// Обрабатываем добавление новых участников
	if update.Message != nil && len(update.Message.NewChatMembers) > 0 {
		b.handleNewChatMembers(update.Message)
		return
	}

	if update.Message == nil {
		return
	}

	msg := update.Message
	b.logger.Infof("Received message from %d: %s", msg.From.ID, msg.Text)

	// Обрабатываем команды
	if msg.IsCommand() {
		// Сохраняем команду в БД для контекста
		text := msg.Text
		if text == "" && msg.Caption != "" {
			text = msg.Caption
		}
		if text != "" {
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

			userMsg := &domain.UserMessage{
				UserID:      msg.From.ID,
				ChatID:      msg.Chat.ID,
				Username:    username,
				MessageText: text,
				MessageType: "command",
			}
			if err := b.db.SaveUserMessage(userMsg); err != nil {
				b.logger.Errorf("Failed to save user command: %v", err)
			}
		}

		b.handleCommand(msg)
		return
	}

	// Обрабатываем обычные сообщения
	b.handleMessage(msg)
}

func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	command := msg.Command()
	_ = msg.CommandArguments() // Игнорируем аргументы пока

	switch command {
	case "start":
		b.handleStart(msg)
	case "start_timer":
		b.handleStartTimer(msg)
	case "help":
		b.handleHelp(msg)
	case "db":
		b.handleDB(msg)
	case "top":
		b.handleTop(msg)
	case "points":
		b.handlePoints(msg)
	case "scan_history":
		b.handleScanHistory(msg)
	case "ai_memory", "memory":
		b.handleAIMemory(msg)
	case "cups":
		b.handleCups(msg)
	case "set_exempt":
		b.handleSetExempt(msg)
	case "remove_exempt":
		b.handleRemoveExempt(msg)
	case "list_users":
		b.handleListUsers(msg)
	case "send_to_chat":
		b.handleSendToChat(msg)
	case "announce_ai":
		b.handleAnnounceAI(msg)
	case "send_wisdom":
		// Ручной запуск рассылки мудрости дня
		b.generateAndSendDailyWisdom()
	case "audit_last24":
		b.auditLast24h()
	default:
		b.logger.Warnf("Unknown command: %s", command)
	}
}

func (b *Bot) handleNewChatMembers(msg *tgbotapi.Message) {
	// Отправляем приветственное сообщение для каждого нового участника
	for _, newMember := range msg.NewChatMembers {
		// Пропускаем ботов
		if newMember.IsBot {
			continue
		}

		// Получаем никнейм пользователя
		username := ""
		if newMember.UserName != "" {
			username = "@" + newMember.UserName
		} else if newMember.FirstName != "" {
			username = newMember.FirstName
			if newMember.LastName != "" {
				username += " " + newMember.LastName
			}
		} else {
			username = fmt.Sprintf("User%d", newMember.ID)
		}

		// Отправляем приветственное сообщение
		b.sendWelcomeMessage(msg.Chat.ID, username, newMember.ID)
	}
}

func (b *Bot) sendWelcomeMessage(chatID int64, username string, userID int64) {
	// Создаем запись пользователя в БД с запущенным таймером
	timerStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	messageLog := &domain.MessageLog{
		UserID:          userID,
		ChatID:          chatID,
		Username:        username,
		Calories:        0,
		StreakDays:      0,
		CupsEarned:      0,
		LastMessage:     timerStartTime,
		HasTrainingDone: false,
		HasSickLeave:    false,
		HasHealthy:      false,
		IsDeleted:       false,
		TimerStartTime:  &timerStartTime, // Сразу устанавливаем время начала таймера
	}

	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to save new user to database: %v", err)
	} else {
		b.logger.Infof("Successfully saved new user %s (ID: %d) to database with timer start time", username, userID)
	}

	// Создаем приветственное сообщение с упоминанием пользователя
	welcomeText := fmt.Sprintf(`%s, добро пожаловать в стаю! 🦁

Я ваш хладнокровный тренер, который следит за тренировками всегда, я все вижу и не оставляю в стае тех, кто не занимается больше 7 дней!

💪 Отчеты о тренировке:
• #training_done — Отправить отчет о тренировке

🏥 Больничный:
• #sick_leave — Взять больничный (приостанавливает таймер)
• #healthy — Выздороветь (возобновляет таймер)

🔄 Обмен:
• #change — Обменять калории на кубки (100 калорий = 42 кубка)

⏰ Как я слежу за тренировками:
• Таймер уже запущен! У тебя есть 7 дней на первую тренировку
• При получении #training_done таймер перезапускается на 7 дней
• Через 6 дней без #training_done - предупреждение
• Через 7 дней без #training_done - удаление из чата

🏆 Награды за тренировки:
• 🏆 За каждую тренировку = 1 КУБОК! 🏆
• 🏆 7 дней подряд = 42 КУБКА! 🏆
• 🏆🏆 14 дней подряд = 42 КУБКА! 🏆🏆
• 🏆🏆🏆 21 день подряд = 42 КУБКА! 🏆🏆🏆
• 🏆🏆🏆 30 дней подряд = 420 КУБКОВ! 🏆🏆🏆
• 🏆🏆🏆🏆 42 дня подряд = 42 КУБКА! 🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆 50 дней подряд = 42 КУБКА! 🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆 60 дней подряд = 420 КУБКОВ! 🏆🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆🏆🏆 90 дней подряд = 420 КУБКОВ! 🏆🏆🏆🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆 100 дней подряд = 4200 КУБКОВ! 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

📋 Правила:
• Отчётом считается любое сообщение с тегом #training_done
• Если заболели — отправь #sick_leave
• После выздоровления — отправь #healthy
• Через 6 дней без отчёта — предупреждение
• Через 7 дней без отчёта — удаление из чата

🎯 Начни прямо сейчас — отправь #training_done!`, username)

	// Отправляем сообщение
	reply := tgbotapi.NewMessage(chatID, welcomeText)

	b.logger.Infof("Sending welcome message to chat %d for new user %s (ID: %d)", chatID, username, userID)
	_, err := b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send welcome message: %v", err)
	} else {
		b.logger.Infof("Successfully sent welcome message to chat %d for new user %s", chatID, username)
	}

	// Запускаем таймер для нового пользователя
	b.startTimer(userID, chatID, username)
}

func (b *Bot) handleMessage(msg *tgbotapi.Message) {
	// Проверяем наличие хештегов в тексте или подписи
	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}

	b.tryHandleSickApprovalReply(msg, text)

	// Проверяем, обращается ли пользователь к боту (для вопросов к ИИ)
	// 1. Упоминание через @ в тексте
	// 2. Ответ на сообщение бота (reply)
	// 3. Выбор бота из списка участников (bot_command или просто упоминание)
	shouldHandleAI := false

	// Проверяем упоминание через @ в тексте
	if msg.Entities != nil && text != "" {
		for _, entity := range msg.Entities {
			if entity.Type == "mention" {
				mentionText := ""
				if entity.Offset+entity.Length <= len(text) {
					mentionText = text[entity.Offset : entity.Offset+entity.Length]
				}

				// Проверяем несколько вариантов имени бота
				botUsername := b.api.Self.UserName
				if botUsername == "" {
					// Если UserName пустой, получаем из текста упоминания
					botUsername = strings.TrimPrefix(mentionText, "@")
				}

				// Проверяем различные форматы упоминания
				if strings.EqualFold(mentionText, "@"+botUsername) ||
					strings.EqualFold(mentionText, botUsername) ||
					strings.Contains(strings.ToLower(text), "@"+strings.ToLower(botUsername)) ||
					strings.Contains(strings.ToLower(text), strings.ToLower(botUsername)+" ") {
					shouldHandleAI = true
					b.logger.Infof("Bot mention detected: %s in message: %s", mentionText, text)
					break
				}
			}
		}
	}

	// Проверяем ответ на сообщение бота
	if !shouldHandleAI && msg.ReplyToMessage != nil {
		if msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.IsBot &&
			msg.ReplyToMessage.From.ID == b.api.Self.ID {
			shouldHandleAI = true
			b.logger.Infof("Reply to bot message detected")
		}
	}

	// Если обращение к боту обнаружено и есть текст вопроса
	// НО сначала сохраняем сообщение в БД для контекста
	if shouldHandleAI && text != "" {
		// Сохраняем вопрос в БД перед обработкой
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

		userMsg := &domain.UserMessage{
			UserID:      msg.From.ID,
			ChatID:      msg.Chat.ID,
			Username:    username,
			MessageText: text,
			MessageType: "question", // Отмечаем как вопрос к ИИ
		}
		if err := b.db.SaveUserMessage(userMsg); err != nil {
			b.logger.Errorf("Failed to save user question: %v", err)
		}

		b.handleAIQuestion(msg, text)
		return
	}

	hasTrainingDone := strings.Contains(strings.ToLower(text), "#training_done")
	hasSickLeave := strings.Contains(strings.ToLower(text), "#sick_leave")
	hasHealthy := strings.Contains(strings.ToLower(text), "#healthy")
	hasChange := strings.Contains(strings.ToLower(text), "#change")

	// Получаем никнейм пользователя
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

	// Сохраняем сообщение в БД для RAG контекста
	if text != "" {
		messageType := "general"
		if hasTrainingDone {
			messageType = "training_done"
		} else if hasSickLeave {
			messageType = "sick_leave"
		} else if hasHealthy {
			messageType = "healthy"
		}

		userMsg := &domain.UserMessage{
			UserID:      msg.From.ID,
			ChatID:      msg.Chat.ID,
			Username:    username,
			MessageText: text,
			MessageType: messageType,
		}
		if err := b.db.SaveUserMessage(userMsg); err != nil {
			b.logger.Errorf("Failed to save user message: %v", err)
		}
	}

	// Получаем существующие данные пользователя
	existingLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		// Если пользователя нет в БД, создаем новую запись
		timerStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
		messageLog := &domain.MessageLog{
			UserID:            msg.From.ID,
			ChatID:            msg.Chat.ID,
			Username:          username,
			Calories:          0,
			StreakDays:        0,
			CalorieStreakDays: 0,
			CupsEarned:        0,
			LastMessage:       timerStartTime,
			HasTrainingDone:   hasTrainingDone,
			HasSickLeave:      false,
			HasHealthy:        false,
			IsDeleted:         false,
			TimerStartTime:    &timerStartTime,
		}

		if err := b.db.SaveMessageLog(messageLog); err != nil {
			b.logger.Errorf("Failed to save message log: %v", err)
		} else {
			b.logger.Infof("Initialized timer state for new user %d (%s) from message", msg.From.ID, username)
			b.startTimer(msg.From.ID, msg.Chat.ID, username)
		}
	} else {
		// Обновляем только необходимые поля, сохраняя streak данные
		existingLog.Username = username
		existingLog.LastMessage = utils.FormatMoscowTime(utils.GetMoscowTime())
		existingLog.HasTrainingDone = hasTrainingDone
		existingLog.IsDeleted = false

		if err := b.db.SaveMessageLog(existingLog); err != nil {
			b.logger.Errorf("Failed to update message log: %v", err)
		}
	}

	// Обрабатываем хештеги
	if hasTrainingDone {
		b.handleTrainingDone(msg)
	} else if hasSickLeave {
		b.handleSickLeave(msg)
	} else if hasHealthy {
		b.handleHealthy(msg)
	} else if hasChange {
		b.handleChange(msg)
	}
}

func (b *Bot) handleTrainingDone(msg *tgbotapi.Message) {
	b.processTrainingDone(msg)
}

func (b *Bot) handleTrainingDoneLegacy(msg *tgbotapi.Message) {
	b.processTrainingDone(msg)
}

func (b *Bot) handleChange(msg *tgbotapi.Message) {
	// Получаем никнейм пользователя
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

	// Получаем текущие данные пользователя
	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка получения данных пользователя")
		b.api.Send(reply)
		return
	}

	// Получаем текущие калории и кубки
	currentCalories := messageLog.Calories
	currentCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get user cups: %v", err)
		currentCups = 0
	}

	// Курс обмена: 100 калорий = 42 кубка
	exchangeRate := 100
	cupsPerExchange := 42
	exchangesCanMake := currentCalories / exchangeRate

	if exchangesCanMake == 0 {
		// Недостаточно калорий для обмена
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("💪 %s, у тебя %d калорий\n\n🔄 Для обмена нужно минимум %d калорий\n🏆 За %d калорий можно получить %d кубков\n\n⏰ Пока рано! Еще потренируйся!\n\n🎯 Продолжай тренироваться и накапливай калории!", username, currentCalories, exchangeRate, exchangeRate, cupsPerExchange))
		b.logger.Infof("Sending insufficient calories message to chat %d", msg.Chat.ID)
		_, err = b.api.Send(reply)
		if err != nil {
			b.logger.Errorf("Failed to send insufficient calories message: %v", err)
		} else {
			b.logger.Infof("Successfully sent insufficient calories message to chat %d", msg.Chat.ID)
		}
		return
	}

	// Выполняем обмен (только полные обмены)
	caloriesToSpend := exchangesCanMake * exchangeRate
	cupsToAdd := exchangesCanMake * cupsPerExchange

	// Списываем калории
	if err := b.db.AddCalories(msg.From.ID, msg.Chat.ID, -caloriesToSpend); err != nil {
		b.logger.Errorf("Failed to spend calories: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при списании калорий")
		b.api.Send(reply)
		return
	}

	// Добавляем кубки
	if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, cupsToAdd); err != nil {
		b.logger.Errorf("Failed to add cups: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при добавлении кубков")
		b.api.Send(reply)
		return
	}

	// Обмен калорий НЕ сбрасывает streak_days
	// streak_days нужен для подсчета серии дней для получения кубков (7 дней = 42 кубка)
	// Обмен калорий - это просто обмен накопленных калорий на кубки

	// Сбрасываем calorie_streak_days после обмена калорий
	if err := b.db.ResetCalorieStreak(msg.From.ID, msg.Chat.ID); err != nil {
		b.logger.Errorf("Failed to reset calorie streak: %v", err)
	} else {
		b.logger.Infof("Successfully reset calorie streak after exchange")
	}

	// Получаем обновленные значения
	newCalories := currentCalories - caloriesToSpend
	newCups := currentCups + cupsToAdd

	// Отправляем сообщение об успешном обмене
	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🔄 Обмен выполнен! 💪\n\n%s сожжено 🔥 %d калорий → 🏆 %d кубка\n\n📊 Твой баланс:\n🔥 Калории: %d\n🏆 Кубки: %d\n\n💡 Курс: %d калорий = %d кубка", username, caloriesToSpend, cupsToAdd, newCalories, newCups, exchangeRate, cupsPerExchange))

	b.logger.Infof("Sending exchange success message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send exchange success message: %v", err)
	} else {
		b.logger.Infof("Successfully sent exchange success message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleStartTimer(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Только администраторы или владелец могут использовать эту команду!")
		b.api.Send(reply)
		return
	}

	// Получаем всех пользователей в чате
	users, err := b.db.GetUsersByChatID(msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get users: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении пользователей")
		b.api.Send(reply)
		return
	}

	// Запускаем таймеры для всех пользователей
	startedCount := 0
	for _, user := range users {
		if b.isUserInChat(msg.Chat.ID, user.UserID) {
			b.startTimer(user.UserID, msg.Chat.ID, "")
			startedCount++
		}
	}

	// Отправляем отчет
	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🐆 Fat Leopard активирован!\n\n⏱️ Запущено таймеров: %d\n⏰ Время: 7 дней\n💪 Действие: Отправь #training_done", startedCount))

	b.logger.Infof("Sending start timer message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send start timer message: %v", err)
	} else {
		b.logger.Infof("Successfully sent start timer message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	helpText := `🤖 LeoPoacherBot - Команды:

📝 Команды администратора:
• /start_timer — Запустить таймеры для всех пользователей
• /db — Показать статистику БД
• /help — Показать это сообщение

🤖 ИИ-помощник:
• Отметьте меня @LeoPoacherBot в сообщении для общения
• Или ответьте (reply) на любое мое сообщение
• Я могу давать советы, показывать статистику и мотивировать!

🏆 Команды пользователей:
• /top — Показать топ пользователей по калориям
• /points — Показать ваши калории
• /cups — Показать ваши заработанные кубки

💪 Отчеты о тренировке:
• #training_done — Отправить отчет о тренировке

🏥 Больничный:
• #sick_leave — Взять больничный (приостанавливает таймер)
• #healthy — Выздороветь (возобновляет таймер)

🔄 Обмен:
• #change — Обменять калории на кубки (100 калорий = 42 кубка)

⏰ Как работает бот:
• При добавлении бота в чат запускаются таймеры для всех участников
• При получении #training_done таймер перезапускается на 7 дней
• Через 6 дней без #training_done - предупреждение
• Через 7 дней без #training_done - удаление из чата
• 🏆 За каждую тренировку = 1 КУБОК! 🏆
• 🏆 7 дней подряд = 42 КУБКА! 🏆
• 🏆🏆 14 дней подряд = 42 КУБКА! 🏆🏆
• 🏆🏆🏆 21 день подряд = 42 КУБКА! 🏆🏆🏆
• 🏆🏆🏆 30 дней подряд = 420 КУБКОВ! 🏆🏆🏆
• 🏆🏆🏆🏆 42 дня подряд = 42 КУБКА! 🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆 50 дней подряд = 42 КУБКА! 🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆 60 дней подряд = 420 КУБКОВ! 🏆🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆🏆🏆 90 дней подряд = 420 КУБКОВ! 🏆🏆🏆🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆 100 дней подряд = 4200 КУБКОВ! 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

📋 Правила:
• Отчётом считается любое сообщение с тегом #training_done
• Если заболели — отправь #sick_leave
• После выздоровления — отправь #healthy
• Через 6 дней без отчёта — предупреждение
• Через 7 дней без отчёта — удаление из чата

Оставайся активным и не становись жирным леопардом! 🦁`

	reply := tgbotapi.NewMessage(msg.Chat.ID, helpText)

	b.logger.Infof("Sending help message to chat %d", msg.Chat.ID)
	_, err := b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send help message: %v", err)
	} else {
		b.logger.Infof("Successfully sent help message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleStart(msg *tgbotapi.Message) {
	welcomeText := `🦁 **Добро пожаловать в LeoPoacherBot!** 🦁

💪 **Этот бот поможет вам оставаться в форме и не стать жирным леопардом!**

📋 **Основные команды:**
• /start — Показать это приветствие
• /help — Показать полную справку
• /start_timer — Запустить таймеры (только для администраторов)

💪 **Отчеты о тренировке:**
• #training_done — Отправить отчет о тренировке

🏥 **Больничный:**
• #sick_leave — Взять больничный (приостанавливает таймер)
• #healthy — Выздороветь (возобновляет таймер)

🔄 **Обмен:**
• #change — Обменять калории на кубки (10 калорий = 1 кубок)

⏰ **Как это работает:**
• При добавлении бота в чат запускаются таймеры для всех участников
• Каждый отчет с #training_done перезапускает таймер на 7 дней
• Через 6 дней без отчета — предупреждение
• Через 7 дней без отчета — удаление из чата
• 🏆 За каждую тренировку = 1 КУБОК! 🏆
• 🏆 7 дней подряд = 42 КУБКА! 🏆
• 🏆🏆 14 дней подряд = 42 КУБКА! 🏆🏆
• 🏆🏆🏆 21 день подряд = 42 КУБКА! 🏆🏆🏆
• 🏆🏆🏆 30 дней подряд = 420 КУБКОВ! 🏆🏆🏆
• 🏆🏆🏆🏆 42 дня подряд = 42 КУБКА! 🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆 50 дней подряд = 42 КУБКА! 🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆 60 дней подряд = 420 КУБКОВ! 🏆🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆🏆🏆 90 дней подряд = 420 КУБКОВ! 🏆🏆🏆🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆 100 дней подряд = 4200 КУБКОВ! 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 **Начни прямо сейчас — отправь #training_done!**`

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcomeText)

	b.logger.Infof("Sending start message to chat %d", msg.Chat.ID)
	_, err := b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send start message: %v", err)
	} else {
		b.logger.Infof("Successfully sent start message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleDB(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Только администраторы или владелец могут использовать эту команду!")
		b.api.Send(reply)
		return
	}

	// Получаем статистику
	stats, err := b.db.GetDatabaseStats()
	if err != nil {
		b.logger.Errorf("Failed to get database stats: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных")
		b.api.Send(reply)
		return
	}

	// Формируем отчет
	report := fmt.Sprintf("📊 Статистика БД:\n\n👥 Всего пользователей: %v\n✅ С training_done: %v\n🏥 На больничном: %v\n💪 Выздоровели: %v",
		stats["total_users"], stats["training_done"], stats["sick_leave"], stats["healthy"])

	reply := tgbotapi.NewMessage(msg.Chat.ID, report)

	b.logger.Infof("Sending DB stats message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send DB stats message: %v", err)
	} else {
		b.logger.Infof("Successfully sent DB stats message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleTop(msg *tgbotapi.Message) {
	// Получаем топ пользователей
	topUsers, err := b.db.GetTopUsers(msg.Chat.ID, 10)
	if err != nil {
		b.logger.Errorf("Failed to get top users: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных")
		b.api.Send(reply)
		return
	}

	if len(topUsers) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "🏆 **Топ пользователей:**\n\n📊 Пока нет данных о тренировках")
		reply.ParseMode = "Markdown"
		b.api.Send(reply)
		return
	}

	// Формируем топ
	topText := "🏆 Топ пользователей по очкам:\n\n"
	for i, user := range topUsers {
		emoji := "🥇"
		if i == 1 {
			emoji = "🥈"
		} else if i == 2 {
			emoji = "🥉"
		} else {
			emoji = fmt.Sprintf("%d️⃣", i+1)
		}
		topText += fmt.Sprintf("%s %s - %d калорий\n", emoji, user.Username, user.Calories)
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, topText)

	b.logger.Infof("Sending top users message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send top users message: %v", err)
	} else {
		b.logger.Infof("Successfully sent top users message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handlePoints(msg *tgbotapi.Message) {
	// Получаем калории пользователя
	calories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get user calories: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных")
		b.api.Send(reply)
		return
	}

	// Получаем никнейм пользователя
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

	// Формируем сообщение
	caloriesText := fmt.Sprintf("🔥 Ваши калории:\n\n👤 %s\n🎯 Всего сожжено калорий: %d\n\n💡 Отправляйте #training_done для сжигания калорий!", username, calories)

	reply := tgbotapi.NewMessage(msg.Chat.ID, caloriesText)

	b.logger.Infof("Sending calories message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send calories message: %v", err)
	} else {
		b.logger.Infof("Successfully sent calories message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleCups(msg *tgbotapi.Message) {
	// Получаем кубки пользователя
	cups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get user cups: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных")
		b.api.Send(reply)
		return
	}

	// Получаем никнейм пользователя
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

	// Формируем сообщение в зависимости от количества кубков
	var cupsText string
	if cups > 420 {
		cupsText = fmt.Sprintf("🌟⚡ СУПЕР-УРОВЕНЬ! ⚡🌟\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n🎊 ВСЕ ОЖИДАНИЯ ПРЕВЗОЙДЕНЫ! 🎊\n\n🦁 Fat Leopard в полном восторге!\n💪 Ты не просто чемпион - ты СУПЕР-ЧЕМПИОН!\n🔥 Твоя сила и мощь безграничны!\n⭐️ Ты вдохновляешь всю стаю!\n👑 Мотивация не верит, что такое бывает!\n🌟 Ты сияешь ярче всех!\n\n🎯 Продолжай в том же духе, супер-леопард!", username, cups)
	} else if cups >= 420 {
		cupsText = fmt.Sprintf("🎊 ПОЗДРАВЛЯЕМ! 🎊\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n🏆 ТЫ ДОСТИГ ЦЕЛИ РОЗЫГРЫША!\n🎁 Участвуешь в розыгрыше футболки Fat Leopard!\n💪 Ты настоящий чемпион!\n🔥 Продолжай тренироваться!", username, cups)
	} else {
		cupsText = fmt.Sprintf("🏆 Ваши кубки:\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n💡 Отправляйте #training_done для получения кубков!\n\n🎊 Розыгрыш футболки Fat Leopard при достижении 420 кубков!", username, cups)
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsText)

	b.logger.Infof("Sending cups message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send cups message: %v", err)
	} else {
		b.logger.Infof("Successfully sent cups message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleSetExempt(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Только администраторы или владелец могут использовать эту команду!")
		b.api.Send(reply)
		return
	}

	// Парсим аргументы команды
	args := strings.Fields(msg.Text)
	if len(args) < 2 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Использование: /set_exempt @username")
		b.api.Send(reply)
		return
	}

	// Извлекаем username из аргумента
	searchUsername := args[1]

	// Логируем поиск для отладки
	b.logger.Infof("Searching for user: '%s' in chat %d", searchUsername, msg.Chat.ID)

	// Находим пользователя по username (функция сама обработает разные форматы)
	userID, err := b.db.GetUserIDByUsername(searchUsername, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get user ID by username '%s': %v", searchUsername, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Пользователь %s не найден в базе данных", searchUsername))
		b.api.Send(reply)
		return
	}

	b.logger.Infof("Found user ID %d for username '%s'", userID, searchUsername)

	// Устанавливаем исключение
	messageLog, err := b.db.GetMessageLog(userID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных пользователя")
		b.api.Send(reply)
		return
	}

	messageLog.IsExemptFromDeletion = true
	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to save message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при сохранении данных")
		b.api.Send(reply)
		return
	}

	// Отменяем таймер если он активен
	b.cancelTimer(userID)

	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Пользователь %s исключен из правила удаления за неактивность", messageLog.Username))
	b.api.Send(reply)
}

func (b *Bot) handleRemoveExempt(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Только администраторы или владелец могут использовать эту команду!")
		b.api.Send(reply)
		return
	}

	// Парсим аргументы команды
	args := strings.Fields(msg.Text)
	if len(args) < 2 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Использование: /remove_exempt @username")
		b.api.Send(reply)
		return
	}

	// Извлекаем username из аргумента
	searchUsername := args[1]

	// Логируем поиск для отладки
	b.logger.Infof("Searching for user: '%s' in chat %d", searchUsername, msg.Chat.ID)

	// Находим пользователя по username (функция сама обработает разные форматы)
	userID, err := b.db.GetUserIDByUsername(searchUsername, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get user ID by username '%s': %v", searchUsername, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Пользователь %s не найден в базе данных", searchUsername))
		b.api.Send(reply)
		return
	}

	b.logger.Infof("Found user ID %d for username '%s'", userID, searchUsername)

	// Убираем исключение
	messageLog, err := b.db.GetMessageLog(userID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных пользователя")
		b.api.Send(reply)
		return
	}

	messageLog.IsExemptFromDeletion = false
	if err := b.db.SaveMessageLog(messageLog); err != nil {
		b.logger.Errorf("Failed to save message log: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при сохранении данных")
		b.api.Send(reply)
		return
	}

	// Запускаем таймер для пользователя
	b.startTimer(userID, msg.Chat.ID, messageLog.Username)

	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Пользователь %s больше не исключен из правила удаления. Таймер запущен.", messageLog.Username))
	b.api.Send(reply)
}

func (b *Bot) handleListUsers(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Только администраторы или владелец могут использовать эту команду!")
		b.api.Send(reply)
		return
	}

	// Получаем всех пользователей в чате
	users, err := b.db.GetUsersByChatID(msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get users: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении списка пользователей")
		b.api.Send(reply)
		return
	}

	if len(users) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "📝 В чате нет пользователей в базе данных")
		b.api.Send(reply)
		return
	}

	// Формируем список пользователей
	var userList strings.Builder
	userList.WriteString("📋 Список пользователей в чате:\n\n")

	for i, user := range users {
		exemptStatus := "❌"
		if user.IsExemptFromDeletion {
			exemptStatus = "✅"
		}

		userList.WriteString(fmt.Sprintf("%d. %s (ID: %d) %s\n",
			i+1, user.Username, user.UserID, exemptStatus))
	}

	userList.WriteString("\n✅ = исключен из удаления\n❌ = подпадает под правило удаления")

	reply := tgbotapi.NewMessage(msg.Chat.ID, userList.String())
	b.api.Send(reply)
}

func (b *Bot) handleSendToChat(msg *tgbotapi.Message) {
	// Проверяем права доступа - только владелец бота может отправлять сообщения в другие чаты
	if msg.From.ID != b.config.OwnerID {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ У вас нет прав для использования этой команды")
		b.api.Send(reply)
		return
	}

	// Получаем аргументы команды
	args := msg.CommandArguments()
	if args == "" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Использование: /send_to_chat <chat_id> <текст_сообщения>")
		b.api.Send(reply)
		return
	}

	// Разбираем аргументы
	parts := strings.SplitN(args, " ", 2)
	if len(parts) != 2 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Использование: /send_to_chat <chat_id> <текст_сообщения>")
		b.api.Send(reply)
		return
	}

	// Парсим chat_id
	idRaw := strings.TrimSpace(parts[0])
	// Нормализация: длинное тире → дефис, убрать неразрывные пробелы
	idRaw = strings.ReplaceAll(idRaw, "–", "-")
	idRaw = strings.ReplaceAll(idRaw, "—", "-")
	idRaw = strings.ReplaceAll(idRaw, "\u00A0", " ")
	// Фильтрация: оставить ведущий '-' и цифры
	var filtered strings.Builder
	for i, r := range idRaw {
		if i == 0 && r == '-' {
			filtered.WriteRune(r)
			continue
		}
		if r >= '0' && r <= '9' {
			filtered.WriteRune(r)
		}
	}
	idClean := filtered.String()
	chatID, err := strconv.ParseInt(idClean, 10, 64)
	if err != nil {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Неверный формат chat_id")
		b.api.Send(reply)
		return
	}

	// Получаем текст сообщения
	messageText := parts[1]

	// Создаем сообщение для отправки
	chatMessage := tgbotapi.NewMessage(chatID, messageText)

	// Отправляем сообщение в указанный чат
	b.logger.Infof("Sending message to chat %d: %s", chatID, messageText)
	_, err = b.api.Send(chatMessage)
	if err != nil {
		errorMsg := fmt.Sprintf("❌ Ошибка при отправке сообщения в чат %d: %v", chatID, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, errorMsg)
		b.api.Send(reply)
		b.logger.Errorf("Failed to send message to chat %d: %v", chatID, err)
	} else {
		botUsername := b.api.Self.UserName
		if botUsername == "" {
			botUsername = fmt.Sprintf("bot_%d", b.api.Self.ID)
		}
		if saveErr := b.db.SaveUserMessage(&domain.UserMessage{
			UserID:      b.api.Self.ID,
			ChatID:      chatID,
			Username:    botUsername,
			MessageText: messageText,
			MessageType: "ai_reply",
		}); saveErr != nil {
			b.logger.Warnf("Failed to persist send_to_chat message for chat %d: %v", chatID, saveErr)
		} else {
			b.logger.Infof("Persisted send_to_chat message for chat %d", chatID)
		}

		successMsg := fmt.Sprintf("✅ Сообщение успешно отправлено в чат %d", chatID)
		reply := tgbotapi.NewMessage(msg.Chat.ID, successMsg)
		b.api.Send(reply)
		b.logger.Infof("Successfully sent message to chat %d", chatID)
	}
}

func (b *Bot) handleAnnounceAI(msg *tgbotapi.Message) {
	// Проверяем права доступа - только владелец бота может отправлять объявления
	if msg.From.ID != b.config.OwnerID {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ У вас нет прав для использования этой команды")
		b.api.Send(reply)
		return
	}

	// Получаем все чаты из БД
	chatIDs, err := b.db.GetAllChatIDs()
	if err != nil {
		b.logger.Errorf("Failed to get chat IDs: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении списка чатов")
		b.api.Send(reply)
		return
	}

	if len(chatIDs) == 0 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Чаты не найдены")
		b.api.Send(reply)
		return
	}

	// Формируем объявление о ИИ
	announcement := `🦁 Леопард ожил! 🎉

Теперь со мной можно общаться! Я стал умнее благодаря ИИ:

💬 Что я умею:
• Давать советы по тренировкам
• Рассказывать твою статистику
• Анализировать твой прогресс
• Мотивировать и поддерживать

🤖 Как со мной общаться:
• Отметь меня @LeoPoacherBot в сообщении
• Или ответь на любое мое сообщение (reply)

Спрашивай меня о чем угодно: тренировки, статистика, мотивация!

💪 Давай вместе становиться лучше!`

	// Отправляем объявление во все чаты
	successCount := 0
	errorCount := 0

	for _, chatID := range chatIDs {
		chatMessage := tgbotapi.NewMessage(chatID, announcement)
		b.logger.Infof("Sending AI announcement to chat %d", chatID)
		_, err := b.api.Send(chatMessage)
		if err != nil {
			b.logger.Errorf("Failed to send announcement to chat %d: %v", chatID, err)
			errorCount++
		} else {
			b.logger.Infof("Successfully sent announcement to chat %d", chatID)
			successCount++
		}
	}

	// Отправляем отчет владельцу
	resultMsg := fmt.Sprintf("✅ Объявление отправлено!\n\nУспешно: %d чатов\nОшибок: %d чатов", successCount, errorCount)
	reply := tgbotapi.NewMessage(msg.Chat.ID, resultMsg)
	b.api.Send(reply)
}

func (b *Bot) isAdmin(chatID, userID int64) bool {
	// Проверяем, является ли пользователь владельцем
	if userID == b.config.OwnerID {
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
			return fmt.Sprintf("%d дн. %d ч.", days, hours)
		}
		return fmt.Sprintf("%d дн.", days)
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

	// Если нет данных о времени, возвращаем полный таймер
	if messageLog.TimerStartTime == nil {
		b.logger.Infof("DEBUG: TimerStartTime is nil, returning full duration")
		return 7 * 24 * time.Hour
	}

	// Парсим время начала таймера
	timerStart, err := utils.ParseMoscowTime(*messageLog.TimerStartTime)
	if err != nil {
		b.logger.Errorf("Failed to parse timer start time: %v", err)
		return 7 * 24 * time.Hour
	}

	// Полное время таймера (7 дней)
	fullTimerDuration := 7 * 24 * time.Hour

	// Если был больничный, учитываем его
	if messageLog.SickLeaveStartTime != nil && messageLog.HasSickLeave && !messageLog.HasHealthy {
		// Пользователь на больничном - таймер приостановлен
		// Возвращаем оставшееся время на момент больничного
		sickLeaveStart, err := utils.ParseMoscowTime(*messageLog.SickLeaveStartTime)
		if err != nil {
			b.logger.Errorf("Failed to parse sick leave start time: %v", err)
			return fullTimerDuration
		}

		// Рассчитываем время, которое прошло до больничного
		timeBeforeSickLeave := sickLeaveStart.Sub(timerStart)

		// Оставшееся время на момент больничного
		remainingTime := fullTimerDuration - timeBeforeSickLeave

		if remainingTime <= 0 {
			return 0 // Время истекло
		}

		return remainingTime
	}

	// Если был больничный и пользователь выздоровел (проверяем по наличию SickLeaveStartTime и SickLeaveEndTime)
	if messageLog.SickLeaveStartTime != nil && messageLog.SickLeaveEndTime != nil && messageLog.HasHealthy {
		b.logger.Infof("DEBUG: User recovered from sick leave, calculating remaining time")
		sickLeaveStart, err := utils.ParseMoscowTime(*messageLog.SickLeaveStartTime)
		if err != nil {
			b.logger.Errorf("Failed to parse sick leave start time: %v", err)
			return fullTimerDuration
		}

		// Рассчитываем время, которое прошло до больничного
		timeBeforeSickLeave := sickLeaveStart.Sub(timerStart)
		b.logger.Infof("DEBUG: Timer start: %v, Sick start: %v, Time before sick: %v", timerStart, sickLeaveStart, timeBeforeSickLeave)

		// Оставшееся время на момент начала больничного
		remainingTimeAtSickStart := fullTimerDuration - timeBeforeSickLeave
		b.logger.Infof("DEBUG: Full duration: %v, Remaining at sick start: %v", fullTimerDuration, remainingTimeAtSickStart)

		// Если время истекло до больничного, возвращаем 0
		if remainingTimeAtSickStart <= 0 {
			b.logger.Infof("DEBUG: Time expired before sick leave, returning 0")
			return 0 // Время истекло
		}

		// После выздоровления возвращаем то же время, что было на момент больничного
		// Время больничного не засчитывается в общий таймер
		b.logger.Infof("User recovered from sick leave. Remaining time at sick start: %v", remainingTimeAtSickStart)
		return remainingTimeAtSickStart
	}

	// Обычный случай - рассчитываем оставшееся время
	// Используем московское время для расчета
	moscowNow := utils.GetMoscowTime()
	elapsedTime := moscowNow.Sub(timerStart)
	remainingTime := fullTimerDuration - elapsedTime

	if remainingTime <= 0 {
		return 0 // Время истекло
	}

	return remainingTime
}
