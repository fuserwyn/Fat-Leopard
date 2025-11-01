package bot

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"
	"leo-bot/internal/models"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

type Bot struct {
	api      *tgbotapi.BotAPI
	db       *database.Database
	logger   logger.Logger
	config   *config.Config
	timers   map[int64]*models.TimerInfo
	aiClient *ai.OpenRouterClient
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
		api:      api,
		db:       db,
		logger:   log,
		config:   cfg,
		timers:   make(map[int64]*models.TimerInfo),
		aiClient: aiClient,
	}, nil
}

func (b *Bot) Start(ctx context.Context) error {
	b.logger.Info("Starting bot...")

	// Восстанавливаем таймеры из базы данных
	if err := b.recoverTimersFromDatabase(); err != nil {
		b.logger.Errorf("Failed to recover timers from database: %v", err)
		// Не останавливаем бота, просто логируем ошибку
	}

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

	// Запускаем ежедневную сводку в 16-20
	go b.startDailySummaryScheduler(ctx)

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

			userMsg := &models.UserMessage{
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
	messageLog := &models.MessageLog{
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

		userMsg := &models.UserMessage{
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

		userMsg := &models.UserMessage{
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
		messageLog := &models.MessageLog{
			UserID:          msg.From.ID,
			ChatID:          msg.Chat.ID,
			Username:        username,
			Calories:        0,
			StreakDays:      0,
			LastMessage:     utils.FormatMoscowTime(utils.GetMoscowTime()),
			HasTrainingDone: hasTrainingDone,
			HasSickLeave:    hasSickLeave,
			HasHealthy:      hasHealthy,
			IsDeleted:       false,
		}

		if err := b.db.SaveMessageLog(messageLog); err != nil {
			b.logger.Errorf("Failed to save message log: %v", err)
		}
	} else {
		// Обновляем только необходимые поля, сохраняя streak данные
		existingLog.Username = username
		existingLog.LastMessage = utils.FormatMoscowTime(utils.GetMoscowTime())
		existingLog.HasTrainingDone = hasTrainingDone
		existingLog.HasSickLeave = hasSickLeave
		existingLog.HasHealthy = hasHealthy
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

	// Сохраняем отчет о тренировке
	trainingLog := &models.TrainingLog{
		UserID:     msg.From.ID,
		Username:   username,
		LastReport: utils.FormatMoscowTime(utils.GetMoscowTime()),
	}

	if err := b.db.SaveTrainingLog(trainingLog); err != nil {
		b.logger.Errorf("Failed to save training log: %v", err)
	}

	// Получаем текущие данные пользователя
	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}

	// Рассчитываем калории и серию
	caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement := b.calculateCalories(messageLog)

	// ДЕБАГ: Логируем результат расчета
	b.logger.Infof("DEBUG handleTrainingDone: caloriesToAdd=%d, newStreakDays=%d, newCalorieStreakDays=%d, weeklyAchievement=%t, twoWeekAchievement=%t, threeWeekAchievement=%t, monthlyAchievement=%t, fortyTwoDayAchievement=%t, fiftyDayAchievement=%t, sixtyDayAchievement=%t, quarterlyAchievement=%t, hundredDayAchievement=%t",
		caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement)

	// Начисляем калории
	if err := b.db.AddCalories(msg.From.ID, msg.Chat.ID, caloriesToAdd); err != nil {
		b.logger.Errorf("Failed to add calories: %v", err)
	} else {
		b.logger.Infof("DEBUG: Successfully added %d calories", caloriesToAdd)
	}

	// Проверяем, достиг ли пользователь 100 калорий для обмена
	if caloriesToAdd > 0 {
		// Получаем обновленное количество калорий
		updatedCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
		if err != nil {
			b.logger.Errorf("Failed to get updated calories: %v", err)
		} else if updatedCalories >= 100 && updatedCalories-caloriesToAdd < 100 {
			// Пользователь только что достиг 100 калорий
			exchangeMessage := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🎉 Поздравляю! 🎉\n\n%s, достигнуто %d калорий!\n\n🔄 Теперь можешь совершить обмен!\n💡 Напиши #change для обмена 100 калорий на 42 кубка!", username, updatedCalories))

			b.logger.Infof("Sending 100 calories achievement message to chat %d", msg.Chat.ID)
			_, err = b.api.Send(exchangeMessage)
			if err != nil {
				b.logger.Errorf("Failed to send 100 calories achievement message: %v", err)
			} else {
				b.logger.Infof("Successfully sent 100 calories achievement message to chat %d", msg.Chat.ID)
			}
		}
	}

	// Обновляем серию только если была добавлена новая тренировка
	if caloriesToAdd > 0 {
		today := utils.GetMoscowDate()

		// Обновляем streak_days для кубков
		b.logger.Infof("DEBUG: Updating streak to %d with date %s", newStreakDays, today)
		if err := b.db.UpdateStreak(msg.From.ID, msg.Chat.ID, newStreakDays, today); err != nil {
			b.logger.Errorf("Failed to update streak: %v", err)
		} else {
			b.logger.Infof("DEBUG: Successfully updated streak to %d", newStreakDays)
		}

		// Обновляем серию дней для калорий
		b.logger.Infof("DEBUG: Updating calorie streak to %d with date %s", newCalorieStreakDays, today)
		if err := b.db.UpdateCalorieStreakWithDate(msg.From.ID, msg.Chat.ID, newCalorieStreakDays, today); err != nil {
			b.logger.Errorf("Failed to update calorie streak: %v", err)
		} else {
			b.logger.Infof("DEBUG: Successfully updated calorie streak to %d", newCalorieStreakDays)
		}
	} else {
		b.logger.Infof("DEBUG: Skipping streak update (caloriesToAdd = 0)")
	}

	// Проверяем, был ли пользователь на больничном
	wasOnSickLeave := messageLog.HasSickLeave && !messageLog.HasHealthy

	// Начисляем кубки только если была добавлена новая тренировка
	if caloriesToAdd > 0 {
		// Начисляем 1 кубок за каждую тренировку
		if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 1); err != nil {
			b.logger.Errorf("Failed to add daily cup: %v", err)
		} else {
			b.logger.Infof("Successfully added 1 cup for daily training")
		}

		// Начисляем дополнительные кубки за achievements (но НЕ отправляем сообщения пока)
		if weeklyAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add weekly cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 42 cups for weekly achievement")
			}
		}

		if twoWeekAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add two-week cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 42 cups for two-week achievement")
			}
		}

		if threeWeekAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add three-week cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 42 cups for three-week achievement")
			}
		}

		if monthlyAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add monthly cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for monthly achievement")
			}
		}

		if fortyTwoDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add 42-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 42 cups for 42-day achievement")
			}
		}

		if fiftyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 42); err != nil {
				b.logger.Errorf("Failed to add 50-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 42 cups for 50-day achievement")
			}
		}

		if sixtyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add 60-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for 60-day achievement")
			}
		}

		if quarterlyAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add quarterly cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for quarterly achievement")
			}
		}

		if hundredDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 4200); err != nil {
				b.logger.Errorf("Failed to add 100-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 4200 cups for 100-day achievement")
			}
		}
	}

	// ВСЕГДА отправляем ответ при получении #training_done
	// Получаем текущее количество кубков пользователя
	currentCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get user cups for confirmation message: %v", err)
		currentCups = 0
	}

	// Проверяем, есть ли achievement
	hasAnyAchievement := weeklyAchievement || twoWeekAchievement || threeWeekAchievement || monthlyAchievement || fortyTwoDayAchievement || fiftyDayAchievement || sixtyDayAchievement || quarterlyAchievement || hundredDayAchievement

	b.logger.Infof("DEBUG: hasAnyAchievement=%t, caloriesToAdd=%d", hasAnyAchievement, caloriesToAdd)

	if !hasAnyAchievement {
		if caloriesToAdd > 0 {
			// Получаем общее количество калорий для отображения
			totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
			if err != nil {
				b.logger.Errorf("Failed to get total calories for message: %v", err)
				totalCalories = 0
			}

			// Новая тренировка БЕЗ achievement - отправляем обычное подтверждение
			reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Ты тренируешься дней подряд: %d\n🔥 +%d калорий\n🔥 Всего калорий: %d\n🏆 +1 кубок за тренировку!\n🏆 Всего кубков: %d\n\n⏰ Таймер перезапускается на 7 дней\n\n🎯 Продолжай тренироваться и не забывай отправлять #training_done!", newStreakDays, caloriesToAdd, totalCalories, currentCups))

			b.logger.Infof("Sending training done message to chat %d", msg.Chat.ID)
			_, err = b.api.Send(reply)
			if err != nil {
				b.logger.Errorf("Failed to send training done message: %v", err)
			} else {
				b.logger.Infof("Successfully sent training done message to chat %d", msg.Chat.ID)
			}
		} else {
			// Дополнительная тренировка в тот же день
			// Начисляем 1 кубок за дополнительную тренировку
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 1); err != nil {
				b.logger.Errorf("Failed to add cup for double training: %v", err)
			} else {
				b.logger.Infof("Successfully added 1 cup for double training")
			}

			// Получаем обновленное количество кубков
			currentCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
			if err != nil {
				b.logger.Errorf("Failed to get user cups for double training message: %v", err)
				currentCups = 0
			}

			reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🦁 Какой мотивированный леопард! Еще одна тренировка сегодня! 💪\n\n🔥 Твоя мотивация впечатляет\n🏆 +1 кубок за дополнительную тренировку!\n🏆 Всего кубков: %d\n\n⏰ Таймер уже перезапущен на 7 дней\n\n🎯 Завтра снова отправляй #training_done для продолжения серии!", currentCups))

			b.logger.Infof("Sending already trained today message to chat %d", msg.Chat.ID)
			_, err = b.api.Send(reply)
			if err != nil {
				b.logger.Errorf("Failed to send already trained today message: %v", err)
			} else {
				b.logger.Infof("Successfully sent already trained today message to chat %d", msg.Chat.ID)
			}
		}
	}

	// Отправляем сообщения об achievements (вместо обычного подтверждения)
	if hasAnyAchievement {
		b.logger.Infof("Sending achievement messages instead of regular confirmation")

		if weeklyAchievement {
			b.sendWeeklyCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}
		if twoWeekAchievement {
			b.sendTwoWeekCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}
		if threeWeekAchievement {
			b.sendThreeWeekCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}
		if monthlyAchievement {
			b.sendMonthlyCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}
		if fortyTwoDayAchievement {
			b.sendFortyTwoDayCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}
		if fiftyDayAchievement {
			b.sendFiftyDayCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}
		if sixtyDayAchievement {
			b.sendSixtyDayCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}
		if quarterlyAchievement {
			b.sendQuarterlyCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}
		if hundredDayAchievement {
			b.sendHundredDayCupsReward(msg, username, newStreakDays, caloriesToAdd)
		}

		// Проверяем супер-уровень после начисления кубков
		totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
		if err != nil {
			b.logger.Errorf("Failed to get user cups for super level check: %v", err)
		} else if totalCups > 420 {
			// Отправляем сообщение о супер-уровне
			b.sendSuperLevelMessage(msg, username, totalCups)
		}
	}

	// Если пользователь был на больничном, сбрасываем флаги больничного и помечаем как здорового
	if wasOnSickLeave {
		// Отправляем предупреждение о забытом #healthy
		warningMessage := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("⚠️ Внимание, %s!\n\nТы забыл отправить #healthy перед тренировкой!\n\n✅ Я автоматически засчитал выздоровление, но в следующий раз не забывай отправлять #healthy перед #training_done", username))
		b.logger.Infof("Sending forgotten #healthy warning to user %d (%s)", msg.From.ID, username)
		b.api.Send(warningMessage)

		messageLog.HasSickLeave = false
		messageLog.HasHealthy = true
		messageLog.SickLeaveStartTime = nil
		if err := b.db.SaveMessageLog(messageLog); err != nil {
			b.logger.Errorf("Failed to reset sick leave flags: %v", err)
		}
		b.logger.Infof("Reset sick leave flags and marked as healthy for user %d (%s) after training during sick leave", msg.From.ID, username)
	}

	// Запускаем новый таймер
	b.startTimer(msg.From.ID, msg.Chat.ID, username)
}

