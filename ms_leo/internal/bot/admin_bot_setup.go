package bot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// setupAdminBotCommands — в меню «/» у админов в личке появляется /admin.
func (b *Bot) setupAdminBotCommands() {
	if b == nil || b.config == nil || b.api == nil {
		return
	}
	adminIDs := b.config.AdminTelegramUserIDs()
	if len(adminIDs) == 0 {
		return
	}
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Начать"},
		{Command: "admin", Description: "⚙️ Админ-панель"},
		{Command: "help", Description: "Справка"},
	}
	for _, adminID := range adminIDs {
		cfg := tgbotapi.NewSetMyCommandsWithScope(
			tgbotapi.NewBotCommandScopeChat(adminID),
			commands...,
		)
		if _, err := b.api.Request(cfg); err != nil {
			b.logger.Warnf("setMyCommands admin=%d: %v", adminID, err)
		}
	}
}
