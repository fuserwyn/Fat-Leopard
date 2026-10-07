package config

import (
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"leo-bot/internal/prompts"
)

type Config struct {
	APIToken       string
	OwnerID        int64
	AdminIDs       []int64
	AlphaTesterIDs []int64 // §10: тестеры альфы — события тегируются is_alpha
	DatabaseURL    string
	// TrackerDatabaseURL — Postgres только трекера (карточки, вложения, автономия, jobs).
	// Пусто — доска остаётся в DatabaseURL (тесты и локалка).
	TrackerDatabaseURL    string
	LogLevel              string
	OpenRouterAPIKey      string
	OpenRouterModel       string        // Модель OpenRouter (по умолчанию deepseek/deepseek-chat)
	OpenRouterVisionModel string        // Vision-модель для анализа фото (основная — текстовая); пусто = vision выключен
	// LeoTasksModel — модель OpenRouter, которой Лео придумывает задачи и спринты
	// для доски (LEO_TASKS_MODEL). Пусто — та же, что для остального.
	LeoTasksModel string
	OpenRouterTimeout     time.Duration // HTTP-таймаут к OpenRouter (весь запрос + чтение тела)
	ScanHistoryOnStart    bool          // Сканировать историю при старте (по умолчанию false)

	// Платный доступ к Fat Leopard MiniApp (Telegram Payments + ЮKassa).
	// Архитектура mini-app-only: TG-группы как сущности больше нет, MonetizedChatID
	// используется только как стабильный «pack id» в БД (paywall_access_requests / training_state).
	// Способы оплаты: PAYMENT_PROVIDER_TOKEN (карта в Telegram), ЮKassa (YOOKASSA_*),
	// PAYMENT_STARS_ENABLED + сумма звёзд (дополнительно к RUB), либо PAYMENT_CURRENCY=XTR (только звёзды, устаревший режим).
	PaywallEnabled bool
	// PaywallEntryFree — вход в мини-апп бесплатный (PAYWALL_ENTRY_FREE, по умолчанию true).
	// Платёжная инфраструктура при этом остаётся включённой (PAYWALL_ENABLED), потому что
	// оплата нужна для возврата после кика за 8 дней неактивности и для добровольных донатов
	// из профиля. Чтобы вернуть платный вход для новичков — PAYWALL_ENTRY_FREE=false.
	PaywallEntryFree       bool
	MonetizedChatID        int64  // Логический ID стаи в БД (исторически — chat id Telegram-группы; группа больше не используется).
	MonetizedChatInviteURL string // Deprecated: оставлено только для обратной совместимости со старыми env. Не используется в новом флоу.
	// Deprecated: оставлено для совместимости со старыми env, в новом mini-app flow не используется.
	PaywallInviteCreatesJoinRequest bool
	PaymentProviderToken            string // токен провайдера из BotFather (не коммитить в git)
	PaymentCurrency                 string // RUB и др. ISO 4217, либо XTR (Telegram Stars: 1 единица = 1 звезда)
	PaymentAmountMinorUnits         int    // копейки для RUB; для XTR — число звёзд (см. PAYMENT_STARS_AMOUNT / PAYMENT_AMOUNT_MINOR_UNITS)
	PaymentInvoiceTitle             string
	PaymentInvoiceDesc              string
	// Доп. счёт Telegram Stars при PAYMENT_CURRENCY≠XTR (например RUB + ЮKassa и параллельно звёзды).
	PaymentStarsEnabled bool
	PaymentStarsAmount  int

	// ЮKassa (оплата по ссылке); вебхук — отдельный сервис ms_payments (docker-compose payment-webhook).
	YookassaShopID          string
	YookassaSecretKey       string
	YookassaReturnURL       string // redirect после оплаты, https. Группы у нас больше нет — кладём ссылку на бота, например https://t.me/<bot_username>
	YookassaNotificationURL string // POST payment.succeeded на этот URL (лучше задать = публичный URL ms_payments …/api/v1/webhook/payment)
	// Сумма/валюта для CreatePayment (при PAYMENT_CURRENCY=XTR — в рублях из PAYMENT_AMOUNT_RUB / PAYMENT_YOOKASSA_*).
	YookassaAmountMinor int
	YookassaCurrency    string

	// Донаты из профиля мини-аппа: добровольная поддержка проекта, доступ не выдаётся.
	// Звёзды (DONATE_STARS_TIERS + всегда 1, 5, 10, 100, 1000) идут через createInvoiceLink + WebApp.openInvoice,
	// карта РФ (DONATE_CARD_TIERS_RUB) — через ту же ЮKassa, что и платный возврат.
	DonateStarsTiers   []int // номиналы в звёздах, по возрастанию
	DonateCardTiersRub []int // номиналы в рублях, по возрастанию

	// Prompts — встроенные тексты из ms_leo/internal/prompts/data/ (//go:embed), без переопределения через env.
	Prompts prompts.Bundle

	// MiniappPublicBaseURL — публичный origin HTTP API мини-приложения (MINIAPP_PUBLIC_BASE_URL), без path.
	// Используется для канонизации URL фото тренировки в ленте; должен совпадать с тем, что в VITE_MINIAPP_API_URL.
	MiniappPublicBaseURL string

	// MiniappWebAppURL — https-URL самой мини-аппы (тот же, что прописан в @BotFather как web_app).
	// Если задан — оплатившему на /start и после оплаты вешаем inline-кнопку «Открыть» с web_app,
	// чтобы мини-аппу можно было открыть одним тапом, не разворачивая меню. Пусто → кнопку не шлём
	// (остаётся только синяя menu-кнопка). Домен должен быть привязан к боту в @BotFather.
	MiniappWebAppURL string

	// R2 (Cloudflare) — объектное хранилище для фото тренировок. Если заданы все поля,
	// фото грузятся в бакет, иначе — на локальный диск (как раньше). S3-совместимый API.
	R2AccountID       string
	R2AccessKeyID     string
	R2SecretAccessKey string
	R2Bucket          string
	R2PublicBaseURL   string // публичный адрес бакета (pub-xxx.r2.dev или свой домен), без / на конце

	// RAG (Qdrant): изолированные сессии personal_leo и pack_group.
	RAGEnabled        bool
	QdrantURL         string
	QdrantAPIKey      string
	QdrantCollection  string
	RAGEmbeddingModel string

	// Свой трекер (ms_tracker): TRACKER_URL + TRACKER_SECRET.
	// Старые имена MYVIBELAB_URL / BOARD_SSO_SECRET ещё читаются как запас.
	BoardSecret string
	BoardURL    string
	BoardRepo   string
	// Ветка, в которой работают задачи доски. Пусто — ветка по умолчанию
	// (прод). В экспериментальном окружении здесь leo-lab, чтобы автономные
	// задачи Лео не уезжали в прод.
	BoardBranch string
	// Модель, которой трекер гонит задачи этой доски. Пусто — как настроено у
	// владельца доски. На тестовом стенде это cursor-auto: эксперименты Лео
	// незачем гонять дорогой моделью.
	BoardModel string
	// Модель ревью и теста. Пусто — cursor-composer: он читает код в репо.
	BoardReviewModel string

	// Раздел «Ресурсы» в админке: расход Railway за месяц и пересчёт оплат в
	// доллары. Ставки — из тарифов Railway, курс — руками, чтобы не тянуть
	// внешний сервис ради одной цифры.
	RailwayToken string
	// Тестовый стенд, которым админ управляет из мини-аппа прода: окружение,
	// сервисы приложений (без баз — их не гасим) и адрес мини-аппа стенда.
	LabEnvironmentID      string
	LabServices           string
	LabMiniappURL         string
	RailwayProjectID      string
	RailwayEnvironmentID  string
	UsagePriceRAMGBMonth  float64
	UsagePriceCPUMonth    float64
	UsagePriceDiskGBMonth float64
	UsagePriceEgressGB    float64
	UsageMinutesPerMonth  float64
	UsdRubRate            float64
	UsdPerStar            float64
}

