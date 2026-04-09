package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"

	"leo-bot/internal/bot"
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"
)

func main() {
	// Загружаем конфигурацию
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Инициализируем логгер
	logger := logger.New(cfg.LogLevel)

	// Подключаемся к базе данных
	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		logger.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	// Создаем бота
	bot, err := bot.New(cfg, db, logger)
	if err != nil {
		logger.Fatalf("Failed to create bot: %v", err)
	}

	// Создаем контекст для graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Запускаем бота в горутине
	go func() {
		if err := bot.Start(ctx); err != nil {
			logger.Errorf("Bot error: %v", err)
		}
	}()

	// Ждем сигнала для graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down...")
	cancel()
} 