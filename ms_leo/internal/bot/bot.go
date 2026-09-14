package bot

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/domain"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/logger"
	"leo-bot/internal/metrics"
	"leo-bot/internal/moderation"
	"leo-bot/internal/prompts"
	"leo-bot/internal/rag"
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
	ragStore             rag.Store
	sickApprovalWatchers map[int64]chan struct{}
	sickApprovalMutex    sync.Mutex
	adminSessions        map[int64]*adminSession
	adminSessionsMutex   sync.Mutex
	// privateBottomKeyboardKind — последняя reply-клавиатура внизу лички: "admin" | "support".
	privateBottomKeyboardKind sync.Map
	userSupportSessions       map[int64]struct{}
	userSupportSessionsMutex  sync.Mutex
	// Очередь ответов Лео для мини-аппа (личка): poll без БД. Несколько реплик бота — один процесс.
	miniappPersonalMu    sync.Mutex
	miniappPersonalQueue map[int64][]string
	// miniappReplyOrigin — пока активна обработка сообщения из мини-аппа,
	// ключ userID → канал ответов в очередь мини-аппа. Используется в helper-ах
	// notify*, чтобы НЕ дублировать сообщения Лео в TG-личку (см. требование:
	// «не дублировать в ТГ из мини-аппа, кроме предупреждений 5/6/7 дней»).
	// Маркер ставит/снимает runMiniAppPrivateTextWorker.
	miniappReplyOrigin sync.Map
	// miniappTrainingPhotoURL — следующий URL фото для отчёта #training_done из мини‑аппа (съедается при сохранении user_messages).
	miniappTrainingPhotoURL sync.Map // int64 (user id) -> string
	ugcModerationGate       *moderation.Gate
	ugcModerationLimiter    *moderation.Limiter
	// Динамические администраторы, добавленные владельцем через бот (кэш из БД).
	dynamicAdmins   map[int64]struct{}
	dynamicAdminsMu sync.RWMutex
}

// leopardOnboardingBodyText — полный текст онбординга Fat Leopard (редактируй здесь).
const leopardOnboardingBodyText = `Добро пожаловать в стаю, Fat Leopard 🐆🔥

Здесь не нужно быть идеальным — нужно просто двигаться. Пробежка, йога, прогулка или 10 отжиманий — всё считается.

⚡️ КАК ОТМЕТИТЬ ТРЕНИРОВКУ
Открой мини-апп Fat Leopard и нажми «+» — заполни тип, минуты и интенсивность. Одного отчёта в день достаточно.

🏆 КУБКИ И СТРИК
За отчёт начисляются кубки по формуле (длина и суть тренировки). Дни подряд без пропуска растят стрик и открывают ачивки.

⏰ ЧТО БУДЕТ, ЕСЛИ ПРОПУСКАТЬ
- День 5 без тренировки — предупреждение в личку
- День 6 — второе предупреждение
- День 7 — кубки обнуляются
- День 8 — удаление из стаи

Чтобы получать предупреждения, открой диалог с ботом: /start в личке

🏆 АЧИВКИ
Ачивки даются на отметках 7, 14, 21, 30, 42, 50 и 100 дней подряд.

Ачивку можно потратить на:
- Заморозку — стрик под защитой 7 дней
- Спасение — в критический момент ачивка не даёт тебя удалить

❄️ ПЛАТНАЯ ЗАМОРОЗКА
Нет ачивок, но нужна пауза? 42 ₽ за 7 дней.

🔄 ВЕРНУТЬСЯ В СТАЮ
Был удалён — возвращайся за 210 ₽. Кубки и ачивки не сохраняются.

🎯 Начни прямо сейчас — отметь тренировку в мини-аппе`

func New(cfg *config.Config, db *database.Database, log logger.Logger) (*Bot, error) {
	api, err := tgbotapi.NewBotAPI(cfg.APIToken)
	if err != nil {
		return nil, fmt.Errorf("failed to create bot: %w", err)
	}

	// Создаем таблицы в базе данных
	if err := db.CreateTables(); err != nil {
		return nil, fmt.Errorf("failed to create tables: %w", err)
	}
	if err := db.AttachTrackerDatabase(cfg.TrackerDatabaseURL); err != nil {
		return nil, fmt.Errorf("tracker database: %w", err)
	}

	// §10: множество альфа-тестеров — события этих юзеров помечаются is_alpha.
	db.SetAlphaTesterIDs(cfg.AlphaTesterIDs)

	// Создаем клиент OpenRouter для ИИ
	var aiClient *ai.OpenRouterClient
	if cfg.OpenRouterAPIKey != "" {
		aiClient = ai.NewOpenRouterClient(cfg.OpenRouterAPIKey, cfg.OpenRouterModel, cfg.Prompts, log, cfg.OpenRouterTimeout)
		aiClient.SetVisionModel(cfg.OpenRouterVisionModel)
		log.Infof("OpenRouter AI client initialized with model: %s (vision: %s)", cfg.OpenRouterModel, cfg.OpenRouterVisionModel)
	} else {
		log.Warn("OpenRouter API key not provided, AI features will be disabled")
	}

	var ragStore rag.Store = rag.NoopStore{}
	if cfg.RAGEnabled && cfg.QdrantURL != "" && cfg.OpenRouterAPIKey != "" {
		emb := ai.NewEmbeddingClient(cfg.OpenRouterAPIKey, cfg.RAGEmbeddingModel, cfg.OpenRouterTimeout)
		ragStore = rag.NewQdrantStore(rag.QdrantConfig{
			URL:        cfg.QdrantURL,
			APIKey:     cfg.QdrantAPIKey,
			Collection: cfg.QdrantCollection,
		}, emb, log)
		log.Infof("RAG/Qdrant enabled: url=%s collection=%s", cfg.QdrantURL, cfg.QdrantCollection)
	} else if cfg.RAGEnabled {
		log.Warn("RAG_ENABLED=true but QDRANT_URL or OPENROUTER_API_KEY missing — RAG disabled")
	}

	limiter := moderation.NewLimiter()
	b := &Bot{
		api:                  api,
		db:                   db,
		logger:               log,
		config:               cfg,
		timers:               make(map[int64]*domain.TimerInfo),
		aiClient:             aiClient,
		ragStore:             ragStore,
		sickApprovalWatchers: make(map[int64]chan struct{}),
		adminSessions:        make(map[int64]*adminSession),
		userSupportSessions:  make(map[int64]struct{}),
		miniappPersonalQueue: make(map[int64][]string),
		ugcModerationLimiter: limiter,
		ugcModerationGate:    moderation.NewGate(limiter),
		dynamicAdmins:        make(map[int64]struct{}),
	}
	if aiClient != nil {
		aiClient.SetLivePrompts(b.livePrompts)
	}
	b.reloadDynamicAdmins()
	return b, nil
}

