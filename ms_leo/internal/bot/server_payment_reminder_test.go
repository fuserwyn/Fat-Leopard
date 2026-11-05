package bot

import (
	"strings"
	"testing"
	"time"

	"leo-bot/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestServerPaymentAdminIndex(t *testing.T) {
	cases := []struct {
		y    int
		m    time.Month
		want int
	}{
		{2026, time.November, 0}, // Стас
		{2026, time.December, 1}, // Анна
		{2027, time.January, 2},  // lofi
		{2027, time.February, 0}, // снова Стас
		{2027, time.March, 1},
		{2026, time.October, 2}, // до старта очередь идёт по тому же кругу
	}
	for _, c := range cases {
		got := serverPaymentAdminIndex(time.Date(c.y, c.m, 20, 12, 0, 0, 0, moscowLocation()), 3)
		if got != c.want {
			t.Errorf("%d-%02d: got %d want %d", c.y, c.m, got, c.want)
		}
	}
	if serverPaymentAdminIndex(time.Now(), 0) != -1 {
		t.Error("пустая очередь → -1")
	}
}

func TestServerPaymentReminderDue(t *testing.T) {
	loc := moscowLocation()
	cases := []struct {
		d, h int
		want bool
	}{
		{19, 23, false},
		{20, 9, false},
		{20, 10, true},
		{21, 0, true},
		{1, 12, false},
	}
	for _, c := range cases {
		if got := serverPaymentReminderDue(time.Date(2026, 11, c.d, c.h, 0, 0, 0, loc)); got != c.want {
			t.Errorf("day=%d hour=%d: got %v want %v", c.d, c.h, got, c.want)
		}
	}
}

func TestServerPaymentReminderText(t *testing.T) {
	s := serverPaymentReminderText(time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC), "Анна")
	if !strings.Contains(s, "ноябрь 2026") || !strings.Contains(s, "В следующем месяце — Анна.") {
		t.Fatalf("текст: %q", s)
	}
	if strings.Contains(serverPaymentReminderText(time.Date(2027, 1, 20, 0, 0, 0, 0, time.UTC), ""), "следующем") {
		t.Fatal("без следующего админа строки быть не должно")
	}
	var nilBot *Bot
	if nilBot.runServerPaymentReminder(time.Now()) {
		t.Fatal("nil bot")
	}
}

func TestServerPaymentReminderRotationIntegration(t *testing.T) {
	const stas, anna, lofi = int64(9001), int64(9002), int64(9003)
	b, tg, _ := newIntegrationBot(t, func(c *config.Config) {
		c.ServerPaymentAdminIDs = []int64{stas, anna, lofi}
	})
	loc := moscowLocation()

	if b.runServerPaymentReminder(time.Date(2026, 11, 19, 12, 0, 0, 0, loc)) {
		t.Fatal("19-го слать рано")
	}
	if !b.runServerPaymentReminder(time.Date(2026, 11, 20, 10, 5, 0, 0, loc)) {
		t.Fatal("20 ноября — напоминание Стасу")
	}
	if b.runServerPaymentReminder(time.Date(2026, 11, 20, 10, 10, 0, 0, loc)) {
		t.Fatal("повтор в тот же месяц не шлём")
	}
	if got := tg.textsTo(stas); len(got) != 1 || !strings.Contains(got[0], "ноябрь 2026") || !strings.Contains(got[0], "К оплате: 385 ⭐") {
		t.Fatalf("Стас: %v", got)
	}
	// Без Railway и SERVER_PAYMENT_STARS ссылка всё равно есть — на тариф Hobby.
	if msgs := tg.sent("sendMessage"); len(msgs) != 1 || !strings.Contains(msgs[0].Form.Get("reply_markup"), "https://t.me/$invoice-test") {
		t.Fatalf("кнопка оплаты Стасу: %+v", msgs)
	}

	// Декабрь — Анна, но Telegram отвечает ошибкой: отметка снимается, следующий тик пробует снова.
	tg.fail["sendMessage"] = true
	if b.runServerPaymentReminder(time.Date(2026, 12, 20, 11, 0, 0, 0, loc)) {
		t.Fatal("ошибка отправки — не отправлено")
	}
	tg.fail["sendMessage"] = false
	if !b.runServerPaymentReminder(time.Date(2026, 12, 20, 11, 5, 0, 0, loc)) {
		t.Fatal("повтор после ошибки — Анне")
	}
	// Январь — lofi (бот лежал 20-го, догоняем 22-го).
	if !b.runServerPaymentReminder(time.Date(2027, 1, 22, 9, 0, 0, 0, loc)) {
		t.Fatal("январь — lofi")
	}
	// Февраль — снова Стас.
	if !b.runServerPaymentReminder(time.Date(2027, 2, 20, 10, 0, 0, 0, loc)) {
		t.Fatal("февраль — Стас")
	}
	if n := len(tg.textsTo(stas)); n != 2 {
		t.Fatalf("Стасу должно прийти 2 напоминания, пришло %d", n)
	}
	if n := len(tg.textsTo(lofi)); n != 1 {
		t.Fatalf("lofi: %d", n)
	}
	// Анне: одна неудачная попытка (запрос ушёл) + одна успешная.
	if n := len(tg.textsTo(anna)); n != 2 {
		t.Fatalf("Анна: %d", n)
	}
}

