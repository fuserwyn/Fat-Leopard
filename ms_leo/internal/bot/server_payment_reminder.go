package bot

import (
	"context"
	"fmt"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Напоминание админам об оплате ресурсов сервера: 20-го числа каждого месяца (МСК)
// одному админу по очереди. Очередь — SERVER_PAYMENT_ADMIN_IDS (по порядку),
// первый в ней платит в ноябре 2026, второй — в декабре, третий — в январе, и снова по кругу.

const (
	serverPaymentReminderDay  = 20
	serverPaymentReminderHour = 10 // с 10:00 МСК, чтобы не будить ночью
	serverPaymentReminderTick = 5 * time.Minute
)

// serverPaymentRotationStart — месяц, когда платит первый админ очереди.
var serverPaymentRotationStart = time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC)

// serverPaymentAdminIndex — номер админа в очереди для месяца t (по кругу из n).
func serverPaymentAdminIndex(t time.Time, n int) int {
	if n <= 0 {
		return -1
	}
	months := (t.Year()-serverPaymentRotationStart.Year())*12 + int(t.Month()) - int(serverPaymentRotationStart.Month())
	idx := months % n
	if idx < 0 {
		idx += n
	}
	return idx
}

// serverPaymentReminderDue — пора ли слать напоминание за месяц now (время МСК).
// Позже 20-го тоже пора: если бот лежал 20-го, напоминание придёт после подъёма.
func serverPaymentReminderDue(now time.Time) bool {
	if now.Day() > serverPaymentReminderDay {
		return true
	}
	return now.Day() == serverPaymentReminderDay && now.Hour() >= serverPaymentReminderHour
}

func serverPaymentReminderText(month time.Time, nextAdminLabel string) string {
	text := fmt.Sprintf("💳 Напоминание: пора оплатить ресурсы сервера Fat Leopard за %s %d.\n\nВ этом месяце платишь ты.",
		monthNameNominative(month.Month()), month.Year())
	if nextAdminLabel != "" {
		text += "\nВ следующем месяце — " + nextAdminLabel + "."
	}
	return text
}

func monthNameNominative(m time.Month) string {
	names := []string{"январь", "февраль", "март", "апрель", "май", "июнь",
		"июль", "август", "сентябрь", "октябрь", "ноябрь", "декабрь"}
	return names[m-1]
}

func (b *Bot) startServerPaymentReminderScheduler(ctx context.Context) {
	if b == nil {
		return
	}
	ticker := time.NewTicker(serverPaymentReminderTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.runServerPaymentReminder(time.Now())
		}
	}
}

// runServerPaymentReminder шлёт напоминание очередному админу, если пора и за месяц ещё не слали.
// true — напоминание отправлено сейчас.
func (b *Bot) runServerPaymentReminder(now time.Time) bool {
	if b == nil || b.db == nil || b.api == nil || b.config == nil {
		return false
	}
	now = now.In(moscowLocation())
	if !serverPaymentReminderDue(now) {
		return false
	}
	admins := b.config.ServerPaymentRotation()
	idx := serverPaymentAdminIndex(now, len(admins))
	if idx < 0 {
		return false
	}
	adminID := admins[idx]
	month := now.Format("2006-01")
	claimed, err := b.db.ClaimServerPaymentReminder(month, adminID)
	if err != nil {
		b.logger.Errorf("server payment reminder %s: %v", month, err)
		return false
	}
	if !claimed {
		return false
	}
	next := ""
	if len(admins) > 1 {
		next = b.supportDisplayName(admins[(idx+1)%len(admins)])
	}
	if _, err := b.api.Send(tgbotapi.NewMessage(adminID, serverPaymentReminderText(now, next))); err != nil {
		b.logger.Errorf("server payment reminder %s to %d: %v", month, adminID, err)
		if err := b.db.ReleaseServerPaymentReminder(month); err != nil {
			b.logger.Errorf("server payment reminder %s release: %v", month, err)
		}
		return false
	}
	b.logger.Infof("server payment reminder %s sent to admin %d", month, adminID)
	return true
}