func (b *Bot) Start(ctx context.Context) error {
	b.logger.Info("Starting bot...")
	if b.ragStore != nil && b.ragStore.Enabled() {
		if err := b.ragStore.EnsureCollection(ctx); err != nil {
			b.logger.Warnf("RAG ensure collection: %v", err)
		}
	}
	if b.config.PaywallEnabled {
		if b.config.MonetizedChatID == 0 {
			b.logger.Warn("PAYWALL_ENABLED=true but MONETIZED_CHAT_ID is not set")
		}
		if !b.config.PaywallPaymentReady() {
			b.logger.Warn("PAYWALL_ENABLED=true but payment is not configured: PAYMENT_STARS_ENABLED + сумма, или PAYMENT_CURRENCY=XTR, или PAYMENT_PROVIDER_TOKEN, или YOOKASSA_* с RUB/суммой")
		}
		// MONETIZED_CHAT_INVITE_URL / PAYWALL_INVITE_CREATES_JOIN_REQUEST — устаревшие опции:
		// после миграции на мини-апп TG-группа как сущности нет, ссылки в группу не создаём.
		// Оставлены в config.Config для обратной совместимости со старыми .env, но не используются.
		if b.config.PaywallPaymentReady() {
			if b.config.PaywallUsesStars() {
				b.logger.Infof("Paywall: Telegram Stars (%d ⭐), provider_token пустой", b.config.PaywallStarsInvoiceAmount())
			}
			if b.config.PaywallUsesTelegramProviderInvoice() {
				b.logger.Info("Paywall: счёт в Telegram через PAYMENT_PROVIDER_TOKEN (карта провайдера)")
			}
			if b.config.PaywallYookassaReady() {
				b.logger.Info("Paywall: ЮKassa — ссылка в ЛС; вебхук в ту же БД")
				if strings.TrimSpace(b.config.YookassaNotificationURL) == "" {
					b.logger.Warn("YOOKASSA_NOTIFICATION_URL пуст — уведомления идут только на URL из ЛК ЮKassa. Если вебхук не приходит, задай YOOKASSA_NOTIFICATION_URL=https://<ms_payments>/api/v1/webhook/payment")
				}
			}
		}
	}

	// Восстанавливаем таймеры из базы данных
	if err := b.recoverTimersFromDatabase(); err != nil {
		b.logger.Errorf("Failed to recover timers from database: %v", err)
		// Не останавливаем бота, просто логируем ошибку
	}

	b.restoreSickApprovalWatchers()
	b.setupAdminBotCommands()

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
	go b.startOutboxWorker(ctx)
	// Publish отложенных админских постов ленты (см. startScheduledAdminPostsWorker).
	go b.startScheduledAdminPostsWorker(ctx)
	// Автономный Лео: сам придумывает спринты, пока админ держит режим
	// включённым (см. leo_autonomy.go).
	go b.startLeoAutonomyScheduler(ctx)
	// Созревшие карточки трекера: when_at наступил — сами в «В работе»
	// (см. tracker_run.go). Без этого «Сейчас» / «через 1 мин» так и висели.
	go b.startTrackerDueScheduler(ctx)
	// Повторное уведомление админам, если аппрув завис больше часа (см. tracker_approval_reminder.go).
	go b.startTrackerApprovalReminderScheduler(ctx)
	// Periodic-страховка от пропущенных киков (см. startInactivityKickWatchdog).
	go b.startInactivityKickWatchdog(ctx)
	// Напоминания «внеси тренировку» в локальный час пользователя (см. startWorkoutReminderScheduler).
	go b.startWorkoutReminderScheduler(ctx)
	// Подписка на «мудрость дня» в личку бота (см. startDailyWisdomSubscriptionScheduler).
	go b.startDailyWisdomSubscriptionScheduler(ctx)
	// Release Notes от Лео раз в две недели (см. release_notes.go).
	go b.startReleaseNotesScheduler(ctx)

	updatesCh := b.runGetUpdatesWithWebApp(ctx)

	for {
		select {
		case update := <-updatesCh:
			go b.handleUpdate(update)
		case <-ctx.Done():
			b.logger.Info("Bot stopped")
			return nil
		}
	}
}

// runGetUpdatesWithWebApp — long poll getUpdates + подстановка web_app_data в text (как в библиотеке, плюс merge).
func (b *Bot) runGetUpdatesWithWebApp(ctx context.Context) <-chan tgbotapi.Update {
	buf := 100
	if b.api != nil && b.api.Buffer > 0 {
		buf = b.api.Buffer
	}
	ch := make(chan tgbotapi.Update, buf)
	config := tgbotapi.NewUpdate(0)
	config.Timeout = 60
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			resp, err := b.api.Request(&config)
			if err != nil {
				b.logger.Errorf("getUpdates: %v, retry in 3s", err)
				time.Sleep(3 * time.Second)
				continue
			}
			updates, err := UnmarshalUpdatesWithWebApp(resp.Result)
			if err != nil {
				b.logger.Errorf("parse updates: %v, retry in 3s", err)
				time.Sleep(3 * time.Second)
				continue
			}
			for _, u := range updates {
				if u.UpdateID >= config.Offset {
					config.Offset = u.UpdateID + 1
					select {
					case ch <- u:
					case <-ctx.Done():
						return
					}
				}
			}
		}
	}()
	return ch
}

func (b *Bot) getUserLocalNow(offsetFromMoscow int) time.Time {
	return utils.GetMoscowTime().Add(time.Duration(offsetFromMoscow) * time.Hour)
}

func (b *Bot) getUserLocalDate(offsetFromMoscow int) string {
	return b.getUserLocalNow(offsetFromMoscow).Format("2006-01-02")
}

func (b *Bot) handleUpdate(update tgbotapi.Update) {
	metrics.BotUpdatesReceived.Inc()

	// Обрабатываем callback queries (нажатия на inline кнопки)
	if update.CallbackQuery != nil {
		b.handleCallbackQuery(update.CallbackQuery)
		return
	}

	// Счёт в звёздах на донат из профиля и счёт за платный возврат различаем по payload
	// (dn_<id> против pw_<id>): у них разные суммы, таблицы и последствия.
	if update.PreCheckoutQuery != nil {
		metrics.PaymentRequests.WithLabelValues("precheckout").Inc()
		if IsDonatePayload(update.PreCheckoutQuery.InvoicePayload) {
			b.handleDonatePreCheckout(update.PreCheckoutQuery)
			return
		}
		b.handlePaywallPreCheckout(update.PreCheckoutQuery)
		return
	}

	// Миграция на мини-апп: бот молчит во всех групповых TG-чатах.
	// Вся механика (отчёты, sick/healthy, лента, чат стаи, онбординг) живёт в мини-аппе и личке с ботом.
	// Личка (private) и платёжные апдейты (CallbackQuery / PreCheckout / SuccessfulPayment) обрабатываются как раньше.
	// ChatJoinRequest / NewChatMembers больше не используются: TG-группа как сущность убрана.
	if update.Message != nil && update.Message.Chat != nil &&
		(update.Message.Chat.Type == "group" || update.Message.Chat.Type == "supergroup") {
		return
	}

	if update.Message == nil {
		return
	}

	msg := update.Message
	if msg.SuccessfulPayment != nil {
		metrics.PaymentRequests.WithLabelValues("success").Inc()
		if IsDonatePayload(msg.SuccessfulPayment.InvoicePayload) {
			b.handleDonateSuccessfulPayment(msg)
			return
		}
		b.handlePaywallSuccessfulPayment(msg)
		return
	}

	b.dispatchTextMessageFromUser(msg, nil, "")
}

