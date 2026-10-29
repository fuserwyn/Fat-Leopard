package bot

import (
	"context"
	"strings"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Мудрость дня всегда приходит в 04:20 по локальному времени пользователя.
// Это фиксированное время (не настраивается): галочка в мини-аппе — только вкл/выкл.
const (
	wisdomLocalHour   = 4
	wisdomLocalMinute = 20
)

// WisdomSubscriptionView — настройки подписки на «мудрость дня» для отдачи в мини-апп.
type WisdomSubscriptionView struct {
	Enabled    bool `json:"enabled"`
	RemindHour int  `json:"remind_hour"`
}

// GetWisdomSubscriptionForViewer — настройки подписки текущего пользователя (дефолт, если не настроено).
func (b *Bot) GetWisdomSubscriptionForViewer(viewerUserID int64, initD initdata.InitData) (WisdomSubscriptionView, error) {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return WisdomSubscriptionView{}, err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return WisdomSubscriptionView{}, err
	}
	s, err := b.db.GetWisdomSubscriptionSettings(viewerUserID, b.config.MonetizedChatID)
	if err != nil {
		return WisdomSubscriptionView{}, err
	}
	return WisdomSubscriptionView{Enabled: s.Enabled, RemindHour: s.RemindHour}, nil
}

// SaveWisdomSubscriptionForViewer — сохранить настройки подписки текущего пользователя.
// Час не настраивается: мудрость всегда приходит в 04:20 локального времени, поэтому
// входящий remindHour игнорируется, а в БД пишется фиксированный wisdomLocalHour.
func (b *Bot) SaveWisdomSubscriptionForViewer(viewerUserID int64, initD initdata.InitData, enabled bool, _ int) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if err := b.assertPackFeedSocialViewer(viewerUserID); err != nil {
		return err
	}
	return b.db.SaveWisdomSubscriptionSettings(viewerUserID, b.config.MonetizedChatID, database.WisdomSubscriptionSettings{
		Enabled:    enabled,
		RemindHour: wisdomLocalHour,
	})
}

// wisdomSubscriptionTick — как часто планировщик проверяет, у кого настал «час мудрости».
// Раз в минуту: окно срабатывания — целый локальный час (localHour == remind_hour), а
// идемпотентность держит last_sent_msk_date, так что лишних отправок не будет.
const wisdomSubscriptionTick = 1 * time.Minute

// startDailyWisdomSubscriptionScheduler — фоновая рассылка «мудрости дня» в личку бота тем,
// кто сам подписался в мини-аппе. Время — локальный час пользователя (TZ из
// training_state.timezone_offset_from_moscow). Текст берётся из суточного лога daily_wisdom_log,
// который наполняет generateAndSendDailyWisdom (04:20 МСК).
func (b *Bot) startDailyWisdomSubscriptionScheduler(ctx context.Context) {
	if b == nil {
		return
	}
	if b.config == nil || b.config.MonetizedChatID == 0 {
		b.logger.Info("daily wisdom subscription scheduler: skipped, MONETIZED_CHAT_ID не настроен")
		return
	}
	b.logger.Infof("daily wisdom subscription scheduler: started, tick=%s", wisdomSubscriptionTick)
	ticker := time.NewTicker(wisdomSubscriptionTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			b.logger.Info("daily wisdom subscription scheduler: stopped")
			return
		case <-ticker.C:
			b.runDailyWisdomSubscriptionSweep()
		}
	}
}

// runDailyWisdomSubscriptionSweep — одна итерация: всем, у кого сейчас «их» локальный час,
// шлём свежую «мудрость дня» в личку. Идемпотентно по локальной дате юзера.
func (b *Bot) runDailyWisdomSubscriptionSweep() {
	if b == nil || b.db == nil || b.config == nil {
		return
	}
	packChatID := b.config.MonetizedChatID
	candidates, err := b.db.ListDueWisdomSubscriptionCandidates(packChatID)
	if err != nil {
		b.logger.Errorf("daily wisdom subscription sweep: list candidates: %v", err)
		return
	}
	if len(candidates) == 0 {
		return
	}
	// Час у кого-то настал? Тогда достаём сегодняшнюю мудрость один раз на всю итерацию.
	wisdom, ok, err := b.db.GetLatestDailyWisdom()
	if err != nil {
		b.logger.Errorf("daily wisdom subscription sweep: get latest wisdom: %v", err)
		return
	}
	if !ok || strings.TrimSpace(wisdom) == "" {
		// Мудрость ещё не сгенерирована (например, до первого 04:20 после деплоя) — молча ждём.
		return
	}
	now := utils.GetMoscowTime()
	sent := 0
	for _, c := range candidates {
		// Мудрость приходит в фиксированные 04:20 локального времени пользователя.
		// Окно — с 04:20 до конца часа: если свип чуть опоздал, отправка всё равно
		// сработает, а идемпотентность по локальной дате не даст задублировать.
		due, todayLocal := localDailySendDue(now, c.TimezoneOffsetFromMoscow, wisdomLocalHour, wisdomLocalMinute, c.LastSentMskDate.String)
		if !due {
			continue
		}
		b.sendDailyWisdomDM(c.UserID, c.Username, wisdom)
		if err := b.db.MarkWisdomSubscriptionSent(c.UserID, packChatID, todayLocal); err != nil {
			b.logger.Warnf("daily wisdom subscription mark sent user=%d: %v", c.UserID, err)
		}
		sent++
	}
	if sent > 0 {
		b.logger.Infof("daily wisdom subscription sweep: sent=%d (of %d candidates)", sent, len(candidates))
	}
}

func dailyWisdomDMText(username, wisdom string) string {
	who := normalizeUserDisplayName(username)
	prefix := ""
	if strings.TrimSpace(who) != "" {
		prefix = who + ", "
	}
	return "🐆 " + prefix + "мудрость дня от Лео:\n\n" + strings.TrimSpace(wisdom)
}

// sendDailyWisdomDM — пуш в очередь мини-аппа + DM в Telegram (как напоминания о тренировке).
func (b *Bot) sendDailyWisdomDM(userID int64, username, wisdom string) {
	if b == nil || userID == 0 || strings.TrimSpace(wisdom) == "" {
		return
	}
	text := dailyWisdomDMText(username, wisdom)
	b.miniappPersonalPush(userID, text)
	if b.api == nil {
		return
	}
	if _, err := b.api.Send(tgbotapi.NewMessage(userID, text)); err != nil {
		b.logger.Warnf("daily wisdom DM user=%d: %v", userID, err)
	}
}
