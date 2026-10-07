package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"leo-bot/internal/bot"
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"
	"leo-bot/internal/metrics"
	"leo-bot/internal/miniappapi"
	"github.com/prometheus/client_golang/prometheus/promhttp"
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

	// HTTP для Mini App: POST init_data + text → тот же путь, что getUpdates (личка).
	// В Railway укажи публичный URL этого сервиса в VITE бота/мини-апpa (без path).
	if p := os.Getenv("PORT"); p != "" {
		// Слушаем на [::] (IPv6, принимает и IPv4): Railway private network (*.railway.internal)
		// работает только по IPv6, иначе Prometheus не достучится до /metrics.
		addr := "[::]:" + p
		mediaDir := "data/miniapp_media"
		if abs, err := filepath.Abs(mediaDir); err == nil {
			mediaDir = abs
		}
		_ = os.MkdirAll(mediaDir, 0750)
		// Берём base из конфигурации; если пусто — miniappapi/workout_upload сам
		// вычислит публичный origin из X-Forwarded-* / Host (вместо localhost).
		publicBase := strings.TrimSpace(cfg.MiniappPublicBaseURL)
		// R2 (Cloudflare): если заданы все R2_* — фото грузятся в бакет, иначе на локальный диск.
		r2, err := miniappapi.NewR2Storage(cfg.R2AccountID, cfg.R2AccessKeyID, cfg.R2SecretAccessKey, cfg.R2Bucket, cfg.R2PublicBaseURL)
		if err != nil {
			logger.Errorf("R2 init failed, fallback to local disk: %v", err)
			r2 = nil
		}
		if r2 != nil {
			logger.Infof("Workout photos: Cloudflare R2 bucket=%s", cfg.R2Bucket)
		} else {
			logger.Infof("Workout photos: local disk %s", mediaDir)
		}
		h := miniappapi.New(bot, cfg.APIToken, logger, publicBase, mediaDir, r2)
		// Wrap handler с middleware для метрик
		wrapped := metrics.HTTPMiddleware(miniappapi.LimitBody(h))
		// Добавляем /metrics эндпоинт
		mux := http.NewServeMux()
		mux.Handle("/metrics", promhttp.Handler())
		mux.Handle("/", wrapped)
		// Долгий ответ ИИ: WriteTimeout 0 = без лимита на запись тела ответа (иначе обрыв посреди JSON).
		srv := &http.Server{
			Addr:              addr,
			Handler:           mux,
			ReadHeaderTimeout: 20 * time.Second,
			// На весь запрос с телом: фото с медленного мобильного успевает,
			// а соединение, которое тянет байт в минуту, не висит вечно.
			ReadTimeout:       2 * time.Minute,
			WriteTimeout:      0,
			IdleTimeout:       120 * time.Second,
		}
		go func() {
			logger.Infof("Mini App API listening on %s", addr)
			if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				logger.Errorf("HTTP server: %v", err)
			}
		}()
	}

	// Ждем сигнала для graceful shutdown
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	logger.Info("Shutting down...")
	cancel()
} 