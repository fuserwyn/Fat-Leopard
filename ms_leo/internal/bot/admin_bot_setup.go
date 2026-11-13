package bot

import (
	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// setupAdminBotCommands — в меню «/» у adminов в личке появляется /admin.
func (b *Bot) setupAdminBotCommands() {
	if b == nil || b.config == nil || b.api == nil {
		return
	}
	adminIDs := b.config.AdminTelegramUserIDs()
	if len(adminIDs) == 0 {
		return
	}
	commands := []tgbotapi.BotCommand{
		{Command: "start", Description: "Start"},
		{Command: "admin", Description: "⚙️ Admin panel"},
		{Command: "help", Description: "Help"},
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
