package bot

import (
	"errors"
	"strings"
	"time"

	initdata "github.com/telegram-mini-apps/init-data-golang"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"

	"leo-bot/internal/database"
	"leo-bot/internal/game/leopardmoney"
	"leo-bot/internal/moderation"
)

// PrivateTextMessageFromInitUser — синтетическое входящее сообщение, как в личке.
func PrivateTextMessageFromInitUser(d initdata.InitData, text string) *tgbotapi.Message {
	u := d.User
	tgU := tgbotapiUserFromInitData(u)
	return &tgbotapi.Message{
		MessageID: 0,
		From:      tgU,
		Date:      int(time.Now().Unix()),
		Chat:      privateChatForUser(tgU),
		Text:      text,
	}
}

func tgbotapiUserFromInitData(u initdata.User) *tgbotapi.User {
	if u.ID == 0 {
		return nil
	}
	return &tgbotapi.User{
		ID:           u.ID,
		IsBot:        u.IsBot,
		FirstName:    u.FirstName,
		LastName:     u.LastName,
		UserName:     u.Username,
		LanguageCode: u.LanguageCode,
	}
}

func privateChatForUser(u *tgbotapi.User) *tgbotapi.Chat {
	if u == nil {
		return nil
	}
	return &tgbotapi.Chat{
		ID:        u.ID,
		Type:      "private",
		FirstName: u.FirstName,
		LastName:  u.LastName,
		UserName:  u.UserName,
	}
}

// MiniAppTextProcessResult — ответ бота в личку: сразу reply_text, либо pending + poll очереди.
type MiniAppTextProcessResult struct {
	ReplyText string `json:"reply_text,omitempty"`
	Pending   bool   `json:"pending,omitempty"`
	Blocked   bool   `json:"blocked,omitempty"`
	BlockCode string `json:"block_code,omitempty"`
}

func isMiniappTrainingReport(text string) bool {
	return leopardmoney.IsTrainingReportLine(text)
}

// ProcessMiniAppPrivateText — валидация initData; обработка в фоне, HTTP не ждёт ИИ.
// Текущий текст «без фото»: снимаем отложенный URL фото, чтобы не приклеить к чужому действию.
func (b *Bot) ProcessMiniAppPrivateText(d initdata.InitData, text string) MiniAppTextProcessResult {
	b.clearMiniappTrainingPhotoURL(d.User.ID)
	return b.processMiniAppPrivateCore(d, text, "")
}

// ProcessMiniAppPrivateTextWithTrainingPhoto — тот же путь; URL фото передаётся в воркер без sync.Map (надёжно для ленты).
func (b *Bot) ProcessMiniAppPrivateTextWithTrainingPhoto(d initdata.InitData, text, publicPhotoURL string) MiniAppTextProcessResult {
	b.clearMiniappTrainingPhotoURL(d.User.ID)
	return b.processMiniAppPrivateCore(d, text, strings.TrimSpace(publicPhotoURL))
}

