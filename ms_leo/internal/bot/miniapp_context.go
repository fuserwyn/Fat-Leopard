package bot

import (
	"errors"
	"fmt"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// ErrMiniAppChatMismatch — initData указывает на другой чат, чем MONETIZED_CHAT_ID (другая группа).
var ErrMiniAppChatMismatch = errors.New("initdata chat does not match paywall / pack chat")

// AssertMiniAppPackChatAligns — мини-апп «Стая» = MONETIZED_CHAT_ID в .env. Если в подписи Telegram есть chat
// и это group/supergroup, id обязан совпасть. Если мини-апп открыт из лички, chat часто пуст — тогда сравнивать нечего.
func (b *Bot) AssertMiniAppPackChatAligns(d initdata.InitData) error {
	want := b.config.MonetizedChatID
	if want == 0 {
		return nil
	}
	if d.Chat.ID == 0 {
		return nil
	}
	switch d.Chat.Type {
	case initdata.ChatTypeGroup, initdata.ChatTypeSupergroup:
		if d.Chat.ID != want {
			return fmt.Errorf("%w: telegram chat %d, configured pack %d", ErrMiniAppChatMismatch, d.Chat.ID, want)
		}
	default:
		// private / channel / пусто — id не id группы «Стаи»
	}
	return nil
}
