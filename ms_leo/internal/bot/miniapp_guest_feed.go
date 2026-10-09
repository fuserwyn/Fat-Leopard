package bot

import (
	"regexp"
	"strings"
)

// Гостевой просмотр: человек пришёл по чужой ссылке, в стае его нет, но мини-апп
// показывает ему несколько свежих тренировок и кнопку «Вступить». Наружу уходит
// только витрина: имя, текст отчёта, фото тренировки, стрик и время. Без id
// авторов, аватаров, комментариев и реакций — это остаётся внутри стаи.

const (
	// guestFeedLimit — сколько постов видит гость.
	guestFeedLimit = 5
	// guestFeedScan — сколько свежих записей ленты просматриваем, чтобы набрать тренировки.
	guestFeedScan = 50
	// guestJoinStartPayload — параметр /start у кнопки «Вступить» (источник в аналитике — src-guest).
	guestJoinStartPayload = "src-guest_feed"
)

// guestFeedFallbackName — имя по умолчанию из SQL ленты ('user' || id): гостю id не показываем.
var guestFeedFallbackName = regexp.MustCompile(`^user\d+$`)

// GuestFeedView — ответ гостевой ленты.
type GuestFeedView struct {
	// InPack — смотрящий уже в стае: мини-апп может открыть обычную ленту.
	InPack bool `json:"in_pack"`
	// Items — свежие тренировки стаи в формате ленты (урезанные поля).
	Items []PackFeedItem `json:"items"`
	// JoinURL — ссылка на бота для кнопки «Вступить»; пусто — имя бота неизвестно.
	JoinURL string `json:"join_url"`
}

// guestFeedName — имя автора для гостя: без id и без пустых строк.
func guestFeedName(name string) string {
	name = strings.TrimSpace(name)
	if name == "" || guestFeedFallbackName.MatchString(name) {
		return "Участник стаи"
	}
	return name
}

// guestJoinLink — ссылка t.me/<бот>?start=src-guest_feed для кнопки «Вступить».
func guestJoinLink(botName string) string {
	botName = strings.TrimSpace(botName)
	if botName == "" {
		return ""
	}
	return "https://t.me/" + botName + "?start=" + guestJoinStartPayload
}

// PackGuestFeedForViewer — несколько свежих тренировок стаи для того, кто ещё не вступил.
// Доступ не проверяется: витрина открыта любому с валидной подписью Telegram.
func (b *Bot) PackGuestFeedForViewer(viewerUserID int64) (GuestFeedView, error) {
	out := GuestFeedView{Items: []PackFeedItem{}}
	if b == nil || b.config == nil {
		return out, nil
	}
	out.JoinURL = guestJoinLink(b.botUsername())
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		// Стая не настроена (локальный режим) — закрывать приложение не от чего.
		out.InPack = true
		return out, nil
	}
	if b.db == nil {
		return out, nil
	}
	if viewerUserID != 0 {
		if b.config.IsAdminTelegramUser(viewerUserID) {
			out.InPack = true
		} else if ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.paywallEntryRequiresPayment()); err == nil {
			out.InPack = ok
		}
	}
	rows, err := b.db.ListPackActivityFeedDesc(chatID, nil, guestFeedScan)
	if err != nil {
		return out, err
	}
	for _, r := range rows {
		if len(out.Items) >= guestFeedLimit {
			break
		}
		if r == nil || r.MessageType != "training_done" || strings.TrimSpace(r.MessageText) == "" {
			continue
		}
		out.Items = append(out.Items, PackFeedItem{
			ID:               r.ID,
			Username:         guestFeedName(r.Username),
			Type:             r.MessageType,
			Source:           "feed",
			Text:             r.MessageText,
			CreatedAt:        r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
			StreakDays:       r.StreakDays,
			TrainingPhotoURL: b.canonicalMiniappTrainingPhotoURL(r.TrainingPhotoURL),
		})
	}
	return out, nil
}

// guestFeedButtonText — web_app-кнопка под способами оплаты: открыть ленту стаи гостем.
const guestFeedButtonText = "👀 Посмотреть ленту стаи"
