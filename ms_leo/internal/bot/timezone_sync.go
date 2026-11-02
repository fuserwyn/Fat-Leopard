package bot

import (
	"context"
	"strings"
	"time"
	_ "time/tzdata" // зоны внутри бинарника: в образе может не быть /usr/share/zoneinfo
)

// Мини-апп шлёт IANA-зону устройства (Europe/Belgrade). Сервер раз в
// timezoneResyncTick пересчитывает по ней смещение от МСК: переход на
// зимнее/летнее время сдвигает напоминания сам, без открытия мини-аппа.
// Переезд в другую зону сервер по-прежнему узнаёт, только когда человек
// открыл мини-апп: Telegram пояс пользователя боту не сообщает.
const timezoneResyncTick = 10 * time.Minute

// timezoneOffsetFromMoscow — смещение зоны name от МСК в часах на момент now.
// false — зона неизвестна или смещение вне -12..+12.
func timezoneOffsetFromMoscow(name string, now time.Time) (int, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return 0, false
	}
	_, sec := now.In(loc).Zone()
	_, msk := now.In(moscowLocation()).Zone()
	diff := float64(sec-msk) / 3600
	off := int(diff + 0.5)
	if diff < 0 {
		off = int(diff - 0.5)
	}
	if off < miniappProfileTzOffsetMin || off > miniappProfileTzOffsetMax {
		return 0, false
	}
	return off, true
}

// SaveTimezoneNameFromMiniapp — запоминает IANA-зону устройства; неизвестную зону молча пропускаем.
func (b *Bot) SaveTimezoneNameFromMiniapp(userID, packChatID int64, name string) error {
	if b == nil || b.db == nil {
		return nil
	}
	name = strings.TrimSpace(name)
	if _, ok := timezoneOffsetFromMoscow(name, time.Now()); !ok {
		return nil
	}
	return b.db.PatchTrainingStateTimezoneNameIfExists(userID, packChatID, name)
}

// GetTimezoneNameForAPI — сохранённая IANA-зона пользователя; пусто, если ещё не приходила.
func (b *Bot) GetTimezoneNameForAPI(userID, packChatID int64) string {
	if b == nil || b.db == nil || packChatID == 0 {
		return ""
	}
	name, err := b.db.GetTimezoneNameFromTrainingState(userID, packChatID)
	if err != nil {
		b.logger.Errorf("miniapp tz name load user=%d pack=%d: %v", userID, packChatID, err)
		return ""
	}
	return name
}

func (b *Bot) startTimezoneResyncScheduler(ctx context.Context) {
	if b == nil || b.config == nil || b.config.MonetizedChatID == 0 {
		return
	}
	b.runTimezoneResync(time.Now())
	ticker := time.NewTicker(timezoneResyncTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.runTimezoneResync(time.Now())
		}
	}
}

// runTimezoneResync — приводит смещение каждого к его IANA-зоне. Возвращает, скольким поправили.
func (b *Bot) runTimezoneResync(now time.Time) int {
	if b == nil || b.db == nil || b.config == nil {
		return 0
	}
	packChatID := b.config.MonetizedChatID
	list, err := b.db.ListTrainingStateTimezones(packChatID)
	if err != nil {
		b.logger.Warnf("timezone resync: list: %v", err)
		return 0
	}
	n := 0
	for _, t := range list {
		off, ok := timezoneOffsetFromMoscow(t.Name, now)
		if !ok || off == t.Offset {
			continue
		}
		if err := b.db.PatchTrainingStateTimezoneOffsetIfExists(t.UserID, packChatID, off); err != nil {
			b.logger.Warnf("timezone resync user=%d: %v", t.UserID, err)
			continue
		}
		n++
	}
	if n > 0 {
		b.logger.Infof("timezone resync: поправили смещение у %d", n)
	}
	return n
}
