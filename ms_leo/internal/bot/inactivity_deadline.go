package bot

import (
	"fmt"
	"strings"
	"time"

	"leo-bot/internal/domain"
	"leo-bot/internal/i18n"
	"leo-bot/internal/utils"
)

// inactiveWarningLocalHour — локальный час отправки предупреждений о неактивности (day 5/6/7).
const inactiveWarningLocalHour = 9

// userLocalLoc — фиксированная зона юзера: UTC+3 (МСК) + смещение относительно МСК.
func userLocalLoc(tzOffsetFromMoscow int) *time.Location {
	return time.FixedZone("UserLocal", (3+tzOffsetFromMoscow)*3600)
}

// localDailySendDue — пора ли отправить ежедневное сообщение (напоминание, мудрость days),
// которое должно прийти в hour:minute по локальному времени юзера, и какую дату отметить
// отправленной. Окно — от hour:minute до конца часа. Идемпотентность — по ЛОКАЛЬНОЙ дате:
// с датой МСК при смене пояса сообщение терялось (Белград → Москва: в 23:00 по Белграду
// уже наступили сутки МСК, и напоминание в 23:00 МСК в day возвращения не приходило).
func localDailySendDue(now time.Time, tzOffsetFromMoscow, hour, minute int, lastSentDate string) (bool, string) {
	localNow := now.In(userLocalLoc(tzOffsetFromMoscow))
	today := localNow.Format("2006-01-02")
	if localNow.Hour() != hour || localNow.Minute() < minute {
		return false, today
	}
	if strings.TrimSpace(lastSentDate) == today {
		return false, today
	}
	return true, today
}

// inactiveWarningAtLocal — 9:00 локального TZ в календарный day, за hoursBefore до кика в полночь.
func inactiveWarningAtLocal(removalAt time.Time, hoursBefore int, loc *time.Location) time.Time {
	t := removalAt.Add(-time.Duration(hoursBefore) * time.Hour).In(loc)
	y, m, d := t.Date()
	return time.Date(y, m, d, inactiveWarningLocalHour, 0, 0, 0, loc)
}

// removalDeadlineLocal — момент кика за неактивность: 00:00 локального TZ юзера
// **следующего** календарного days после days, на который приходится (lastTraining + 7×24ч).
func removalDeadlineLocal(lastTraining time.Time, tzOffsetFromMoscow int) time.Time {
	loc := userLocalLoc(tzOffsetFromMoscow)
	t := lastTraining.In(loc).Add(7 * 24 * time.Hour)
	y, m, d := t.Date()
	startOfThatCalendarDay := time.Date(y, m, d, 0, 0, 0, 0, loc)
	return startOfThatCalendarDay.AddDate(0, 0, 1)
}

// nextCalendarMidnightAfterMoscow — 00:00 МСК **следующего** календарного days относительно момента t (как «до конца суток после #healthy»).
func nextCalendarMidnightAfterMoscow(t time.Time) time.Time {
	loc := utils.GetMoscowTime().Location()
	d := t.In(loc)
	y, m, day := d.Date()
	startOfDay := time.Date(y, m, day, 0, 0, 0, 0, loc)
	return startOfDay.AddDate(0, 0, 1)
}