func (b *Bot) processMiniAppPrivateCore(d initdata.InitData, text string, trainingPhotoURL string) MiniAppTextProcessResult {
	out := MiniAppTextProcessResult{}
	if text == "" || b == nil {
		return out
	}
	if d.User.ID == 0 {
		return out
	}
	if err := b.AssertMiniAppPackChatAligns(d); err != nil {
		return out
	}

	if isMiniappTrainingReport(text) {
		checkText := moderation.TextForTrainingModeration(text)
		if _, err := b.enforceUGC(checkText, moderation.SurfaceTrainingNote, d.User.ID); err != nil {
			var mod *ModerationBlockedError
			out.Blocked = true
			if errors.As(err, &mod) && mod != nil {
				out.ReplyText = mod.Message
				out.BlockCode = mod.APICode
			} else {
				out.ReplyText = moderation.UserWarnings[moderation.ReasonProfanity]
				out.BlockCode = "moderation_blocked"
			}
			b.trackModerationBlocked("workout_note", out.BlockCode, d.User.ID)
			return out
		}
	} else {
		if _, err := b.enforceLeoChat(text, d.User.ID); err != nil {
			var mod *ModerationBlockedError
			out.Blocked = true
			if errors.As(err, &mod) && mod != nil {
				out.ReplyText = mod.Message
				out.BlockCode = mod.APICode
			} else {
				out.ReplyText = moderation.UserWarnings[moderation.ReasonProfanity]
				out.BlockCode = "moderation_blocked"
			}
			b.trackModerationBlocked("leo_chat", out.BlockCode, d.User.ID)
			return out
		}
		// Воронка UGC: сообщение юзера Лео прошло гейт.
		b.db.TrackEvent(database.AnalyticsEvent{Name: database.EventLeoChatMessageSent, TelegramID: d.User.ID})
	}

	_ = PrivateTextMessageFromInitUser(d, text)
	b.miniappPersonalClear(d.User.ID)
	b.savePersonalChatMessage(d.User.ID, "user", text)

	// Отчёт о тренировке из мини-аппа: синхронный dispatch → reply_text сразу (без poll).
	if isMiniappTrainingReport(text) {
		msg := PrivateTextMessageFromInitUser(d, text)
		ch := make(chan string, 32)
		b.markMiniappOrigin(d.User.ID, ch)
		var syncReply string
		func() {
			defer b.unmarkMiniappOrigin(d.User.ID)
			defer func() {
				if r := recover(); r != nil {
					b.logger.Errorf("miniapp training sync dispatch panic: %v", r)
				}
			}()
			b.dispatchTextMessageFromUser(msg, ch, trainingPhotoURL)
			for {
				select {
				case t := <-ch:
					if tr := strings.TrimSpace(t); tr != "" {
						b.savePersonalChatMessage(d.User.ID, "leo", tr)
						if syncReply == "" {
							syncReply = tr
						} else {
							syncReply = syncReply + "\n\n" + tr
						}
					}
				default:
					return
				}
			}
		}()
		// Воронка 2 (активация): тренировка из мини-аппа залогирована.
		b.db.TrackEvent(database.AnalyticsEvent{
			Name:       database.EventWorkoutLogged,
			TelegramID: d.User.ID,
			Payload:    map[string]any{"has_photo": strings.TrimSpace(trainingPhotoURL) != "", "surface": "miniapp"},
		})
		if strings.TrimSpace(syncReply) != "" {
			// Комментарий Лео вернулся синхронно (промт 1).
			b.db.TrackEvent(database.AnalyticsEvent{Name: database.EventLeoCommentReceived, TelegramID: d.User.ID})
			out.ReplyText = syncReply
			return out
		}
		b.logger.Warnf("miniapp training_done: empty reply after dispatch user=%d", d.User.ID)
		out.ReplyText = b.userText(d.User.ID,
			"✅ Отчёт принят. Если не видишь сводку с кубками и стриком — загляни в личку с ботом в Telegram.",
			"✅ Workout logged. If you don't see cups and streak summary — check your Telegram DM with the bot.")
		return out
	}

	go b.runMiniAppPrivateTextWorker(d, text, trainingPhotoURL)
	out.Pending = true
	return out
}

