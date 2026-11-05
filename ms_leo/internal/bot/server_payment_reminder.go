package bot

import (
	"context"
	"fmt"
	"math"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Напоминание админам об оплате ресурсов сервера: 20-го числа каждого месяца (МСК)
// одному админу по очереди. Очередь — SERVER_PAYMENT_ADMIN_IDS (по порядку),
// первый в ней платит в ноябре 2026, второй — в декабре, третий — в январе, и снова по кругу.
// В сообщении — кнопка оплаты звёздами на сумму расхода Railway по курсу USD_PER_STAR.
// Railway не ответил — берём SERVER_PAYMENT_STARS; и его нет — тариф Railway Hobby ($5).
// Ссылка на оплату приходит всегда: если Telegram не выдал счёт, напоминание
// откладывается до следующего тика и только после serverPaymentLinkRetryDays уходит без кнопки.

const (
	serverPaymentReminderDay  = 20
	serverPaymentReminderHour = 10 // с 10:00 МСК, чтобы не будить ночью
	serverPaymentReminderTick = 5 * time.Minute
	// serverPaymentDefaultUSD — запасная сумма, если ни Railway, ни SERVER_PAYMENT_STARS нет: тариф Hobby.
	serverPaymentDefaultUSD = 5.0
	// serverPaymentDefaultUsdPerStar — курс звезды, если USD_PER_STAR не задан (как в config).
	serverPaymentDefaultUsdPerStar = 0.013
	// serverPaymentLinkRetryDays — сколько дней после 20-го ждём ссылку на счёт, прежде чем слать без неё.
	serverPaymentLinkRetryDays = 3
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

// serverPaymentStarsForCost — сколько звёзд покрывает расход; округляем вверх, чтобы хватило.
func serverPaymentStarsForCost(usd, usdPerStar float64) int {
	if usd <= 0 || usdPerStar <= 0 {
		return 0
	}
	return int(math.Ceil(usd / usdPerStar))
}

// serverPaymentAmount — сумма счёта: расход Railway в звёздах, иначе SERVER_PAYMENT_STARS.
// usd = 0 — расход неизвестен, сумма запасная.
func (b *Bot) serverPaymentAmount() (stars int, usd float64) {
	if strings.TrimSpace(b.config.RailwayToken) != "" && strings.TrimSpace(b.config.RailwayProjectID) != "" {
		_, total, err := b.railwayCost()
		if err != nil {
			b.logger.Errorf("server payment amount: railway: %v", err)
		} else if n := serverPaymentStarsForCost(total, b.config.UsdPerStar); n > 0 {
			return n, total
		}
	}
	if b.config.ServerPaymentStars > 0 {
		return b.config.ServerPaymentStars, 0
	}
	rate := b.config.UsdPerStar
	if rate <= 0 {
		rate = serverPaymentDefaultUsdPerStar
	}
	return serverPaymentStarsForCost(serverPaymentDefaultUSD, rate), 0
}

// serverPaymentWaitLink — счёт не создался: подождать и повторить, а не слать напоминание без ссылки.
func serverPaymentWaitLink(now time.Time, stars int, link string) bool {
	return link == "" && stars > 0 && now.Day() < serverPaymentReminderDay+serverPaymentLinkRetryDays
}

// serverPaymentAmountLine — строка с суммой в напоминании.
func serverPaymentAmountLine(stars int, usd float64) string {
	if stars <= 0 {
		return ""
	}
	if usd <= 0 {
		return fmt.Sprintf("\n\nК оплате: %d ⭐", stars)
	}
	return fmt.Sprintf("\n\nРасход Railway: $%.2f — к оплате %d ⭐", usd, stars)
}

// serverPaymentReminderMessage — сообщение админу; link пустой — без кнопки оплаты.
func serverPaymentReminderMessage(adminID int64, text, link string, stars int) tgbotapi.MessageConfig {
	m := tgbotapi.NewMessage(adminID, text)
	if link != "" {
		m.ReplyMarkup = tgbotapi.NewInlineKeyboardMarkup(tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonURL(fmt.Sprintf("Оплатить %d ⭐", stars), link),
		))
	}
	return m
}

// serverPaymentInvoiceLink — счёт в звёздах за месяц; без ссылки напоминание всё равно уходит.
func (b *Bot) serverPaymentInvoiceLink(adminID int64, month time.Time, stars int) string {
	if stars <= 0 {
		return ""
	}
	donationID, err := b.db.InsertDonation(adminID, "stars", stars, "XTR")
	if err != nil {
		b.logger.Errorf("server payment invoice admin=%d: %v", adminID, err)
		return ""
	}
	period := fmt.Sprintf("%s %d", monthNameNominative(month.Month()), month.Year())
	link, err := b.createStarsInvoiceLink(donationID, stars, "Сервер Fat Leopard", "Оплата сервера Fat Leopard", "Оплата ресурсов сервера за "+period)
	if err != nil {
		b.logger.Errorf("server payment invoice admin=%d: %v", adminID, err)
		return ""
	}
	return link
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
	stars, usd := b.serverPaymentAmount()
	link := b.serverPaymentInvoiceLink(adminID, now, stars)
	if serverPaymentWaitLink(now, stars, link) {
		b.logger.Errorf("server payment reminder %s: no invoice link, retry later", month)
		if err := b.db.ReleaseServerPaymentReminder(month); err != nil {
			b.logger.Errorf("server payment reminder %s release: %v", month, err)
		}
		return false
	}
	text := serverPaymentReminderText(now, next)
	if link != "" {
		text += serverPaymentAmountLine(stars, usd)
	} else if stars > 0 {
		text += "\n\nСсылку на оплату создать не удалось — оплати напрямую в Railway."
	}
	if _, err := b.api.Send(serverPaymentReminderMessage(adminID, text, link, stars)); err != nil {
		b.logger.Errorf("server payment reminder %s to %d: %v", month, adminID, err)
		if err := b.db.ReleaseServerPaymentReminder(month); err != nil {
			b.logger.Errorf("server payment reminder %s release: %v", month, err)
		}
		return false
	}
	b.logger.Infof("server payment reminder %s sent to admin %d", month, adminID)
	return true
}