func Load() (*Config, error) {
	// Загружаем .env файл если он существует
	godotenv.Load()

	ownerID, _ := strconv.ParseInt(getEnv("OWNER_ID", "0"), 10, 64)
	adminIDs := parseAdminIDs(getEnv("ADMIN_IDS", ""))
	alphaTesterIDs := parseAdminIDs(getEnv("ALPHA_TESTER_IDS", ""))

	// Парсим булевое значение для ScanHistoryOnStart
	scanHistoryOnStart := false
	if scanHistoryStr := getEnv("SCAN_HISTORY_ON_START", "false"); scanHistoryStr == "true" || scanHistoryStr == "1" || scanHistoryStr == "TRUE" {
		scanHistoryOnStart = true
	}

	monetizedChatID, _ := strconv.ParseInt(getEnv("MONETIZED_CHAT_ID", "0"), 10, 64)
	currency := strings.ToUpper(strings.TrimSpace(getEnv("PAYMENT_CURRENCY", "RUB")))
	if currency == "" {
		currency = "RUB"
	}
	amountMinor := paymentAmountMinorFromEnv(currency)
	paywallEnabled := getEnv("PAYWALL_ENABLED", "false") == "true" || getEnv("PAYWALL_ENABLED", "false") == "1"
	entryFree := true
	switch strings.ToLower(strings.TrimSpace(getEnv("PAYWALL_ENTRY_FREE", "true"))) {
	case "false", "0", "no":
		entryFree = false
	}

	apiToken := getEnv("FAT_LEOPARD_API_TOKEN", "")
	if apiToken == "" {
		apiToken = getEnv("API_TOKEN", "")
	}

	// YOOKASSA_RETURN_URL — куда Yookassa возвращает пользователя в браузере после оплаты.
	// Группы Telegram больше нет, поэтому fallback к MONETIZED_CHAT_INVITE_URL убран:
	// если переменная не задана, используем нейтральный https://t.me, чтобы случайно не открыть
	// инвайт в устаревшую группу. В .env удобно поставить https://t.me/<your_bot_username>.
	ykReturn := strings.TrimSpace(getEnv("YOOKASSA_RETURN_URL", ""))
	if ykReturn == "" {
		ykReturn = "https://t.me"
	}

	orTimeout := 3 * time.Minute
	if s := strings.TrimSpace(getEnv("OPENROUTER_HTTP_TIMEOUT_SECONDS", "")); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			orTimeout = time.Duration(n) * time.Second
		}
	}

	inviteJoinReq := true
	switch strings.ToLower(strings.TrimSpace(getEnv("MONETIZED_INVITE_CREATES_JOIN_REQUEST", "true"))) {
	case "false", "0", "no":
		inviteJoinReq = false
	}

	starsAddonEnabled := parseEnvBool(getEnv("PAYMENT_STARS_ENABLED", "false"))
	starsAddonAmount := paymentStarsAddonAmountFromEnv(starsAddonEnabled)
	ykMinor, ykCur := yookassaAmountAndCurrencyFromEnv(currency, amountMinor)

	return &Config{
		APIToken:              apiToken,
		OwnerID:               ownerID,
		BoardSecret:           firstNonEmpty(getEnv("TRACKER_SECRET", ""), getEnv("BOARD_SSO_SECRET", "")),
		BoardURL:              firstNonEmpty(getEnv("TRACKER_URL", ""), getEnv("MYVIBELAB_URL", "")),
		BoardRepo:             getEnv("BOARD_REPO", "fuserwyn/Fat-Leopard"),
		BoardBranch:           getEnv("BOARD_BRANCH", ""),
		BoardModel:            getEnv("BOARD_MODEL", ""),
		BoardReviewModel:      getEnv("BOARD_REVIEW_MODEL", "cursor-composer"),
		RailwayToken:          getEnv("RAILWAY_API_TOKEN", ""),
		LabEnvironmentID:      getEnv("LAB_ENVIRONMENT_ID", ""),
		LabServices:           getEnv("LAB_SERVICE_IDS", ""),
		LabMiniappURL:         getEnv("LAB_MINIAPP_URL", ""),
		RailwayProjectID:      getEnv("RAILWAY_PROJECT_ID", ""),
		RailwayEnvironmentID:  getEnv("RAILWAY_ENVIRONMENT_ID", ""),
		UsagePriceRAMGBMonth:  parseFloatEnv("USAGE_PRICE_RAM_GB_MONTH", 10),
		UsagePriceCPUMonth:    parseFloatEnv("USAGE_PRICE_CPU_MONTH", 20),
		UsagePriceDiskGBMonth: parseFloatEnv("USAGE_PRICE_DISK_GB_MONTH", 0.15),
		UsagePriceEgressGB:    parseFloatEnv("USAGE_PRICE_EGRESS_GB", 0.05),
		UsageMinutesPerMonth:  parseFloatEnv("USAGE_MINUTES_PER_MONTH", 43800),
		UsdRubRate:            parseFloatEnv("USD_RUB_RATE", 90),
		UsdPerStar:            parseFloatEnv("USD_PER_STAR", 0.013),
		AdminIDs:              adminIDs,
		AlphaTesterIDs:        alphaTesterIDs,
		DatabaseURL:           getEnv("DATABASE_URL", "postgresql://postgres:password@localhost:5432/leo_bot_db?sslmode=disable"),
		TrackerDatabaseURL:    strings.TrimSpace(getEnv("TRACKER_DATABASE_URL", "")),
		LogLevel:              getEnv("LOG_LEVEL", "info"),
		OpenRouterAPIKey:      getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:       getEnv("OPENROUTER_MODEL", "deepseek/deepseek-chat"),
		OpenRouterVisionModel: getEnv("OPENROUTER_VISION_MODEL", "openai/gpt-4o-mini"),
		LeoTasksModel:         getEnv("LEO_TASKS_MODEL", "anthropic/claude-opus-5.5"),
		OpenRouterTimeout:     orTimeout,
		ScanHistoryOnStart:    scanHistoryOnStart,
		Prompts:               prompts.DefaultBundle(),

		PaywallEnabled:                  paywallEnabled,
		PaywallEntryFree:                entryFree,
		MonetizedChatID:                 monetizedChatID,
		MonetizedChatInviteURL:          strings.TrimSpace(getEnv("MONETIZED_CHAT_INVITE_URL", "")),
		PaywallInviteCreatesJoinRequest: inviteJoinReq,
		PaymentProviderToken:            getEnv("PAYMENT_PROVIDER_TOKEN", ""),
		PaymentCurrency:                 currency,
		PaymentAmountMinorUnits:         amountMinor,
		PaymentInvoiceTitle:             getEnv("PAYMENT_INVOICE_TITLE", "Доступ к Fat Leopard MiniApp"),
		PaymentInvoiceDesc:              getEnv("PAYMENT_INVOICE_DESCRIPTION", "Доступ к мини-приложению Fat Leopard после оплаты; открытие — автоматическое."),
		PaymentStarsEnabled:             starsAddonEnabled,
		PaymentStarsAmount:              starsAddonAmount,

		YookassaShopID:          strings.TrimSpace(getEnv("YOOKASSA_SHOP_ID", "")),
		YookassaSecretKey:       strings.TrimSpace(getEnv("YOOKASSA_SECRET_KEY", "")),
		YookassaReturnURL:       ykReturn,
		YookassaNotificationURL: strings.TrimSpace(getEnv("YOOKASSA_NOTIFICATION_URL", "")),
		YookassaAmountMinor:     ykMinor,
		YookassaCurrency:        ykCur,

		// 1, 5, 10, 100, 1000 всегда в списке. 150 скрыт (#31): даже если
		// DONATE_STARS_TIERS на Railway всё ещё «50,150,500», кнопка не вернётся.
		DonateStarsTiers: excludeAmountTiers(
			parseAmountTiers("1,5,7,10,100,1000,"+getEnv("DONATE_STARS_TIERS", "50,150,500")),
			parseAmountTiers("150,"+getEnv("DONATE_STARS_HIDDEN", "")),
		),
		DonateCardTiersRub: parseAmountTiers("10,50,100,1000," + getEnv("DONATE_CARD_TIERS_RUB", "100,300,1000")),

		MiniappPublicBaseURL: strings.TrimSpace(getEnv("MINIAPP_PUBLIC_BASE_URL", "")),
		MiniappWebAppURL:     strings.TrimSpace(getEnv("MINIAPP_WEB_APP_URL", "")),

		R2AccountID:       strings.TrimSpace(getEnv("R2_ACCOUNT_ID", "")),
		R2AccessKeyID:     strings.TrimSpace(getEnv("R2_ACCESS_KEY_ID", "")),
		R2SecretAccessKey: strings.TrimSpace(getEnv("R2_SECRET_ACCESS_KEY", "")),
		R2Bucket:          strings.TrimSpace(getEnv("R2_BUCKET", "")),
		R2PublicBaseURL:   strings.TrimRight(strings.TrimSpace(getEnv("R2_PUBLIC_BASE_URL", "")), "/"),

		RAGEnabled:        parseEnvBool(getEnv("RAG_ENABLED", "false")),
		QdrantURL:         strings.TrimSpace(getEnv("QDRANT_URL", "")),
		QdrantAPIKey:      strings.TrimSpace(getEnv("QDRANT_API_KEY", "")),
		QdrantCollection:  strings.TrimSpace(getEnv("QDRANT_COLLECTION", "leo_chat_rag")),
		RAGEmbeddingModel: strings.TrimSpace(getEnv("RAG_EMBEDDING_MODEL", "openai/text-embedding-3-small")),
	}, nil
}

