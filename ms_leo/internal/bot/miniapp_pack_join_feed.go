package bot

import (
	"fmt"
	"strings"

	"leo-bot/internal/domain"
	"leo-bot/internal/moderation"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

const (
	userMessageTypePackJoin     = "pack_join"
	userMessageTypePackRejoin   = "pack_rejoin"
	userMessageTypeDailyWisdom  = "daily_wisdom"
	userMessageTypePackRemoved  = "pack_removed"
	userMessageTypeAdminPost    = "admin_post"
	userMessageTypeAdminPoll    = "admin_poll"
)

// Текст для ленты стаи: короткое объявление (приветствие остаётся только в личке).
// displayName — это «@username» или «Имя Фамилия» из displayNameFromInitData; пустую
// строку обрабатываем фоллбэком, чтобы не вылезло «: .» в карточке.
func packJoinMiniappFeedPublicText(displayName string) string {
	d := strings.TrimSpace(displayName)
	if d == "" {
		return "В стае новый участник."
	}
	return fmt.Sprintf("В стае новый участник: %s.", d)
}

func packRejoinMiniappFeedPublicText() string {
	return "Участник вернулся в стаю. Я написал ему в личку — с возвращением и напоминание про ритм стаи."
}

// Персональное приветствие — только в Telegram-личку вступившему (не дублировать целиком в ленту).
func packJoinMiniappWelcomeText(displayName string) string {
	d := strings.TrimSpace(displayName)
	if d == "" {
		d = "участник"
	}
	return fmt.Sprintf(
		"Привет, %s! Я Лео — добро пожаловать в стаю. Отмечай тренировки в мини-аппе (кнопка «+») и следи за дедлайном. Рад видеть тебя здесь! 🐆",
		d,
	)
}

func packRejoinMiniappWelcomeText(displayName string) string {
	d := strings.TrimSpace(displayName)
	if d == "" {
		d = "участник"
	}
	return fmt.Sprintf(
		"%s, с возвращением! Я Лео — продолжай отмечать тренировки в мини-аппе. Рад снова видеть тебя в стае! 🐆",
		d,
	)
}

func (b *Bot) sendPackWelcomeDM(telegramUserID int64, text string) {
	if b == nil || b.api == nil || telegramUserID == 0 {
		return
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	m := tgbotapi.NewMessage(telegramUserID, t)
	if _, err := b.api.Send(m); err != nil {
		b.logger.Warnf("pack welcome DM user=%d: %v", telegramUserID, err)
	}
}

// savePackJoinMiniappFeed — строка ленты (нейтральный публичный текст) + личное приветствие в ЛС.
func (b *Bot) savePackJoinMiniappFeed(chatID, userID int64, username, msgType, feedPublicText, dmText string) {
	if b == nil || b.db == nil || userID == 0 {
		return
	}
	if b.config.MonetizedChatID == 0 || chatID != b.config.MonetizedChatID {
		return
	}
	um := &domain.UserMessage{
		UserID:      userID,
		ChatID:      chatID,
		Username:    strings.TrimSpace(username),
		MessageText: strings.TrimSpace(feedPublicText),
		MessageType: msgType,
	}
	if err := b.db.SaveUserMessage(um); err != nil {
		b.logger.Warnf("miniapp pack feed %s user=%d: %v", msgType, userID, err)
		return
	}
	b.sendPackWelcomeDM(userID, dmText)
}

// saveDailyWisdomPackFeed — «мудрость дня» в ленту мини-аппа (одна запись в чате стаи, автор Лео).
func (b *Bot) saveDailyWisdomPackFeed(wisdom string) {
	if b == nil || b.db == nil {
		return
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return
	}
	t := strings.TrimSpace(wisdom)
	if t == "" {
		return
	}
	um := &domain.UserMessage{
		UserID:      0,
		ChatID:      chatID,
		Username:    "Лео",
		MessageText: t,
		MessageType: userMessageTypeDailyWisdom,
	}
	if err := b.db.SaveUserMessage(um); err != nil {
		b.logger.Warnf("miniapp pack feed daily_wisdom: %v", err)
	}
}

func (b *Bot) savePackRemovedMiniappFeed(chatID, userID int64, username string) {
	if b == nil || b.db == nil || userID == 0 {
		return
	}
	if b.config.MonetizedChatID == 0 || chatID != b.config.MonetizedChatID {
		return
	}
	um := &domain.UserMessage{
		UserID:      userID,
		ChatID:      chatID,
		Username:    strings.TrimSpace(username),
		MessageText: PackRemovedFeedNotice(b.livePrompts(), username),
		MessageType: userMessageTypePackRemoved,
	}
	if err := b.db.SaveUserMessage(um); err != nil {
		b.logger.Warnf("miniapp pack feed pack_removed user=%d: %v", userID, err)
	}
}

// Автор админского поста ленты: "leo" — от имени Лео, иначе — от имени Админа.
const (
	adminPostAuthorLeo   = "leo"
	adminPostAuthorAdmin = "admin"
)

// adminPostAuthorUsername — отображаемое имя автора карточки ленты по коду автора.
func adminPostAuthorUsername(author string) string {
	if author == adminPostAuthorLeo {
		return "Лео"
	}
	return "Админ"
}

// saveAdminCustomPackFeed — немедленная публикация админского поста в ленту стаи (с PRE-модерацией).
func (b *Bot) saveAdminCustomPackFeed(adminUserID int64, author, text string) error {
	t := strings.TrimSpace(text)
	if t == "" {
		return fmt.Errorf("empty text")
	}
	if err := b.enforceAdminBroadcast(t, moderation.SurfaceAdminPost); err != nil {
		return err
	}
	if _, err := b.publishAdminPackFeedPost(adminUserID, author, t); err != nil {
		return err
	}
	b.logger.Infof("admin custom pack feed post published by admin=%d author=%s", adminUserID, author)
	return nil
}

// publishAdminPackFeedPost — записывает админский пост в ленту стаи без модерации.
// Используется и при немедленной публикации (после enforceAdminBroadcast), и планировщиком
// отложенных постов (модерация уже прошла в момент постановки в очередь).
func (b *Bot) publishAdminPackFeedPost(authorUserID int64, author, text string) (int64, error) {
	if b == nil || b.db == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return 0, fmt.Errorf("pack feed unavailable")
	}
	t := strings.TrimSpace(text)
	if t == "" {
		return 0, fmt.Errorf("empty text")
	}
	packChatID := b.config.MonetizedChatID
	um := &domain.UserMessage{
		UserID:      0,
		ChatID:      packChatID,
		Username:    adminPostAuthorUsername(author),
		MessageText: t,
		MessageType: userMessageTypeAdminPost,
	}
	msgID, err := b.db.SaveUserMessageReturningID(um)
	if err != nil {
		return 0, err
	}
	if msgID > 0 {
		b.notifyPackMemberMentionsInFeedPost(packChatID, authorUserID, adminPostAuthorUsername(author), t, msgID)
	}
	return msgID, nil
}