func (b *Bot) handleSickLeave(msg *tgbotapi.Message) {
	// Получаем данные пользователя
	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}

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
	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🏥 Больничный принят! 🤒\n\n⏸️ Таймер приостановлен на время болезни\n\n❄️ После выздоровления останется: %s до удаления\n\n💪 Выздоравливай и возвращайся к тренировкам!\n\n📝 Когда поправишься, отправь #healthy для возобновления таймера", remainingTimeFormatted))

	b.logger.Infof("Sending sick leave message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send sick leave message: %v", err)
	} else {
		b.logger.Infof("Successfully sent sick leave message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleHealthy(msg *tgbotapi.Message) {
	// Получаем данные о времени таймера и больничного
	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get message log: %v", err)
		return
	}

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
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⏰ Время истекло! 🚫\n\n💪 Выздоровление принято, но время таймера уже истекло.\n\n🦁 Ням-ням, вкусненько! Я питаюсь ленивыми леопардами и становлюсь жирнее!\n\n💪 Ты ведь не хочешь стать как я?\n\nТогда тренируйтесь и отправляйте отчеты!")
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
	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("💪 Выздоровление принято! 🎉\n\n⏰ Таймер возобновлён с места остановки!\n\n⏳ До удаления осталось: %s\n\n🦁 Пора сжечь жир, накопленный за время отсутствия!", remainingTimeFormatted))

	b.logger.Infof("Sending healthy message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send healthy message: %v", err)
	} else {
		b.logger.Infof("Successfully sent healthy message to chat %d", msg.Chat.ID)
	}
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
	chatID, err := strconv.ParseInt(parts[0], 10, 64)
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
		successMsg := fmt.Sprintf("✅ Сообщение успешно отправлено в чат %d", chatID)
		reply := tgbotapi.NewMessage(msg.Chat.ID, successMsg)
		b.api.Send(reply)
		b.logger.Infof("Successfully sent message to chat %d", chatID)
	}
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
	removalTask := make(chan bool)

	timerStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
	timerInfo := &models.TimerInfo{
		UserID:         userID,
		ChatID:         chatID,
		Username:       username,
		WarningTask:    warningTask,
		RemovalTask:    removalTask,
		TimerStartTime: timerStartTime,
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
	warningTime := duration - 24*time.Hour // Предупреждение за 1 день до удаления
	if warningTime < 0 {
		warningTime = duration / 2 // Fallback если время слишком короткое
	}

	// Запускаем предупреждение
	go func() {
		time.Sleep(warningTime)
		select {
		case <-warningTask:
			return // Таймер отменен
		default:
			b.sendWarning(userID, chatID, username)
		}
	}()

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

	b.logger.Infof("Started timer for user %d (%s) - warning in %v, removal in %v", userID, username, warningTime, duration)
}