// LabServiceIDs — сервисы тестового стенда, которые гасим и поднимаем.
func (c *Config) LabServiceIDs() []string {
	if c == nil {
		return nil
	}
	var out []string
	for _, part := range strings.Split(c.LabServices, ",") {
		if id := strings.TrimSpace(part); id != "" {
			out = append(out, id)
		}
	}
	return out
}

// IsAdminTelegramUser — владелец или любой id из ADMIN_IDS.
func (c *Config) IsAdminTelegramUser(userID int64) bool {
	if c == nil || userID == 0 {
		return false
	}
	if c.OwnerID != 0 && userID == c.OwnerID {
		return true
	}
	for _, id := range c.AdminIDs {
		if id == userID {
			return true
		}
	}
	return false
}

// AdminTelegramUserIDs — уникальные Telegram ID всех админов (OWNER_ID + ADMIN_IDS).
func (c *Config) AdminTelegramUserIDs() []int64 {
	if c == nil {
		return nil
	}
	seen := make(map[int64]struct{})
	var out []int64
	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(c.OwnerID)
	for _, id := range c.AdminIDs {
		add(id)
	}
	return out
}

// PaywallUsesStars — счёт в Telegram Stars (XTR): режим PAYMENT_CURRENCY=XTR или доп. PAYMENT_STARS_ENABLED.
func (c *Config) PaywallUsesStars() bool {
	if c.PaymentCurrency == "XTR" && c.PaymentAmountMinorUnits > 0 {
		return true
	}
	return c.PaymentStarsEnabled && c.PaymentStarsAmount > 0
}

