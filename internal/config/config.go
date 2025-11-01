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
	OpenRouterModel    string // Модель OpenRouter (по умолчанию deepseek/deepseek-r1-0528)
	ScanHistoryOnStart bool   // Сканировать историю при старте (по умолчанию false)
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

	return &Config{
		APIToken:           getEnv("API_TOKEN", ""),
		OwnerID:            ownerID,
		DatabaseURL:        getEnv("DATABASE_URL", "postgresql://postgres:password@localhost:5432/leo_bot_db?sslmode=disable"),
		LogLevel:           getEnv("LOG_LEVEL", "info"),
		OpenRouterAPIKey:   getEnv("OPENROUTER_API_KEY", ""),
		OpenRouterModel:    getEnv("OPENROUTER_MODEL", "deepseek/deepseek-r1-0528"),
		ScanHistoryOnStart: scanHistoryOnStart,
	}, nil
}

func getEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