// restoreTimerWithDuration восстанавливает таймер без обновления timer_start_time в БД
func (b *Bot) restoreTimerWithDuration(userID, chatID int64, username string, duration time.Duration, existingTimerStartTime string) {
	// Отменяем существующие таймеры
	b.cancelTimer(userID)

	// Создаем новые таймеры
	warningTask := make(chan bool)
	removalTask := make(chan bool)

	timerInfo := &models.TimerInfo{
		UserID:         userID,
		ChatID:         chatID,
		Username:       username,
		WarningTask:    warningTask,
		RemovalTask:    removalTask,
		TimerStartTime: existingTimerStartTime, // Используем существующее время из БД
	}

	b.timers[userID] = timerInfo

	// НЕ обновляем timer_start_time в БД - используем существующее значение

	// Рассчитываем время предупреждения (6 дней до удаления)
	warningTime := duration - 24*time.Hour // Предупреждение за 1 день до удаления
	if warningTime < 0 {
		warningTime = duration / 2 // Fallback если время слишком короткое
	}

	// Запускаем предупреждение
	go func() {
		time.Sleep(warningTime)
		select {
		case <-warningTask:
			return // Таймер отменен
		default:
			b.sendWarning(userID, chatID, username)
		}
	}()

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

	b.logger.Infof("Restored timer for user %d (%s) - warning in %v, removal in %v (timer start time: %s)", userID, username, warningTime, duration, existingTimerStartTime)
}

func (b *Bot) cancelTimer(userID int64) {
	if timer, exists := b.timers[userID]; exists {
		close(timer.WarningTask)
		close(timer.RemovalTask)
		delete(b.timers, userID)
		b.logger.Infof("Cancelled timer for user %d", userID)
	}
}

func (b *Bot) sendWarning(userID, chatID int64, username string) {
	message := fmt.Sprintf("⚠️ Предупреждение!\n\n%s, ты не отправляешь отчет о тренировке уже 6 дней!\n\n🦁 Ням-ням, вкусненько! Я питаюсь ленивыми леопардами и становлюсь жирнее!\n\n💪 Ты ведь не хочешь стать как я?\n\n⏰ У тебя остался 1 день до удаления из чата!\n\n🎯 Отправь #training_done прямо сейчас!", username)

	msg := tgbotapi.NewMessage(chatID, message)
	b.logger.Infof("Sending warning to user %d (%s)", userID, username)
	_, err := b.api.Send(msg)
	if err != nil {
		b.logger.Errorf("Failed to send warning: %v", err)
	} else {
		b.logger.Infof("Successfully sent warning to user %d (%s)", userID, username)
	}
}

