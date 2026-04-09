package main

import (
	"flag"
	"fmt"
	"log"

	"leo-bot/internal/config"
	"leo-bot/internal/database"
)

func main() {
	// Парсим аргументы командной строки
	var configPath string
	flag.StringVar(&configPath, "config", "config.yaml", "Path to config file")
	flag.Parse()

	// Загружаем конфигурацию
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	// Подключаемся к базе данных
	db, err := database.New(cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}
	defer db.Close()

	fmt.Println("🚀 Starting database migrations...")

	// Запускаем миграции
	if err := db.RunMigrations(); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	fmt.Println("✅ All migrations completed successfully!")
}
