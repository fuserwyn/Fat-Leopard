package config

import (
	"os"
	"strconv"

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
	// в @BotFather подключён провайдер и выдан PAYMENT_PROVIDER_TOKEN; сумма и валюта сверяются в pre_checkout и successful_payment.
	PaywallEnabled          bool
	MonetizedChatID         int64  // ID группы (например -100...)
	PaymentProviderToken    string // токен провайдера из BotFather (не коммитить в git)
	PaymentCurrency         string // ISO 4217, напр. RUB
	PaymentAmountMinorUnits int    // минимальные единицы валюты (копейки для RUB)
	PaymentInvoiceTitle     string
	PaymentInvoiceDesc      string
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
	amountMinor, _ := strconv.Atoi(getEnv("PAYMENT_AMOUNT_MINOR_UNITS", "10000")) // по умолчанию 100.00 RUB
	if amountMinor <= 0 {
		amountMinor = 10000
	}
	currency := getEnv("PAYMENT_CURRENCY", "RUB")
	if currency == "" {
		currency = "RUB"
	}
	paywallEnabled := getEnv("PAYWALL_ENABLED", "false") == "true" || getEnv("PAYWALL_ENABLED", "false") == "1"

	apiToken := getEnv("FAT_LEOPARD_API_TOKEN", "")
	if apiToken == "" {
		apiToken = getEnv("API_TOKEN", "")
	}

	return &Config{
		APIToken:           apiToken,
		OwnerID:            ownerID,
		DatabaseURL:        getEnv("DATABASE_URL", "postgresql://postgres:password@localhost:5432/leo_bot_db?sslmode=disable"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		OpenRouterAPIKey:   getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:    getEnv("OPENROUTER_MODEL", "deepseek/deepseek-chat"),
		ScanHistoryOnStart: scanHistoryOnStart,

		PaywallEnabled:          paywallEnabled,
		MonetizedChatID:         monetizedChatID,
		PaymentProviderToken:    getEnv("PAYMENT_PROVIDER_TOKEN", ""),
		PaymentCurrency:         currency,
		PaymentAmountMinorUnits: amountMinor,
		PaymentInvoiceTitle:     getEnv("PAYMENT_INVOICE_TITLE", "Доступ в группу"),
		PaymentInvoiceDesc:      getEnv("PAYMENT_INVOICE_DESCRIPTION", "Разовый доступ после оплаты заявка будет одобрена автоматически."),
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
