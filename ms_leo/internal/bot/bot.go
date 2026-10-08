package bot

import (
	"context"
	"fmt"
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
	"leo-bot/internal/rag"
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
	// Челленджи: проваливает те, где пропущен день, даже если участник не заходил.
	go b.startChallengeSweepScheduler(ctx)
	// Оплата прошла, а вебхук ms_payments не дошёл — дожимаем доступ сами (см. paywall_reconciler.go).
	go b.startPaywallYookassaReconciler(ctx)
	// Итоги недели стаи в понедельник и цель +5 после закрытой недели (см. pack_week_summary.go).
	go b.startPackWeekSummaryScheduler(ctx)

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

	// Карточка контакта в личке — пополняет список «сообщить, когда вступит в стаю».
	if msg.Contact != nil && b.handleSharedContact(msg) {
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