// ProcessMiniAppLeoPhoto — личный чат с Лео: юзер прислал фото (с подписью или без)
// и ждёт рекомендаций. Подпись модерируется как обычное сообщение Лео; фото
// сохраняется в историю лички и описывается vision-моделью, после чего ответ
// генерится обычным путём Лео (персона + RAG + память). Ответ — асинхронно (poll).
func (b *Bot) ProcessMiniAppLeoPhoto(d initdata.InitData, caption, photoURL string) MiniAppTextProcessResult {
	out := MiniAppTextProcessResult{}
	photoURL = strings.TrimSpace(photoURL)
	if b == nil || d.User.ID == 0 || photoURL == "" {
		return out
	}
	if err := b.AssertMiniAppPackChatAligns(d); err != nil {
		return out
	}
	caption = strings.TrimSpace(caption)
	if caption != "" {
		if _, err := b.enforceLeoChat(caption, d.User.ID); err != nil {
			var mod *ModerationBlockedError
			out.Blocked = true
			if errors.As(err, &mod) && mod != nil {
				out.ReplyText = mod.Message
				out.BlockCode = mod.APICode
			} else {
				out.ReplyText = moderation.UserWarnings[moderation.ReasonProfanity]
				out.BlockCode = "moderation_blocked"
			}
			b.trackModerationBlocked("leo_chat", out.BlockCode, d.User.ID)
			return out
		}
	}
	// Воронка UGC: сообщение юзера Лео прошло гейт.
	b.db.TrackEvent(database.AnalyticsEvent{Name: database.EventLeoChatMessageSent, TelegramID: d.User.ID})

	b.miniappPersonalClear(d.User.ID)
	b.savePersonalChatMessageWithPhoto(d.User.ID, "user", caption, photoURL)

	go b.runMiniAppLeoPhotoWorker(d, caption, photoURL)
	out.Pending = true
	return out
}

// buildLeoPhotoQuestion собирает «синтетический» вопрос для Лео по фото:
// описание изображения (vision) + подпись юзера. Передаётся в обычный путь ИИ.
func buildLeoPhotoQuestion(caption, desc string) string {
	var sb strings.Builder
	sb.WriteString("The user sent a photo in DM and wants advice about it.\n")
	d := strings.TrimSpace(desc)
	c := strings.TrimSpace(caption)
	if d != "" {
		sb.WriteString("[What's in the photo (vision model): " + d + "]\n")
	}
	if c != "" {
		sb.WriteString("Caption/question: " + c + "\n")
	}
	switch {
	case d == "" && c == "":
		// Фото рассмотреть не удалось и подписи нет — честно попроси описать словами.
		sb.WriteString("Couldn't read the photo and there is no caption. Kindly ask them to describe the shot and how you can help.\n")
	case d == "" && c != "":
		// Нет описания, но есть подпись — отвечай по подписи, не выдумывая содержимое фото.
		sb.WriteString("Couldn't read the image — answer from the caption only; don't guess what's in the photo.\n")
	case d != "" && c == "":
		sb.WriteString("No caption — give useful advice from what's visible (technique, form, food, gear — as appropriate).\n")
	}
	return strings.TrimSpace(sb.String())
}

func (b *Bot) runMiniAppLeoPhotoWorker(d initdata.InitData, caption, photoURL string) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Errorf("miniapp leo photo worker panic: %v", r)
		}
	}()
	desc := ""
	if b.aiClient != nil && b.aiClient.HasVision() {
		desc = b.describeImageForLeo(photoURL, caption)
	}
	question := buildLeoPhotoQuestion(caption, desc)
	msg := PrivateTextMessageFromInitUser(d, question)
	ch := make(chan string, 32)
	b.markMiniappOrigin(d.User.ID, ch)
	defer b.unmarkMiniappOrigin(d.User.ID)
	b.dispatchTextMessageFromUser(msg, ch, "")
	for {
		select {
		case t := <-ch:
			tr := strings.TrimSpace(t)
			if tr == "" {
				continue
			}
			b.miniappPersonalPush(d.User.ID, tr)
		default:
			return
		}
	}
}

func (b *Bot) runMiniAppPrivateTextWorker(d initdata.InitData, text string, trainingPhotoURL string) {
	defer func() {
		if r := recover(); r != nil {
			b.logger.Errorf("miniapp private worker panic: %v", r)
		}
	}()
	msg := PrivateTextMessageFromInitUser(d, text)
	ch := make(chan string, 32)
	b.markMiniappOrigin(d.User.ID, ch)
	defer b.unmarkMiniappOrigin(d.User.ID)
	b.dispatchTextMessageFromUser(msg, ch, trainingPhotoURL)
	for {
		select {
		case t := <-ch:
			tr := strings.TrimSpace(t)
			if tr == "" {
				continue
			}
			b.miniappPersonalPush(d.User.ID, tr)
		default:
			return
		}
	}
}