func (b *Bot) removeUser(userID, chatID int64, username string) {
	b.logger.Infof("Attempting to remove user %d (%s) from chat %d", userID, username, chatID)

	// Пытаемся удалить пользователя из чата
	_, err := b.api.Request(tgbotapi.BanChatMemberConfig{
		ChatMemberConfig: tgbotapi.ChatMemberConfig{
			ChatID: chatID,
			UserID: userID,
		},
		UntilDate: time.Now().Add(30 * 24 * time.Hour).Unix(), // Бан на 30 дней
	})

	if err != nil {
		b.logger.Errorf("Failed to remove user %d: %v", userID, err)
		// Отправляем сообщение об ошибке
		errorMsg := tgbotapi.NewMessage(chatID, fmt.Sprintf("❌ Не удалось удалить пользователя %s из чата", username))
		b.api.Send(errorMsg)
	} else {
		// Отправляем сообщение об удалении
		message := fmt.Sprintf("🚫 Пользователь удален!\n\n@%s был удален из чата за неактивность.\n\n🦁 Ням-ням, вкусненько!\n\n💪 Ты ведь не хочешь стать как я?\n\nТогда тренируйтесь и отправляйте отчеты!", username)
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

func (b *Bot) isAdmin(chatID, userID int64) bool {
	// Проверяем, является ли пользователь владельцем
	if userID == b.config.OwnerID {
		return true
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

func (b *Bot) calculateCalories(messageLog *models.MessageLog) (int, int, int, bool, bool, bool, bool, bool, bool, bool, bool, bool) {
	today := utils.GetMoscowDate()

	// ДЕБАГ: Логируем входные данные
	b.logger.Infof("DEBUG calculateCalories: today=%s, LastTrainingDate=%v, StreakDays=%d, CalorieStreakDays=%d",
		today, messageLog.LastTrainingDate, messageLog.StreakDays, messageLog.CalorieStreakDays)

	// Проверяем, была ли уже тренировка сегодня
	if messageLog.LastTrainingDate != nil && *messageLog.LastTrainingDate == today {
		b.logger.Infof("DEBUG: Уже тренировались сегодня, возвращаем 0 калорий")
		return 0, messageLog.StreakDays, messageLog.CalorieStreakDays, false, false, false, false, false, false, false, false, false // Уже тренировались сегодня
	}

	// Рассчитываем новую серию для кубков (StreakDays)
	newStreakDays := 1

	if messageLog.LastTrainingDate != nil {
		yesterday := utils.GetMoscowTime().AddDate(0, 0, -1)
		yesterdayStr := utils.GetMoscowDateFromTime(yesterday)
		b.logger.Infof("DEBUG: Сравниваем LastTrainingDate=%s с yesterday=%s", *messageLog.LastTrainingDate, yesterdayStr)

		if *messageLog.LastTrainingDate == yesterdayStr {
			// Продолжаем серию
			newStreakDays = messageLog.StreakDays + 1
			b.logger.Infof("DEBUG: Продолжаем серию: %d + 1 = %d", messageLog.StreakDays, newStreakDays)
		} else {
			// Серия прервана, начинаем заново
			newStreakDays = 1
			b.logger.Infof("DEBUG: Серия прервана, начинаем заново: %d", newStreakDays)
		}
	} else {
		// Если нет данных о последней тренировке, но есть streak, продолжаем его
		if messageLog.StreakDays > 0 {
			newStreakDays = messageLog.StreakDays + 1
			b.logger.Infof("DEBUG: Нет данных о последней тренировке, продолжаем streak: %d + 1 = %d", messageLog.StreakDays, newStreakDays)
		}
	}

	// Рассчитываем новую серию для калорий (CalorieStreakDays)
	newCalorieStreakDays := 1

	if messageLog.LastTrainingDate != nil {
		yesterday := utils.GetMoscowTime().AddDate(0, 0, -1)
		yesterdayStr := utils.GetMoscowDateFromTime(yesterday)
		b.logger.Infof("DEBUG: Сравниваем LastTrainingDate=%s с yesterday=%s для калорий", *messageLog.LastTrainingDate, yesterdayStr)

		if *messageLog.LastTrainingDate == yesterdayStr {
			// Продолжаем серию калорий
			newCalorieStreakDays = messageLog.CalorieStreakDays + 1
			b.logger.Infof("DEBUG: Продолжаем серию калорий: %d + 1 = %d", messageLog.CalorieStreakDays, newCalorieStreakDays)
		} else {
			// Серия калорий прервана, начинаем заново
			newCalorieStreakDays = 1
			b.logger.Infof("DEBUG: Серия калорий прервана, начинаем заново: %d", newCalorieStreakDays)
		}
	} else {
		// Если нет данных о последней тренировке, но есть calorie streak, продолжаем его
		if messageLog.CalorieStreakDays > 0 {
			newCalorieStreakDays = messageLog.CalorieStreakDays + 1
			b.logger.Infof("DEBUG: Нет данных о последней тренировке, продолжаем calorie streak: %d + 1 = %d", messageLog.CalorieStreakDays, newCalorieStreakDays)
		}
	}

	// Система калорий: количество калорий = количество дней в серии
	// calorie_streak_days=4 → +4 калории, calorie_streak_days=5 → +5 калорий
	caloriesToAdd := newCalorieStreakDays
	b.logger.Infof("DEBUG: Калории равны количеству дней в серии: %d калорий", caloriesToAdd)

	// Бонус за возвращение после больничного
	if messageLog.HasSickLeave && messageLog.HasHealthy {
		caloriesToAdd += 2
	}

	// Проверяем, достиг ли пользователь недельной серии (7 дней подряд)
	weeklyAchievement := newStreakDays == 7

	// Проверяем, достиг ли пользователь двухнедельной серии (14 дней подряд)
	twoWeekAchievement := newStreakDays == 14

	// Проверяем, достиг ли пользователь трехнедельной серии (21 день подряд)
	threeWeekAchievement := newStreakDays == 21

	// Проверяем, достиг ли пользователь месячной серии (30 дней подряд)
	monthlyAchievement := newStreakDays == 30

	// Проверяем, достиг ли пользователь серии 42 дня подряд
	fortyTwoDayAchievement := newStreakDays == 42

	// Проверяем, достиг ли пользователь серии 50 дней подряд
	fiftyDayAchievement := newStreakDays == 50

	// Проверяем, достиг ли пользователь серии 60 дней подряд
	sixtyDayAchievement := newStreakDays == 60

	// Проверяем, достиг ли пользователь квартальной серии (90 дней подряд) - теперь 420 кубков
	quarterlyAchievement := newStreakDays == 90

	// Проверяем, достиг ли пользователь серии 100 дней подряд - 4200 кубков
	hundredDayAchievement := newStreakDays == 100

	// ДЕБАГ: Логируем результат
	b.logger.Infof("DEBUG calculateCalories RESULT: caloriesToAdd=%d, newStreakDays=%d, newCalorieStreakDays=%d, weekly=%t, twoWeek=%t, threeWeek=%t, monthly=%t, fortyTwo=%t, fifty=%t, sixty=%t, quarterly=%t, hundred=%t",
		caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement)

	return caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement
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

func (b *Bot) calculateRemainingTime(messageLog *models.MessageLog) time.Duration {
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

func (b *Bot) sendWeeklyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	b.logger.Infof("DEBUG sendWeeklyCupsReward called for user %s (streak: %d days)", username, streakDays)

	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for weekly reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for weekly reward: %v", err)
		totalCups = 0
	}

	// Создаем сообщение с 42 кубками
	cupsMessage := fmt.Sprintf(`🏆 НЕВЕРОЯТНО! 🏆

%s, ты тренируешься уже %d дней подряд! 



🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🎯 +42 КУБКА за твою недельную серию! 🎯
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +42 кубка
🏆 Всего кубков: %d
🦁 Fat Leopard гордится тобой! 
💪 Ты настоящий чемпион!
🔥 Продолжай в том же духе!

#weekly_champion #42_cups #training_streak`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем сообщение с кубками
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending weekly cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send weekly cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent weekly cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendMonthlyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for monthly reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for monthly reward: %v", err)
		totalCups = 0
	}

	// Создаем сообщение с 420 кубками
	cupsMessage := fmt.Sprintf(`🏆🏆🏆 ЛЕГЕНДА! 🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 



🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +420 КУБКОВ ЗА ТВОЮ МЕСЯЧНУЮ СЕРИЮ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +420 кубков
🏆 Всего кубков: %d
🦁 Fat Leopard в шоке от твоей мотивации! 
💪 Ты абсолютная легенда!
🔥 Ты вдохновляешь всех вокруг!
⭐ Ты настоящий чемпион чемпионов!

#monthly_legend #420_cups #training_legend`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем сообщение с кубками
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending monthly cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send monthly cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent monthly cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendQuarterlyCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for quarterly reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for quarterly reward: %v", err)
		totalCups = 0
	}

	// Создаем сообщение с 420 кубками
	cupsMessage := fmt.Sprintf(`🏆🏆🏆🏆 ЛЕГЕНДАРНО! 🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +420 КУБКОВ ЗА ТВОЮ КВАРТАЛЬНУЮ СЕРИЮ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +420 кубков
🏆 Всего кубков: %d
🦁 Fat Leopard в шоке от твоей мотивации! 
💪 Ты абсолютная легенда!
🔥 Ты переписываешь законы мотивации!
⭐ Ты король тренировок!
👑 Ты повелитель мотивации!

#quarterly_legend #420_cups #training_king`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем сообщение с кубками
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending quarterly cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send quarterly cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent quarterly cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendTwoWeekCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for two-week reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for two-week reward: %v", err)
		totalCups = 0
	}

	// Создаем сообщение с 42 кубками
	cupsMessage := fmt.Sprintf(`🏆🏆 НЕВЕРОЯТНО! 🏆🏆

%s, ты тренируешься уже %d дней подряд! 



🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🎯 +42 КУБКА за твою двухнедельную серию! 🎯
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +42 кубка
🏆 Всего кубков: %d
🦁 Fat Leopard в восторге от твоей мотивации! 
💪 Ты настоящий воин!
🔥 Твоя сила растет с каждым днем!
⭐ Ты вдохновляешь всю стаю!

#two_week_champion #42_cups #training_warrior`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем сообщение с кубками
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending two-week cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send two-week cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent two-week cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendThreeWeekCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for three-week reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for three-week reward: %v", err)
		totalCups = 0
	}

	// Создаем сообщение с 42 кубками
	cupsMessage := fmt.Sprintf(`🏆🏆🏆 ФЕНОМЕНАЛЬНО! 🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 



🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🎯 +42 КУБКА за твою трехнедельную серию! 🎯
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +42 кубка
🏆 Всего кубков: %d
🦁 Fat Leopard поражен твоей силой воли! 
💪 Ты абсолютный чемпион!
🔥 Твоя мотивация не знает границ!
⭐ Ты легенда среди леопардов!
👑 Ты король мотивации!

#three_week_legend #42_cups #training_king`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем сообщение с кубками
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending three-week cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send three-week cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent three-week cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendFortyTwoDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for 42-day reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for 42-day reward: %v", err)
		totalCups = 0
	}

	// Создаем сообщение с 42 кубками
	cupsMessage := fmt.Sprintf(`🏆🏆🏆🏆 ПОТРЯСАЮЩЕ! 🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🎯 +42 КУБКА за твою серию 42 дня! 🎯
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +42 кубка
🏆 Всего кубков: %d
🦁 Fat Leopard восхищен твоей настойчивостью! 
💪 Ты настоящий боец!
🔥 Твоя дисциплина впечатляет!
⭐ Ты показываешь мастер-класс мотивации!
👑 Ты герой стаи!

#fortytwo_days_hero #42_cups #training_master`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем сообщение с кубками
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending 42-day cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send 42-day cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent 42-day cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendFiftyDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for 50-day reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for 50-day reward: %v", err)
		totalCups = 0
	}

	// Создаем сообщение с 42 кубками
	cupsMessage := fmt.Sprintf(`🏆🏆🏆🏆🏆 ВЕЛИКОЛЕПНО! 🏆🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🎯 +42 КУБКА за твою серию 50 дней! 🎯
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +42 кубка
🏆 Всего кубков: %d
🦁 Fat Leopard в восторге от твоего упорства! 
💪 Ты превосходный воин!
🔥 Твоя стойкость поражает!
⭐ Ты образец для подражания!
👑 Ты покоритель вершин!

#fifty_days_legend #42_cups #training_excellence`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем сообщение с кубками
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending 50-day cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send 50-day cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent 50-day cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendSixtyDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for 60-day reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for 60-day reward: %v", err)
		totalCups = 0
	}

	// Создаем сообщение с 420 кубками
	cupsMessage := fmt.Sprintf(`🏆🏆🏆🏆🏆🏆 ИСКЛЮЧИТЕЛЬНО! 🏆🏆🏆🏆🏆🏆

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 +420 КУБКОВ ЗА ТВОЮ СЕРИЮ 60 ДНЕЙ! 🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +420 кубков
🏆 Всего кубков: %d
🦁 Fat Leopard не может поверить в твою силу воли! 
💪 Ты непобедимый титан!
🔥 Твоя энергия заряжает всех вокруг!
⭐ Ты вдохновляешь на подвиги!
👑 Ты владыка мотивации!
🌟 Ты светишь как звезда!

#sixty_days_titan #420_cups #training_perfection`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем сообщение с кубками
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending 60-day cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send 60-day cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent 60-day cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendHundredDayCupsReward(msg *tgbotapi.Message, username string, streakDays int, caloriesAdded int) {
	// Получаем текущее количество калорий
	totalCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total calories for 100-day reward: %v", err)
		totalCalories = 0
	}

	// Получаем текущее количество кубков
	totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get total cups for 100-day reward: %v", err)
		totalCups = 0
	}

	// Создаем невероятное поздравление с 4200 кубками
	cupsMessage := fmt.Sprintf(`🌟⚡🎇🎆🎊 НЕВЕРОЯТНОЕ ДОСТИЖЕНИЕ! 🎊🎆🎇⚡🌟

%s, ты тренируешься уже %d дней подряд! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯🌟⚡ +4200 КУБКОВ ЗА НЕВЕРОЯТНУЮ СЕРИЮ 100 ДНЕЙ! ⚡🌟🎯

🔥 +%d калорий
🔥 Всего калорий: %d
🏆 +4200 кубков
🏆 Всего кубков: %d

🌟 НЕВЕРОЯТНОЕ ПОЗДРАВЛЕНИЕ! 🌟
🦁 Fat Leopard падает в обморок от твоей божественной силы воли! 
💪 Ты превзошел все мыслимые пределы!
🔥 Твоя мотивация переписывает законы физики!
⭐ Ты абсолютный бог тренировок!
👑 Ты император всех императоров!
🌟 Ты сияешь ярче всех звезд во вселенной!
💎 Ты бриллиант среди алмазов!
🚀 Ты покоритель невозможного!

🎊 ВСЕ ГАЛАКТИКИ АПЛОДИРУЮТ ТЕБЕ! 🎊

#hundred_days_god #4200_cups #training_divinity #ultimate_achievement`, username, streakDays, caloriesAdded, totalCalories, totalCups)

	// Отправляем невероятное поздравление
	reply := tgbotapi.NewMessage(msg.Chat.ID, cupsMessage)

	b.logger.Infof("Sending 100-day cups reward to chat %d for user %s (streak: %d days)", msg.Chat.ID, username, streakDays)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send 100-day cups reward: %v", err)
	} else {
		b.logger.Infof("Successfully sent 100-day cups reward to chat %d for user %s", msg.Chat.ID, username)
	}
}

func (b *Bot) sendSuperLevelMessage(msg *tgbotapi.Message, username string, totalCups int) {
	// Создаем сообщение о супер-уровне
	superMessage := fmt.Sprintf(`🌟⚡ СУПЕР-УРОВЕНЬ ДОСТИГНУТ! ⚡🌟

%s, ты накопил %d кубков! 

🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆
🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎊 ВСЕ ОЖИДАНИЯ ПРЕВЗОЙДЕНЫ! 🎊

🦁 Fat Leopard в полном восторге! 
💪 Ты не просто чемпион - ты СУПЕР-ЧЕМПИОН!
🔥 Твоя сила и мощь безграничны!
⭐️ Ты вдохновляешь всю стаю!
👑 Мотивация не верит, что такое бывает!
🌟 Ты сияешь ярче всех!

🎯 Продолжай в том же духе, супер-леопард!

#super_level #%d_cups #motivation_king`, username, totalCups, totalCups)

	// Отправляем сообщение о супер-уровне
	reply := tgbotapi.NewMessage(msg.Chat.ID, superMessage)

	b.logger.Infof("Sending super level message to chat %d for user %s (total cups: %d)", msg.Chat.ID, username, totalCups)
	_, err := b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send super level message: %v", err)
	} else {
		b.logger.Infof("Successfully sent super level message to chat %d for user %s", msg.Chat.ID, username)
	}
}

// startDailySummaryScheduler запускает планировщик ежедневных сводок в 16:20
func (b *Bot) startDailySummaryScheduler(ctx context.Context) {
	if b.aiClient == nil {
		b.logger.Warn("AI client not available, daily summary scheduler disabled")
		return
	}

	// Используем московское время
	loc, _ := time.LoadLocation("Europe/Moscow")
	lastSentDate := ""
	ticker := time.NewTicker(1 * time.Minute) // Проверяем каждую минуту
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().In(loc)
			hour := now.Hour()
			minute := now.Minute()

			// Проверяем, наступило ли время 16:20
			if hour == 16 && minute == 20 {
				today := now.Format("2006-01-02")
				// Отправляем сводку только один раз в день
				if lastSentDate != today {
					// Генерируем сводку за вчерашний день
					yesterday := now.AddDate(0, 0, -1)
					b.logger.Infof("Generating daily summary at 16:20 for date: %s", yesterday.Format("2006-01-02"))
					b.generateAndSendDailySummary(yesterday)
					lastSentDate = today
				}
			}
		}
	}
}