// dispatchTextMessageFromUser — тот же путь, что личка с ботом (и Mini App API с initData).
// personalReplyCh — не nil только из Mini App: одно дублирование персонального ответа (см. #training_done).
// trainingPhotoURLOverride — непустой при POST /api/miniapp/workout с фото (без sync.Map).
func (b *Bot) dispatchTextMessageFromUser(msg *tgbotapi.Message, personalReplyCh chan<- string, trainingPhotoURLOverride string) {
	b.logger.Infof("Received message from %d: %s", msg.From.ID, msg.Text)

	if msg.Chat != nil && msg.Chat.IsPrivate() && msg.From != nil && b.isAdminTelegramUser(msg.From.ID) {
		b.syncPrivateBottomKeyboard(msg.Chat.ID, msg.From.ID)
	}

	// Отчёт о тренировке из мини-аппа — не ответ мастеру и не вопрос в поддержку.
	// Иначе у админа с открытым мастером (или у юзера в сессии поддержки) отчёт
	// молча проглатывался: ни стрика, ни кубков, в мини-аппе «загляни в личку».
	miniappTrainingReport := personalReplyCh != nil && leopardmoney.IsTrainingReportLine(msg.Text)

	// Админ-мастер перехватывает сообщения владельца в личке при активной сессии.
	if !miniappTrainingReport && b.handleAdminFlowMessage(msg) {
		return
	}

	// Поддержка в личке (оплата, доступ) — до мини-аппа, без Лео.
	if !miniappTrainingReport && b.handleUserSupportFlowMessage(msg) {
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
	b.handleMessage(msg, personalReplyCh, trainingPhotoURLOverride)
}

func (b *Bot) handleCommand(msg *tgbotapi.Message) {
	command := msg.Command()
	_ = msg.CommandArguments() // Игнорируем аргументы пока

	switch command {
	case "start":
		b.handleStart(msg)
	case "rejoin":
		b.handleRejoin(msg)
	case "start_timer":
		b.handleStartTimer(msg)
	case "help":
		b.handleHelp(msg)
	case "db":
		b.handleDB(msg)
	case "top":
		b.handleTop(msg)
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
	default:
		b.logger.Warnf("Unknown command: %s", command)
	}
}

func (b *Bot) handleMessage(msg *tgbotapi.Message, personalReplyCh chan<- string, trainingPhotoURLOverride string) {
	// Проверяем наличие хештегов в тексте или подписи
	text := msg.Text
	if text == "" && msg.Caption != "" {
		text = msg.Caption
	}

	b.tryHandleSickApprovalReply(msg, text)

	if text != "" && strings.Contains(strings.ToLower(text), "#change") {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ #change больше не работает: обмен калорий убран. Сейчас в игре кубки и стрик — отмечай тренировки в мини-аппе («+»).")
		if _, err := b.api.Send(reply); err != nil {
			b.logger.Errorf("send #change deprecation reply: %v", err)
		}
		return
	}

	// Хештеги команд (#sick_leave, #healthy, …). Отчёт о тренировке — только формат мини-аппа, не #training_done.
	hasTrainingReport := leopardmoney.IsTrainingReportLine(text)
	hasSickLeave := strings.Contains(strings.ToLower(text), "#sick_leave")
	hasHealthy := strings.Contains(strings.ToLower(text), "#healthy")
	hasCommand := hasTrainingReport || hasSickLeave || hasHealthy

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

		var trainingPhotoURL string
		alreadyOnSickLeave := false
		if hasSickLeave {
			// Стейт больничного храним на pack-row; если больничный уже активен —
			// повторный #sick_leave не пишем в ленту второй раз.
			if kickLog, kerr := b.db.GetMessageLog(msg.From.ID, b.kickChatIDForMessage(msg)); kerr == nil && kickLog != nil && kickLog.HasSickLeave {
				alreadyOnSickLeave = true
			}
		}
		stateChatID := b.packTrainingStateChatID(msg)

		if hasTrainingReport {
			if trainingPhotoURLOverride != "" {
				trainingPhotoURL = trainingPhotoURLOverride
			} else {
				trainingPhotoURL = b.takeMiniappTrainingPhotoURL(msg.From.ID)
			}
		}
		var trainingDoneFeedMsgID int64
		if text != "" && !(hasSickLeave && alreadyOnSickLeave) {
			messageType := "general"
			if hasTrainingReport {
				messageType = "training_done"
			} else if hasSickLeave {
				messageType = "sick_leave"
			} else if hasHealthy {
				messageType = "healthy"
			}

			userMsg := &domain.UserMessage{
				UserID:           msg.From.ID,
				ChatID:           msg.Chat.ID,
				Username:         username,
				MessageText:      text,
				MessageType:      messageType,
				TrainingPhotoURL: trainingPhotoURL,
			}
			if hasTrainingReport {
				id, err := b.db.SaveUserMessageReturningID(userMsg)
				if err != nil {
					b.logger.Errorf("Failed to save user message: %v", err)
				} else {
					trainingDoneFeedMsgID = id
					// Лента мини-аппа и реакции/треды читают user_messages с chat_id = стая.
					// Отчёт из лички / мини-аппа пишется с chat_id = private (как у Telegram); дублируем строку для ленты.
					if b.config.MonetizedChatID != 0 && msg.Chat != nil && msg.Chat.Type == "private" {
						mirror := &domain.UserMessage{
							UserID:           userMsg.UserID,
							ChatID:           b.config.MonetizedChatID,
							Username:         userMsg.Username,
							MessageText:      userMsg.MessageText,
							MessageType:      userMsg.MessageType,
							TrainingPhotoURL: trainingPhotoURL,
						}
						feedID, errM := b.db.SaveUserMessageReturningID(mirror)
						if errM != nil {
							b.logger.Warnf("mirror training_done to pack feed user_messages: %v", errM)
						} else {
							trainingDoneFeedMsgID = feedID
						}
					}
				}
			} else {
				// #sick_leave / #healthy — приватные события, в ленту стаи их не дублируем
				// (только #training_done зеркалится в pack feed выше).
				if err := b.db.SaveUserMessage(userMsg); err != nil {
					b.logger.Errorf("Failed to save user message: %v", err)
				}
			}
		}

		// Получаем существующие данные пользователя
		existingLog, err := b.db.GetMessageLog(msg.From.ID, stateChatID)
		if err != nil {
			// Если пользователя нет в БД, создаем новую запись
			timerStartTime := utils.FormatMoscowTime(utils.GetMoscowTime())
			messageLog := &domain.MessageLog{
				UserID:          msg.From.ID,
				ChatID:          stateChatID,
				Username:        username,
				StreakDays:      0,
				CupsEarned:      0,
				LastMessage:     timerStartTime,
				HasTrainingDone: hasTrainingReport,
				HasSickLeave:    false,
				HasHealthy:      false,
				IsDeleted:       false,
				TimerStartTime:  &timerStartTime,
			}

			if err := b.db.SaveMessageLog(messageLog); err != nil {
				b.logger.Errorf("Failed to save message log: %v", err)
			} else {
				b.logger.Infof("Initialized timer state for new user %d (%s) from message", msg.From.ID, username)
				b.startTimer(msg.From.ID, stateChatID, username)
			}
		} else {
			// Обновляем только необходимые поля, сохраняя streak данные
			existingLog.Username = username
			existingLog.LastMessage = utils.FormatMoscowTime(utils.GetMoscowTime())
			existingLog.HasTrainingDone = hasTrainingReport
			existingLog.IsDeleted = false

			if err := b.db.SaveMessageLog(existingLog); err != nil {
				b.logger.Errorf("Failed to update message log: %v", err)
			}
		}

		// Обрабатываем хештеги
		if hasTrainingReport {
			b.handleTrainingDone(msg, personalReplyCh, trainingDoneFeedMsgID)
		} else if hasSickLeave {
			b.handleSickLeave(msg)
		} else if hasHealthy {
			b.handleHealthy(msg)
		}
		return // Выходим, не обрабатывая через ИИ
	}

	// В режиме поддержки в личке — только в поддержку, не в Лео.
	if msg.Chat != nil && msg.Chat.IsPrivate() && msg.From != nil && b.userInSupportSession(msg.From.ID) {
		if text != "" || msg.Caption != "" {
			_ = b.handleUserSupportFlowMessage(msg)
			return
		}
	}

	// Если нет команд — вопросы к ИИ: в личке с ботом — любой текст; в группах — @ или ответ на бота.
	shouldHandleAI := false
	// Личка: Type == "private" или (иногда) пустой Type — в личке chat_id совпадает с id отправителя.
	if msg.Chat != nil && text != "" && msg.From != nil &&
		(msg.Chat.IsPrivate() || msg.Chat.ID == msg.From.ID) {
		shouldHandleAI = true
	} else {
		// Проверяем упоминание через @ в тексте
		if msg.Entities != nil && text != "" {
			for _, entity := range msg.Entities {
				if entity.Type == "mention" {
					mentionText := ""
					if entity.Offset+entity.Length <= len(text) {
						mentionText = text[entity.Offset : entity.Offset+entity.Length]
					}

					botUsername := b.api.Self.UserName
					if botUsername == "" {
						botUsername = strings.TrimPrefix(mentionText, "@")
					}

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

		if !shouldHandleAI && msg.ReplyToMessage != nil {
			if msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.IsBot &&
				msg.ReplyToMessage.From.ID == b.api.Self.ID {
				shouldHandleAI = true
				b.logger.Infof("Reply to bot message detected")
			}
		}
	}

	// Реплика из мини-аппа пришла в TG с префиксом — не запускаем ИИ повторно (ответ уже в приложении или не было @).
	if msg.Chat != nil && msg.Chat.ID == b.config.MonetizedChatID &&
		(msg.Chat.Type == "supergroup" || msg.Chat.Type == "group") &&
		text != "" && strings.HasPrefix(strings.TrimSpace(text), "💬 Мини-апп") {
		shouldHandleAI = false
	}

	// Если обращение к боту обнаружено и есть текст вопроса
	if shouldHandleAI && text != "" {
		isPrivateLeo := msg.Chat != nil && msg.From != nil &&
			(msg.Chat.IsPrivate() || msg.Chat.ID == msg.From.ID)
		if isPrivateLeo {
			if _, err := b.enforceLeoChat(text, msg.From.ID); err != nil {
				var mod *ModerationBlockedError
				reply := "⚠️ Сообщение не отправлено."
				if errors.As(err, &mod) && mod != nil && strings.TrimSpace(mod.Message) != "" {
					reply = "⚠️ " + mod.Message
				}
				miniReply := func(s string) {
					if personalReplyCh == nil || s == "" {
						return
					}
					select {
					case personalReplyCh <- s:
					default:
					}
				}
				miniReply(reply)
				if personalReplyCh == nil && b.api != nil {
					b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, reply))
				}
				return
			}
		}

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

		// Нативная личка Telegram: дублируем в miniapp_personal_chat, чтобы вкладка «Лео» в мини-аппе
		// совпадала с перепиской в TG (пишем в БД и при personalReplyCh!=nil — уже сделано в processMiniAppPrivateCore).
		if personalReplyCh == nil && msg.Chat != nil && msg.From != nil &&
			(msg.Chat.IsPrivate() || msg.Chat.ID == msg.From.ID) {
			b.savePersonalChatMessage(msg.From.ID, "user", text)
		}

		compactCtx := personalReplyCh != nil
		if !compactCtx && msg.Chat != nil {
			if msg.Chat.IsPrivate() || (msg.From != nil && msg.Chat.ID == msg.From.ID) {
				compactCtx = true // личка: меньше контекста — быстрее ответ без потери смысла
			}
		}

		b.handleAIQuestion(msg, text, personalReplyCh, personalReplyCh != nil, compactCtx)
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

		// Обновляем LastMessage в training_state
		rowChat := b.packTrainingStateChatID(msg)
		messageLog, err := b.db.GetMessageLog(msg.From.ID, rowChat)
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

func (b *Bot) handleTrainingDone(msg *tgbotapi.Message, personalReplyCh chan<- string, trainingUserMessageID int64) {
	b.handleLeopardMoneyTrainingDone(msg, personalReplyCh, trainingUserMessageID)
}

func (b *Bot) evaluateSickLeaveJustification(text string, messageLog *domain.MessageLog) bool {
	clean := strings.TrimSpace(strings.ToLower(text))
	clean = strings.ReplaceAll(clean, "#sick_leave", "")
	clean = strings.ReplaceAll(clean, "#sickleave", "")
	clean = strings.ReplaceAll(clean, "#healthy", "")
	clean = strings.ReplaceAll(clean, "#здоров", "")

	heuristicsApprove, hasNegative := sickleave.EvaluateHeuristics(clean)

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

func (b *Bot) handleStartTimer(msg *tgbotapi.Message) {
	// Проверяем права администратора
	if !b.isAdmin(msg.Chat.ID, msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Только администраторы или владелец могут использовать эту команду!")
		b.api.Send(reply)
		return
	}

	packScope := b.packTrainingStateChatID(msg)

	// Получаем всех пользователей в чате
	users, err := b.db.GetUsersByChatID(packScope)
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
			b.startTimer(user.UserID, packScope, "")
			startedCount++
		}
	}

	// Отправляем отчет
	reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("🐆 Fat Leopard активирован!\n\n⏱️ Запущено таймеров: %d\n⏰ Время: 7 дней\n💪 Действие: отметь тренировку в мини-аппе", startedCount))

	b.logger.Infof("Sending start timer message to chat %d", msg.Chat.ID)
	_, err = b.api.Send(reply)
	if err != nil {
		b.logger.Errorf("Failed to send start timer message: %v", err)
	} else {
		b.logger.Infof("Successfully sent start timer message to chat %d", msg.Chat.ID)
	}
}

// leopardOnboardingBody — длинный онбординг (legacy): группы / paywall выключен.
// При активном paywall оплативший в личке получает на /start короткий текст как после оплаты (paywallPostPaymentUserText).
func leopardOnboardingBody() string {
	return leopardOnboardingBodyText
}

func welcomeStartText() string {
	return leopardOnboardingBody()
}

func (b *Bot) handleHelp(msg *tgbotapi.Message) {
	if msg.From != nil && msg.Chat.IsPrivate() && b.paywallActive() && b.paywallPrivateNeedsPayFirst(msg.From.ID) {
		b.ensurePaywallInvoiceSent(msg.From.ID)
		if !b.paywallPrivateNeedsPayFirst(msg.From.ID) {
			return
		}
		if err := b.sendPaywallUnpaidPrivateScreen(msg.Chat.ID); err != nil {
			b.logger.Errorf("Failed to send paywall-only help: %v", err)
		}
		return
	}

	helpText := `🦁 Fat Leopard — Справка

🐆 Вход:
• Нажми /start — доступ к мини-приложению открывается сразу и бесплатно
• Отмечай любое движение каждый день: 8 дней без активности — и ты выбываешь из стаи
• Вернуться после выбывания можно только за оплату (картой РФ или звёздами Telegram)

❤️ Поддержать проект:
• Кнопка «Задонатить» в профиле мини-приложения
• Звёздами Telegram — из любой страны, картой — для РФ
• Донат добровольный: он не влияет на доступ и не отменяет выбывание

💬 Поддержка:
• Нажми кнопку «💬 Поддержка» внизу экрана
• Или напиши нам — твоё сообщение придёт команде напрямую
• Отвечаем в личном чате с ботом

Оставайся активным! 🦁`

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
	// Вход в приложение на компьютере: ссылка t.me/<bot>?start=auth_<nonce>.
	// Показываем только подтверждение — витрина тут не к месту.
	if msg != nil && msg.Chat != nil && msg.Chat.IsPrivate() {
		if nonce := ParseDesktopStartPayload(msg.CommandArguments()); nonce != "" {
			b.handleDesktopLoginStart(msg, nonce)
			return
		}
	}
	// Фиксируем визит в личке
	if msg.From != nil && msg.Chat.IsPrivate() && b.db != nil {
		username := msg.From.UserName
		firstName := msg.From.FirstName
		lastName := msg.From.LastName
		go func() {
			if err := b.db.RecordBotVisit(msg.From.ID, username, firstName, lastName); err != nil {
				b.logger.Warnf("RecordBotVisit: %v", err)
			}
		}()
		// Воронка 1: bot_started с channel attribution из deep-link (?start=src-...).
		b.db.TrackEvent(database.AnalyticsEvent{
			Name:       database.EventBotStarted,
			TelegramID: msg.From.ID,
			Source:     parseStartSource(msg.CommandArguments()),
		})
	}
	// После оплаты ЮKassa вебхук может опоздать — подтягиваем succeeded и выдаём доступ до проверки paywall.
	if msg.From != nil && msg.Chat.IsPrivate() && b.paywallActive() {
		if b.config.PaywallYookassaReady() {
			b.paywallTrySyncYookassaPayment(msg.From.ID)
		}
		b.paywallTryFinishPaidAccessDelivery(msg.From.ID)
	}
	// Донат картой мог быть оплачен в браузере без возврата в мини-апп — догоняем «спасибо».
	// В горутине: это HTTP к ЮKassa, а /start ждать не должен.
	if msg.From != nil && msg.Chat.IsPrivate() {
		userID := msg.From.ID
		go b.DonateSyncPendingForUser(userID)
	}
	// Меню-кнопка LeopardMiniApp в ЛС: только paid+не кикнутым (после sync выше).
	if msg.From != nil && msg.Chat.IsPrivate() {
		b.applyMiniappMenuButtonForUser(msg.From.ID)
	}

	if msg.From != nil && msg.Chat.IsPrivate() && b.paywallActive() && b.paywallPrivateNeedsPayFirst(msg.From.ID) {
		b.ensurePaywallInvoiceSent(msg.From.ID)
		if b.paywallPrivateNeedsPayFirst(msg.From.ID) {
			b.logger.Infof("Sending paywall-only /start to chat %d", msg.Chat.ID)
			if err := b.sendPaywallUnpaidPrivateScreen(msg.Chat.ID); err != nil {
				b.logger.Errorf("Failed to send paywall /start: %v", err)
			}
			return
		}
		// Оплата могла подтянуться (sync) — показываем полный /start оплатившему, без второго сообщения только со ссылкой.
	}

	// Бесплатный вход: доступ есть, значит с этого /start заводим профиль стаи и таймер.
	if msg.From != nil && msg.Chat.IsPrivate() {
		b.EnsureFreeEntryFromStart(msg.From.ID, displayNameFromTelegramUser(msg.From))
	}

	welcomeText := welcomeStartText()
	// Короткий текст «ты в стае» — всем, у кого доступ есть: оплатившим и вошедшим бесплатно.
	if msg.Chat.IsPrivate() && b.paywallActive() && msg.From != nil && !b.paywallPrivateNeedsPayFirst(msg.From.ID) {
		b.logger.Infof("/start access granted user=%d free_entry=%t snapshot=%s",
			msg.From.ID, b.freeEntryActive(), b.db.PaywallAccessDebugSnapshot(msg.From.ID, b.config.MonetizedChatID))
		welcomeText = b.paywallPostPaymentUserText()
	}

	reply := tgbotapi.NewMessage(msg.Chat.ID, welcomeText)
	if msg.Chat.IsPrivate() && msg.From != nil {
		if b.isAdminTelegramUser(msg.From.ID) {
			welcomeText += "\n\n⚙️ Админ-панель — кнопка «" + botAdminReplyButtonText + "» внизу экрана (или /admin)."
			reply.Text = welcomeText
		}
		if kb := b.privateBottomReplyKeyboard(msg.From.ID); kb != nil {
			reply.ReplyMarkup = kb
		}
		// Оплатившему добавляем сообщение с inline-кнопкой «Открыть» мини-аппу —
		// отдельным сообщением, чтобы не конфликтовать с reply-клавиатурой выше.
		if b.paywallActive() && !b.paywallPrivateNeedsPayFirst(msg.From.ID) {
			if ikb := b.miniappOpenInlineKeyboard(msg.From.ID); ikb != nil {
				open := tgbotapi.NewMessage(msg.Chat.ID, "Открыть тренировки — тапни кнопку ниже 👇")
				open.ReplyMarkup = *ikb
				if _, err := b.api.Send(open); err != nil {
					b.logger.Warnf("send miniapp open button user=%d: %v", msg.From.ID, err)
				}
			}
		}
	}

	b.logger.Infof("Sending start message to chat %d", msg.Chat.ID)
	_, errSend := b.api.Send(reply)
	if errSend != nil {
		b.logger.Errorf("Failed to send start message: %v", errSend)
	} else {
		b.logger.Infof("Successfully sent start message to chat %d", msg.Chat.ID)
	}
}

// handleRejoin — после миграции на мини-апп TG-группа выпилена; /rejoin превратили в напоминание открыть мини-апп.
func (b *Bot) handleRejoin(msg *tgbotapi.Message) {
	if !msg.Chat.IsPrivate() {
		_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "ℹ️ Команда /rejoin работает в личке с ботом."))
		return
	}
	if !b.paywallActive() || msg.From == nil {
		_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "ℹ️ Платный вход сейчас не используется."))
		return
	}
	if b.paywallPrivateNeedsPayFirst(msg.From.ID) {
		reply := tgbotapi.NewMessage(msg.Chat.ID, "⚠️ Сначала оплати доступ. Нажми /start, чтобы получить счёт.")
		if kb := b.privateBottomReplyKeyboard(msg.From.ID); kb != nil {
			reply.ReplyMarkup = kb
		}
		if _, err := b.api.Send(reply); err == nil {
			if ik := b.paywallUnpaidInlineKeyboard(); ik != nil {
				methods := tgbotapi.NewMessage(msg.Chat.ID, "Способы оплаты:")
				methods.ReplyMarkup = ik
				_, _ = b.api.Send(methods)
			}
		}
		return
	}
	_, _ = b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, "✅ Доступ активен. Открой мини-приложение бота — внизу экрана в этом чате (или через меню ⋮)."))
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
	rowChat := b.packTrainingStateChatID(msg)
	// Получаем топ пользователей
	topUsers, err := b.db.GetTopUsers(rowChat, 10)
	if err != nil {
		b.logger.Errorf("Failed to get top users: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных")
		b.api.Send(reply)
		return
	}

	if len(topUsers) == 0 {
		emptyText := "🏆 **Топ пользователей:**\n\n📊 Пока нет данных о тренировках"
		reply := tgbotapi.NewMessage(msg.Chat.ID, emptyText)
		reply.ParseMode = "Markdown"
		b.api.Send(reply)
		return
	}

	topText := "🏆 Топ пользователей по кубкам:\n\n"
	for i, user := range topUsers {
		emoji := "🥇"
		if i == 1 {
			emoji = "🥈"
		} else if i == 2 {
			emoji = "🥉"
		} else {
			emoji = fmt.Sprintf("%d️⃣", i+1)
		}
		topText += fmt.Sprintf("%s %s — %d %s\n", emoji, user.Username, user.CupsEarned, cupsWordForm(user.CupsEarned))
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

func (b *Bot) handleCups(msg *tgbotapi.Message) {
	rowChat := b.packTrainingStateChatID(msg)
	// Получаем кубки пользователя
	cups, err := b.db.GetUserCups(msg.From.ID, rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get user cups: %v", err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Ошибка при получении данных")
		b.api.Send(reply)
		return
	}

	// Получаем пол пользователя для гендерной адаптации
	messageLog, err := b.db.GetMessageLog(msg.From.ID, rowChat)
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
		cupsText = fmt.Sprintf("🎊 ПОЗДРАВЛЯЕМ! 🎊\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n🏆 ТЫ %s ЦЕЛИ РОЗЫГРЫША!\n🎁 Участвуешь в розыгрыше футболки Fat Leopard!\n💪 Ты настоящий %s!\n🔥 Продолжай тренироваться!", username, cups, strings.ToUpper(forms.Reached), forms.Champion)
	} else {
		cupsText = fmt.Sprintf("🏆 Ваши кубки:\n\n👤 %s\n🎯 Всего заработано кубков: %d\n\n💡 Отмечайте тренировки в мини-аппе для получения кубков!\n\n🎊 Розыгрыш футболки Fat Leopard при достижении 420 кубков!", username, cups)
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

	rowChat := b.packTrainingStateChatID(msg)

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
	b.logger.Infof("Searching for user: '%s' in chat %d", searchUsername, rowChat)

	// Находим пользователя по username (функция сама обработает разные форматы)
	userID, err := b.db.GetUserIDByUsername(searchUsername, rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get user ID by username '%s': %v", searchUsername, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Пользователь %s не найден в базе данных", searchUsername))
		b.api.Send(reply)
		return
	}

	b.logger.Infof("Found user ID %d for username '%s'", userID, searchUsername)

	// Устанавливаем исключение
	messageLog, err := b.db.GetMessageLog(userID, rowChat)
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

	rowChat := b.packTrainingStateChatID(msg)

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
	b.logger.Infof("Searching for user: '%s' in chat %d", searchUsername, rowChat)

	// Находим пользователя по username (функция сама обработает разные форматы)
	userID, err := b.db.GetUserIDByUsername(searchUsername, rowChat)
	if err != nil {
		b.logger.Errorf("Failed to get user ID by username '%s': %v", searchUsername, err)
		reply := tgbotapi.NewMessage(msg.Chat.ID, fmt.Sprintf("❌ Пользователь %s не найден в базе данных", searchUsername))
		b.api.Send(reply)
		return
	}

	b.logger.Infof("Found user ID %d for username '%s'", userID, searchUsername)

	// Убираем исключение
	messageLog, err := b.db.GetMessageLog(userID, rowChat)
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
	b.startTimer(userID, rowChat, messageLog.Username)

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

	rowChat := b.packTrainingStateChatID(msg)

	// Получаем всех пользователей в чате
	users, err := b.db.GetUsersByChatID(rowChat)
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
	// Проверяем права доступа - только админ из env может отправлять сообщения в другие чаты.
	if !b.config.IsAdminTelegramUser(msg.From.ID) {
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
	// Проверяем права доступа - только админ из env может отправлять объявления.
	if !b.config.IsAdminTelegramUser(msg.From.ID) {
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

// handleAIQuestion обрабатывает вопрос пользователя к ИИ.
// personalReplyCh — дублирует доставленный в Telegram текст в Mini App (HTTP reply_text), если задан.
// skipTelegram — не слать в Telegram (общий чат мини-апpa: ответ только в БД/HTTP).
// compactContext — меньше истории/без RAG-примеров: быстрее и меньше токенов (мини-апп HTTP и pack-чат).
func (b *Bot) handleAIQuestion(msg *tgbotapi.Message, questionText string, personalReplyCh chan<- string, skipTelegram bool, compactContext bool) {
	b.logger.Infof("handleAIQuestion called for user %d with text: %s", msg.From.ID, questionText)
	miniReply := func(s string) {
		if personalReplyCh == nil || s == "" {
			return
		}
		select {
		case personalReplyCh <- s:
		default:
			b.logger.Warnf("miniapp reply channel full, drop duplicate fragment user_id=%d", msg.From.ID)
		}
	}

	if b.aiClient == nil {
		b.logger.Warn("AI client is nil, cannot process question")
		help := "❌ ИИ функции недоступны. Проверьте настройки OpenRouter API."
		miniReply(help)
		if !skipTelegram {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, help))
		}
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
		hint := "💬 Привет! 👋 Задай мне вопрос!\n\nНапример:\n• Что я делал вчера?\n• Как мой прогресс?\n• Что улучшить в тренировках?\n• Как лечиться?"
		miniReply(hint)
		if !skipTelegram {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, hint))
		}
		return
	}

	b.logger.Infof("Processing AI question: %s", questionText)

	stateChat := b.packTrainingStateChatID(msg)
	ctxChannel := b.aiContextChannel(msg, skipTelegram, compactContext)
	ragCtx, ragCancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer ragCancel()

	histLimit := 50
	if compactContext {
		histLimit = 18
	}
	// Получаем историю тренировок пользователя
	history, err := b.db.GetUserTrainingHistory(msg.From.ID, stateChat, histLimit)
	if err != nil {
		b.logger.Errorf("Failed to get user training history: %v", err)
		t := "❌ Ошибка при получении истории тренировок"
		miniReply(t)
		if !skipTelegram {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, t))
		}
		return
	}

	var aiSec aiQuestionSections
	aiSec.initRules()

	interlocutorName := strings.TrimSpace(msg.From.UserName)
	if interlocutorName == "" {
		interlocutorName = fmt.Sprintf("user%d", msg.From.ID)
	}

	if len(history) > 0 {
		for _, hm := range history {
			aiSec.facts.WriteString(formatTrainingFactLine(
				hm.CreatedAt.Format("2006-01-02 15:04"),
				hm.Username,
				msg.From.ID,
				hm.MessageType,
				hm.MessageText,
			))
			aiSec.facts.WriteString("\n")
		}
	}

	// Добавляем предыдущее сообщение бота только если пользователь отвечает на него
	lastBotMessageText := ""
	if msg.ReplyToMessage != nil && msg.ReplyToMessage.From != nil && msg.ReplyToMessage.From.IsBot && msg.ReplyToMessage.From.ID == b.api.Self.ID {
		replyText := strings.TrimSpace(msg.ReplyToMessage.Text)
		if replyText == "" && msg.ReplyToMessage.Caption != "" {
			replyText = strings.TrimSpace(msg.ReplyToMessage.Caption)
		}
		if replyText != "" {
			aiSec.thread.WriteString("• [бот] Лео: ")
			aiSec.thread.WriteString(replyText)
			aiSec.thread.WriteString("\n")
			lastBotMessageText = replyText
		}
	}

	userLog, err := b.db.GetMessageLog(msg.From.ID, stateChat)
	if err == nil && userLog != nil {
		if strings.TrimSpace(userLog.Username) != "" {
			interlocutorName = userLog.Username
		}
		cups, _ := b.db.GetUserCups(msg.From.ID, stateChat)
		remaining := ""
		if userLog.TimerStartTime != nil {
			if rt := b.calculateRemainingTime(userLog); rt > 0 {
				remaining = "до удаления: " + b.formatDurationToDays(rt)
				if userLog.HasSickLeave {
					remaining = "после #healthy: " + b.formatDurationToDays(rt)
				}
			} else {
				remaining = "таймер истёк"
			}
		}
		aiSec.users.WriteString(formatUserEntityLine(userLog, cups, remaining))
		aiSec.users.WriteString("\n")
	}

	// Недавний контекст беседы: общий чат — только pack_group; личка — user_messages / личный чат.
	if ctxChannel == rag.ChannelPackGroup {
		b.appendPackGroupSQLContext(stateChat, &aiSec.thread, 12)
	} else {
		recentLimit := 10
		if compactContext {
			recentLimit = 5
		}
		end := time.Now()
		start := end.Add(-2 * time.Hour)
		recentChat, err := b.db.GetMessagesInRange(msg.Chat.ID, start, end)
		if err == nil && len(recentChat) > 0 {
			count := 0
			for i := len(recentChat) - 1; i >= 0 && count < recentLimit; i-- {
				text := strings.TrimSpace(recentChat[i].MessageText)
				if text == "" {
					continue
				}
				if len(text) > 300 {
					text = text[:300] + "…"
				}
				ts := recentChat[i].CreatedAt.In(time.FixedZone("MSK", 3*3600)).Format("2006-01-02 15:04")
				aiSec.thread.WriteString("• [" + ts + "] ")
				aiSec.thread.WriteString(text)
				aiSec.thread.WriteString("\n")
				count++
			}
		}
	}

	// Добавляем анти‑повторы: последние ответы ИИ для этого пользователя
	{
		maxReplies := 5
		maxSnippet := 400
		lookbackDays := 30
		if compactContext {
			maxReplies = 2
			maxSnippet = 220
			lookbackDays = 7
		}
		end := time.Now()
		start := end.AddDate(0, 0, -lookbackDays)
		recent, err := b.db.GetUserMessagesAcrossTrainingScope(msg.From.ID, stateChat, start, end)
		if err == nil {
			var lastReplies []string
			for i := len(recent) - 1; i >= 0 && len(lastReplies) < maxReplies; i-- {
				if strings.ToLower(recent[i].MessageType) == "ai_reply" {
					lastReplies = append(lastReplies, recent[i].MessageText)
				}
			}
			if len(lastReplies) > 0 {
				for _, r := range lastReplies {
					txt := r
					if len(txt) > maxSnippet {
						txt = txt[:maxSnippet] + "…"
					}
					aiSec.thread.WriteString("• [бот] Лео: ")
					aiSec.thread.WriteString(txt)
					aiSec.thread.WriteString("\n")
				}
			}
		}
	}

	// Легкий RAG по чату — тяжёлый по токенам; для HTTP мини-аппа и pack-чата опускаем, чтобы быстрее ответить.
	if !compactContext {
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
				for _, p := range picked {
					aiSec.facts.WriteString("• [пример отчёта] ")
					aiSec.facts.WriteString(p)
					aiSec.facts.WriteString("\n")
				}
			}
		}
	}

	// «Печатает…» только в Telegram, не в режиме «только мини-апп».
	var typingDone chan struct{}
	if !skipTelegram {
		b.api.Send(tgbotapi.NewChatAction(msg.Chat.ID, tgbotapi.ChatTyping))
		typingDone = make(chan struct{})
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
	}

	// Пытаемся определить пол из сообщения или имени
	detectedGender := b.detectGenderFromMessage(questionText)
	if detectedGender == "" && msg.From.FirstName != "" {
		detectedGender = b.detectGenderFromName(msg.From.FirstName)
	}

	// Обновляем пол в базе данных, если он определен
	if detectedGender != "" {
		if err := b.updateUserGender(msg.From.ID, stateChat, detectedGender); err != nil {
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
	if len(mentionedUsernames) == 0 && (strings.Contains(questionLower, "пол") || strings.Contains(questionLower, "статистик") || strings.Contains(questionLower, "сколько") || strings.Contains(questionLower, "кубк") || strings.Contains(questionLower, "про ") || strings.Contains(questionLower, "расскажи") || strings.Contains(questionLower, "достижен") || strings.Contains(questionLower, "у него") || strings.Contains(questionLower, "у неё") || strings.Contains(questionLower, "его") || strings.Contains(questionLower, "её")) {
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
		userID, err := b.db.GetUserIDByUsername(searchUsername, stateChat)
		if err == nil && userID != msg.From.ID {
			// Нашли другого пользователя, получаем всю информацию о нём
			otherUserLog, err := b.db.GetMessageLog(userID, stateChat)
			if err == nil {
				cups, _ := b.db.GetUserCups(userID, stateChat)
				remaining := ""
				if otherUserLog.TimerStartTime != nil {
					if rt := b.calculateRemainingTime(otherUserLog); rt > 0 {
						remaining = "до удаления: " + b.formatDurationToDays(rt)
					}
				}
				aiSec.users.WriteString(formatUserEntityLine(otherUserLog, cups, remaining))
				aiSec.users.WriteString("\n")
			}
			break // Нашли одного пользователя, достаточно
		}
	}

	// Если спрашивают про список участников ("какие участники", "кто есть", "список участников", "какого пола участники")
	questionLower = strings.ToLower(questionText)
	if strings.Contains(questionLower, "участник") || strings.Contains(questionLower, "кто есть") || strings.Contains(questionLower, "список") {
		users, err := b.db.GetUsersByChatID(stateChat)
		if err == nil && len(users) > 0 {
			for i, user := range users {
				if i >= 15 {
					aiSec.users.WriteString(fmt.Sprintf("… и ещё %d участников\n", len(users)-15))
					break
				}
				cups, _ := b.db.GetUserCups(user.UserID, stateChat)
				remaining := ""
				if user.TimerStartTime != nil {
					if rt := b.calculateRemainingTime(user); rt > 0 {
						remaining = "до удаления: " + b.formatDurationToDays(rt)
					}
				}
				aiSec.users.WriteString(formatUserEntityLine(user, cups, remaining))
				aiSec.users.WriteString("\n")
			}
		}
	}

	// RAG + история диалога: изолированные сессии (личка vs общий чат).
	b.appendRAGContext(ragCtx, ctxChannel, msg.From.ID, stateChat, questionText, &aiSec.facts)
	if ctxChannel == rag.ChannelPersonalLeo && b.config.MonetizedChatID != 0 && b.db != nil {
		if b.ragStore == nil || !b.ragStore.Enabled() {
			personalHist, histErr := b.db.ListMiniappPersonalChat(msg.From.ID, b.config.MonetizedChatID, 10, 0)
			if histErr == nil && len(personalHist) > 0 {
				for _, h := range personalHist {
					role := "пользователь"
					if h.Role == "leo" {
						role = "бот"
					}
					ts := strings.TrimSpace(h.CreatedAt)
					if ts == "" {
						ts = "—"
					}
					aiSec.thread.WriteString(fmt.Sprintf("• [%s] %s: %s\n", ts, role, h.Text))
				}
			}
		}
	}

	// Генерируем ответ с помощью ИИ
	finalQuestion := questionText
	if lastBotMessageText != "" {
		finalQuestion = fmt.Sprintf(
			"МОЁ ПРЕДЫДУЩЕЕ СООБЩЕНИЕ:\n%s\n\nПОЛЬЗОВАТЕЛЬ ОТВЕТИЛ ТАК: %s\n\nСОХРАНИ ЛОГИКУ ПРЕДЫДУЩЕГО СООБЩЕНИЯ. ЕСЛИ ЕГО ОСПАРИВАЮТ ИЛИ ПРОСЛЕЖИВАЕТСЯ ХИТРОСТЬ, ПРОДОЛЖАЙ СТРОГО НАСТАИВАТЬ, ТРЕБУЙ ДОКАЗАТЕЛЬСТВ И НЕ СМЕНЯЙ ТОН НА ПОДДЕРЖИВАЮЩИЙ БЕЗ НОВЫХ ФАКТОВ.",
			lastBotMessageText,
			questionText,
		)
	}

	finalQuestion += b.livePrompts().CombinedChatInstructionSuffix()

	userPrompt := prompts.FormatAIQuestionUserMessage(prompts.AIQuestionUserPayload{
		InterlocutorName: interlocutorName,
		InterlocutorID:   msg.From.ID,
		UsersBlock:       aiSec.users.String(),
		RulesBlock:       aiSec.rules.String(),
		FactsBlock:       aiSec.facts.String(),
		ChatThread:       aiSec.thread.String(),
		RouterHint:       prompts.RouterHintForQuestion(questionText),
		Question:         finalQuestion,
	})

	answer, err := b.aiClient.AnswerUserQuestion("", userPrompt)
	if err != nil {
		b.logger.Errorf("Failed to generate AI answer: %v", err)

		// Проверяем, является ли это ошибкой настройки политики данных
		errorMsg := err.Error()
		if strings.Contains(errorMsg, "data policy") || strings.Contains(errorMsg, "Model Training") {
			help := "❌ ИИ функции требуют настройки OpenRouter API.\n\nДля бесплатных моделей нужно:\n1. Перейди на https://openrouter.ai/settings/privacy\n2. Включи опцию 'Model Training'\n\nПосле этого ИИ заработает!"
			miniReply(help)
			if !skipTelegram {
				b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, help))
			}
			if typingDone != nil {
				close(typingDone)
			}
			return
		}

		var et string
		if strings.Contains(err.Error(), "context deadline exceeded") || strings.Contains(err.Error(), "Client.Timeout") {
			et = "❌ ИИ не успел ответить вовремя. Попробуй ещё раз или сформулируй вопрос короче."
		} else {
			et = fmt.Sprintf("❌ Ошибка при генерации ответа ИИ: %v", err)
		}
		miniReply(et)
		if !skipTelegram {
			b.api.Send(tgbotapi.NewMessage(msg.Chat.ID, et))
		}
		if typingDone != nil {
			close(typingDone)
		}
		return
	}

	answer = ai.SanitizeTextForUser(answer)
	if answer == "" {
		answer = "Сформулируй, пожалуйста, вопрос короче — отвечу по сути."
	}

	miniReply(answer)
	// Отправляем ответ с реплаем на исходное сообщение
	if !skipTelegram {
		reply := tgbotapi.NewMessage(msg.Chat.ID, answer)
		if msg.MessageID != 0 {
			reply.ReplyToMessageID = msg.MessageID
		}
		b.logger.Infof("Sending AI answer to user %d in chat %d (replying to message %d)", msg.From.ID, msg.Chat.ID, msg.MessageID)
		_, err = b.api.Send(reply)
		if err != nil {
			b.logger.Errorf("Failed to send AI answer: %v", err)
		} else if msg.From != nil && strings.TrimSpace(answer) != "" {
			b.savePersonalChatMessage(msg.From.ID, "leo", answer)
		}
	}
	if typingDone != nil {
		close(typingDone)
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
		reply := tgbotapi.NewMessage(msg.Chat.ID, "❌ Эта команда доступна только админам бота")
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
		menuText := `🦁 Главное меню

Доступные команды:
/help - Помощь
/top - Топ пользователей по кубкам
/cups - Статистика по кубкам

💪 Тренировку отмечайте в мини-аппе Fat Leopard (кнопка «+»)`

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

// getUnifiedTrainingPrompt генерирует единый промпт для AI после #training_done
func (b *Bot) getUnifiedTrainingPrompt(streakDays, _ int, achievementCount int, wasOnSickLeave bool) string {
	now := utils.GetMoscowTime()
	hour := now.Hour()
	weekday := now.Weekday()
	formatHint := " Если в отчёте есть строка вида «тип, N мин, инт. X/5», то `инт.` означает интенсивность нагрузки по шкале 1..5, а не интервалы."

	prompts := []string{
		"Ты Fat Leopard — строгий тренер, который сам любит поесть. Напиши 2-3 предложения после отчёта о тренировке: первое — конкретный комментарий к упражнениям из сообщения, второе — короткое замечание или совет. Можно лёгкий намёк, что ты «не съешь» того, кто тренируется. Используй ТОЛЬКО упражнения из сообщения. Не повторяй цифры. Без Markdown.",

		"Ты Fat Leopard — ленивый, но справедливый. Ответь на отчёт о тренировке (2-3 предложения): первое — отметь упражнения, второе — короткий совет. Тон: немного завидуешь дисциплине, но признаёшь результат. Используй ТОЛЬКО этот отчёт. Без Markdown.",

		"Ответь как тренер после тренировки (2-3 предложения): первое — искренний комментарий к упражнениям из сообщения (без шаблонов), второе — один практический совет. Пиши живым языком. Используй ТОЛЬКО этот отчёт. Не повторяй цифры. Без Markdown.",

		"Ты Fat Leopard. Напиши 2-3 предложения после отчёта: первое — оценка тренировки с упоминанием упражнений, второе — конкретное замечание. Можно лёгкий юмор в духе «я бы уже спал, а ты ещё в деле». Используй ТОЛЬКО упражнения из сообщения. Без Markdown.",

		"Ответь как наставник после тренировки (2-3 предложения): первое — что именно понравилось в отчёте, второе — один конкретный совет. Избегай общих фраз про 'дух', 'волю', 'помни'. Используй ТОЛЬКО этот отчёт. Без Markdown.",

		"Напиши 2-3 предложения после отчёта о тренировке: первое — отметь конкретные упражнения, второе — короткое наблюдение о технике или подходе. Тон: поддерживающий, но с лёгкой строгостью. Используй ТОЛЬКО этот отчёт. Не повторяй цифры. Без Markdown.",

		"Fat Leopard одобряет. Напиши 2-3 предложения: первое — комментарий к упражнениям из сообщения, второе — короткое замечание. Тон: дружелюбно-строгий. Используй ТОЛЬКО этот отчёт. Без Markdown.",

		"Ответь на отчёт о тренировке (2-3 предложения): первое — конкретная оценка упражнений, второе — практический совет. Будь конкретным, не используй абстрактные фразы. Используй ТОЛЬКО этот отчёт. Без Markdown.",
	}

	if wasOnSickLeave {
		prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения) после тренировки: пользователь недавно вернулся после больничного. Первое — похвали за возвращение и отметь упражнения, второе — короткий практический совет. НЕ используй фразы 'помни', 'дух', 'воля'. Используй ТОЛЬКО упражнения из сообщения. Без Markdown.")
	}

	if streakDays >= 7 && streakDays < 14 {
		prompts = append(prompts, "Сделай одно цельное сообщение (2-3 предложения): пользователь уже неделю тренируется подряд — это важный рубеж! Первое — отметь это и упражнения, второе — короткое наблюдение. Избегай фраз 'помни', 'дух', 'воля'. Используй упражнения из сообщения. Без Markdown.")
	}

	if streakDays >= 21 {
		prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения): пользователь показывает отличную дисциплину с длинным стриком. Первое — признай это и отметь упражнения, второе — конкретное замечание о тренировке. НЕ используй абстрактные фразы про 'дух', 'волю', 'помни'. Используй упражнения из сообщения. Без Markdown.")
	}

	if hour >= 17 && hour < 22 {
		prompts = append(prompts,
			"Тренировка в конце дня — ты закрыл его правильно. Напиши 2-3 предложения: первое — отметь упражнения из сообщения, второе — короткое наблюдение (например про восстановление или сон). Тон: поддерживающий, без пафоса. Используй ТОЛЬКО этот отчёт. Без Markdown.",
			"Вечерняя тренировка — не все на это способны. Напиши 2-3 предложения: первое — комментарий к упражнениям, второе — лёгкий намёк, что завтра будет легче. Fat Leopard одобряет. Используй ТОЛЬКО этот отчёт. Без Markdown.",
			"Конец дня, а он всё ещё тренируется. Напиши 2-3 предложения: первое — конкретный комментарий к отчёту, второе — короткий совет (восстановление, сон, завтра). Используй ТОЛЬКО этот отчёт. Без Markdown.")
	}

	if hour >= 22 || hour < 6 {
		prompts = append(prompts,
			"Тренировка в конце дня — ты не сдался. Напиши 2-3 предложения: первое — отметь упражнения, второе — короткий комментарий. Можно лёгкий юмор: Fat Leopard уже спал, а ты ещё в деле. Используй ТОЛЬКО этот отчёт. Без Markdown.",
			"Поздняя тренировка — особое упорство. Напиши 2-3 предложения: первое — комментарий к упражнениям, второе — совет про сон и восстановление. Используй ТОЛЬКО этот отчёт. Без Markdown.")
	}

	if weekday == time.Saturday || weekday == time.Sunday {
		prompts = append(prompts, "Напиши одно связное сообщение (2-3 предложения): тренировка в выходной день — это настоящая преданность! Первое — похвали и отметь упражнения, второе — практическое замечание. НЕ используй фразы 'помни', 'дух', 'воля'. Используй упражнения из сообщения. Без Markdown.")
	}

	if achievementCount >= leopardmoney.MaxAchievements {
		prompts = append(prompts, "Сделай одно цельное сообщение (2-3 предложения): у пользователя максимум ачивок за стрики — это опытный участник. Первое — обратись как к ветерану, отметь упражнения, второе — конкретное наблюдение. Избегай абстрактных фраз про 'дух', 'волю', 'помни'. Используй упражнения из сообщения. Без Markdown.")
	}

	return prompts[now.Unix()%int64(len(prompts))] + formatHint
}