// PaywallStarsInvoiceAmount — число звёзд в sendInvoice для XTR.
func (c *Config) PaywallStarsInvoiceAmount() int {
	if c.PaymentCurrency == "XTR" {
		return c.PaymentAmountMinorUnits
	}
	if c.PaymentStarsEnabled {
		return c.PaymentStarsAmount
	}
	return 0
}

// PaywallUsesTelegramProviderInvoice — sendInvoice с PAYMENT_PROVIDER_TOKEN (не XTR-only режим).
func (c *Config) PaywallUsesTelegramProviderInvoice() bool {
	if c.PaymentCurrency == "XTR" {
		return false
	}
	return strings.TrimSpace(c.PaymentProviderToken) != ""
}

// PaywallYookassaReady — ЮKassa: заданы ключи и положительная сумма (для XTR — в рублях, см. PAYMENT_AMOUNT_RUB / PAYMENT_YOOKASSA_*).
func (c *Config) PaywallYookassaReady() bool {
	if c.YookassaShopID == "" || c.YookassaSecretKey == "" {
		return false
	}
	return c.YookassaAmountMinor > 0 && c.YookassaCurrency != ""
}

// PaywallPaymentReady — хотя бы один способ: звёзды, провайдер Telegram, ЮKassa.
func (c *Config) PaywallPaymentReady() bool {
	if c.PaywallUsesStars() {
		return true
	}
	if c.PaywallUsesTelegramProviderInvoice() {
		return true
	}
	return c.PaywallYookassaReady()
}