// generateAndSendDailySummary генерирует и отправляет ежедневную сводку
func (b *Bot) generateAndSendDailySummary(date time.Time) {
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
		b.generateSummaryForChat(chatID, date)
	}
}

// generateSummaryForChat генерирует сводку для конкретного чата
func (b *Bot) generateSummaryForChat(chatID int64, date time.Time) {
	// Получаем сообщения за день
	messages, err := b.db.GetDailyMessages(chatID, date)
	if err != nil {
		b.logger.Errorf("Failed to get daily messages for chat %d: %v", chatID, err)
		return
	}

	if len(messages) == 0 {
		return // Нет сообщений за день
	}

	// Группируем сообщения по пользователям
	userMap := make(map[int64]*ai.UserTrainingData)
	for _, msg := range messages {
		if userMap[msg.UserID] == nil {
			// Получаем данные пользователя
			userLog, err := b.db.GetMessageLog(msg.UserID, msg.ChatID)
			if err != nil {
				continue
			}

			cups, _ := b.db.GetUserCups(msg.UserID, msg.ChatID)
			userMap[msg.UserID] = &ai.UserTrainingData{
				UserID:       msg.UserID,
				Username:     msg.Username,
				HasTraining:  false,
				HasSickLeave: false,
				HasHealthy:   false,
				StreakDays:   userLog.StreakDays,
				Calories:     userLog.Calories,
				Cups:         cups,
			}
		}

		user := userMap[msg.UserID]
		if msg.MessageType == "training_done" {
			user.HasTraining = true
			if user.TrainingMessage == "" {
				user.TrainingMessage = msg.MessageText
			}
		} else if msg.MessageType == "sick_leave" {
			user.HasSickLeave = true
		} else if msg.MessageType == "healthy" {
			user.HasHealthy = true
		}
	}

	// Преобразуем map в slice
	var usersData []ai.UserTrainingData
	for _, user := range userMap {
		usersData = append(usersData, *user)
	}

	if len(usersData) == 0 {
		return
	}

	// Генерируем сводку с помощью ИИ
	summary, err := b.aiClient.GenerateDailySummary(usersData)
	if err != nil {
		b.logger.Errorf("Failed to generate daily summary: %v", err)

		// Если ошибка связана с настройкой политики данных, пропускаем сводку
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "data policy") || strings.Contains(errorMsg, "Model Training") {
			b.logger.Warnf("Skipping daily summary due to OpenRouter data policy configuration. User needs to enable 'Model Training' at https://openrouter.ai/settings/privacy")
		}
		return
	}

	// Удаляем markdown форматирование (**) перед отправкой
	summary = strings.ReplaceAll(summary, "**", "")

	// Отправляем сводку в чат
	reply := tgbotapi.NewMessage(chatID, summary)
	b.logger.Infof("Sending daily summary to chat %d", chatID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send daily summary: %v", err)
	} else {
		b.logger.Infof("Successfully sent daily summary to chat %d", chatID)
	}
}