// getVariedTrainingPrompt генерирует разнообразные промпты для AI в зависимости от контекста (оставлено для совместимости)
func (b *Bot) getVariedTrainingPrompt(streakDays, _, totalCups int, wasOnSickLeave bool) string {
	now := utils.GetMoscowTime()
	hour := now.Hour()
	weekday := now.Weekday()

	// Базовые стили промптов
	prompts := []string{
		"Сделай очень короткую (1–2 предложения) дружелюбную, но строгую приписку после отчёта о тренировке. Не повторяй цифры из сообщения, не перечисляй правила. КРИТИЧЕСКИ ВАЖНО: используй ТОЛЬКО те упражнения и детали, которые указаны в сообщении пользователя. НЕ выдумывай детали, которых нет. Без Markdown.",

		"Напиши короткую (1–2 предложения) мотивирующую приписку от лица строгого, но справедливого тренера Fat Leopard после отчёта о тренировке. Будь конкретным про упражнения из сообщения, но не повторяй цифры. Без Markdown.",

		"Сделай короткий (1–2 предложения) комментарий после тренировки: поддерживающий, но с лёгкой строгостью. Упомяни конкретные упражнения из сообщения пользователя, но не цифры. Без Markdown.",

		"Напиши короткую (1–2 предложения) приписку после отчёта о тренировке в стиле мудрого наставника: дружелюбно, но требовательно. Используй ТОЛЬКО упражнения из сообщения пользователя. Без Markdown.",

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
		prompts = append(prompts, "Напиши короткую (1–2 предложения) приписку: пользователь показывает отличную дисциплину с длинным стриком. Признай это, но оставайся строгим. Используй упражнения из сообщения. Без Markdown.")
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
func (b *Bot) getVariedWisdomPrompt(streakDays, _, totalCups int) string {
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
		prompts = append(prompts, "Напиши одну короткую мысль (1 предложение) о том, как длинный стрик тренировок меняет человека. Используй упражнения из сообщения. Без Markdown.")
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