// PaywallUsesTelegramInvoice — любой sendInvoice (звёзды и/или провайдер).
func (c *Config) PaywallUsesTelegramInvoice() bool {
	return c.PaywallUsesStars() || c.PaywallUsesTelegramProviderInvoice()
}

// DonateStarsReady — донат звёздами доступен. В отличие от платного входа звёздам не нужен
// ни provider token, ни PAYMENT_STARS_ENABLED: XTR-счёт бот выставляет сам, достаточно номиналов.
func (c *Config) DonateStarsReady() bool {
	return c != nil && len(c.DonateStarsTiers) > 0
}

// DonateCardReady — донат картой РФ: те же ключи ЮKassa, что у платного возврата, плюс номиналы.
func (c *Config) DonateCardReady() bool {
	if c == nil || len(c.DonateCardTiersRub) == 0 {
		return false
	}
	return c.YookassaShopID != "" && c.YookassaSecretKey != ""
}

// DonateStarsTierAllowed — сумма звёзд из списка номиналов (защита от подмены суммы в запросе мини-аппа).
func (c *Config) DonateStarsTierAllowed(stars int) bool {
	if c == nil {
		return false
	}
	for _, t := range c.DonateStarsTiers {
		if t == stars {
			return true
		}
	}
	return false
}

// DonateCardTierAllowed — сумма в рублях из списка номиналов.
func (c *Config) DonateCardTierAllowed(rub int) bool {
	if c == nil {
		return false
	}
	for _, t := range c.DonateCardTiersRub {
		if t == rub {
			return true
		}
	}
	return false
}

