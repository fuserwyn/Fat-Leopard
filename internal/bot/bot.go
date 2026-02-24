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
	adminSessions        map[int64]*adminSession
	adminSessionsMutex   sync.Mutex
}

var (
	sickLeavePositiveKeywords = []string{
		"болен", "болею", "болит", "заболел", "заболела", "забол", "заболева", "простыл", "простуд", "температур", "кашля", "кашель", "грипп", "орви", "ангин", "плохо", "лежу", "честно", "правда", "шанс", "выздоров", "выздоравли", "таблет", "врач", "болезн", "недомог", "жар", "сон", "боляч", "мигрен", "лихорад", "fever", "flu", "cold", "ill", "sick",
	}
	sickLeaveSupportKeywords = []string{
		"дай шанс", "прошу", "пожалуйста", "исправлюсь", "буду тренироваться", "честно-честно", "умоляю", "пожал", "верь", "поверь", "обещаю",
	}
	sickLeaveNegativeKeywords = []string{
		"делами", "работаю", "работа", "работе", "работ", "work", "workout", "воркаут", "лень", "просто не", "не хочу", "другие дела", "прогул", "хитр", "обман", "схитрить", "занят", "занята",
	}
)

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
		adminSessions:        make(map[int64]*adminSession),
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

	// Админ-мастер перехватывает сообщения владельца в личке при активной сессии.
	if b.handleAdminFlowMessage(msg) {
		return
	}

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
	case "admin":
		b.handleAdmin(msg)
	case "audit_last24":
		b.auditLast24h()
	case "set_chat_type":
		b.handleSetChatType(msg)
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

	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(chatID)
	if err != nil {
		chatType = "training" // По умолчанию
	}

	// Создаем приветственное сообщение с упоминанием пользователя
	var welcomeText string
	if chatType == "writing" {
		welcomeText = fmt.Sprintf(`%s, добро пожаловать в стаю писателей! 🦁

Я ваш хладнокровный литературный наставник, который следит за писательством всегда, я все вижу и не оставляю в стае тех, кто не пишет больше 7 дней!

📝 Отчеты о писательской работе:
• #writing_done — Отправить отчет о писательской сессии

🏥 Больничный:
• #sick_leave — Взять больничный (приостанавливает таймер)
• #healthy — Выздороветь (возобновляет таймер)

🔄 Обмен:
• #change — Обменять слова на кубки (100 слов = 42 кубка)

⏰ Как я слежу за писательством:
• Таймер уже запущен! У тебя есть 7 дней на первую писательскую сессию
• При получении #writing_done таймер перезапускается на 7 дней
• Через 6 дней без #writing_done - предупреждение
• Через 7 дней без #writing_done - удаление из чата

🏆 Награды за писательские сессии:
• 🏆 За каждую писательскую сессию = 1 КУБОК! 🏆
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
• Отчётом считается любое сообщение с тегом #writing_done
• Если заболели — отправь #sick_leave
• После выздоровления — отправь #healthy
• Через 6 дней без отчёта — предупреждение
• Через 7 дней без отчёта — удаление из чата

🎯 Начни прямо сейчас — отправь #writing_done!`, username)
	} else {
		welcomeText = fmt.Sprintf(`%s, добро пожаловать в стаю! 🦁

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
	}

	// Отправляем сообщение
	reply := tgbotapi.NewMessage(chatID, welcomeText)

	b.logger.Infof("Sending welcome message to chat %d for new user %s (ID: %d)", chatID, username, userID)
	_, errSend := b.api.Send(reply)
	if errSend != nil {
		b.logger.Errorf("Failed to send welcome message: %v", errSend)
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

	// КРИТИЧЕСКИ ВАЖНО: Сначала проверяем хештеги команд (#training_done, #writing_done, #sick_leave, и т.д.)
	// Команды имеют приоритет над ИИ-обработкой
	// Проверяем тип чата для определения правильного хештега
	chatTypeForCommand, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatTypeForCommand = "training" // По умолчанию
	}

	hasTrainingDone := strings.Contains(strings.ToLower(text), "#training_done")
	hasWritingDone := strings.Contains(strings.ToLower(text), "#writing_done")
	// Для чатов писательства принимаем оба хештега для совместимости, но приоритет у #writing_done
	hasTrainingDone = hasTrainingDone || (hasWritingDone && chatTypeForCommand == "writing")
	hasSickLeave := strings.Contains(strings.ToLower(text), "#sick_leave")
	hasHealthy := strings.Contains(strings.ToLower(text), "#healthy")
	hasChange := strings.Contains(strings.ToLower(text), "#change")
	hasCommand := hasTrainingDone || hasWritingDone || hasSickLeave || hasHealthy || hasChange

	// Если есть команда, обрабатываем её и НЕ обрабатываем через ИИ
	if hasCommand {
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
			if hasTrainingDone || hasWritingDone {
				messageType = "training_done" // Используем единый тип для обоих хештегов
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
		return // Выходим, не обрабатывая через ИИ
	}

	// Если нет команд, проверяем обращение к боту (для вопросов к ИИ)
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

	// Если дошли сюда, значит нет ни команд, ни обращения к боту - сохраняем обычное сообщение в БД
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

		// Сохраняем в user_messages для контекста
		userMsg := &domain.UserMessage{
			UserID:      msg.From.ID,
			ChatID:      msg.Chat.ID,
			Username:    username,
			MessageText: text,
			MessageType: "general", // Обычное сообщение
		}
		if err := b.db.SaveUserMessage(userMsg); err != nil {
			b.logger.Errorf("Failed to save user message: %v", err)
		}

		// Обновляем LastMessage в message_log
		messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
		if err == nil {
			messageLog.Username = username
			messageLog.LastMessage = text
			messageLog.IsDeleted = false
			if err := b.db.SaveMessageLog(messageLog); err != nil {
				b.logger.Errorf("Failed to update message log: %v", err)
			}
		}
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

	// КРИТИЧЕСКИ ВАЖНО: Перезапускаем таймер СРАЗУ, чтобы отменить старый таймер
	// и предотвратить удаление пользователя, если таймер уже сработал
	b.startTimer(msg.From.ID, msg.Chat.ID, username)

	// Сохраняем отчет о тренировке
	trainingLog := &domain.TrainingLog{
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

	// Фиксируем кубки до начислений, чтобы сохранить точный cups_added в training_sessions.
	cupsBefore, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Warnf("Failed to get initial cups before training session save: %v", err)
		cupsBefore = 0
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

	// Определяем пол пользователя из БД или по имени
	userGender := strings.TrimSpace(strings.ToLower(messageLog.Gender))
	if userGender == "" {
		// Если пол не указан в БД, пытаемся определить по имени
		userGender = b.detectGenderFromName(msg.From.FirstName)
		if userGender != "" {
			// Сохраняем определенный пол в БД
			if err := b.updateUserGender(msg.From.ID, msg.Chat.ID, userGender); err != nil {
				b.logger.Warnf("Failed to update user gender: %v", err)
			}
		}
	}

	// Автоматически определяем тип чата на основе содержимого сообщения
	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}

	// Проверяем текущий тип чата перед проверкой содержимого
	currentType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		currentType = "training" // По умолчанию
	}

	// Автоматически переключаем тип чата только если еще не "writing"
	if currentType != "writing" && b.shouldDetectChatTypeAsWriting(text, msg.Chat.ID) {
		// Автоматически устанавливаем тип чата как "writing", если в сообщении есть контекст писательства
		if err := b.db.SetChatType(msg.Chat.ID, "writing"); err != nil {
			b.logger.Warnf("Failed to auto-set chat type to writing: %v", err)
		} else {
			b.logger.Infof("Auto-detected chat type as 'writing' for chat %d based on message content", msg.Chat.ID)
			// Уведомляем участников чата об автоматическом переключении типа (только один раз при переключении)
			notification := tgbotapi.NewMessage(msg.Chat.ID, `🦁 Fat Leopard обнаружил контекст писательства в ваших сообщениях!

✅ Тип чата автоматически переключен на "писательство"

Теперь я буду вести отдельный контекст для лучшего понимания вашего литературного творчества.

📝 Я готов помочь с развитием сюжета, персонажей и стиля!

💡 Теперь используйте #writing_done вместо #training_done для отчетов о писательской работе!`)
			b.api.Send(notification)
		}
	}

	// Рассчитываем калории и серию
	caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement, oneHundredEightyDayAchievement, twoHundredDayAchievement, twoHundredFortyDayAchievement := b.calculateCalories(messageLog)

	// ДЕБАГ: Логируем результат расчета
	b.logger.Infof("DEBUG handleTrainingDone: caloriesToAdd=%d, newStreakDays=%d, newCalorieStreakDays=%d, weeklyAchievement=%t, twoWeekAchievement=%t, threeWeekAchievement=%t, monthlyAchievement=%t, fortyTwoDayAchievement=%t, fiftyDayAchievement=%t, sixtyDayAchievement=%t, quarterlyAchievement=%t, hundredDayAchievement=%t, oneHundredEightyDayAchievement=%t, twoHundredDayAchievement=%t, twoHundredFortyDayAchievement=%t",
		caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement, oneHundredEightyDayAchievement, twoHundredDayAchievement, twoHundredFortyDayAchievement)

	// Начисляем калории
	if err := b.db.AddCalories(msg.From.ID, msg.Chat.ID, caloriesToAdd); err != nil {
		b.logger.Errorf("Failed to add calories: %v", err)
	} else {
		b.logger.Infof("DEBUG: Successfully added %d calories", caloriesToAdd)
	}

	// Проверяем, достиг ли пользователь 100 калорий/слов для обмена
	if caloriesToAdd > 0 {
		// Получаем обновленное количество калорий/слов
		updatedCalories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
		if err != nil {
			b.logger.Errorf("Failed to get updated calories: %v", err)
		} else if updatedCalories >= 100 && updatedCalories-caloriesToAdd < 100 {
			// Определяем тип чата для адаптации текста
			chatTypeForExchange, err := b.db.GetChatType(msg.Chat.ID)
			if err != nil {
				chatTypeForExchange = "training" // По умолчанию
			}
			// Пользователь только что достиг 100 калорий/слов
			var messageText string
			if chatTypeForExchange == "writing" {
				messageText = fmt.Sprintf("🎉 Поздравляю! 🎉\n\n%s, достигнуто %d %s!\n\n🔄 Теперь можешь совершить обмен!\n💡 Напиши #change для обмена 100 %s на 42 кубка!", username, updatedCalories, getWordForm(updatedCalories), getWordForm(100))
			} else {
				messageText = fmt.Sprintf("🎉 Поздравляю! 🎉\n\n%s, достигнуто %d калорий!\n\n🔄 Теперь можешь совершить обмен!\n💡 Напиши #change для обмена 100 калорий на 42 кубка!", username, updatedCalories)
			}

			// Короткая ИИ‑приписка про обмен
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
				var q string
				if chatTypeForExchange == "writing" {
					q = "Сделай короткую приписку (1–2 предложения): дружелюбно и по делу предложи обмен через #change. Обязательно поясни, что после обмена СЛОВА обнулятся и начнут накапливаться заново; обмен имеет смысл, если ожидается перерыв в писательстве. Укажи, что серия и кубки продолжаются как обычно. КРИТИЧЕСКИ ВАЖНО: используй ТОЛЬКО слово 'слова', НЕ упоминай 'калории', 'тренировки' или 'спорт'. Не повторяй цифры из текста, без Markdown."
				} else {
					q = "Сделай короткую приписку (1–2 предложения): дружелюбно и по делу предложи обмен через #change. Обязательно поясни, что после обмена калории обнулятся и начнут накапливаться заново; обмен имеет смысл, если ожидается перерыв в тренировках. Укажи, что серия и кубки продолжаются как обычно. Не повторяй цифры из текста, без Markdown."
				}
				var ctxBuilder strings.Builder
				ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", username))
				// Добавляем пол пользователя в контекст
				genderNormalized := strings.TrimSpace(strings.ToLower(userGender))
				if genderNormalized != "" {
					var genderText string
					if genderNormalized == "f" {
						genderText = "женский"
					} else if genderNormalized == "m" {
						genderText = "мужской"
					}
					if genderText != "" {
						ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
					}
				}
				ctxBuilder.WriteString(fmt.Sprintf("Текущие калории: %d\n", updatedCalories))
				ctxBuilder.WriteString(fmt.Sprintf("Текущие кубки: %d\n", totalCups))
				if add, err := b.aiClient.AnswerUserQuestion(q, ctxBuilder.String()); err == nil {
					add = strings.TrimSpace(strings.ReplaceAll(add, "**", ""))
					if add != "" {
						messageText = messageText + "\n\n" + add
					}
				} else {
					b.logger.Warnf("AI addendum generation (exchange) failed: %v", err)
				}
			}

			exchangeMessage := tgbotapi.NewMessage(msg.Chat.ID, messageText)

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
			// Проверяем кубки до начисления
			cupsBefore, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add monthly cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for monthly achievement")
				// Проверяем, достиг ли пользователь 420 кубков и является ли 3-м
				if cupsBefore < 420 {
					b.checkMerchGiveawayCompletion(msg, msg.Chat.ID)
				}
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
			// Проверяем кубки до начисления
			cupsBefore, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add 60-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for 60-day achievement")
				// Проверяем, достиг ли пользователь 420 кубков и является ли 3-м
				if cupsBefore < 420 {
					b.checkMerchGiveawayCompletion(msg, msg.Chat.ID)
				}
			}
		}

		if quarterlyAchievement {
			// Проверяем кубки до начисления
			cupsBefore, _ := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add quarterly cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for quarterly achievement")
				// Проверяем, достиг ли пользователь 420 кубков и является ли 3-м
				if cupsBefore < 420 {
					b.checkMerchGiveawayCompletion(msg, msg.Chat.ID)
				}
			}
		}

		if hundredDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 4200); err != nil {
				b.logger.Errorf("Failed to add 100-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 4200 cups for 100-day achievement")
			}
		}

		if oneHundredEightyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add 180-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for 180-day achievement")
			}
		}
		if twoHundredDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add 200-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for 200-day achievement")
			}
		}
		if twoHundredFortyDayAchievement {
			if err := b.db.AddCups(msg.From.ID, msg.Chat.ID, 420); err != nil {
				b.logger.Errorf("Failed to add 240-day cups: %v", err)
			} else {
				b.logger.Infof("Successfully added 420 cups for 240-day achievement")
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

	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
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

			// Новая тренировка БЕЗ achievement - готовим базовый текст
			// Адаптируем текст в зависимости от типа чата
			var messageText string
			if chatType == "writing" {
				messageText = fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Ты пишешь дней подряд: %d\n📝 +%d %s\n📝 Всего %s: %d\n🏆 +1 кубок за писательскую сессию!\n🏆 Всего кубков: %d\n\n⏰ Таймер перезапускается на 7 дней", newStreakDays, caloriesToAdd, getWordForm(caloriesToAdd), getWordForm(totalCalories), totalCalories, currentCups)
			} else {
				messageText = fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Ты тренируешься дней подряд: %d\n🔥 +%d калорий\n🔥 Всего калорий: %d\n🏆 +1 кубок за тренировку!\n🏆 Всего кубков: %d\n\n⏰ Таймер перезапускается на 7 дней", newStreakDays, caloriesToAdd, totalCalories, currentCups)
			}

			// Дополняем короткой ИИ-припиской по текущему контексту
			if b.aiClient != nil {
				// Индикатор набора, чтобы показать «typing...» пока генерируется ответ
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

				// Формируем вопрос для единого AI-сообщения (объединяем приписку и мудрость в одно)
				question := b.getUnifiedTrainingPrompt(newStreakDays, totalCalories, currentCups, wasOnSickLeave, chatType)
				var ctxBuilder strings.Builder
				ctxBuilder.WriteString("КРИТИЧЕСКИ ВАЖНО: Отвечай ТОЛЬКО на этот отчёт. НЕ используй историю чата, последние сообщения или сообщения других участников. Комментируй исключительно то, что написано в этом сообщении.\n\n")
				ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", username))
				// Добавляем пол пользователя в контекст
				genderNormalized := strings.TrimSpace(strings.ToLower(userGender))
				if genderNormalized != "" {
					var genderText string
					if genderNormalized == "f" {
						genderText = "женский"
					} else if genderNormalized == "m" {
						genderText = "мужской"
					}
					if genderText != "" {
						ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
					}
				}
				// Добавляем текст сообщения пользователя
				trainingText := msg.Text
				if trainingText == "" && msg.Caption != "" {
					trainingText = msg.Caption
				}
				if trainingText != "" {
					// Убираем хэштег #training_done из текста для контекста
					trainingTextClean := strings.ReplaceAll(trainingText, "#training_done", "")
					trainingTextClean = strings.TrimSpace(trainingTextClean)
					if trainingTextClean != "" {
						if chatType == "writing" {
							ctxBuilder.WriteString(fmt.Sprintf("Сообщение о писательской работе: %s\n", trainingTextClean))
						} else {
							ctxBuilder.WriteString(fmt.Sprintf("Сообщение о тренировке: %s\n", trainingTextClean))
						}
					}
				}
				ctxBuilder.WriteString(fmt.Sprintf("Серия: %d дней\n", newStreakDays))
				if chatType == "writing" {
					ctxBuilder.WriteString(fmt.Sprintf("Добавлено %s: %d\n", getWordForm(caloriesToAdd), caloriesToAdd))
					ctxBuilder.WriteString(fmt.Sprintf("Текущие %s: %d\n", getWordForm(totalCalories), totalCalories))
				} else {
					ctxBuilder.WriteString(fmt.Sprintf("Добавлено калорий: %d\n", caloriesToAdd))
					ctxBuilder.WriteString(fmt.Sprintf("Текущие калории: %d\n", totalCalories))
				}
				ctxBuilder.WriteString(fmt.Sprintf("Кубков всего: %d\n", currentCups))

				// Добавляем контекст времени для разнообразия
				now := utils.GetMoscowTime()
				hour := now.Hour()
				weekday := now.Weekday()
				weekdayNames := []string{"воскресенье", "понедельник", "вторник", "среда", "четверг", "пятница", "суббота"}
				if chatType == "writing" {
					ctxBuilder.WriteString(fmt.Sprintf("Время писательской сессии: %s, %d:00\n", weekdayNames[weekday], hour))
				} else {
					ctxBuilder.WriteString(fmt.Sprintf("Время тренировки: %s, %d:00\n", weekdayNames[weekday], hour))
				}

				// Определяем время суток
				var timeOfDay string
				if hour >= 5 && hour < 12 {
					timeOfDay = "утро"
				} else if hour >= 12 && hour < 17 {
					timeOfDay = "день"
				} else if hour >= 17 && hour < 22 {
					timeOfDay = "вечер"
				} else {
					timeOfDay = "ночь"
				}
				ctxBuilder.WriteString(fmt.Sprintf("Время суток: %s\n", timeOfDay))

				if wasOnSickLeave {
					if chatType == "writing" {
						ctxBuilder.WriteString("Недавно был больничный, теперь снова за писательством.\n")
					} else {
						ctxBuilder.WriteString("Недавно был больничный, теперь снова в строю.\n")
					}
				}

				// Вычисляем, сколько дней прошло с последней тренировки/писательской сессии
				// ТОЛЬКО для чатов тренировок - не добавляем для писательства
				if chatType != "writing" && messageLog.LastTrainingDate != nil {
					lastTrainingDate, err := time.Parse("2006-01-02", *messageLog.LastTrainingDate)
					if err == nil {
						today := utils.GetMoscowTime()
						todayDate := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
						lastTrainingDateOnly := time.Date(lastTrainingDate.Year(), lastTrainingDate.Month(), lastTrainingDate.Day(), 0, 0, 0, 0, lastTrainingDate.Location())
						daysSinceLastTraining := int(todayDate.Sub(lastTrainingDateOnly).Hours() / 24)
						// Если прошло больше 5 дней с последней тренировки, добавляем шутку про обед
						if daysSinceLastTraining > 5 {
							ctxBuilder.WriteString(fmt.Sprintf("ВАЖНО: С последней тренировки прошло %d дней — это очень редко! Если будешь продолжать так редко заниматься, станешь обедом. Добавь легкий юмор про это в приписку.\n", daysSinceLastTraining))
						}
					}
				}

				// Добавляем информацию о приближении к достижениям для мотивации
				if newStreakDays == 6 {
					if chatType == "writing" {
						ctxBuilder.WriteString("ВАЖНО: Завтра будет 7 дней подряд писательства — важный рубеж! Можешь мягко намекнуть на это.\n")
					} else {
						ctxBuilder.WriteString("ВАЖНО: Завтра будет 7 дней подряд — важный рубеж! Можешь мягко намекнуть на это.\n")
					}
				} else if newStreakDays == 13 {
					if chatType == "writing" {
						ctxBuilder.WriteString("ВАЖНО: Завтра будет 14 дней подряд писательства — отличный результат! Можешь мягко намекнуть на это.\n")
					} else {
						ctxBuilder.WriteString("ВАЖНО: Завтра будет 14 дней подряд — отличный результат! Можешь мягко намекнуть на это.\n")
					}
				} else if newStreakDays == 20 {
					if chatType == "writing" {
						ctxBuilder.WriteString("ВАЖНО: Завтра будет 21 день подряд писательства — впечатляющая серия! Можешь мягко намекнуть на это.\n")
					} else {
						ctxBuilder.WriteString("ВАЖНО: Завтра будет 21 день подряд — впечатляющая серия! Можешь мягко намекнуть на это.\n")
					}
				}

				// КРИТИЧЕСКИ ВАЖНО: НЕ добавляем историю сообщений и контекст других участников.
				// Ответ должен быть ТОЛЬКО на этот отчёт о тренировке/писательстве — без смешивания с другими сообщениями чата.

				if aiResponse, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
					aiResponse = strings.TrimSpace(strings.ReplaceAll(aiResponse, "**", ""))
					if aiResponse != "" {
						messageText = messageText + "\n\n" + aiResponse
					}
				} else {
					b.logger.Warnf("AI response generation failed: %v", err)
				}
			}

			// Добавляем фразу о продолжении тренировок/писательства в самом конце
			if chatType == "writing" {
				messageText = messageText + "\n\n🎯 Продолжай писать и не забывай отправлять #writing_done!"
			} else {
				messageText = messageText + "\n\n🎯 Продолжай тренироваться и не забывай отправлять #training_done!"
			}

			reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)

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

			// Адаптируем текст для дополнительной тренировки/писательской сессии
			var messageText string
			if chatType == "writing" {
				messageText = fmt.Sprintf("🦁 Какой мотивированный леопард! Еще одна писательская сессия сегодня! 💪\n\n🔥 Твоя мотивация впечатляет\n🏆 +1 кубок за дополнительную писательскую сессию!\n🏆 Всего кубков: %d\n\n⏰ Таймер уже перезапущен на 7 дней\n\n🎯 Завтра снова отправляй #writing_done для продолжения серии!", currentCups)
			} else {
				messageText = fmt.Sprintf("🦁 Какой мотивированный леопард! Еще одна тренировка сегодня! 💪\n\n🔥 Твоя мотивация впечатляет\n🏆 +1 кубок за дополнительную тренировку!\n🏆 Всего кубков: %d\n\n⏰ Таймер уже перезапущен на 7 дней\n\n🎯 Завтра снова отправляй #training_done для продолжения серии!", currentCups)
			}

			// Короткая ИИ-приписка и здесь
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

				// Единое сообщение для дополнительной тренировки/писательской сессии (объединяем приписку и мудрость) - БЕЗ общих фраз
				var doubleTrainingPrompts []string
				if chatType == "writing" {
					doubleTrainingPrompts = []string{
						"Двойная писательская сессия за день — особое вдохновение! Напиши 2-3 предложения: первое — отметь упорство и писательскую работу, второе — короткое практическое замечание. Используй ТОЛЬКО этот отчёт. Без Markdown.",
						"Вторая писательская сессия за день. Напиши 2-3 предложения: первое — похвали за энергию и отметь работу, второе — конкретное наблюдение. Используй ТОЛЬКО этот отчёт. Без Markdown.",
					}
				} else {
					doubleTrainingPrompts = []string{
						"Двойная тренировка за день — Fat Leopard впечатлён! Напиши 2-3 предложения: первое — отметь упорство и упражнения, второе — короткое замечание. Можно лёгкий юмор: «таких я не ем». Используй ТОЛЬКО этот отчёт. Без Markdown.",
						"Вторая тренировка за день — не все на это способны. Напиши 2-3 предложения: первое — похвали за энергию и отметь упражнения, второе — короткий совет по восстановлению. Используй ТОЛЬКО этот отчёт. Без Markdown.",
						"Двойная тренировка — особое упорство! Напиши 2-3 предложения: первое — отметь упражнения, второе — комментарий. Fat Leopard одобряет. Используй ТОЛЬКО этот отчёт. Без Markdown.",
						"Ещё одна тренировка сегодня — энергия впечатляет. Напиши 2-3 предложения: первое — отметь упражнения, второе — практический совет. Используй ТОЛЬКО этот отчёт. Без Markdown.",
					}
				}
				now := utils.GetMoscowTime()
				question := doubleTrainingPrompts[now.Unix()%int64(len(doubleTrainingPrompts))]
				var ctxBuilder strings.Builder
				ctxBuilder.WriteString("КРИТИЧЕСКИ ВАЖНО: Отвечай ТОЛЬКО на этот отчёт. НЕ используй историю чата или сообщения других участников.\n\n")
				ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", username))
				// Добавляем пол пользователя в контекст
				genderNormalized := strings.TrimSpace(strings.ToLower(userGender))
				if genderNormalized != "" {
					var genderText string
					if genderNormalized == "f" {
						genderText = "женский"
					} else if genderNormalized == "m" {
						genderText = "мужской"
					}
					if genderText != "" {
						ctxBuilder.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
					}
				}
				// Добавляем текст сообщения пользователя
				trainingTextDouble := msg.Text
				if trainingTextDouble == "" && msg.Caption != "" {
					trainingTextDouble = msg.Caption
				}
				if trainingTextDouble != "" {
					// Убираем хэштеги #training_done и #writing_done из текста для контекста
					trainingTextClean := strings.ReplaceAll(trainingTextDouble, "#training_done", "")
					trainingTextClean = strings.ReplaceAll(trainingTextClean, "#writing_done", "")
					trainingTextClean = strings.TrimSpace(trainingTextClean)
					if trainingTextClean != "" {
						if chatType == "writing" {
							ctxBuilder.WriteString(fmt.Sprintf("Сообщение о писательской работе: %s\n", trainingTextClean))
						} else {
							ctxBuilder.WriteString(fmt.Sprintf("Сообщение о тренировке: %s\n", trainingTextClean))
						}
					}
				}
				if chatType == "writing" {
					ctxBuilder.WriteString("Уже была писательская сессия сегодня, это повторная.\n")
				} else {
					ctxBuilder.WriteString("Уже была тренировка сегодня, это повторная.\n")
				}
				ctxBuilder.WriteString(fmt.Sprintf("Кубков всего: %d\n", currentCups))
				if wasOnSickLeave {
					if chatType == "writing" {
						ctxBuilder.WriteString("Недавно был больничный, теперь снова за писательством.\n")
					} else {
						ctxBuilder.WriteString("Недавно был больничный, теперь снова в строю.\n")
					}
				}

				// КРИТИЧЕСКИ ВАЖНО: НЕ добавляем историю сообщений и контекст других участников.
				// Ответ только на этот отчёт — без смешивания с другими сообщениями чата.

				if aiResponse, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String()); err == nil {
					aiResponse = strings.TrimSpace(strings.ReplaceAll(aiResponse, "**", ""))
					if aiResponse != "" {
						messageText = messageText + "\n\n" + aiResponse
					}
				} else {
					b.logger.Warnf("AI response generation (double) failed: %v", err)
				}
			}

			reply := tgbotapi.NewMessage(msg.Chat.ID, messageText)

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

		// Получаем пол пользователя для правильных форм слов
		messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
		userGender := ""
		if err == nil {
			userGender = strings.TrimSpace(strings.ToLower(messageLog.Gender))
			if userGender == "" {
				userGender = b.detectGenderFromName(msg.From.FirstName)
			}
		}

		if weeklyAchievement {
			b.sendWeeklyCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}
		if twoWeekAchievement {
			b.sendTwoWeekCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}
		if threeWeekAchievement {
			b.sendThreeWeekCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}
		if monthlyAchievement {
			b.sendMonthlyCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}
		if fortyTwoDayAchievement {
			b.sendFortyTwoDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}
		if fiftyDayAchievement {
			b.sendFiftyDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}
		if sixtyDayAchievement {
			b.sendSixtyDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}
		if quarterlyAchievement {
			b.sendQuarterlyCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}
		if hundredDayAchievement {
			b.sendHundredDayCupsReward(msg, username, newStreakDays, caloriesToAdd, userGender)
		}

		// Проверяем супер-уровень после начисления кубков
		totalCups, err := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
		if err != nil {
			b.logger.Errorf("Failed to get user cups for super level check: %v", err)
		} else if totalCups >= 420 {
			// Проверяем, является ли этот пользователь 3-м с 420+ кубками
			usersWith420Cups, err := b.db.CountUsersWithCups(msg.Chat.ID, 420)
			if err != nil {
				b.logger.Errorf("Failed to count users with 420+ cups: %v", err)
			} else if usersWith420Cups == 3 {
				// 3-й участник достиг 420 кубков - розыгрыш завершен
				merchMessage := tgbotapi.NewMessage(msg.Chat.ID, `🎉🎊 РОЗЫГРЫШ ЗАВЕРШЕН! 🎊🎉
				
Третий участник достиг 420 кубков! 
				
🏆 Розыгрыш футболки Fat Leopard официально закрыт!
				
Поздравляем всех участников, которые набрали 420+ кубков! 🦁💪`)
				if _, err := b.api.Send(merchMessage); err != nil {
					b.logger.Errorf("Failed to send merch giveaway completion message: %v", err)
				}
			}

			// Отправляем сообщение о супер-уровне (если еще не достигли 10000)
			if totalCups < 10000 {
				b.sendSuperLevelMessage(msg, username, totalCups, userGender)
			}
		}
	}

	// Если пользователь был на больничном, сбрасываем флаги больничного и помечаем как здорового
	if wasOnSickLeave {
		// Отправляем предупреждение о забытом #healthy
		warningMessage := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("⚠️ Внимание, %s!\n\nне забывай отправлять #healthy перед тренировкой!\n\n✅ Я автоматически засчитал выздоровление, но в следующий раз не забывай отправлять #healthy перед #training_done", username))
		b.logger.Infof("Sending forgotten #healthy warning to user %d (%s)", msg.From.ID, username)
		b.api.Send(warningMessage)

		// ВАЖНО: перечитываем актуальную запись из БД, чтобы не перетереть свежие
		// начисления кубков/калорий и обновления серий устаревшим messageLog.
		latestLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
		if err != nil {
			b.logger.Errorf("Failed to refresh message log before sick leave reset: %v", err)
			latestLog = messageLog // fallback на старую запись, чтобы не блокировать флоу
		}

		latestLog.HasSickLeave = false
		latestLog.HasHealthy = true
		latestLog.SickLeaveStartTime = nil
		if err := b.db.SaveMessageLog(latestLog); err != nil {
			b.logger.Errorf("Failed to reset sick leave flags: %v", err)
		}
		b.logger.Infof("Reset sick leave flags and marked as healthy for user %d (%s) after training during sick leave", msg.From.ID, username)
	}

	// Сохраняем сессию в отдельную таблицу для аналитики (начиная с 2026-03-01).
	// Это позволяет отвечать на вопросы: "что делал", "сколько тренировок", "сколько кубков за день".
	sessionDate := utils.GetMoscowDate()
	if sessionDate >= "2026-03-01" {
		cupsAfter, cupsErr := b.db.GetUserCups(msg.From.ID, msg.Chat.ID)
		if cupsErr != nil {
			b.logger.Warnf("Failed to get cups after training session save: %v", cupsErr)
			cupsAfter = cupsBefore
		}
		cupsAdded := cupsAfter - cupsBefore
		if cupsAdded < 0 {
			cupsAdded = 0
		}

		sessionText := text
		sessionText = strings.ReplaceAll(sessionText, "#training_done", "")
		sessionText = strings.ReplaceAll(sessionText, "#writing_done", "")
		sessionText = strings.TrimSpace(sessionText)

		if err := b.db.SaveTrainingSession(&domain.TrainingSession{
			UserID:         msg.From.ID,
			ChatID:         msg.Chat.ID,
			SessionDate:    sessionDate,
			MessageText:    sessionText,
			TrainingsCount: 1,
			CupsAdded:      cupsAdded,
			IsBonus:        false,
		}); err != nil {
			b.logger.Errorf("Failed to save training session: %v", err)
		} else {
			// Логика бонуса по вашему правилу:
			// 1) бонус считается ТОЛЬКО в день возврата после пропуска;
			// 2) считаем тренировки в предыдущие 7 дней (без текущего дня);
			// 3) бонус = floor(count/3) * 10;
			// 4) в день серии 7/7 (weeklyAchievement) эта логика не применяется;
			// 5) новое окно стартует с даты выдачи бонуса.
			nowMSK := utils.GetMoscowTime()
			yesterday := utils.GetMoscowDateFromTime(nowMSK.AddDate(0, 0, -1))
			returnAfterSkip := messageLog.LastTrainingDate != nil &&
				*messageLog.LastTrainingDate != yesterday &&
				*messageLog.LastTrainingDate != sessionDate

			if returnAfterSkip && !weeklyAchievement {
				// Базовое окно: предыдущие 7 дней (без текущего дня).
				windowEndDate := utils.GetMoscowDateFromTime(nowMSK.AddDate(0, 0, -1))
				windowStartDate := utils.GetMoscowDateFromTime(nowMSK.AddDate(0, 0, -7))

				// Если бонус уже был, окно не может начинаться раньше даты последнего бонуса.
				lastBonusDate, lastBonusErr := b.db.GetLastBonusSessionDate(msg.From.ID, msg.Chat.ID)
				if lastBonusErr != nil {
					b.logger.Errorf("Failed to get last bonus date: %v", lastBonusErr)
				} else if lastBonusDate != nil && *lastBonusDate > windowStartDate {
					windowStartDate = *lastBonusDate
				}

				sessionCountInWindow, cntErr := b.db.CountTrainingSessionsInDateRange(msg.From.ID, msg.Chat.ID, windowStartDate, windowEndDate)
				if cntErr != nil {
					b.logger.Errorf("Failed to count training sessions for 7-day return bonus: %v", cntErr)
				} else {
					bonusCups := (sessionCountInWindow / 3) * 10
					if bonusCups > 0 {
						if addErr := b.db.AddCups(msg.From.ID, msg.Chat.ID, bonusCups); addErr != nil {
							b.logger.Errorf("Failed to add 7-day return bonus cups: %v", addErr)
						} else {
							// Маркер бонуса: фиксируем дату старта следующего окна.
							if saveBonusErr := b.db.SaveTrainingSession(&domain.TrainingSession{
								UserID:         msg.From.ID,
								ChatID:         msg.Chat.ID,
								SessionDate:    sessionDate,
								MessageText:    "BONUS_7D_WINDOW",
								TrainingsCount: 0,
								CupsAdded:      bonusCups,
								IsBonus:        true,
							}); saveBonusErr != nil {
								b.logger.Errorf("Failed to save 7-day return bonus marker: %v", saveBonusErr)
							}

							chatTypeForBonus, typeErr := b.db.GetChatType(msg.Chat.ID)
							if typeErr != nil {
								chatTypeForBonus = "training"
							}
							bonusText := fmt.Sprintf("🎁 Бонус активности: в предыдущие 7 дней у тебя %d тренировок. +%d кубков 🏆", sessionCountInWindow, bonusCups)
							if chatTypeForBonus == "writing" {
								bonusText = fmt.Sprintf("🎁 Бонус активности: в предыдущие 7 дней у тебя %d писательских сессий. +%d кубков 🏆", sessionCountInWindow, bonusCups)
							}
							if _, sendErr := b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, bonusText)); sendErr != nil {
								b.logger.Errorf("Failed to send 7-day return bonus message: %v", sendErr)
							}
						}
					}
				}
			}
		}
	}

	// Таймер уже перезапущен в начале функции для предотвращения race condition
}

