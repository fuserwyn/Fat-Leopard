package bot

import (
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/domain"
	"leo-bot/internal/logger"
	"leo-bot/internal/moderation"
	"leo-bot/internal/rag"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// NewForTest собирает бота без похода в Telegram и без ИИ: для сквозных
// тестов API мини-аппа на настоящей базе. api — клиент, смотрящий на
// поддельный сервер Telegram (tgbotapi.NewBotAPIWithAPIEndpoint).
func NewForTest(cfg *config.Config, db *database.Database, log logger.Logger, api *tgbotapi.BotAPI) *Bot {
	limiter := moderation.NewLimiter()
	b := &Bot{
		api:                  api,
		db:                   db,
		logger:               log,
		config:               cfg,
		timers:               make(map[int64]*domain.TimerInfo),
		ragStore:             rag.NoopStore{},
		sickApprovalWatchers: make(map[int64]chan struct{}),
		adminSessions:        make(map[int64]*adminSession),
		userSupportSessions:  make(map[int64]struct{}),
		miniappPersonalQueue: make(map[int64][]string),
		ugcModerationLimiter: limiter,
		ugcModerationGate:    moderation.NewGate(limiter),
		dynamicAdmins:        make(map[int64]struct{}),
	}
	b.reloadDynamicAdmins()
	return b
}