func TestServerPaymentReminderMessageButton(t *testing.T) {
	if m := serverPaymentReminderMessage(1, "текст", "", 0); m.ReplyMarkup != nil {
		t.Error("без ссылки кнопки нет")
	}
	m := serverPaymentReminderMessage(1, "текст", "https://t.me/$inv", 500)
	kb, ok := m.ReplyMarkup.(tgbotapi.InlineKeyboardMarkup)
	if !ok || len(kb.InlineKeyboard) != 1 || kb.InlineKeyboard[0][0].Text != "Оплатить 500 ⭐" || *kb.InlineKeyboard[0][0].URL != "https://t.me/$inv" {
		t.Errorf("кнопка оплаты: %+v", m.ReplyMarkup)
	}
}

func TestServerPaymentStarsForCost(t *testing.T) {
	cases := []struct {
		usd, rate float64
		want      int
	}{
		{13.34, 0.013, 1027}, // округляем вверх, чтобы расход покрылся
		{13, 0.013, 1000},
		{0, 0.013, 0},
		{13.34, 0, 0},
	}
	for _, c := range cases {
		if got := serverPaymentStarsForCost(c.usd, c.rate); got != c.want {
			t.Errorf("$%.2f по %.3f: got %d want %d", c.usd, c.rate, got, c.want)
		}
	}
}

func TestServerPaymentAmountLine(t *testing.T) {
	if got := serverPaymentAmountLine(0, 0); got != "" {
		t.Errorf("без суммы: %q", got)
	}
	if got := serverPaymentAmountLine(500, 0); !strings.Contains(got, "К оплате: 500 ⭐") {
		t.Errorf("запасная сумма: %q", got)
	}
	if got := serverPaymentAmountLine(1027, 13.34); !strings.Contains(got, "$13.34") || !strings.Contains(got, "1027 ⭐") {
		t.Errorf("расход Railway: %q", got)
	}
}

func TestServerPaymentReminderRetriesInvoiceLink(t *testing.T) {
	const stas = int64(9101)
	b, tg, _ := newIntegrationBot(t, func(c *config.Config) {
		c.ServerPaymentAdminIDs = []int64{stas}
		c.ServerPaymentStars = 700
	})
	loc := moscowLocation()

	// Telegram не выдал счёт — напоминание без ссылки не шлём, ждём следующего тика.
	tg.fail["createInvoiceLink"] = true
	if b.runServerPaymentReminder(time.Date(2026, 11, 20, 10, 0, 0, 0, loc)) {
		t.Fatal("без ссылки 20-го не шлём")
	}
	if got := tg.textsTo(stas); len(got) != 0 {
		t.Fatalf("напоминание без ссылки: %v", got)
	}
	tg.fail["createInvoiceLink"] = false
	if !b.runServerPaymentReminder(time.Date(2026, 11, 20, 10, 5, 0, 0, loc)) {
		t.Fatal("счёт создался — напоминание уходит")
	}
	msgs := tg.sent("sendMessage")
	if len(msgs) != 1 || !strings.Contains(msgs[0].Form.Get("text"), "К оплате: 700 ⭐") ||
		!strings.Contains(msgs[0].Form.Get("reply_markup"), "Оплатить 700") {
		t.Fatalf("напоминание с кнопкой: %+v", msgs)
	}

	// Декабрь: счёт так и не создаётся — после окна повторов шлём без кнопки, но с пояснением.
	tg.reset()
	tg.fail["createInvoiceLink"] = true
	if b.runServerPaymentReminder(time.Date(2026, 12, 22, 23, 0, 0, 0, loc)) {
		t.Fatal("22-го ещё ждём ссылку")
	}
	if !b.runServerPaymentReminder(time.Date(2026, 12, 23, 10, 0, 0, 0, loc)) {
		t.Fatal("23-го шлём без ссылки")
	}
	msgs = tg.sent("sendMessage")
	if len(msgs) != 1 || msgs[0].Form.Get("reply_markup") != "" || !strings.Contains(msgs[0].Form.Get("text"), "Ссылку на оплату создать не удалось") {
		t.Fatalf("напоминание без ссылки: %+v", msgs)
	}
}

func TestServerPaymentWaitLink(t *testing.T) {
	loc := moscowLocation()
	d20 := time.Date(2026, 11, 20, 10, 0, 0, 0, loc)
	if !serverPaymentWaitLink(d20, 385, "") {
		t.Error("20-го без ссылки ждём")
	}
	if serverPaymentWaitLink(d20, 385, "https://t.me/$inv") || serverPaymentWaitLink(d20, 0, "") {
		t.Error("есть ссылка или нет суммы — не ждём")
	}
	if serverPaymentWaitLink(time.Date(2026, 11, 23, 0, 0, 0, 0, loc), 385, "") {
		t.Error("после окна повторов не ждём")
	}
}