func (b *Bot) evaluateSickLeaveHeuristics(text string) (approved bool, hasNegative bool) {
	if text == "" {
		return false, false
	}

	for _, neg := range sickLeaveNegativeKeywords {
		if strings.Contains(text, neg) {
			return false, true
		}
	}

	score := 0
	for _, pos := range sickLeavePositiveKeywords {
		if strings.Contains(text, pos) {
			score++
		}
	}
	if strings.Contains(text, "боле") {
		score++
	}
	if strings.Contains(text, "забол") {
		score++
	}
	if strings.Contains(text, "простуд") {
		score++
	}
	if strings.Contains(text, "температ") {
		score++
	}
	if strings.Contains(text, "кашл") {
		score++
	}
	if strings.Contains(text, "плохое самочувствие") {
		score++
	}
	for _, sup := range sickLeaveSupportKeywords {
		if strings.Contains(text, sup) {
			score++
		}
	}
	return score >= 1, false
}

func (b *Bot) evaluateSickLeaveJustification(text string, messageLog *domain.MessageLog) bool {
	clean := strings.TrimSpace(strings.ToLower(text))
	clean = strings.ReplaceAll(clean, "#sick_leave", "")
	clean = strings.ReplaceAll(clean, "#sickleave", "")
	clean = strings.ReplaceAll(clean, "#healthy", "")
	clean = strings.ReplaceAll(clean, "#здоров", "")

	heuristicsApprove, hasNegative := b.evaluateSickLeaveHeuristics(clean)

	if heuristicsApprove {
		return true
	}
	if hasNegative {
		return false
	}
	if b.aiClient == nil || clean == "" {
		return false
	}

	var ctxBuilder strings.Builder
	ctxBuilder.WriteString("Оцени убедительность больничного запроса.\n")
	if messageLog != nil {
		ctxBuilder.WriteString(fmt.Sprintf("Пользователь: %s\n", messageLog.Username))
		ctxBuilder.WriteString(fmt.Sprintf("StreakDays: %d\n", messageLog.StreakDays))
		ctxBuilder.WriteString(fmt.Sprintf("CalorieStreakDays: %d\n", messageLog.CalorieStreakDays))
		ctxBuilder.WriteString(fmt.Sprintf("HasSickLeave: %t\n", messageLog.HasSickLeave))
		ctxBuilder.WriteString(fmt.Sprintf("HasHealthy: %t\n", messageLog.HasHealthy))
	}
	ctxBuilder.WriteString(fmt.Sprintf("Текст запроса: \"%s\"\n", clean))
	ctxBuilder.WriteString("Эвристика не нашла явных признаков ни болезни, ни обмана.\n")

	question := "Если сообщение описывает реальную болезнь, ответь строго словом APPROVE. " +
		"Если это похоже на отговорку (работа, дела, лень и т.п.), ответь строго словом REJECT. " +
		"Никаких других слов или пояснений."

	answer, err := b.aiClient.AnswerUserQuestion(question, ctxBuilder.String())
	if err != nil {
		b.logger.Errorf("AI sick leave evaluation failed: %v", err)
		return false
	}

	normalized := strings.ToUpper(strings.TrimSpace(answer))
	if strings.Contains(normalized, "APPROVE") {
		return true
	}
	if strings.Contains(normalized, "REJECT") {
		return false
	}

	return false
}

