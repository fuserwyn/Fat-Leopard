package bot

import (
	"database/sql"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"leo-bot/internal/config"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// commandUpdate — обновление Telegram с командой в личке, как его присылает API.
func commandUpdate(userID int64, username, text string) tgbotapi.Update {
	cmd := text
	if i := strings.Index(text, " "); i > 0 {
		cmd = text[:i]
	}
	return tgbotapi.Update{Message: &tgbotapi.Message{
		MessageID: 1,
		From:      &tgbotapi.User{ID: userID, UserName: username, FirstName: "Тест"},
		Chat:      &tgbotapi.Chat{ID: userID, Type: "private"},
		Text:      text,
		Entities:  []tgbotapi.MessageEntity{{Type: "bot_command", Offset: 0, Length: len(utf16.Encode([]rune(cmd)))}},
	}}
}

func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	for i := 0; i < 100; i++ {
		if cond() {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatalf("не дождались: %s", what)
}

func count(t *testing.T, db *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%v\n%s", err, query)
	}
	return n
}

// /start у новичка: вход бесплатный, профиль создаётся сразу, источник
// перехода попадает в аналитику.
func TestStartCommandForNewcomer(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)

	b.handleUpdate(commandUpdate(itUser, "newbie", "/start src-vk_ads"))

	if n := count(t, db, `SELECT COUNT(*) FROM training_state WHERE user_id = $1 AND chat_id = $2 AND NOT COALESCE(is_deleted, FALSE)`, itUser, itPack); n != 1 {
		t.Fatalf("новичок должен сразу оказаться в стае: %d", n)
	}
	texts := strings.Join(tg.textsTo(itUser), "\n---\n")
	if !strings.Contains(texts, "Ура, ты в стае") || strings.Contains(texts, "Ты выбыл из стаи") || strings.Contains(texts, "Способы оплаты") {
		t.Errorf("новичку — приветствие без экрана оплаты: %s", texts)
	}
	if strings.Contains(texts, "Админ-панель") {
		t.Error("обычному участнику про админку не пишем")
	}
	eventually(t, "событие bot_started с источником", func() bool {
		return count(t, db, `SELECT COUNT(*) FROM events WHERE event_name = 'bot_started' AND telegram_id = $1 AND source = 'vk_ads'`, itUser) == 1
	})
	eventually(t, "запись о визите", func() bool {
		return count(t, db, `SELECT COUNT(*) FROM bot_visits WHERE user_id = $1`, itUser) == 1
	})

	// Повторный /start второго профиля не заводит.
	b.handleUpdate(commandUpdate(itUser, "newbie", "/start"))
	if n := count(t, db, `SELECT COUNT(*) FROM training_state WHERE user_id = $1`, itUser); n != 1 {
		t.Errorf("профилей после повторного /start: %d", n)
	}
}

