package config

import (
	"math"
	"os"
	"strconv"
	"strings"

	"github.com/joho/godotenv"
)

type Config struct {
	APIToken           string
	OwnerID            int64
	DatabaseURL        string
	LogLevel           string
	OpenRouterAPIKey   string
	OpenRouterModel    string // Модель OpenRouter (по умолчанию deepseek/deepseek-chat)
	ScanHistoryOnStart bool   // Сканировать историю при старте (по умолчанию false)

	// Платный доступ в группу (Telegram Payments + заявки на вступление).
	// Принципы: бот — админ группы с правом одобрять заявки; группа с включёнными заявками;
	// PAYMENT_PROVIDER_TOKEN (карта в Telegram), опционально PAYMENT_STARS_ENABLED + сумма звёзд (дополнительно к RUB/ЮKassa),
	// либо только PAYMENT_CURRENCY=XTR (устаревший режим «только звёзды»).
	PaywallEnabled          bool
	MonetizedChatID         int64  // ID группы (например -100...)
	MonetizedChatInviteURL  string // Запасная ссылка; лучше оставить пустой — бот создаст ссылку через API (нужны права админа в группе)
	// По API createChatInviteLink: true = ссылка с заявкой на вступление (как «подать заявку»), false = обычное вступление.
	PaywallInviteCreatesJoinRequest bool
	PaymentProviderToken    string // токен провайдера из BotFather (не коммитить в git)
	PaymentCurrency         string // RUB и др. ISO 4217, либо XTR (Telegram Stars: 1 единица = 1 звезда)
	PaymentAmountMinorUnits int    // копейки для RUB; для XTR — число звёзд (см. PAYMENT_AMOUNT_STARS / PAYMENT_AMOUNT_MINOR_UNITS)
	PaymentInvoiceTitle     string
	PaymentInvoiceDesc      string
	// Доп. счёт Telegram Stars при PAYMENT_CURRENCY≠XTR (например RUB + ЮKassa и параллельно звёзды).
	PaymentStarsEnabled bool
	PaymentStarsAmount  int

	// ЮKassa (оплата по ссылке); вебхук — отдельный сервис ms_payments (docker-compose payment-webhook).
	YookassaShopID          string
	YookassaSecretKey       string
	YookassaReturnURL       string // redirect после оплаты, https (например приглашение в группу или t.me)
	YookassaNotificationURL string // POST payment.succeeded на этот URL (лучше задать = публичный URL ms_payments …/api/v1/webhook/payment)
	// Сумма/валюта для CreatePayment (при PAYMENT_CURRENCY=XTR — в рублях из PAYMENT_AMOUNT_RUB / PAYMENT_YOOKASSA_*).
	YookassaAmountMinor int
	YookassaCurrency    string
}

func Load() (*Config, error) {
	// Загружаем .env файл если он существует
	godotenv.Load()

	ownerID, _ := strconv.ParseInt(getEnv("OWNER_ID", "0"), 10, 64)

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

	apiToken := getEnv("FAT_LEOPARD_API_TOKEN", "")
	if apiToken == "" {
		apiToken = getEnv("API_TOKEN", "")
	}

	ykReturn := strings.TrimSpace(getEnv("YOOKASSA_RETURN_URL", ""))
	if ykReturn == "" {
		ykReturn = strings.TrimSpace(getEnv("MONETIZED_CHAT_INVITE_URL", ""))
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
		APIToken:           apiToken,
		OwnerID:            ownerID,
		DatabaseURL:        getEnv("DATABASE_URL", "postgresql://postgres:password@localhost:5432/leo_bot_db?sslmode=disable"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		OpenRouterAPIKey:   getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:    getEnv("OPENROUTER_MODEL", "deepseek/deepseek-chat"),
		ScanHistoryOnStart: scanHistoryOnStart,

		PaywallEnabled:                  paywallEnabled,
		MonetizedChatID:                 monetizedChatID,
		MonetizedChatInviteURL:          strings.TrimSpace(getEnv("MONETIZED_CHAT_INVITE_URL", "")),
		PaywallInviteCreatesJoinRequest: inviteJoinReq,
		PaymentProviderToken:    getEnv("PAYMENT_PROVIDER_TOKEN", ""),
		PaymentCurrency:         currency,
		PaymentAmountMinorUnits: amountMinor,
		PaymentInvoiceTitle:     getEnv("PAYMENT_INVOICE_TITLE", "Доступ в группу"),
		PaymentInvoiceDesc:      getEnv("PAYMENT_INVOICE_DESCRIPTION", "Разовый доступ после оплаты заявка будет одобрена автоматически."),
		PaymentStarsEnabled:     starsAddonEnabled,
		PaymentStarsAmount:      starsAddonAmount,

		YookassaShopID:          strings.TrimSpace(getEnv("YOOKASSA_SHOP_ID", "")),
		YookassaSecretKey:       strings.TrimSpace(getEnv("YOOKASSA_SECRET_KEY", "")),
		YookassaReturnURL:       ykReturn,
		YookassaNotificationURL: strings.TrimSpace(getEnv("YOOKASSA_NOTIFICATION_URL", "")),
		YookassaAmountMinor:     ykMinor,
		YookassaCurrency:        ykCur,
	}, nil
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

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

func parseEnvBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "true", "1", "yes":
		return true
	default:
		return false
	}
}

// paymentStarsAddonAmountFromEnv: PAYMENT_STARS_AMOUNT или PAYMENT_AMOUNT_STARS (только если флаг включён).
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
	raw2 := strings.TrimSpace(os.Getenv("PAYMENT_AMOUNT_STARS"))
	if raw2 != "" {
		if v, err := strconv.Atoi(raw2); err == nil && v > 0 {
			return v
		}
	}
	return 0
}

// paymentAmountMinorFromEnv: для RUB — PAYMENT_AMOUNT_RUB перекрывает копейки;
// для XTR — PAYMENT_STARS_AMOUNT, затем PAYMENT_AMOUNT_STARS, затем PAYMENT_AMOUNT_MINOR_UNITS.
func paymentAmountMinorFromEnv(currency string) int {
	cur := strings.TrimSpace(strings.ToUpper(currency))
	if cur == "" {
		cur = "RUB"
	}
	if cur == "XTR" {
		starsRawPrimary := strings.TrimSpace(os.Getenv("PAYMENT_STARS_AMOUNT"))
		if starsRawPrimary != "" {
			if v, err := strconv.Atoi(starsRawPrimary); err == nil && v > 0 {
				return v
			}
		}
		starsRaw := strings.TrimSpace(os.Getenv("PAYMENT_AMOUNT_STARS"))
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
	amountMinor, _ := strconv.Atoi(getEnv("PAYMENT_AMOUNT_MINOR_UNITS", "10000"))
	if amountMinor <= 0 {
		return 10000
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
