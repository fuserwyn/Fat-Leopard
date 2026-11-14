package bot

import (
	"strings"

	"leo-bot/internal/domain"
	"leo-bot/internal/utils"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// MiniAppOnboardingResult — итог EnsureMiniAppOnboarding для ответа мини-аппу.
type MiniAppOnboardingResult struct {
	// InPack — у пользователя есть доступ к стае (paywall или message_log) либо это owner.
	InPack bool
	// Deleted — пользователь выбыл за неактивность и должен видеть экран блокировки мини-аппа.
	Deleted bool
	// AccessState — active / deleted / out.
	AccessState string
	// JustOnboarded — true только в первый успешный заход после оплаты,
	// когда мы стартовали таймер и записали pack_join/pack_rejoin в ленту.
	JustOnboarded bool
	// IsRejoin — карточка ушла как pack_rejoin (return_count > 1), иначе pack_join.
	IsRejoin bool
}

// EnsureMiniAppOnboarding — идемпотентный онбординг при открытии мини-аппа.
//   - при бесплатном входе (PAYWALL_ENTRY_FREE) новичку заводим профиль стаи здесь же и онбордим;
//     единственный, кому вход закрыт, — выбывший за неактивность: ему нужен платный возврат в боте;
//   - если пользователь не в стае (нет paywall_access и нет живой записи training_state) — возвращаем InPack=false и ничего не пишем;
//   - если в стае и таймер ещё не стартовал (timer_start_time IS NULL и не is_deleted) — стартуем таймер и пишем карточку приветствия в ленту;
//   - дополнительно создаём минимальный message_log в private chat, чтобы #sick_leave / #healthy из мини-аппа имели куда писать стейт.
//
// Для оплативших таймер и карточка ленты выставляются раньше — в paywallDeliverAccessAfterPayment
// (как только outbox_worker обработал событие paywall_access_restore_requested). Поэтому в обычном
// сценарии «оплатил → открыл мини-апп» сюда заходим уже с timer_start_time IS NOT NULL и просто
// отдаём InPack=true без дублирования. Ветку «стартуем таймер тут» оставляем как fallback для
// owner и legacy-юзеров, у которых message_log есть, а timer_start_time почему-то пустой.
//
// Заменяет sendWelcomeMessage / handleNewChatMembers, которые раньше срабатывали при добавлении в TG-группу.
func (b *Bot) EnsureMiniAppOnboarding(d initdata.InitData) (MiniAppOnboardingResult, error) {
	out := MiniAppOnboardingResult{AccessState: "out"}
	if b == nil || b.db == nil || d.User.ID == 0 {
		return out, nil
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return out, nil
	}
	userID := d.User.ID
	username := displayNameFromInitData(d)

	isOwner := b.config.IsAdminTelegramUser(userID)

	// Доступ: владелец / активная оплата / живой message_log в стае.
	if isOwner {
		out.InPack = true
		out.AccessState = "active"
	} else {
		mlAny, err := b.db.GetMessageLogAnyState(userID, chatID)
		if err == nil && mlAny != nil && mlAny.IsDeleted {
			out.Deleted = true
			out.AccessState = "deleted"
			return out, nil
		}
		ok, err := b.db.UserInPackOrPaid(userID, chatID, b.paywallEntryRequiresPayment())
		if err != nil {
			return out, err
		}
		// Бесплатный вход: новичок приходит без платежа, поэтому строку стаи создаём здесь
		// (раньше её создавала только оплата). Кикнутого это не затрагивает — он вышел выше
		// по ветке Deleted, а INSERT всё равно не перезаписал бы существующую строку.
		if !ok && b.freeEntryActive() {
			created, cerr := b.db.EnsureFreeEntryProfile(userID, chatID, username)
			if cerr != nil {
				return out, cerr
			}
			ok = created
		}
		out.InPack = ok
		if !ok {
			return out, nil
		}
		out.AccessState = "active"
	}

	b.SyncMiniappTelegramPhotoFromInit(userID, chatID, strings.TrimSpace(d.User.PhotoURL))

	// На всякий случай — приватный message_log для лички (sick/healthy через #хэштеги).
	b.ensurePrivateMessageLogForMiniApp(userID, username)

	ml, err := b.db.GetMessageLog(userID, chatID)
	if err != nil || ml == nil {
		// Нет записи в стае: для оплативших такого быть не должно (paywallDeliverAccessAfterPayment создаёт строку),
		// но если случилось — ничего не делаем, пусть платёжный путь докатит запись.
		b.logger.Warnf("miniapp onboarding: no message_log user=%d chat=%d: %v", userID, chatID, err)
		return out, nil
	}
	if ml.IsDeleted {
		// is_deleted=true означает «выбыл за неактивность». Возвращаться можно только через новую оплату.
		out.InPack = false
		out.Deleted = true
		out.AccessState = "deleted"
		return out, nil
	}
	// Старый баг: после оплаты ReactivateReturnedUser вызывали с пустым username → NULL в training_state.
	if strings.TrimSpace(ml.Username) == "" {
		fill := strings.TrimSpace(username)
		if fill != "" {
			ml.Username = fill
			if err := b.db.SaveMessageLog(ml); err != nil {
				b.logger.Warnf("miniapp onboarding: backfill pack username user=%d: %v", userID, err)
			}
		}
	}
	if ml.TimerStartTime != nil && strings.TrimSpace(*ml.TimerStartTime) != "" {
		// Онбординг уже был.
		return out, nil
	}

	// Первый онбординг.
	b.startTimer(userID, chatID, username)

	out.JustOnboarded = true
	if rc, e := b.db.GetUserReturnCount(userID, chatID); e == nil && rc > 1 {
		out.IsRejoin = true
	}
	if out.IsRejoin {
		b.savePackJoinMiniappFeed(
			chatID,
			userID,
			username,
			userMessageTypePackRejoin,
			packRejoinMiniappFeedPublicText(),
			packRejoinMiniappWelcomeText(b.EffectiveUILang(userID), username),
		)
	} else {
		b.savePackJoinMiniappFeed(
			chatID,
			userID,
			username,
			userMessageTypePackJoin,
			packJoinMiniappFeedPublicText(username),
			packJoinMiniappWelcomeText(b.EffectiveUILang(userID), username),
		)
	}
	b.logger.Infof("miniapp onboarding: started timer + feed card user=%d rejoin=%t", userID, out.IsRejoin)
	return out, nil
}

// ensurePrivateMessageLogForMiniApp — если включена стая, подстраховывает одну строку training_state на (user_id, MONETIZED_CHAT_ID).
// Раньше создавали дубликат (user_id, user_id); состояние ведём только на id стаи.
// Если MONETIZED_CHAT_ID не задан (локальный режим), остаётся легаси-строка (user_id, user_id).
func (b *Bot) ensurePrivateMessageLogForMiniApp(userID int64, username string) {
	if b == nil || b.db == nil || userID == 0 {
		return
	}
	packChat := userID
	if b.config != nil && b.config.MonetizedChatID != 0 {
		packChat = b.config.MonetizedChatID
	}
	if existing, err := b.db.GetMessageLog(userID, packChat); err == nil && existing != nil {
		return
	}
	now := utils.FormatMoscowTime(utils.GetMoscowTime())
	ml := &domain.MessageLog{
		UserID:          userID,
		ChatID:          packChat,
		Username:        strings.TrimSpace(username),
		StreakDays:      0,
		CupsEarned:      0,
		LastMessage:     now,
		HasTrainingDone: false,
		HasSickLeave:    false,
		HasHealthy:      false,
		IsDeleted:       false,
		TimerStartTime:  &now,
	}
	if err := b.db.SaveMessageLog(ml); err != nil {
		b.logger.Warnf("miniapp onboarding: failed to seed private message_log user=%d: %v", userID, err)
	}
}