// /start у выбывшего показывает экран оплаты и заводит одну заявку.
func TestStartCommandForKickedMember(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "kicked", true)

	b.handleUpdate(commandUpdate(itUser, "kicked", "/start"))
	b.handleUpdate(commandUpdate(itUser, "kicked", "/start"))

	texts := strings.Join(tg.textsTo(itUser), "\n---\n")
	if !strings.Contains(texts, "Ты выбыл из стаи") || !strings.Contains(texts, "Способы оплаты") || strings.Contains(texts, "Ура, ты в стае") {
		t.Errorf("выбывшему — экран оплаты, а не приветствие: %s", texts)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM paywall_access_requests WHERE user_id = $1 AND status = 'pending'`, itUser); n != 1 {
		t.Errorf("заявок на оплату: %d", n)
	}
	if !isKicked(t, db, itUser) {
		t.Error("/start сам по себе в стаю не возвращает")
	}

	tg.reset()
	b.handleUpdate(commandUpdate(itUser, "kicked", "/help"))
	if texts := strings.Join(tg.textsTo(itUser), "\n"); !strings.Contains(texts, "Ты выбыл из стаи") || strings.Contains(texts, "Справка") {
		t.Errorf("/help у выбывшего — тоже экран оплаты: %s", texts)
	}
	tg.reset()
	b.handleUpdate(commandUpdate(itUser, "kicked", "/rejoin"))
	if texts := strings.Join(tg.textsTo(itUser), "\n"); !strings.Contains(texts, "Сначала оплати доступ") {
		t.Errorf("/rejoin у выбывшего: %s", texts)
	}
}

// Админу /start подсказывает про админ-панель; справка, топ и кубки отвечают.
func TestInfoCommands(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "first", false)
	seedMember(t, db, itUser+1, "second", false)
	if _, err := db.Exec(`UPDATE training_state SET cups_earned = CASE user_id WHEN $1 THEN 300 ELSE 120 END WHERE chat_id = $2`, itUser, itPack); err != nil {
		t.Fatal(err)
	}

	b.handleUpdate(commandUpdate(itAdmin, "boss", "/start"))
	if texts := strings.Join(tg.textsTo(itAdmin), "\n"); !strings.Contains(texts, "Админ-панель") {
		t.Errorf("админу — подсказка про панель: %s", texts)
	}

	tg.reset()
	b.handleUpdate(commandUpdate(itUser, "first", "/help"))
	if texts := tg.textsTo(itUser); len(texts) != 1 || !strings.Contains(texts[0], "Справка") {
		t.Errorf("/help: %q", texts)
	}

	tg.reset()
	b.handleUpdate(commandUpdate(itUser, "first", "/top"))
	top := strings.Join(tg.textsTo(itUser), "\n")
	if !strings.Contains(top, "Топ пользователей") || strings.Index(top, "first") > strings.Index(top, "second") || !strings.Contains(top, "300") {
		t.Errorf("/top — по убыванию кубков: %s", top)
	}

	tg.reset()
	b.handleUpdate(commandUpdate(itUser, "first", "/cups"))
	if texts := strings.Join(tg.textsTo(itUser), "\n"); !strings.Contains(texts, "300") || !strings.Contains(texts, "@first") {
		t.Errorf("/cups: %s", texts)
	}

	tg.reset()
	b.handleUpdate(commandUpdate(itUser, "first", "/rejoin"))
	if texts := strings.Join(tg.textsTo(itUser), "\n"); !strings.Contains(texts, "Доступ активен") {
		t.Errorf("/rejoin у активного: %s", texts)
	}

	// Служебные команды — только админам.
	tg.reset()
	b.handleUpdate(commandUpdate(itUser, "first", "/db"))
	if texts := strings.Join(tg.textsTo(itUser), "\n"); !strings.Contains(texts, "Только администраторы") {
		t.Errorf("/db не-админу: %s", texts)
	}
	tg.reset()
	b.handleUpdate(commandUpdate(itAdmin, "boss", "/db"))
	if texts := strings.Join(tg.textsTo(itAdmin), "\n"); !strings.Contains(texts, "Статистика БД") {
		t.Errorf("/db админу: %s", texts)
	}
}

// Сообщения из групп бот игнорирует; платёжные обновления уходят в нужный
// обработчик — донат и оплата возврата не путаются.
func TestUpdateRouting(t *testing.T) {
	b, tg, db := newIntegrationBot(t, func(c *config.Config) {
		starsConfig(c)
		c.DonateStarsTiers = []int{50}
	})
	seedMember(t, db, itUser, "leopard", true)

	b.handleUpdate(tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: itUser}, Chat: &tgbotapi.Chat{ID: -500, Type: "supergroup"}, Text: "бег, 30 мин, интенсивность 3/5",
	}})
	if len(tg.calls) != 0 || count(t, db, `SELECT COUNT(*) FROM training_sessions`) != 0 {
		t.Fatal("сообщение из группы не должно ничего менять")
	}

	_, donationID, err := b.CreateDonateStarsInvoiceLink(itUser, 50)
	if err != nil {
		t.Fatal(err)
	}
	reqID, _ := b.db.InsertPaywallAccessRequest(itUser, itPack)
	from := &tgbotapi.User{ID: itUser}

	// Донат: закрывается донат, доступ не выдаётся.
	b.handleUpdate(tgbotapi.Update{PreCheckoutQuery: &tgbotapi.PreCheckoutQuery{ID: "p1", From: from, Currency: "XTR", TotalAmount: 50, InvoicePayload: donatePayload(donationID)}})
	b.handleUpdate(tgbotapi.Update{Message: &tgbotapi.Message{From: from, Chat: &tgbotapi.Chat{ID: itUser, Type: "private"},
		SuccessfulPayment: &tgbotapi.SuccessfulPayment{Currency: "XTR", TotalAmount: 50, InvoicePayload: donatePayload(donationID), TelegramPaymentChargeID: "d"}}})
	if st, _, _ := donationRow(t, db, donationID); st != "completed" {
		t.Errorf("донат через общий вход обновлений: %s", st)
	}
	if hasAccess(t, b, itUser) {
		t.Fatal("донат не должен открывать доступ")
	}

	// Оплата возврата: выдаётся доступ.
	b.handleUpdate(tgbotapi.Update{Message: &tgbotapi.Message{From: from, Chat: &tgbotapi.Chat{ID: itUser, Type: "private"},
		SuccessfulPayment: &tgbotapi.SuccessfulPayment{Currency: "XTR", TotalAmount: 350, InvoicePayload: paywallPayloadPrefix + itoa(reqID), TelegramPaymentChargeID: "p"}}})
	if !hasAccess(t, b, itUser) || isKicked(t, db, itUser) {
		t.Fatal("оплата возврата через общий вход обновлений должна вернуть в стаю")
	}
}