// inactivityKickDeadline — абсолютный момент удаления из стаи за неактивность (если не будет #training_done), по правилам больничного.
func inactivityKickDeadline(ml *domain.MessageLog, now time.Time) (time.Time, bool) {
	if ml == nil || ml.TimerStartTime == nil {
		return time.Time{}, false
	}
	if ml.IsExemptFromDeletion || ml.IsDeleted {
		return time.Time{}, false
	}
	ts := *ml.TimerStartTime
	if ts == "" {
		return time.Time{}, false
	}
	timerStart, err := utils.ParseMoscowTime(ts)
	if err != nil {
		return time.Time{}, false
	}
	D0 := removalDeadlineLocal(timerStart, ml.TimezoneOffsetFromMoscow)

	// Активный больничный: дедлайн сдвигается на время болезни (таймер «заморожен»).
	if ml.HasSickLeave && !ml.HasHealthy && ml.SickLeaveStartTime != nil {
		sickStart, err2 := utils.ParseMoscowTime(*ml.SickLeaveStartTime)
		if err2 != nil {
			return time.Time{}, false
		}
		return D0.Add(now.Sub(sickStart)), true
	}

	// После #healthy: дедлайн = D₀ + длительность больничного (= момент выхода + остаток на старте больничного).
	if ml.SickLeaveStartTime != nil && ml.SickLeaveEndTime != nil && ml.HasHealthy {
		sickStart, e1 := utils.ParseMoscowTime(*ml.SickLeaveStartTime)
		sickEnd, e2 := utils.ParseMoscowTime(*ml.SickLeaveEndTime)
		if e1 != nil || e2 != nil {
			return D0, true
		}
		if timerStart.After(sickEnd) {
			return D0, true
		}
		frozenRemaining := D0.Sub(sickStart)
		if frozenRemaining < 0 {
			frozenRemaining = 0
		}
		shifted := sickEnd.Add(frozenRemaining)
		grace := nextCalendarMidnightAfterMoscow(sickEnd)
		if shifted.After(grace) {
			return shifted, true
		}
		return grace, true
	}

	// Защита от рассинхрона флагов больничного (легаси-импорт с обоими флагами,
	// has_healthy без end_time и т.п.): если по данным юзер «вышел из больничного»,
	// но корректный дедлайн не посчитался, НЕ кикаем мгновенно по старому D0 —
	// даём грейс минимум до ближайшей полуночи после now.
	if ml.HasHealthy && ml.SickLeaveStartTime != nil {
		grace := nextCalendarMidnightAfterMoscow(now)
		if D0.Before(grace) {
			return grace, true
		}
	}

	return D0, true
}

// formatInactivityRemovalSummary — остаток до кика и локальный дедлайн (как в профиле мини-аппа).
func (b *Bot) formatInactivityRemovalSummary(lang i18n.Lang, ml *domain.MessageLog) (remaining time.Duration, remainingText, deadlineLocal string) {
	if b == nil || ml == nil {
		return 0, "", ""
	}
	remaining = b.calculateRemainingTime(ml)
	remainingText = b.formatDurationLang(lang, remaining)
	now := utils.GetMoscowTime()
	if dl, ok := inactivityKickDeadline(ml, now); ok && remaining > 0 {
		loc := userLocalLoc(ml.TimezoneOffsetFromMoscow)
		deadlineLocal = dl.In(loc).Format("02.01.2006, 15:04")
	}
	return remaining, remainingText, deadlineLocal
}

// formatDurationLang — «N дней M ч.» / «N days M h.» на языке получателя.
func (b *Bot) formatDurationLang(lang i18n.Lang, duration time.Duration) string {
	if !socialRU(lang) {
		return b.formatDurationToDays(duration)
	}
	days := int(duration.Hours() / 24)
	hours := int(duration.Hours()) % 24
	minutes := int(duration.Minutes()) % 60
	switch {
	case days > 0 && hours > 0:
		return fmt.Sprintf("%d %s %d ч.", days, daysWordFormLang(lang, days), hours)
	case days > 0:
		return fmt.Sprintf("%d %s", days, daysWordFormLang(lang, days))
	case hours > 0 && minutes > 0:
		return fmt.Sprintf("%d ч. %d мин.", hours, minutes)
	case hours > 0:
		return fmt.Sprintf("%d ч.", hours)
	default:
		return fmt.Sprintf("%d мин.", minutes)
	}
}

func sickLeaveRemovalNotice(lang i18n.Lang, remainingText, deadlineLocal string, afterRecovery bool) string {
	var b strings.Builder
	if afterRecovery {
		b.WriteString(pickLang(lang, "⏳ До удаления осталось: ", "⏳ Time until removal: "))
	} else {
		b.WriteString(pickLang(lang, "⏳ До удаления: ", "⏳ Until removal: "))
	}
	b.WriteString(remainingText)
	if !afterRecovery {
		b.WriteString(pickLang(lang, " — столько останется после выздоровления", " — that much remains after recovery"))
	}
	if deadlineLocal != "" {
		b.WriteString(pickLang(lang, "\n📅 Крайний срок: ", "\n📅 Deadline: "))
		b.WriteString(deadlineLocal)
		b.WriteString(pickLang(lang, " (твоё время)", " (your local time)"))
	}
	return b.String()
}
