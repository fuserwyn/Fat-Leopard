package bot

import (
	"strconv"
	"strings"
	"sync"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Меню-кнопка LeopardMiniApp в ЛС-чате с ботом должна показываться только тем,
// у кого активный paywall-доступ и кто не кикнут за неактивность (см. требование
// пользователя: «синяя кнопка LeopardMiniApp появлялась только когда оплачен
// доступ и ты не кикнут»). Глобально кнопку прописывает @BotFather (web_app);
// per-user мы переопределяем её через setChatMenuButton:
//
//	paid + не кикнут → menu_button = {"type":"default"} → Telegram берёт глобальную
//	    web_app-кнопку из BotFather (LeopardMiniApp) и показывает её этому юзеру.
//	не paid / кикнут → menu_button = {"type":"commands"} → web_app в ЛС скрыт,
//	    остаётся обычное меню команд.
//
// Состояние кешируется в menuButtonState, чтобы не дёргать Telegram API на каждом
// /start или после каждого сообщения. Кеш живёт пока работает процесс — этого
// достаточно: при рестарте мы заново синхронизируем состояние при первом контакте.
const (
	miniappMenuButtonDefault  = "default"
	miniappMenuButtonCommands = "commands"
)

// menuButtonState хранит последний успешно выставленный режим per-user.
// Запись «default» / «commands». Используется как кеш, чтобы избежать дублей.
var menuButtonState sync.Map

// applyMiniappMenuButtonForUser приводит per-user setChatMenuButton к режиму,
// который соответствует текущему статусу пользователя. Если paywall выключен —
// ничего не делаем (в @BotFather прописана кнопка как пожелает админ).
func (b *Bot) applyMiniappMenuButtonForUser(userID int64) {
	if b == nil || userID == 0 {
		return
	}
	if b.api == nil {
		return
	}
	if !b.paywallActive() {
		return
	}
	want := miniappMenuButtonDefault
	if b.paywallPrivateNeedsPayFirst(userID) {
		want = miniappMenuButtonCommands
	}
	if v, ok := menuButtonState.Load(userID); ok {
		if s, _ := v.(string); s == want {
			return
		}
	}
	if err := b.setUserMenuButton(userID, want); err != nil {
		b.logger.Warnf("setChatMenuButton user=%d kind=%s: %v", userID, want, err)
		return
	}
	menuButtonState.Store(userID, want)
	b.logger.Infof("miniapp menu button: user=%d set to %s", userID, want)
}

// setUserMenuButton дёргает sync setChatMenuButton с явным chat_id (= userID для ЛС).
// Прокидываем сырые параметры через MakeRequest: tgbotapi.v5 не имеет хелпера для
// этого метода, а нам важна только пара полей.
func (b *Bot) setUserMenuButton(userID int64, kind string) error {
	params := tgbotapi.Params{
		"chat_id": strconv.FormatInt(userID, 10),
	}
	switch kind {
	case miniappMenuButtonDefault:
		params["menu_button"] = `{"type":"default"}`
	case miniappMenuButtonCommands:
		params["menu_button"] = `{"type":"commands"}`
	default:
		params["menu_button"] = `{"type":"default"}`
	}
	_, err := b.api.MakeRequest("setChatMenuButton", params)
	return err
}

// miniappOpenInlineKeyboard возвращает inline-клавиатуру с одной web_app-кнопкой
// «Открыть», которой оплатившему юзеру достаточно тапнуть прямо в сообщении, чтобы
// открыть мини-аппу (не разворачивая синюю menu-кнопку). Возвращает nil, если:
//   - не задан MINIAPP_WEB_APP_URL (тогда полагаемся только на menu-кнопку);
//   - юзер ещё не оплатил / кикнут (paywallPrivateNeedsPayFirst) — кнопку не показываем.
//
// Внимание Telegram: inline web_app-кнопки работают только в ЛС и только если домен
// URL привязан к боту в @BotFather (тот же web_app, что у menu-кнопки).
func (b *Bot) miniappOpenInlineKeyboard(userID int64) *tgbotapi.InlineKeyboardMarkup {
	if b == nil || userID == 0 {
		return nil
	}
	url := strings.TrimSpace(b.config.MiniappWebAppURL)
	if url == "" {
		return nil
	}
	if b.paywallActive() && b.paywallPrivateNeedsPayFirst(userID) {
		return nil
	}
	btn := tgbotapi.NewInlineKeyboardButtonWebApp("🐆 Open LeopardMiniApp", tgbotapi.WebAppInfo{URL: url})
	kb := tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(btn))
	return &kb
}

// invalidateMiniappMenuButtonCache сбрасывает кеш для юзера: используем после
// событий, меняющих статус (оплата / кик / истечение). Следующий applyMiniapp...
// увидит «нет в кеше» и обязательно отправит запрос в Telegram.
func invalidateMiniappMenuButtonCache(userID int64) {
	menuButtonState.Delete(userID)
}