// parseAmountTiers — «50,150,500» → [50 150 500]: только положительные, без дублей, по возрастанию.
func parseAmountTiers(raw string) []int {
	seen := make(map[int]struct{})
	var out []int
	for _, part := range strings.Split(raw, ",") {
		n, err := strconv.Atoi(strings.TrimSpace(part))
		if err != nil || n <= 0 {
			continue
		}
		if _, dup := seen[n]; dup {
			continue
		}
		seen[n] = struct{}{}
		out = append(out, n)
	}
	sort.Ints(out)
	return out
}

// excludeAmountTiers — убрать номиналы из списка (доска: «удали 150 звёзд»).
func excludeAmountTiers(all, hidden []int) []int {
	if len(all) == 0 || len(hidden) == 0 {
		return all
	}
	drop := make(map[int]struct{}, len(hidden))
	for _, n := range hidden {
		if n > 0 {
			drop[n] = struct{}{}
		}
	}
	out := make([]int, 0, len(all))
	for _, n := range all {
		if _, skip := drop[n]; skip {
			continue
		}
		out = append(out, n)
	}
	return out
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func parseEnvBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

// parseFloatEnv — число из окружения; мусор и пустое значение → по умолчанию.
func parseFloatEnv(key string, defaultValue float64) float64 {
	raw := strings.TrimSpace(getEnv(key, ""))
	if raw == "" {
		return defaultValue
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil || v < 0 {
		return defaultValue
	}
	return v
}

func parseAdminIDs(raw string) []int64 {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		switch r {
		case ',', ';', '\n', '\r', '\t', ' ':
			return true
		default:
			return false
		}
	})
	out := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// paymentStarsAddonAmountFromEnv: PAYMENT_STARS_AMOUNT (только если флаг включён).
func paymentStarsAddonAmountFromEnv(starsEnabled bool) int {
	if !starsEnabled {
		return 0
	}
	raw := strings.TrimSpace(os.Getenv("PAYMENT_STARS_AMOUNT"))
	if raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			return v
		}
	}
	return 0
}