// handleAIQuestion обрабатывает вопрос пользователя к ИИ
func (b *Bot) handleAIQuestion(msg *tgbotapi.Message, questionText string) {
	b.logger.Infof("handleAIQuestion called for user %d with text: %s", msg.From.ID, questionText)

	if b.aiClient == nil {
		b.logger.Warn("AI client is nil, cannot process question")
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ ИИ функции недоступны. Проверьте настройки OpenRouter API.")
		b.api.Send(reply)
		return
	}

	// Удаляем упоминание бота из вопроса
	botUsername := b.api.Self.UserName
	if botUsername != "" {
		questionText = strings.ReplaceAll(questionText, "@"+botUsername, "")
		questionText = strings.ReplaceAll(questionText, botUsername, "")
	}
	// Удаляем все упоминания в формате @username
	questionText = strings.ReplaceAll(questionText, "@", "")
	questionText = strings.TrimSpace(questionText)

	if questionText == "" {
		b.logger.Infof("Question text is empty after cleaning")
		reply := tgbotapi.NewMessage(msg.Chat.ID, "💬 Привет! 👋 Задай мне вопрос!\n\nНапример:\n• Что я делал вчера?\n• Как мой прогресс?\n• Что улучшить в тренировках?\n• Как лечиться?")
		b.api.Send(reply)
		return
	}

	b.logger.Infof("Processing AI question: %s", questionText)

	// Получаем историю тренировок пользователя
	history, err := b.db.GetUserTrainingHistory(msg.From.ID, msg.Chat.ID, 50)
	if err != nil {
		b.logger.Errorf("Failed to get user training history: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении истории тренировок")
		b.api.Send(reply)
		return
	}

	// Формируем полный контекст о пользователе
	var contextText strings.Builder
	contextText.WriteString("=== ИСТОРИЯ ТРЕНИРОВОК ПОЛЬЗОВАТЕЛЯ ===\n\n")

	// Добавляем историю сообщений
	if len(history) > 0 {
		for _, msg := range history {
			messageType := ""
			if msg.MessageType == "training_done" {
				messageType = " [ТРЕНИРОВКА]"
			} else if msg.MessageType == "sick_leave" {
				messageType = " [БОЛЬНИЧНЫЙ]"
			} else if msg.MessageType == "healthy" {
				messageType = " [ВЫЗДОРОВЛЕНИЕ]"
			}
			contextText.WriteString(fmt.Sprintf("[%s]%s %s: %s\n",
				msg.CreatedAt.Format("2006-01-02 15:04"), messageType, msg.Username, msg.MessageText))
		}
	} else {
		contextText.WriteString("История пуста\n")
	}

	// Получаем полные данные пользователя
	userLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	if err == nil {
		cups, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)

		contextText.WriteString("\n=== ТЕКУЩАЯ СТАТИСТИКА ===\n")
		contextText.WriteString(fmt.Sprintf("👤 Пользователь: %s\n", userLog.Username))
		contextText.WriteString(fmt.Sprintf("🔥 Всего калорий: %d\n", userLog.Calories))
		contextText.WriteString(fmt.Sprintf("🏆 Всего кубков: %d\n", cups))
		contextText.WriteString(fmt.Sprintf("💪 Серия тренировок: %d дней подряд\n", userLog.StreakDays))
		contextText.WriteString(fmt.Sprintf("📈 Серия калорий: %d дней подряд\n", userLog.CalorieStreakDays))

		if userLog.LastTrainingDate != nil {
			contextText.WriteString(fmt.Sprintf("📅 Последняя тренировка: %s\n", *userLog.LastTrainingDate))
		}

		if userLog.HasSickLeave {
			contextText.WriteString("🏥 Статус: На больничном\n")
			if userLog.SickLeaveStartTime != nil {
				contextText.WriteString(fmt.Sprintf("   Начало больничного: %s\n", *userLog.SickLeaveStartTime))
			}
		} else if userLog.HasHealthy {
			contextText.WriteString("✅ Статус: Здоров\n")
			if userLog.SickLeaveEndTime != nil {
				contextText.WriteString(fmt.Sprintf("   Выздоровление: %s\n", *userLog.SickLeaveEndTime))
			}
		} else {
			contextText.WriteString("✅ Статус: Активен\n")
		}

		if userLog.TimerStartTime != nil {
			contextText.WriteString(fmt.Sprintf("⏰ Таймер запущен: %s\n", *userLog.TimerStartTime))
		}

		// Добавляем информацию о текущем остатке времени таймера (ВАЖНО: это точное время из БД)
		// Вычисляем реальное время до удаления прямо сейчас
		if userLog.TimerStartTime != nil {
			remainingTime := b.calculateRemainingTime(userLog)
			if remainingTime > 0 {
				remainingTimeFormatted := b.formatDurationToDays(remainingTime)
				if userLog.HasSickLeave {
					contextText.WriteString(fmt.Sprintf("⏳ После выздоровления останется: %s до удаления\n", remainingTimeFormatted))
				} else {
					contextText.WriteString(fmt.Sprintf("⏳ До удаления осталось: %s\n", remainingTimeFormatted))
				}
			} else {
				contextText.WriteString("⏳ Время таймера истекло\n")
			}
		}

		contextText.WriteString(fmt.Sprintf("💬 Последнее сообщение: %s\n", userLog.LastMessage))
	} else {
		contextText.WriteString("\n⚠️ Данные пользователя не найдены\n")
	}

	// Генерируем ответ с помощью ИИ
	answer, err := b.aiClient.AnswerUserQuestion(questionText, contextText.String())
	if err != nil {
		b.logger.Errorf("Failed to generate AI answer: %v", err)

		// Проверяем, является ли это ошибкой настройки политики данных
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "data policy") || strings.Contains(errorMsg, "Model Training") {
			reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ ИИ функции требуют настройки OpenRouter API.\n\nДля бесплатных моделей нужно:\n1. Перейди на https://openrouter.ai/settings/privacy\n2. Включи опцию 'Model Training'\n\nПосле этого ИИ заработает!")
			b.api.Send(reply)
			return
		}

		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Ошибка при генерации ответа ИИ: %v", err))
		b.api.Send(reply)
		return
	}

	// Удаляем markdown форматирование (**) перед отправкой
	answer = strings.ReplaceAll(answer, "**", "")

	// Отправляем ответ с реплаем на исходное сообщение
	reply := tgbotapi.NewMessage(msg.Chat.ID, answer)
	reply.ReplyToMessageID = msg.MessageID // Отвечаем на сообщение пользователя
	b.logger.Infof("Sending AI answer to user %d in chat %d (replying to message %d)", msg.From.ID, msg.Chat.ID, msg.MessageID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send AI answer: %v", err)
	}
}

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
		if strings.Contains(textLower, "#training_done") {
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
		userMsg := &models.UserMessage{
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
	// Проверяем, что команда от владельца
	if msg.From.ID != b.config.OwnerID {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Эта команда доступна только владельцу бота")
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

	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🔄 Начинаю сканирование истории за последние %d дней...\n\n⚠️ ВАЖНО: Telegram Bot API имеет ограничение - можно получить только последние ~100 доступных обновлений, а не всю историю.\n\nДля полной истории за 2 месяца нужно:\n1. Экспортировать данные из Telegram (Settings → Privacy → Export Telegram data)\n2. Или использовать Telegram Client API (MTProto) - более сложная интеграция\n\nБот будет пытаться получить доступные обновления, но это не покроет всю историю.", daysBack))
	b.api.Send(reply)

	// Запускаем сканирование в отдельной горутине
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		b.scanChatHistory(ctx, daysBack)

		// Отправляем отчет
		finalReply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Сканирование истории завершено (последние %d дней)", daysBack))
		b.api.Send(finalReply)
	}()
}

// handleAIMemory обрабатывает команду /ai_memory или /memory для показа информации о долгосрочной памяти AI
func (b *Bot) handleAIMemory(msg *tgbotapi.Message) {
	text := `🧠 Долгосрочная память AI

❌ AI пока ничего не знает о вас.

💡 Как это работает:
1️⃣ Откройте диалог с AI: 🤖 Нейросети → 🧠 Текстовые LLM
2️⃣ Расскажите о себе в диалоге с любой моделью
3️⃣ AI автоматически запоминает важные факты
4️⃣ Память используется во всех будущих диалогах

📝 Пример диалога с AI:
"Привет! Меня зовут Иван, я Python разработчик. Работаю над проектом интернет-магазина на FastAPI."

✅ AI запомнит: имя, профессию, проект, технологии

⚠️ Важно: Факты запоминаются только во время диалога с AI, а не в этом разделе`

	// Создаем inline клавиатуру с кнопкой "Назад"
	keyboard := tgbotapi.NewInlineKeyboardMarkup(
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Назад", "back_to_menu"),
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

	switch data {
	case "back_to_menu":
		// Удаляем сообщение и возвращаемся в меню
		deleteMsg := tgbotapi.NewDeleteMessage(msg.Chat.ID, msg.MessageID)
		b.api.Send(deleteMsg)

		// Отправляем главное меню (можно настроить по своему усмотрению)
		menuText := `🦁 Главное меню

Доступные команды:
/help - Помощь
/top - Топ пользователей
/points - Статистика по калориям
/cups - Статистика по кубкам

💪 Для тренировки используйте:
#training_done - Отчет о тренировке`

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