func (b *Bot) handleChange(msg *tgbotapi.Message) {
	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
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

	// Курс обмена: 100 калорий/слов = 42 кубка
	exchangeRate := 100
	cupsPerExchange := 42
	exchangesCanMake := currentCalories / exchangeRate

	if exchangesCanMake == 0 {
		// Недостаточно калорий/слов для обмена
		var replyText string
		if chatType == "writing" {
			replyText = fmt.Sprintf("💪 %s, у тебя %d %s\n\n🔄 Для обмена нужно минимум %d %s\n🏆 За %d %s можно получить %d кубков\n\n⏰ Пока рано! Еще попиши!\n\n🎯 Продолжай писать и накапливай слова!", username, currentCalories, getWordForm(currentCalories), exchangeRate, getWordForm(exchangeRate), exchangeRate, getWordForm(exchangeRate), cupsPerExchange)
		} else {
			replyText = fmt.Sprintf("💪 %s, у тебя %d калорий\n\n🔄 Для обмена нужно минимум %d калорий\n🏆 За %d калорий можно получить %d кубков\n\n⏰ Пока рано! Еще потренируйся!\n\n🎯 Продолжай тренироваться и накапливай калории!", username, currentCalories, exchangeRate, exchangeRate, cupsPerExchange)
		}
		reply := tgbotapi.NewMessage(msg.Chat.ID, replyText)
		if chatType == "writing" {
			b.logger.Infof("Sending insufficient words message to chat %d", msg.Chat.ID)
		} else {
			b.logger.Infof("Sending insufficient calories message to chat %d", msg.Chat.ID)
		}
		_, err = b.api.Send(reply)
		if err != nil {
			if chatType == "writing" {
				b.logger.Errorf("Failed to send insufficient words message: %v", err)
			} else {
				b.logger.Errorf("Failed to send insufficient calories message: %v", err)
			}
		} else {
			if chatType == "writing" {
				b.logger.Infof("Successfully sent insufficient words message to chat %d", msg.Chat.ID)
			} else {
				b.logger.Infof("Successfully sent insufficient calories message to chat %d", msg.Chat.ID)
			}
		}
		return
	}

	// Выполняем обмен (только полные обмены)
	caloriesToSpend := exchangesCanMake * exchangeRate
	cupsToAdd := exchangesCanMake * cupsPerExchange

	// Списываем калории/слова
	var spendErrorMsg string
	if chatType == "writing" {
		spendErrorMsg = "❌ Ошибка при списании слов"
	} else {
		spendErrorMsg = "❌ Ошибка при списании калорий"
	}
	if err := b.db.AddCalories(msg.From.ID, msg.Chat.ID, -caloriesToSpend); err != nil {
		b.logger.Errorf("Failed to spend calories/words: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, spendErrorMsg)
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
	var replyText string
	if chatType == "writing" {
		replyText = fmt.Sprintf("🔄 Обмен выполнен! 💪\n\n%s написано 📝 %d %s → 🏆 %d кубка\n\n📊 Твой баланс:\n📝 Слова: %d\n🏆 Кубки: %d\n\n💡 Курс: %d %s = %d кубка", username, caloriesToSpend, getWordForm(caloriesToSpend), cupsToAdd, newCalories, newCups, exchangeRate, getWordForm(exchangeRate), cupsPerExchange)
	} else {
		replyText = fmt.Sprintf("🔄 Обмен выполнен! 💪\n\n%s сожжено 🔥 %d калорий → 🏆 %d кубка\n\n📊 Твой баланс:\n🔥 Калории: %d\n🏆 Кубки: %d\n\n💡 Курс: %d калорий = %d кубка", username, caloriesToSpend, cupsToAdd, newCalories, newCups, exchangeRate, cupsPerExchange)
	}
	reply := tgbotapi.NewMessage(msg.Chat.ID, replyText)

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
	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
	}

	var helpText string
	if chatType == "writing" {
		helpText = `🤖 LeoPoacherBot - Команды (Чат писательства):

📝 Команды администратора:
• /start_timer — Запустить таймеры для всех пользователей
• /db — Показать статистику БД
• /help — Показать это сообщение

🤖 ИИ-помощник:
• Отметьте меня @LeoPoacherBot в сообщении для общения
• Или ответьте (reply) на любое мое сообщение
• Я могу давать советы, показывать статистику и мотивировать!

🏆 Команды пользователей:
• /top — Показать топ пользователей по словам
• /points — Показать ваши слова
• /cups — Показать ваши заработанные кубки

📝 Отчеты о писательской работе:
• #writing_done — Отправить отчет о писательской сессии

🏥 Больничный:
• #sick_leave — Взять больничный (приостанавливает таймер)
• #healthy — Выздороветь (возобновляет таймер)

🔄 Обмен:
• #change — Обменять слова на кубки (100 слов = 42 кубка)

⏰ Как работает бот:
• При добавлении бота в чат запускаются таймеры для всех участников
• При получении #writing_done таймер перезапускается на 7 дней
• Через 6 дней без #writing_done - предупреждение
• Через 7 дней без #writing_done - удаление из чата
• 🏆 За каждую писательскую сессию = 1 КУБОК! 🏆
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
• Отчётом считается любое сообщение с тегом #writing_done
• Если заболели — отправь #sick_leave
• После выздоровления — отправь #healthy
• Через 6 дней без отчёта — предупреждение
• Через 7 дней без отчёта — удаление из чата

Оставайся активным и не становись жирным леопардом! 🦁`
	} else {
		helpText = `🤖 LeoPoacherBot - Команды:

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
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, helpText)

	b.logger.Infof("Sending help message to chat %d", msg.Chat.ID)
	_, errSend := b.api.Send(reply)
	if errSend != nil {
		b.logger.Errorf("Failed to send help message: %v", errSend)
	} else {
		b.logger.Infof("Successfully sent help message to chat %d", msg.Chat.ID)
	}
}

func (b *Bot) handleStart(msg *tgbotapi.Message) {
	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
	}

	var welcomeText string
	if chatType == "writing" {
		welcomeText = `🦁 **Добро пожаловать в LeoPoacherBot!** 🦁

📝 **Этот бот поможет вам оставаться активным в писательстве и не стать жирным леопардом!**

📋 **Основные команды:**
• /start — Показать это приветствие
• /help — Показать полную справку
• /start_timer — Запустить таймеры (только для администраторов)

📝 **Отчеты о писательской работе:**
• #writing_done — Отправить отчет о писательской сессии

🏥 **Больничный:**
• #sick_leave — Взять больничный (приостанавливает таймер)
• #healthy — Выздороветь (возобновляет таймер)

🔄 **Обмен:**
• #change — Обменять слова на кубки (100 слов = 42 кубка)

⏰ **Как это работает:**
• При добавлении бота в чат запускаются таймеры для всех участников
• Каждый отчет с #writing_done перезапускает таймер на 7 дней
• Через 6 дней без отчета — предупреждение
• Через 7 дней без отчета — удаление из чата
• 🏆 За каждую писательскую сессию = 1 КУБОК! 🏆
• 🏆 7 дней подряд = 42 КУБКА! 🏆
• 🏆🏆 14 дней подряд = 42 КУБКА! 🏆🏆
• 🏆🏆🏆 21 день подряд = 42 КУБКА! 🏆🏆🏆
• 🏆🏆🏆 30 дней подряд = 420 КУБКОВ! 🏆🏆🏆
• 🏆🏆🏆🏆 42 дня подряд = 42 КУБКА! 🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆 50 дней подряд = 42 КУБКА! 🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆 60 дней подряд = 420 КУБКОВ! 🏆🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆🏆🏆 90 дней подряд = 420 КУБКОВ! 🏆🏆🏆🏆🏆🏆🏆🏆
• 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆 100 дней подряд = 4200 КУБКОВ! 🏆🏆🏆🏆🏆🏆🏆🏆🏆🏆

🎯 **Начни прямо сейчас — отправь #writing_done!**`
	} else {
		welcomeText = `🦁 **Добро пожаловать в LeoPoacherBot!** 🦁

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
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcomeText)

	b.logger.Infof("Sending start message to chat %d", msg.Chat.ID)
	_, errSend := b.api.Send(reply)
	if errSend != nil {
		b.logger.Errorf("Failed to send start message: %v", errSend)
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

	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
	}

	if len(topUsers) == 0 {
		var emptyText string
		if chatType == "writing" {
			emptyText = "🏆 **Топ пользователей:**\n\n📊 Пока нет данных о писательских сессиях"
		} else {
			emptyText = "🏆 **Топ пользователей:**\n\n📊 Пока нет данных о тренировках"
		}
		reply := tgbotapi.NewMessage(msg.Chat.ID, emptyText)
		reply.ParseMode = "Markdown"
		b.api.Send(reply)
		return
	}

	// Формируем топ
	var topText string
	if chatType == "writing" {
		topText = "🏆 Топ пользователей по очкам (слов):\n\n"
	} else {
		topText = "🏆 Топ пользователей по очкам (калорий):\n\n"
	}
	for i, user := range topUsers {
		emoji := "🥇"
		if i == 1 {
			emoji = "🥈"
		} else if i == 2 {
			emoji = "🥉"
		} else {
			emoji = fmt.Sprintf("%d️⃣", i+1)
		}
		if chatType == "writing" {
			topText += fmt.Sprintf("%s %s - %d %s\n", emoji, user.Username, user.Calories, getWordForm(user.Calories))
		} else {
			topText += fmt.Sprintf("%s %s - %d калорий\n", emoji, user.Username, user.Calories)
		}
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
	// Получаем калории/слова пользователя
	calories, err := b.db.GetUserCalories(msg.From.ID, msg.Chat.ID)
	if err != nil {
		b.logger.Errorf("Failed to get user calories: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных")
		b.api.Send(reply)
		return
	}

	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
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
	var caloriesText string
	if chatType == "writing" {
		caloriesText = fmt.Sprintf("📝 Ваши слова:\n\n👤 %s\n🎯 Всего написано %s: %d\n\n💡 Отправляйте #writing_done для написания слов!", username, getWordForm(calories), calories)
	} else {
		caloriesText = fmt.Sprintf("🔥 Ваши калории:\n\n👤 %s\n🎯 Всего сожжено калорий: %d\n\n💡 Отправляйте #training_done для сжигания калорий!", username, calories)
	}

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

	// Определяем тип чата для адаптации текста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		chatType = "training" // По умолчанию
	}

	// Получаем пол пользователя для гендерной адаптации
	messageLog, err := b.db.GetMessageLog(msg.From.ID, msg.Chat.ID)
	userGender := ""
	if err == nil {
		userGender = strings.TrimSpace(strings.ToLower(messageLog.Gender))
		if userGender == "" {
			userGender = b.detectGenderFromName(msg.From.FirstName)
		}
	}
	forms := b.getGenderForms(userGender)

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
		if chatType == "writing" {
			cupsText = fmt.Sprintf("🎊 ПОЗДРАВЛЯЕМ! 🎊\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n🏆 ТЫ %s ЦЕЛИ РОЗЫГРЫША!\n🎁 Участвуешь в розыгрыше футболки Fat Leopard!\n💪 Ты настоящая %s!\n🔥 Продолжай писать!", username, cups, strings.ToUpper(forms.Reached), forms.Champion)
		} else {
			cupsText = fmt.Sprintf("🎊 ПОЗДРАВЛЯЕМ! 🎊\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n🏆 ТЫ %s ЦЕЛИ РОЗЫГРЫША!\n🎁 Участвуешь в розыгрыше футболки Fat Leopard!\n💪 Ты настоящий %s!\n🔥 Продолжай тренироваться!", username, cups, strings.ToUpper(forms.Reached), forms.Champion)
		}
	} else {
		if chatType == "writing" {
			cupsText = fmt.Sprintf("🏆 Ваши кубки:\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n💡 Отправляйте #writing_done для получения кубков!\n\n🎊 Розыгрыш футболки Fat Leopard при достижении 420 кубков!", username, cups)
		} else {
			cupsText = fmt.Sprintf("🏆 Ваши кубки:\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n💡 Отправляйте #training_done для получения кубков!\n\n🎊 Розыгрыш футболки Fat Leopard при достижении 420 кубков!", username, cups)
		}
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

	// Группируем чаты по типу для генерации разных мудростей
	writingChats := []int64{}
	trainingChats := []int64{}

	for _, chatID := range chatIDs {
		chatType, err := b.db.GetChatType(chatID)
		if err != nil {
			chatType = "training" // По умолчанию
		}
		if chatType == "writing" {
			writingChats = append(writingChats, chatID)
		} else {
			trainingChats = append(trainingChats, chatID)
		}
	}

	// Генерируем мудрость для чатов тренировок
	if len(trainingChats) > 0 {
		wisdom, err := b.aiClient.GenerateDailyWisdom("training")
		if err != nil {
			b.logger.Errorf("Failed to generate daily wisdom for training: %v", err)
			// Фолбэк на статическую мудрость
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

		for _, chatID := range trainingChats {
			msg := tgbotapi.NewMessage(chatID, wisdom)
			b.logger.Infof("Sending daily wisdom to training chat %d", chatID)
			if _, err := b.api.Send(msg); err != nil {
				b.logger.Errorf("Failed to send daily wisdom to chat %d: %v", chatID, err)
			}
		}
	}

	// Генерируем мудрость для чатов писательства
	if len(writingChats) > 0 {
		wisdom, err := b.aiClient.GenerateDailyWisdom("writing")
		if err != nil {
			b.logger.Errorf("Failed to generate daily wisdom for writing: %v", err)
			// Фолбэк на статическую мудрость для писательства
			candidates := []string{
				"Тишина внутри сильнее шума вокруг. Дисциплина письма — это форма заботы о творчестве. Начни с малого и будь верен тексту.",
				"Сила слова рождается в простых предложениях. Выбери одну мысль сегодня — и вырази её спокойно.",
				"Перо слушает разум. Разум слушает вдохновение. Ровное дыхание — ровный текст.",
				"Пусть писательская сессия будет краткой, но честной. Постоянство сильнее порывов.",
				"Не ищи идеального момента. Создай его. Терпение и слово — союзники."}
			idx := int(time.Now().Unix() % int64(len(candidates)))
			wisdom = candidates[idx]
		} else {
			wisdom = strings.ReplaceAll(wisdom, "**", "")
		}

		for _, chatID := range writingChats {
			msg := tgbotapi.NewMessage(chatID, wisdom)
			b.logger.Infof("Sending daily wisdom to writing chat %d", chatID)
			if _, err := b.api.Send(msg); err != nil {
				b.logger.Errorf("Failed to send daily wisdom to chat %d: %v", chatID, err)
			}
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
		// Определяем тип чата для адаптации текста
		chatType, err := b.db.GetChatType(um.ChatID)
		if err != nil {
			chatType = "training" // По умолчанию
		}
		var text string
		if chatType == "writing" {
			text = fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Я вижу твою писательскую сессию за %s.\n\n⏰ Бот был перезапущен — отправляю подтверждение сейчас.", um.CreatedAt.In(loc).Format("02.01 15:04"))
		} else {
			text = fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Я вижу твою тренировку за %s.\n\n⏰ Бот был перезапущен — отправляю подтверждение сейчас.", um.CreatedAt.In(loc).Format("02.01 15:04"))
		}
		b.api.Send(tgbotapi.NewMessage(um.ChatID, text))
		return
	}

	caloriesToAdd, newStreakDays, newCalorieStreakDays, weeklyAchievement, twoWeekAchievement, threeWeekAchievement, monthlyAchievement, fortyTwoDayAchievement, fiftyDayAchievement, sixtyDayAchievement, quarterlyAchievement, hundredDayAchievement, oneHundredEightyDayAchievement, twoHundredDayAchievement, twoHundredFortyDayAchievement := b.calculateCalories(messageLog)

	if caloriesToAdd > 0 {
		_ = b.db.AddCalories(um.UserID, um.ChatID, caloriesToAdd)
		_ = b.db.UpdateStreak(um.UserID, um.ChatID, newStreakDays, dateStr)
		_ = b.db.UpdateCalorieStreakWithDate(um.UserID, um.ChatID, newCalorieStreakDays, dateStr)
		_ = b.db.AddCups(um.UserID, um.ChatID, 1)
		if weeklyAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 42)
		}
		if twoWeekAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 42)
		}
		if threeWeekAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 42)
		}
		if monthlyAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 420)
		}
		if fortyTwoDayAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 42)
		}
		if fiftyDayAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 42)
		}
		if sixtyDayAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 420)
		}
		if quarterlyAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 420)
		}
		if hundredDayAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 4200)
		}
		if oneHundredEightyDayAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 420)
		}
		if twoHundredDayAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 420)
		}
		if twoHundredFortyDayAchievement {
			_ = b.db.AddCups(um.UserID, um.ChatID, 420)
		}

		totalCalories, _ := b.db.GetUserCalories(um.UserID, um.ChatID)
		currentCups, _ := b.db.GetUserCups(um.UserID, um.ChatID)
		// Определяем тип чата для адаптации текста
		chatType, err := b.db.GetChatType(um.ChatID)
		if err != nil {
			chatType = "training" // По умолчанию
		}
		var text string
		if chatType == "writing" {
			text = fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Ты пишешь дней подряд: %d\n📝 +%d %s\n📝 Всего %s: %d\n🏆 +1 кубок за писательскую сессию!\n🏆 Всего кубков: %d\n\n⏰ Таймер перезапускается на 7 дней", newStreakDays, caloriesToAdd, getWordForm(caloriesToAdd), getWordForm(totalCalories), totalCalories, currentCups)
		} else {
			text = fmt.Sprintf("✅ Отчёт принят! 💪\n\n🦁 Ты тренируешься дней подряд: %d\n🔥 +%d калорий\n🔥 Всего калорий: %d\n🏆 +1 кубок за тренировку!\n🏆 Всего кубков: %d\n\n⏰ Таймер перезапускается на 7 дней", newStreakDays, caloriesToAdd, totalCalories, currentCups)
		}
		b.api.Send(tgbotapi.NewMessage(um.ChatID, text))
	} else {
		_ = b.db.AddCups(um.UserID, um.ChatID, 1)
		currentCups, _ := b.db.GetUserCups(um.UserID, um.ChatID)
		// Определяем тип чата для адаптации текста
		chatType, err := b.db.GetChatType(um.ChatID)
		if err != nil {
			chatType = "training" // По умолчанию
		}
		var text string
		if chatType == "writing" {
			text = fmt.Sprintf("🦁 Какой мотивированный леопард! Еще одна писательская сессия сегодня! 💪\n\n🏆 +1 кубок за дополнительную писательскую сессию!\n🏆 Всего кубков: %d", currentCups)
		} else {
			text = fmt.Sprintf("🦁 Какой мотивированный леопард! Еще одна тренировка сегодня! 💪\n\n🏆 +1 кубок за дополнительную тренировку!\n🏆 Всего кубков: %d", currentCups)
		}
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
	Calories      int
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

	// Определяем тип чата
	chatType, err := b.db.GetChatType(chatID)
	if err != nil {
		chatType = "training"
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
				Calories:      userLog.Calories,
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

	// Название месяца на русском
	monthNames := []string{"января", "февраля", "марта", "апреля", "мая", "июня",
		"июля", "августа", "сентября", "октября", "ноября", "декабря"}
	monthName := monthNames[month.Month()-1]
	year := month.Year()

	// Формируем отчёт в стиле Fat Leopard
	var sb strings.Builder

	sb.WriteString("📊 Отчёт Fat Leopard за ")
	sb.WriteString(monthName)
	sb.WriteString(fmt.Sprintf(" %d\n\n", year))
	sb.WriteString("Привет, стая! 🦁\n\n")

	// Максимум в месяце
	maxTrainings := 0
	for _, u := range usersData {
		if u.TrainingCount > maxTrainings {
			maxTrainings = u.TrainingCount
		}
	}
	workLabel := "тренировок"
	if chatType == "writing" {
		workLabel = "писательских сессий"
	}
	if maxTrainings > 0 {
		sb.WriteString(fmt.Sprintf("Максимум в месяце: %d %s\n\n", maxTrainings, workLabel))
	}

	// Сводка по каждому: пользователь, сколько тренировок, серия на момент отчёта
	for _, u := range usersData {
		name := u.Username
		if name == "" {
			name = fmt.Sprintf("User%d", u.UserID)
		}

		sb.WriteString(fmt.Sprintf("• %s: %d %s", name, u.TrainingCount, workLabel))
		sb.WriteString(fmt.Sprintf(", серия на момент отчёта: %d дн.", u.StreakDays))
		if chatType == "writing" {
			sb.WriteString(fmt.Sprintf(", %d %s, %d кубков", u.Calories, getWordForm(u.Calories), u.Cups))
		} else {
			sb.WriteString(fmt.Sprintf(", %d калорий, %d кубков", u.Calories, u.Cups))
		}

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
		if chatType == "writing" {
			sb.WriteString("Я бы съел пирог. Вы — пишете. Продолжаем в том же духе! 💪🦁")
		} else {
			sb.WriteString("Я бы съел пиццу. Вы — тренировки. Продолжаем в том же духе! 💪🦁")
		}
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

	// Определяем тип чата для правильного заголовка и контекста
	chatType, err := b.db.GetChatType(msg.Chat.ID)
	if err != nil {
		b.logger.Warnf("Failed to get chat type: %v", err)
		chatType = "training" // По умолчанию
	}

	if chatType == "writing" {
		contextText.WriteString("=== КОНТЕКСТ ЧАТА ПИСАТЕЛЬСТВА ===\n\n")
		contextText.WriteString("Это чат для писательства. Веди себя как мудрый литературный наставник. Помни всю переписку в этом чате и используй её для контекста при ответе.\n\n")
	} else {
		contextText.WriteString("=== ИСТОРИЯ ТРЕНИРОВОК ПОЛЬЗОВАТЕЛЯ ===\n\n")
	}

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

	// Добавляем предыдущее сообщение бота только если пользователь отвечает на него
	lastBotMessageText := ""
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.IsBot && msg.ReplyToMessage.From.ID == b.api.Self.ID {
		replyText := strings.TrimSpace(msg.ReplyToMessage.Text)
		if replyText == "" && msg.ReplyToMessage.Caption != "" {
			replyText = strings.TrimSpace(msg.ReplyToMessage.Caption)
		}
		if replyText != "" {
			contextText.WriteString("\n=== ПОСЛЕДНЕЕ СООБЩЕНИЕ БОТА (ПРОДОЛЖАЙ ЛОГИКУ) ===\n")
			contextText.WriteString(replyText)
			contextText.WriteString("\n")
			lastBotMessageText = replyText
		}
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
		genderNormalized := strings.TrimSpace(strings.ToLower(userLog.Gender))
		if genderNormalized != "" {
			var genderText string
			if genderNormalized == "f" {
				genderText = "женский"
			} else if genderNormalized == "m" {
				genderText = "мужской"
			}
			if genderText != "" {
				contextText.WriteString(fmt.Sprintf("👤 Пол: %s\n", genderText))
			}
		}
	} else {
		contextText.WriteString("\n⚠️ Данные пользователя не найдены\n")
	}

	// Недавний контекст беседы (зависит от типа чата)
	{
		if chatType == "writing" {
			// Для чатов писательства используем полную историю переписки
			writingContext, err := b.db.GetChatWritingContext(msg.Chat.ID, msg.From.ID, 420)
			if err == nil && len(writingContext) > 0 {
				contextText.WriteString("\n=== КОНТЕКСТ ПЕРЕПИСКИ (писательство, последние 420 сообщений) ===\n")
				for _, msg := range writingContext {
					text := strings.TrimSpace(msg.MessageText)
					if text == "" {
						continue
					}
					if len(text) > 300 {
						text = text[:300] + "…"
					}
					ts := msg.CreatedAt.In(time.FixedZone("MSK", 3*3600)).Format("2006-01-02 15:04")
					messageType := ""
					if msg.MessageType == "ai_reply" {
						messageType = " [БОТ]"
					}
					contextText.WriteString(fmt.Sprintf("• [%s]%s %s: %s\n", ts, messageType, msg.Username, text))
				}
			}
		} else {
			// Для чатов тренировок используем последние 2 часа
			end := time.Now()
			start := end.Add(-2 * time.Hour)
			recentChat, err := b.db.GetMessagesInRange(msg.Chat.ID, start, end)
			if err == nil && len(recentChat) > 0 {
				contextText.WriteString("\n=== НЕДАВНИЙ КОНТЕКСТ БЕСЕДЫ (2 часа) ===\n")
				count := 0
				for i := len(recentChat) - 1; i >= 0 && count < 10; i-- {
					text := strings.TrimSpace(recentChat[i].MessageText)
					if text == "" {
						continue
					}
					if len(text) > 300 {
						text = text[:300] + "…"
					}
					ts := recentChat[i].CreatedAt.In(time.FixedZone("MSK", 3*3600)).Format("15:04")
					contextText.WriteString("• [" + ts + "] " + text + "\n")
					count++
				}
			}
		}
	}

	// Добавляем анти‑повторы: последние ответы ИИ для этого пользователя
	{
		// Берем последние 30 дней и собираем до 5 последних ai_reply
		end := time.Now()
		start := end.AddDate(0, 0, -30)
		recent, err := b.db.GetUserMessages(msg.From.ID, msg.Chat.ID, start, end)
		if err == nil {
			var lastReplies []string
			for i := len(recent) - 1; i >= 0 && len(lastReplies) < 5; i-- {
				if strings.ToLower(recent[i].MessageType) == "ai_reply" {
					lastReplies = append(lastReplies, recent[i].MessageText)
				}
			}
			if len(lastReplies) > 0 {
				contextText.WriteString("\n=== МОИ ПОСЛЕДНИЕ ОТВЕТЫ (ИЗБЕГАЙ ПОВТОРОВ ТЕМ) ===\n")
				for _, r := range lastReplies {
					// укоротим строку
					txt := r
					if len(txt) > 400 {
						txt = txt[:400] + "…"
					}
					contextText.WriteString("• " + txt + "\n")
				}
			}
		}
	}

	// Легкий RAG по чату: несколько анонимных примеров удачных тренировок
	{
		end := time.Now()
		start := end.AddDate(0, 0, -14)
		examples, err := b.db.GetMessagesInRange(msg.Chat.ID, start, end)
		if err == nil {
			var picked []string
			for i := len(examples) - 1; i >= 0 && len(picked) < 3; i-- {
				if examples[i].MessageType == "training_done" {
					text := examples[i].MessageText
					if len(text) > 200 {
						text = text[:200] + "…"
					}
					picked = append(picked, text)
				}
			}
			if len(picked) > 0 {
				contextText.WriteString("\n=== ПРИМЕРЫ ИЗ ЭТОГО ЧАТА (АНОНИМНО, ДЛЯ ВАРИАЦИИ СОВЕТОВ) ===\n")
				for _, p := range picked {
					contextText.WriteString("• " + p + "\n")
				}
			}
		}
	}

	// Показываем индикатор набора текста до отправки ответа
	// Отправляем сразу и поддерживаем индикатор каждые ~4 секунды, пока формируется ответ
	b.api.Send(tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping))
	typingDone := make(chan struct{})
	go func(chatID int64, done <-chan struct{}) {
		ticker := time.NewTicker(4 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				b.api.Send(tgbotapi.NewChatAction(chatID, tgbotapi.ChatTyping))
			}
		}
	}(msg.Chat.ID, typingDone)

	// Пытаемся определить пол из сообщения или имени
	detectedGender := b.detectGenderFromMessage(questionText)
	if detectedGender == "" && msg.From.FirstName != "" {
		detectedGender = b.detectGenderFromName(msg.From.FirstName)
	}

	// Обновляем пол в базе данных, если он определен
	if detectedGender != "" {
		if err := b.updateUserGender(msg.From.ID, msg.Chat.ID, detectedGender); err != nil {
			b.logger.Warnf("Failed to update user gender: %v", err)
		}
	}

	// Проверяем, есть ли в вопросе упоминание другого пользователя
	// Извлекаем упоминания (@username) и ищем информацию о них в БД
	words := strings.Fields(questionText)
	var mentionedUsernames []string

	for _, word := range words {
		word = strings.Trim(word, ".,!?;:")
		// Ищем упоминания (@username)
		if strings.HasPrefix(word, "@") {
			searchUsername := strings.TrimPrefix(word, "@")
			if len(searchUsername) >= 2 {
				mentionedUsernames = append(mentionedUsernames, searchUsername)
			}
		}
	}

	// Если упоминаний нет, ищем по словам после ключевых фраз (например, "какого пола Tester" или "про Tester")
	questionLower := strings.ToLower(questionText)
	if len(mentionedUsernames) == 0 && (strings.Contains(questionLower, "пол") || strings.Contains(questionLower, "статистик") || strings.Contains(questionLower, "сколько") || strings.Contains(questionLower, "калори") || strings.Contains(questionLower, "кубк") || strings.Contains(questionLower, "про ") || strings.Contains(questionLower, "расскажи") || strings.Contains(questionLower, "достижен") || strings.Contains(questionLower, "у него") || strings.Contains(questionLower, "у неё") || strings.Contains(questionLower, "его") || strings.Contains(questionLower, "её")) {
		// Ищем потенциальные имена пользователей (слова с заглавной буквы или после ключевых фраз)
		for _, word := range words {
			word = strings.Trim(word, ".,!?;:")
			// Пропускаем слишком короткие слова и служебные
			if len(word) < 2 || word == "какого" || word == "пола" || word == "какой" || word == "про" || word == "о" || word == "расскажи" || word == "у" || word == "него" || word == "неё" || word == "его" || word == "её" || word == "какие" {
				continue
			}
			// Если слово начинается с заглавной буквы, возможно это имя
			if len(word) > 0 && word[0] >= 'A' && word[0] <= 'Z' {
				mentionedUsernames = append(mentionedUsernames, word)
			}
		}
	}

	// Если упоминаний всё ещё нет, но есть местоимения "он", "его", "у него" - ищем в недавнем контексте
	if len(mentionedUsernames) == 0 && (strings.Contains(questionLower, "у него") || strings.Contains(questionLower, "у неё") || strings.Contains(questionLower, "его") || strings.Contains(questionLower, "её")) {
		// Ищем в недавнем контексте беседы (последние 2 часа) упоминания пользователей
		end := time.Now()
		start := end.Add(-2 * time.Hour)
		recentChat, err := b.db.GetMessagesInRange(msg.Chat.ID, start, end)
		if err == nil {
			// Ищем в последних сообщениях упоминания пользователей или имена с заглавной буквы
			for i := len(recentChat) - 1; i >= 0 && i >= len(recentChat)-5; i-- {
				text := recentChat[i].MessageText
				// Ищем @username
				if strings.Contains(text, "@") {
					parts := strings.Fields(text)
					for _, part := range parts {
						if strings.HasPrefix(part, "@") {
							username := strings.TrimPrefix(part, "@")
							username = strings.Trim(username, ".,!?;:")
							if len(username) >= 2 {
								mentionedUsernames = append(mentionedUsernames, username)
								break
							}
						}
					}
				}
				// Ищем слова с заглавной буквы (имена)
				if len(mentionedUsernames) == 0 {
					nameParts := strings.Fields(text)
					for _, namePart := range nameParts {
						namePart = strings.Trim(namePart, ".,!?;:")
						if len(namePart) >= 2 && namePart[0] >= 'A' && namePart[0] <= 'Z' {
							// Проверяем, не является ли это именем пользователя в БД
							mentionedUsernames = append(mentionedUsernames, namePart)
							break
						}
					}
				}
				if len(mentionedUsernames) > 0 {
					break
				}
			}
		}
	}

	// Ищем информацию о найденных пользователях в БД
	for _, searchUsername := range mentionedUsernames {
		userID, err := b.db.GetUserIDByUsername(searchUsername, msg.Chat.ID)
		if err == nil && userID != msg.From.ID {
			// Нашли другого пользователя, получаем всю информацию о нём
			otherUserLog, err := b.db.GetMessageLog(userID, msg.Chat.ID)
			if err == nil {
				contextText.WriteString("\n=== ИНФОРМАЦИЯ О ЗАПРОШЕННОМ ПОЛЬЗОВАТЕЛЕ ===\n")
				contextText.WriteString(fmt.Sprintf("Пользователь: %s (ID: %d)\n", otherUserLog.Username, otherUserLog.UserID))

				// Пол (только если указан)
				genderNormalized := strings.TrimSpace(strings.ToLower(otherUserLog.Gender))
				if genderNormalized != "" {
					var genderInfo string
					// Логируем для отладки
					b.logger.Infof("DEBUG: User %s (%d) gender from DB (raw): '%s', normalized: '%s'", otherUserLog.Username, otherUserLog.UserID, otherUserLog.Gender, genderNormalized)
					if genderNormalized == "f" {
						genderInfo = "женский"
					} else if genderNormalized == "m" {
						genderInfo = "мужской"
					} else {
						// Если не f и не m, логируем ошибку
						b.logger.Warnf("DEBUG: Unknown gender value '%s' (normalized: '%s') for user %s (%d)", otherUserLog.Gender, genderNormalized, otherUserLog.Username, otherUserLog.UserID)
					}
					if genderInfo != "" {
						contextText.WriteString(fmt.Sprintf("Пол: %s\n", genderInfo))
					}
				}

				// Статистика
				cups, _ := b.db.GetUserCups(userID, msg.Chat.ID)
				contextText.WriteString(fmt.Sprintf("🔥 Всего калорий: %d\n", otherUserLog.Calories))
				contextText.WriteString(fmt.Sprintf("🏆 Всего кубков: %d\n", cups))
				contextText.WriteString(fmt.Sprintf("💪 Серия тренировок: %d дней подряд\n", otherUserLog.StreakDays))
				contextText.WriteString(fmt.Sprintf("📈 Серия калорий: %d дней подряд\n", otherUserLog.CalorieStreakDays))

				if otherUserLog.LastTrainingDate != nil {
					contextText.WriteString(fmt.Sprintf("📅 Последняя тренировка: %s\n", *otherUserLog.LastTrainingDate))
				}

				// Статус
				if otherUserLog.HasSickLeave {
					contextText.WriteString("🏥 Статус: На больничном\n")
					if otherUserLog.SickLeaveStartTime != nil {
						contextText.WriteString(fmt.Sprintf("   Начало больничного: %s\n", *otherUserLog.SickLeaveStartTime))
					}
				} else if otherUserLog.HasHealthy {
					contextText.WriteString("✅ Статус: Здоров\n")
				} else {
					contextText.WriteString("✅ Статус: Активен\n")
				}

				// Таймер
				if otherUserLog.TimerStartTime != nil {
					remainingTime := b.calculateRemainingTime(otherUserLog)
					if remainingTime > 0 {
						remainingTimeFormatted := b.formatDurationToDays(remainingTime)
						contextText.WriteString(fmt.Sprintf("⏳ До удаления осталось: %s\n", remainingTimeFormatted))
					}
				}

				contextText.WriteString(fmt.Sprintf("💬 Последнее сообщение: %s\n", otherUserLog.LastMessage))
			}
			break // Нашли одного пользователя, достаточно
		}
	}

	// Если спрашивают про список участников ("какие участники", "кто есть", "список участников", "какого пола участники")
	questionLower = strings.ToLower(questionText)
	if strings.Contains(questionLower, "участник") || strings.Contains(questionLower, "кто есть") || strings.Contains(questionLower, "список") {
		users, err := b.db.GetUsersByChatID(msg.Chat.ID)
		if err == nil && len(users) > 0 {
			contextText.WriteString("\n=== ПОЛНАЯ ИНФОРМАЦИЯ О ВСЕХ УЧАСТНИКАХ ЧАТА ===\n")
			for i, user := range users {
				if i >= 15 { // Ограничиваем до 15 участников для краткости
					contextText.WriteString(fmt.Sprintf("\n... и еще %d участников\n", len(users)-15))
					break
				}

				// Полная информация о каждом участнике
				contextText.WriteString(fmt.Sprintf("\n--- УЧАСТНИК %d: %s (ID: %d) ---\n", i+1, user.Username, user.UserID))

				// Пол (только если указан)
				genderNormalized := strings.TrimSpace(strings.ToLower(user.Gender))
				if genderNormalized != "" {
					var genderText string
					// Логируем для отладки
					b.logger.Infof("DEBUG: User %s (%d) gender from DB (raw): '%s', normalized: '%s'", user.Username, user.UserID, user.Gender, genderNormalized)
					if genderNormalized == "f" {
						genderText = "женский"
					} else if genderNormalized == "m" {
						genderText = "мужской"
					} else {
						// Если не f и не m, логируем ошибку
						b.logger.Warnf("DEBUG: Unknown gender value '%s' (normalized: '%s') for user %s (%d)", user.Gender, genderNormalized, user.Username, user.UserID)
					}
					if genderText != "" {
						contextText.WriteString(fmt.Sprintf("Пол: %s\n", genderText))
					}
				}

				// Статистика
				cups, _ := b.db.GetUserCups(user.UserID, msg.Chat.ID)
				contextText.WriteString(fmt.Sprintf("🔥 Всего калорий: %d\n", user.Calories))
				contextText.WriteString(fmt.Sprintf("🏆 Всего кубков: %d\n", cups))
				contextText.WriteString(fmt.Sprintf("💪 Серия тренировок: %d дней подряд\n", user.StreakDays))
				contextText.WriteString(fmt.Sprintf("📈 Серия калорий: %d дней подряд\n", user.CalorieStreakDays))

				if user.LastTrainingDate != nil {
					contextText.WriteString(fmt.Sprintf("📅 Последняя тренировка: %s\n", *user.LastTrainingDate))
				}

				// Статус
				if user.HasSickLeave {
					contextText.WriteString("🏥 Статус: На больничном\n")
					if user.SickLeaveStartTime != nil {
						contextText.WriteString(fmt.Sprintf("   Начало больничного: %s\n", *user.SickLeaveStartTime))
					}
				} else if user.HasHealthy {
					contextText.WriteString("✅ Статус: Здоров\n")
				} else {
					contextText.WriteString("✅ Статус: Активен\n")
				}

				// Таймер
				if user.TimerStartTime != nil {
					remainingTime := b.calculateRemainingTime(user)
					if remainingTime > 0 {
						remainingTimeFormatted := b.formatDurationToDays(remainingTime)
						contextText.WriteString(fmt.Sprintf("⏳ До удаления осталось: %s\n", remainingTimeFormatted))
					}
				}

				contextText.WriteString(fmt.Sprintf("💬 Последнее сообщение: %s\n", user.LastMessage))
			}
		}
	}

	// Генерируем ответ с помощью ИИ
	// Используем уже определенный chatType из блока выше
	finalQuestion := questionText
	if lastBotMessageText != "" {
		finalQuestion = fmt.Sprintf(
			"МОЁ ПРЕДЫДУЩЕЕ СООБЩЕНИЕ:\n%s\n\nПОЛЬЗОВАТЕЛЬ ОТВЕТИЛ ТАК: %s\n\nСОХРАНИ ЛОГИКУ ПРЕДЫДУЩЕГО СООБЩЕНИЯ. ЕСЛИ ЕГО ОСПАРИВАЮТ ИЛИ ПРОСЛЕЖИВАЕТСЯ ХИТРОСТЬ, ПРОДОЛЖАЙ СТРОГО НАСТАИВАТЬ, ТРЕБУЙ ДОКАЗАТЕЛЬСТВ И НЕ СМЕНЯЙ ТОН НА ПОДДЕРЖИВАЮЩИЙ БЕЗ НОВЫХ ФАКТОВ.",
			lastBotMessageText,
			questionText,
		)
	}

	if chatType == "writing" {
		finalQuestion += "\n\nВАЖНО: Это чат для писательства. Ты мудрый литературный наставник Fat Leopard. Используй весь контекст переписки из этого чата для понимания темы и сюжета. Отвечай в контексте писательства, поддерживай обсуждение литературных тем, помогай с развитием сюжета, персонажей и стиля. Не переходи на темы тренировок, если об этом явно не спрашивают."
	} else {
		finalQuestion += "\n\nОТВЕЧАЙ СТРОГО ПО СУТИ ВОПРОСА ПОЛЬЗОВАТЕЛЯ. СНАЧАЛА ДАЙ ПОЛНЫЙ, ПОДРОБНЫЙ ОТВЕТ ПО ВОПРОСУ. ЕСЛИ ВОПРОС НЕ ПРО ТРЕНИРОВКИ ИЛИ БОЛЬНИЧНЫЙ, НЕ ПЕРЕХОДИ К ЭТИМ ТЕМАМ БЕЗ ЯВНОГО ЗАПРОСА И НЕ ВЫПОЛНЯЙ НЕПРОСИМЫЕ ПРЕДУПРЕЖДЕНИЯ. ЛЮБЫЕ МОТИВИРУЮЩИЕ ДОПОЛНЕНИЯ МОЖНО ДАВАТЬ ТОЛЬКО В КОНЦЕ И ТОЛЬКО ЕСЛИ ОНИ ПОДЧЕРКИВАЮТ СУТЬ ОТВЕТА."
	}

	answer, err := b.aiClient.AnswerUserQuestion(finalQuestion, contextText.String())
	if err != nil {
		b.logger.Errorf("Failed to generate AI answer: %v", err)

		// Проверяем, является ли это ошибкой настройки политики данных
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "data policy") || strings.Contains(errorMsg, "Model Training") {
			reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ ИИ функции требуют настройки OpenRouter API.\n\nДля бесплатных моделей нужно:\n1. Перейди на https://openrouter.ai/settings/privacy\n2. Включи опцию 'Model Training'\n\nПосле этого ИИ заработает!")
			b.api.Send(reply)
			close(typingDone)
			return
		}

		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Ошибка при генерации ответа ИИ: %v", err))
		b.api.Send(reply)
		close(typingDone)
		return
	}

	// Удаляем markdown форматирование (**) перед отправкой
	answer = strings.ReplaceAll(answer, "**", "")

	// Отправляем ответ с реплаем на исходное сообщение
	reply := tgbotapi.NewMessage(msg.Chat.ID, answer)
	reply.ReplyToMessageID = msg.MessageID // Отвечаем на сообщение пользователя
	b.logger.Infof("Sending AI answer to user %d in chat %d (replying to message %d)", msg.From.ID, msg.Chat.ID, msg.MessageID)
	_, err = b.api.Send(reply)
	close(typingDone)
	if err != nil {
		b.logger.Errorf("Failed to send AI answer: %v", err)
	}

	// Сохраняем ответ ИИ для анти‑повторов (тип ai_reply)
	_ = b.db.SaveUserMessage(&domain.UserMessage{
		UserID:      msg.From.ID,
		ChatID:      msg.Chat.ID,
		Username:    b.api.Self.UserName,
		MessageText: answer,
		MessageType: "ai_reply",
		CreatedAt:   time.Now(),
	})
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

	if strings.HasPrefix(data, "admin_") {
		b.handleAdminCallbackQuery(callback)
		return
	}

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

// detectGenderFromName пытается определить пол по русскому имени
func (b *Bot) detectGenderFromName(firstName string) string {
	if firstName == "" {
		return ""
	}
	firstName = strings.ToLower(strings.TrimSpace(firstName))

	// Женские окончания русских имен
	femaleEndings := []string{"а", "я", "ь", "ия", "ина", "ая"}
	for _, ending := range femaleEndings {
		if strings.HasSuffix(firstName, ending) {
			return "f"
		}
	}

	// Мужские имена без окончаний (обычно оканчиваются на согласную, кроме ь)
	// Также имена с окончаниями: ов, ев, ин, ой, ий
	maleEndings := []string{"ов", "ев", "ин", "ой", "ий"}
	for _, ending := range maleEndings {
		if strings.HasSuffix(firstName, ending) {
			return "m"
		}
	}

	// Если имя не оканчивается на характерные окончания, возвращаем пустую строку
	return ""
}

// detectGenderFromMessage пытается определить пол из сообщения пользователя
func (b *Bot) detectGenderFromMessage(text string) string {
	text = strings.ToLower(text)

	// Паттерны, указывающие на женский пол
	femalePatterns := []string{"я девочка", "я девушка", "я женщина", "я девочка", "полина", "ирина", "анна", "мария", "елена", "ольга", "татьяна", "наталья", "светлана"}
	for _, pattern := range femalePatterns {
		if strings.Contains(text, pattern) {
			return "f"
		}
	}

	// Паттерны, указывающие на мужской пол
	malePatterns := []string{"я мальчик", "я парень", "я мужчина", "я парень", "александр", "дмитрий", "иван", "михаил", "сергей", "алексей", "андрей", "максим"}
	for _, pattern := range malePatterns {
		if strings.Contains(text, pattern) {
			return "m"
		}
	}

	// Проверка упоминания рода в обратной связи
	if strings.Contains(text, "род") || strings.Contains(text, "пол") {
		if strings.Contains(text, "женск") || strings.Contains(text, "девочк") || strings.Contains(text, "девушк") {
			return "f"
		}
		if strings.Contains(text, "мужск") || strings.Contains(text, "мальчик") || strings.Contains(text, "парень") {
			return "m"
		}
	}

	return ""
}

// getUnifiedTrainingPrompt генерирует единый промпт для AI, объединяющий приписку и мудрость в одно сообщение
func (b *Bot) getUnifiedTrainingPrompt(streakDays, totalCalories, totalCups int, wasOnSickLeave bool, chatType string) string {
	now := utils.GetMoscowTime()
	hour := now.Hour()
	weekday := now.Weekday()

	var prompts []string

	// Разные промпты для чатов писательства и тренировок
	if chatType == "writing" {
		// Промпты для чатов писательства - ТОЛЬКО про писательство, ТОЛЬКО на этот отчёт
		prompts = []string{
			"Напиши одно связное сообщение (2-3 предложения) после отчёта #training_done: дружелюбно, как литературный наставник. Первое предложение — конкретный комментарий к писательской работе из сообщения (что пишешь, над чем работаешь), второе — короткое наблюдение или совет по писательству. НЕ упоминай тренировки, спорт или физическую активность. НЕ используй фразы типа 'помни', 'укрепление духа', 'сила воли'. НЕ повторяй цифры из сообщения. КРИТИЧЕСКИ ВАЖНО: отвечай ТОЛЬКО на этот отчёт. НЕ используй никакой другой контекст из чата, историю сообщений или сообщения других участников. Используй ТОЛЬКО детали из сообщения пользователя. Без Markdown.",

			"Сделай одно цельное сообщение (2-3 предложения) после писательской сессии: первое — отметь конкретные детали писательской работы из сообщения (что пишешь, над чем работаешь, какие проблемы решаешь), второе — короткий практический совет или наблюдение по писательству. НЕ упоминай тренировки, спорт или физическую активность. Избегай общих фраз про 'дух', 'волю', 'помни'. Будь конкретным про писательскую работу, но не упоминай цифры. КРИТИЧЕСКИ ВАЖНО: отвечай ТОЛЬКО на этот отчёт. НЕ смотри на другие сообщения в чате. Без Markdown.",

			"Напиши одно связное сообщение (2-3 предложения) от лица литературного наставника Fat Leopard: первое — оценка писательской работы с упоминанием конкретных деталей из сообщения, второе — конкретное замечание или совет по писательству. НЕ упоминай тренировки, спорт или физическую активность. НЕ используй абстрактные фразы про 'дух', 'волю', 'помни'. КРИТИЧЕСКИ ВАЖНО: отвечай ТОЛЬКО на этот отчёт. НЕ используй контекст других сообщений. Используй ТОЛЬКО детали из сообщения пользователя. Без Markdown.",

			"Сделай одно цельное сообщение (2-3 предложения) после #training_done: первое — поддерживающий комментарий про писательскую работу из сообщения, второе — практический совет или наблюдение по писательству. НЕ упоминай тренировки, спорт или физическую активность. Избегай фраз 'помни', 'укрепление', 'дух', 'воля'. Не повторяй цифры. КРИТИЧЕСКИ ВАЖНО: отвечай ТОЛЬКО на этот отчёт. НЕ смешивай с другими сообщениями чата. Без Markdown.",

			"Напиши одно связное сообщение (2-3 предложения) в стиле литературного наставника: первое — конкретный комментарий к писательской работе из сообщения (что пишешь, какие сцены, персонажи, сюжет), второе — короткое практическое замечание по писательству. НЕ упоминай тренировки, спорт или физическую активность. НЕ используй общие фразы про 'дух', 'волю', 'помни'. Будь конкретным, но не упоминай цифры. КРИТИЧЕСКИ ВАЖНО: отвечай ТОЛЬКО на этот отчёт. Без Markdown.",

			"Сделай одно цельное сообщение (2-3 предложения) после писательской сессии: первое — отметь детали писательской работы из сообщения, второе — короткий комментарий о процессе написания или развитии текста. НЕ упоминай тренировки, спорт или физическую активность. Избегай абстрактных фраз про 'дух', 'волю', 'помни'. КРИТИЧЕСКИ ВАЖНО: используй ТОЛЬКО этот отчёт. НЕ используй историю чата или сообщения других людей. Без Markdown.",

			"Напиши одно связное сообщение (2-3 предложения): первое — конкретная оценка писательской работы из сообщения (что написано, над чем работаешь), второе — короткое замечание о процессе писательства. НЕ упоминай тренировки, спорт или физическую активность. НЕ используй фразы 'помни', 'укрепление духа', 'сила воли'. Будь конкретным про писательскую работу, не упоминай цифры. КРИТИЧЕСКИ ВАЖНО: отвечай ТОЛЬКО на этот отчёт. Без Markdown.",

			"Сделай одно цельное сообщение (2-3 предложения) после #training_done: первое — комментарий к писательской работе, второе — короткое наблюдение о процессе писательства или развитии произведения. НЕ упоминай тренировки, спорт или физическую активность. Избегай общих фраз про 'дух', 'волю', 'помни'. КРИТИЧЕСКИ ВАЖНО: используй ТОЛЬКО этот отчёт. НЕ смотри на последние сообщения или других участников. Без Markdown.",
		}
	} else {
		// Базовые промпты для тренировок — живой тон, характер Fat Leopard, отвечай ТОЛЬКО на этот отчёт
		prompts = []string{
			"Ты Fat Leopard — строгий тренер, который сам любит поесть. Напиши 2-3 предложения после отчёта о тренировке: первое — конкретный комментарий к упражнениям из сообщения, второе — короткое замечание или совет. Можно лёгкий намёк, что ты «не съешь» того, кто тренируется. Используй ТОЛЬКО упражнения из сообщения. Не повторяй цифры. Без Markdown.",

			"Ты Fat Leopard — ленивый, но справедливый. Ответь на отчёт о тренировке (2-3 предложения): первое — отметь упражнения, второе — короткий совет. Тон: немного завидуешь дисциплине, но признаёшь результат. Используй ТОЛЬКО этот отчёт. Без Markdown.",

			"Ответь как тренер после тренировки (2-3 предложения): первое — искренний комментарий к упражнениям из сообщения (без шаблонов), второе — один практический совет. Пиши живым языком. Используй ТОЛЬКО этот отчёт. Не повторяй цифры. Без Markdown.",

			"Ты Fat Leopard. Напиши 2-3 предложения после отчёта: первое — оценка тренировки с упоминанием упражнений, второе — конкретное замечание. Можно лёгкий юмор в духе «я бы уже спал, а ты ещё в деле». Используй ТОЛЬКО упражнения из сообщения. Без Markdown.",

			"Ответь как наставник после тренировки (2-3 предложения): первое — что именно понравилось в отчёте, второе — один конкретный совет. Избегай общих фраз про 'дух', 'волю', 'помни'. Используй ТОЛЬКО этот отчёт. Без Markdown.",

			"Напиши 2-3 предложения после #training_done: первое — отметь конкретные упражнения, второе — короткое наблюдение о технике или подходе. Тон: поддерживающий, но с лёгкой строгостью. Используй ТОЛЬКО этот отчёт. Не повторяй цифры. Без Markdown.",

			"Fat Leopard одобряет. Напиши 2-3 предложения: первое — комментарий к упражнениям из сообщения, второе — короткое замечание. Тон: дружелюбно-строгий. Используй ТОЛЬКО этот отчёт. Без Markdown.",

			"Ответь на отчёт о тренировке (2-3 предложения): первое — конкретная оценка упражнений, второе — практический совет. Будь конкретным, не используй абстрактные фразы. Используй ТОЛЬКО этот отчёт. Без Markdown.",
		}
	}

	// Специальные промпты в зависимости от контекста - БЕЗ общих фраз
	if wasOnSickLeave {
		if chatType == "writing" {
			prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения) после писательской сессии: пользователь недавно вернулся после больничного. Первое — похвали за возвращение к писательству и отметь писательскую работу, второе — короткий практический совет по писательству. НЕ упоминай тренировки, спорт или физическую активность. НЕ используй фразы 'помни', 'дух', 'воля'. Используй ТОЛЬКО детали о писательской работе из сообщения. Без Markdown.")
		} else {
			prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения) после тренировки: пользователь недавно вернулся после больничного. Первое — похвали за возвращение и отметь упражнения, второе — короткий практический совет. НЕ используй фразы 'помни', 'дух', 'воля'. Используй ТОЛЬКО упражнения из сообщения. Без Markdown.")
		}
	}

	if streakDays >= 7 && streakDays < 14 {
		if chatType == "writing" {
			prompts = append(prompts, "Сделай одно цельное сообщение (2-3 предложения): пользователь уже неделю пишет подряд — это важный рубеж! Первое — отметь это и писательскую работу, второе — короткое наблюдение по писательству. НЕ упоминай тренировки, спорт или физическую активность. Избегай фраз 'помни', 'дух', 'воля'. Используй детали о писательской работе из сообщения. Без Markdown.")
		} else {
			prompts = append(prompts, "Сделай одно цельное сообщение (2-3 предложения): пользователь уже неделю тренируется подряд — это важный рубеж! Первое — отметь это и упражнения, второе — короткое наблюдение. Избегай фраз 'помни', 'дух', 'воля'. Используй упражнения из сообщения. Без Markdown.")
		}
	}

	if streakDays >= 21 {
		if chatType == "writing" {
			prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения): пользователь показывает отличную дисциплину с длинной серией писательства. Первое — признай это и отметь писательскую работу, второе — конкретное замечание о процессе писательства. НЕ упоминай тренировки, спорт или физическую активность. НЕ используй абстрактные фразы про 'дух', 'волю', 'помни'. Используй детали о писательской работе из сообщения. Без Markdown.")
		} else {
			prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения): пользователь показывает отличную дисциплину с длинной серией. Первое — признай это и отметь упражнения, второе — конкретное замечание о тренировке. НЕ используй абстрактные фразы про 'дух', 'волю', 'помни'. Используй упражнения из сообщения. Без Markdown.")
		}
	}

	// Вечер (17-22): конец дня, тренировка после работы
	if hour >= 17 && hour < 22 {
		if chatType == "writing" {
			prompts = append(prompts, "Писательская сессия в конце дня — ты закрыл его правильно. Напиши 2-3 предложения: первое — отметь писательскую работу из сообщения, второе — короткое наблюдение. Используй ТОЛЬКО этот отчёт. Без Markdown.")
		} else {
			prompts = append(prompts,
				"Тренировка в конце дня — ты закрыл его правильно. Напиши 2-3 предложения: первое — отметь упражнения из сообщения, второе — короткое наблюдение (например про восстановление или сон). Тон: поддерживающий, без пафоса. Используй ТОЛЬКО этот отчёт. Без Markdown.",
				"Вечерняя тренировка — не все на это способны. Напиши 2-3 предложения: первое — комментарий к упражнениям, второе — лёгкий намёк, что завтра будет легче. Fat Leopard одобряет. Используй ТОЛЬКО этот отчёт. Без Markdown.",
				"Конец дня, а он всё ещё тренируется. Напиши 2-3 предложения: первое — конкретный комментарий к отчёту, второе — короткий совет (восстановление, сон, завтра). Используй ТОЛЬКО этот отчёт. Без Markdown.")
		}
	}

	// Поздний вечер / ночь (22-6)
	if hour >= 22 || hour < 6 {
		if chatType == "writing" {
			prompts = append(prompts, "Сделай одно цельное сообщение (2-3 предложения): писательская сессия поздним вечером или ночью — это особое вдохновение! Первое — отметь это и писательскую работу, второе — короткий комментарий по писательству. НЕ упоминай тренировки, спорт или физическую активность. Избегай фраз 'помни', 'дух', 'воля'. Используй ТОЛЬКО детали о писательской работе из сообщения. Без Markdown.")
		} else {
			prompts = append(prompts,
				"Тренировка в конце дня — ты не сдался. Напиши 2-3 предложения: первое — отметь упражнения, второе — короткий комментарий. Можно лёгкий юмор: Fat Leopard уже спал, а ты ещё в деле. Используй ТОЛЬКО этот отчёт. Без Markdown.",
				"Поздняя тренировка — особое упорство. Напиши 2-3 предложения: первое — комментарий к упражнениям, второе — совет про сон и восстановление. Используй ТОЛЬКО этот отчёт. Без Markdown.")
		}
	}

	if weekday == time.Saturday || weekday == time.Sunday {
		if chatType == "writing" {
			prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения): писательская работа в выходной день — это настоящая преданность творчеству! Первое — похвали и отметь писательскую работу, второе — практическое замечание по писательству. НЕ упоминай тренировки, спорт или физическую активность. НЕ используй фразы 'помни', 'дух', 'воля'. Используй детали о писательской работе из сообщения. Без Markdown.")
		} else {
			prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения): тренировка в выходной день — это настоящая преданность! Первое — похвали и отметь упражнения, второе — практическое замечание. НЕ используй фразы 'помни', 'дух', 'воля'. Используй упражнения из сообщения. Без Markdown.")
		}
	}

	if totalCups >= 1000 {
		if chatType == "writing" {
			prompts = append(prompts, "Сделай одно цельное сообщение (2-3 предложения): пользователь накопил много кубков — это опытный писатель. Первое — обратись как к ветерану писательства, отметь писательскую работу, второе — конкретное наблюдение по писательству. НЕ упоминай тренировки, спорт или физическую активность. Избегай абстрактных фраз про 'дух', 'волю', 'помни'. Используй детали о писательской работе из сообщения. Без Markdown.")
		} else {
			prompts = append(prompts, "Сделай одно цельное сообщение (2-3 предложения): пользователь накопил много кубков — это опытный участник. Первое — обратись как к ветерану, отметь упражнения, второе — конкретное наблюдение. Избегай абстрактных фраз про 'дух', 'волю', 'помни'. Используй упражнения из сообщения. Без Markdown.")
		}
	}

	// Выбираем случайный промпт
	selectedPrompt := prompts[now.Unix()%int64(len(prompts))]
	return selectedPrompt
}

// getVariedTrainingPrompt генерирует разнообразные промпты для AI в зависимости от контекста (оставлено для совместимости)
func (b *Bot) getVariedTrainingPrompt(streakDays, totalCalories, totalCups int, wasOnSickLeave bool) string {
	now := utils.GetMoscowTime()
	hour := now.Hour()
	weekday := now.Weekday()

	// Базовые стили промптов
	prompts := []string{
		"Сделай очень короткую (1–2 предложения) дружелюбную, но строгую приписку после отчёта #training_done. Не повторяй цифры из сообщения, не перечисляй правила. КРИТИЧЕСКИ ВАЖНО: используй ТОЛЬКО те упражнения и детали, которые указаны в сообщении пользователя. НЕ выдумывай детали, которых нет. Без Markdown.",

		"Напиши короткую (1–2 предложения) мотивирующую приписку от лица строгого, но справедливого тренера Fat Leopard после отчёта о тренировке. Будь конкретным про упражнения из сообщения, но не повторяй цифры. Без Markdown.",

		"Сделай короткий (1–2 предложения) комментарий после тренировки: поддерживающий, но с лёгкой строгостью. Упомяни конкретные упражнения из сообщения пользователя, но не цифры. Без Markdown.",

		"Напиши короткую (1–2 предложения) приписку после #training_done в стиле мудрого наставника: дружелюбно, но требовательно. Используй ТОЛЬКО упражнения из сообщения пользователя. Без Markdown.",

		"Сделай очень короткую (1–2 предложения) приписку после тренировки: энергично и мотивирующе, но с ноткой строгости. Будь конкретным про упражнения, не упоминай цифры. Без Markdown.",
	}

	// Специальные промпты в зависимости от контекста
	if wasOnSickLeave {
		prompts = append(prompts, "Напиши короткую (1–2 предложения) приписку после тренировки: пользователь недавно вернулся после больничного, похвали за возвращение, но напомни о важности регулярности. Используй ТОЛЬКО упражнения из сообщения. Без Markdown.")
	}

	if streakDays >= 7 && streakDays < 14 {
		prompts = append(prompts, "Сделай короткую (1–2 предложения) приписку: пользователь уже неделю тренируется подряд — это важный рубеж! Похвали, но призови не останавливаться. Используй упражнения из сообщения. Без Markdown.")
	}

	if streakDays >= 21 {
		prompts = append(prompts, "Напиши короткую (1–2 предложения) приписку: пользователь показывает отличную дисциплину с длинной серией. Признай это, но оставайся строгим. Используй упражнения из сообщения. Без Markdown.")
	}

	if hour >= 22 || hour < 6 {
		prompts = append(prompts, "Сделай короткую (1–2 предложения) приписку: тренировка поздним вечером или ночью — это особое упорство! Отметь это, но используй ТОЛЬКО упражнения из сообщения. Без Markdown.")
	}

	if weekday == time.Saturday || weekday == time.Sunday {
		prompts = append(prompts, "Напиши короткую (1–2 предложения) приписку: тренировка в выходной день — это настоящая преданность делу! Похвали, но оставайся требовательным. Используй упражнения из сообщения. Без Markdown.")
	}

	if totalCups >= 1000 {
		prompts = append(prompts, "Сделай короткую (1–2 предложения) приписку: пользователь накопил много кубков — это опытный участник. Обратись к нему как к ветерану, но не снижай требований. Используй упражнения из сообщения. Без Markdown.")
	}

	// Выбираем случайный промпт
	selectedPrompt := prompts[now.Unix()%int64(len(prompts))]
	return selectedPrompt
}

// getVariedWisdomPrompt генерирует разнообразные промпты для мудрости
func (b *Bot) getVariedWisdomPrompt(streakDays, totalCalories, totalCups int) string {
	now := utils.GetMoscowTime()

	// Базовые стили мудрости
	prompts := []string{
		"Дай одну очень короткую мудрую мысль (1 предложение) для участника после успешной тренировки: спокойно, уважительно, как наставник; без пафоса и без повторения чисел. КРИТИЧЕСКИ ВАЖНО: используй ТОЛЬКО те упражнения и детали, которые указаны в сообщении пользователя. НЕ выдумывай детали, которых нет. Без Markdown.",

		"Напиши одну короткую философскую мысль (1 предложение) о тренировке: глубоко, но просто. Используй ТОЛЬКО упражнения из сообщения пользователя. Без Markdown.",

		"Дай одну короткую мотивирующую мысль (1 предложение) в стиле мудрого тренера после тренировки. Используй упражнения из сообщения, не упоминай цифры. Без Markdown.",

		"Напиши одну короткую вдохновляющую мысль (1 предложение) о важности дисциплины в тренировках. Будь конкретным про упражнения из сообщения. Без Markdown.",

		"Дай одну короткую мудрую мысль (1 предложение) о том, как каждая тренировка приближает к цели. Используй ТОЛЬКО упражнения из сообщения пользователя. Без Markdown.",
	}

	// Специальные промпты
	if streakDays >= 30 {
		prompts = append(prompts, "Напиши одну короткую мысль (1 предложение) о том, как длинная серия тренировок меняет человека. Используй упражнения из сообщения. Без Markdown.")
	}

	if totalCups >= 500 {
		prompts = append(prompts, "Дай одну короткую мудрую мысль (1 предложение) о накопленном опыте и дисциплине. Используй упражнения из сообщения. Без Markdown.")
	}

	// Выбираем случайный промпт
	selectedPrompt := prompts[now.Unix()%int64(len(prompts))]
	return selectedPrompt
}

// updateUserGender обновляет пол пользователя в базе данных
func (b *Bot) updateUserGender(userID, chatID int64, gender string) error {
	if gender == "" {
		return nil
	}

	userLog, err := b.db.GetMessageLog(userID, chatID)
	if err != nil {
		return err
	}

	// Обновляем только если пол еще не установлен
	if userLog.Gender == "" {
		userLog.Gender = gender
		return b.db.SaveMessageLog(userLog)
	}

	return nil
}

// shouldDetectChatTypeAsWriting определяет, нужно ли автоматически установить тип чата как "writing"
// на основе содержимого сообщения
func (b *Bot) shouldDetectChatTypeAsWriting(text string, chatID int64) bool {
	// Проверяем, не установлен ли уже тип чата
	currentType, err := b.db.GetChatType(chatID)
	if err == nil && currentType == "writing" {
		return false // Уже установлен как writing
	}

	// Ключевые слова, указывающие на писательство
	writingKeywords := []string{
		"editingssssss",
	}

	textLower := strings.ToLower(text)
	for _, keyword := range writingKeywords {
		if strings.Contains(textLower, keyword) {
			b.logger.Infof("Detected writing keyword '%s' in message, will set chat type to 'writing'", keyword)
			return true
		}
	}

	return false
}

// handleSetChatType устанавливает тип чата (training/writing)
// Использование: /set_chat_type <training|writing>
func (b *Bot) handleSetChatType(msg *tgbotapi.Message) {
	args := strings.Fields(msg.CommandArguments())
	if len(args) < 1 {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Использование: /set_chat_type <training|writing>")
		b.api.Send(reply)
		return
	}

	chatType := strings.ToLower(strings.TrimSpace(args[0]))
	if chatType != "training" && chatType != "writing" {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Тип чата должен быть 'training' или 'writing'")
		b.api.Send(reply)
		return
	}

	if err := b.db.SetChatType(msg.Chat.ID, chatType); err != nil {
		b.logger.Errorf("Failed to set chat type: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Ошибка при установке типа чата: %v", err))
		b.api.Send(reply)
		return
	}

	chatTypeText := "тренировок"
	if chatType == "writing" {
		chatTypeText = "писательства"
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("✅ Тип чата установлен: %s\n\nТеперь бот будет вести отдельный контекст для чата %s.", chatTypeText, chatTypeText))
	b.api.Send(reply)
}