// paymentAmountMinorFromEnv: для RUB — PAYMENT_AMOUNT_RUB перекрывает копейки;
// для XTR — PAYMENT_STARS_AMOUNT, затем PAYMENT_AMOUNT_MINOR_UNITS.
func paymentAmountMinorFromEnv(currency string) int {
	cur := strings.TrimSpace(strings.ToUpper(currency))
	if cur == "" {
		cur = "RUB"
	}
	if cur == "XTR" {
		starsRaw := strings.TrimSpace(os.Getenv("PAYMENT_STARS_AMOUNT"))
		if starsRaw != "" {
			if v, err := strconv.Atoi(starsRaw); err == nil && v > 0 {
				return v
			}
		}
		n, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("PAYMENT_AMOUNT_MINOR_UNITS")))
		if n > 0 {
			return n
		}
		return 100
	}
	rubRaw := strings.TrimSpace(os.Getenv("PAYMENT_AMOUNT_RUB"))
	if rubRaw != "" && cur == "RUB" {
		s := strings.ReplaceAll(strings.ReplaceAll(rubRaw, ",", "."), " ", "")
		if v, err := strconv.ParseFloat(s, 64); err == nil && v >= 0 {
			minor := int(math.Round(v * 100))
			if minor > 0 {
				return minor
			}
		}
	}
	amountMinor, _ := strconv.Atoi(getEnv("PAYMENT_AMOUNT_MINOR_UNITS", "9900"))
	if amountMinor <= 0 {
		return 9900
	}
	return amountMinor
}

// yookassaAmountAndCurrencyFromEnv: для RUB/USD и т.д. — как основной платёж; для XTR — только рублевая сумма (звёзды в другом поле).
func yookassaAmountAndCurrencyFromEnv(paymentCurrency string, paymentAmountMinor int) (minor int, currency string) {
	cur := strings.TrimSpace(strings.ToUpper(paymentCurrency))
	if cur != "XTR" {
		if paymentAmountMinor <= 0 {
			return 0, ""
		}
		if cur == "" {
			cur = "RUB"
		}
		return paymentAmountMinor, cur
	}
	// XTR + ЮKassa в RUB
	if raw := strings.TrimSpace(os.Getenv("PAYMENT_YOOKASSA_AMOUNT_RUB")); raw != "" {
		s := strings.ReplaceAll(strings.ReplaceAll(raw, ",", "."), " ", "")
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			return int(math.Round(v * 100)), "RUB"
		}
	}
	if raw := strings.TrimSpace(os.Getenv("PAYMENT_AMOUNT_RUB")); raw != "" {
		s := strings.ReplaceAll(strings.ReplaceAll(raw, ",", "."), " ", "")
		if v, err := strconv.ParseFloat(s, 64); err == nil && v > 0 {
			return int(math.Round(v * 100)), "RUB"
		}
	}
	if n, _ := strconv.Atoi(strings.TrimSpace(os.Getenv("PAYMENT_YOOKASSA_AMOUNT_MINOR_UNITS"))); n > 0 {
		return n, "RUB"
	}
	return 0, ""
}
