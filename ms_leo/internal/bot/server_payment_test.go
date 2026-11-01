package bot

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"
	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestServerPaymentDue(t *testing.T) {
	loc := moscowLocation()
	cases := []struct {
		d, h int
		want bool
	}{{1, 0, false}, {1, 14, false}, {1, 15, true}, {2, 0, true}, {30, 23, true}}
	for _, c := range cases {
		if got := serverPaymentDue(time.Date(2026, 11, c.d, c.h, 0, 0, 0, loc)); got != c.want {
			t.Errorf("day=%d hour=%d: got %v want %v", c.d, c.h, got, c.want)
		}
	}
}

// Telegram запоминает allowed_updates: кнопка и оплата звёздами должны быть запрошены явно.
func TestGetUpdatesRequestsPaymentUpdates(t *testing.T) {
	got := make(chan string, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/getMe") {
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"t","username":"t"}}`))
			return
		}
		_ = r.ParseForm()
		select {
		case got <- r.FormValue("allowed_updates"):
		default:
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":[]}`))
	}))
	defer srv.Close()
	api, err := tgbotapi.NewBotAPIWithAPIEndpoint("1:T", srv.URL+"/bot%s/%s")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	(&Bot{api: api, logger: logger.New("error")}).runGetUpdatesWithWebApp(ctx)
	select {
	case allowed := <-got:
		for _, want := range []string{"message", "chat_member", "callback_query", "pre_checkout_query"} {
			if !strings.Contains(allowed, `"`+want+`"`) {
				t.Errorf("allowed_updates %q: нет %s", allowed, want)
			}
		}
	case <-time.After(5 * time.Second):
		t.Fatal("getUpdates не вызван")
	}
}

type serverPaymentFakeTG struct {
	mu   sync.Mutex
	sent map[string][]string // chat_id → тексты
	fail bool
}

func (f *serverPaymentFakeTG) handler(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	_ = r.ParseMultipartForm(1 << 20)
	f.mu.Lock()
	defer f.mu.Unlock()
	switch method {
	case "getMe":
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1,"is_bot":true,"first_name":"t","username":"t"}}`))
	case "createInvoiceLink":
		_, _ = w.Write([]byte(`{"ok":true,"result":"https://t.me/$inv"}`))
	case "sendMessage":
		if f.fail {
			_, _ = w.Write([]byte(`{"ok":false,"error_code":403,"description":"bot was blocked"}`))
			return
		}
		f.sent[r.FormValue("chat_id")] = append(f.sent[r.FormValue("chat_id")], r.FormValue("text"))
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"chat":{"id":1}}}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}
}

func (f *serverPaymentFakeTG) texts(id int64) []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.sent[strconv.FormatInt(id, 10)]...)
}

const (
	serverPaymentTestPayer = int64(9001)
	serverPaymentTestOwner = int64(9000)
)

// newServerPaymentTestBot требует LEO_TEST_PG_DSN на отдельную пустую базу: чистит только server_payment_bills.
func newServerPaymentTestBot(t *testing.T, tune func(*config.Config)) (*Bot, *serverPaymentFakeTG) {
	t.Helper()
	dsn := os.Getenv("LEO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("LEO_TEST_PG_DSN не задан")
	}
	db, err := database.New(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if err := db.CreateTables(); err != nil {
		t.Fatal(err)
	}
	for _, month := range []string{"2026-11", "2026-12"} {
		if err := db.DeleteServerPaymentBill(month); err != nil {
			t.Fatal(err)
		}
	}
	tg := &serverPaymentFakeTG{sent: map[string][]string{}}
	srv := httptest.NewServer(http.HandlerFunc(tg.handler))
	t.Cleanup(srv.Close)
	api, err := tgbotapi.NewBotAPIWithAPIEndpoint("1:T", srv.URL+"/bot%s/%s")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		OwnerID: serverPaymentTestOwner, ServerPaymentUserID: serverPaymentTestPayer,
		ServerPaymentRub: 100, ServerPaymentStars: 86, YookassaShopID: "shop", YookassaSecretKey: "secret",
	}
	if tune != nil {
		tune(cfg)
	}
	return &Bot{api: api, db: db, logger: logger.New("error"), config: cfg}, tg
}

// Фиксированный счёт раз в месяц; карта: чужому нельзя, ссылка переиспользуется, оплата закрывает опросом.
func TestServerPaymentCardFlowIntegration(t *testing.T) {
	created, status := 0, "pending"
	oldCreate, oldGet := serverPaymentCreateCard, serverPaymentGetPayment
	defer func() { serverPaymentCreateCard, serverPaymentGetPayment = oldCreate, oldGet }()
	serverPaymentCreateCard = func(_, _ string, amountMinor int, _, _, _, _ string, _ map[string]string) (string, string, error) {
		created++
		if amountMinor != 10000 {
			t.Errorf("сумма платежа: %d", amountMinor)
		}
		return "yk-1", "https://kassa.test/1", nil
	}
	serverPaymentGetPayment = func(_, _, _ string) (*yookassa.PaymentStatusInfo, error) {
		return &yookassa.PaymentStatusInfo{Status: status, Paid: status == "succeeded"}, nil
	}
	b, tg := newServerPaymentTestBot(t, nil)
	now := time.Date(2026, 11, 1, 15, 0, 0, 0, moscowLocation())

	if b.runServerPaymentBilling(now.Add(-time.Hour)) {
		t.Fatal("до 15:00 счёт не выставляем")
	}
	if !b.runServerPaymentBilling(now) {
		t.Fatal("счёт не ушёл")
	}
	if b.runServerPaymentBilling(now.Add(time.Minute)) {
		t.Fatal("второй счёт за месяц")
	}
	got := tg.texts(serverPaymentTestPayer)
	if len(got) != 1 || !strings.Contains(got[0], "за ноябрь 2026: 100 ₽ или 86 ⭐") {
		t.Fatalf("счёт: %v", got)
	}

	press := func(from int64) tgbotapi.Update {
		return tgbotapi.Update{CallbackQuery: &tgbotapi.CallbackQuery{ID: "cb", From: &tgbotapi.User{ID: from}, Data: serverPaymentCardCallbackPrefix + "2026-11"}}
	}
	b.handleUpdate(press(serverPaymentTestPayer + 1))
	if created != 0 || len(tg.texts(serverPaymentTestPayer+1)) != 0 {
		t.Fatal("чужому ссылка не положена")
	}
	b.handleUpdate(press(serverPaymentTestPayer))
	b.handleUpdate(press(serverPaymentTestPayer))
	if created != 1 || len(tg.texts(serverPaymentTestPayer)) != 3 {
		t.Fatalf("повторное нажатие даёт ту же ссылку: created=%d %v", created, tg.texts(serverPaymentTestPayer))
	}

	b.syncServerPaymentCardPayments()
	if len(tg.texts(serverPaymentTestPayer)) != 3 {
		t.Fatal("до оплаты благодарить рано")
	}
	status = "succeeded"
	b.syncServerPaymentCardPayments()
	got = tg.texts(serverPaymentTestPayer)
	if len(got) != 4 || !strings.Contains(got[3], "Счёт за сервер оплачен: 100 ₽") {
		t.Fatalf("после оплаты: %v", got)
	}
	b.handleUpdate(press(serverPaymentTestPayer))
	if created != 1 || len(tg.texts(serverPaymentTestPayer)) != 4 {
		t.Fatal("оплаченный счёт повторно не выставляем")
	}
}

// Тестовый счёт: только владельцу, на небольшую сумму, без предупреждения и без паузы.
func TestServerPaymentTestBillIntegration(t *testing.T) {
	b, tg := newServerPaymentTestBot(t, func(c *config.Config) { c.ServerPaymentSuspendEnabled = true })
	command := func(from int64) *tgbotapi.Message {
		return &tgbotapi.Message{From: &tgbotapi.User{ID: from}, Chat: &tgbotapi.Chat{ID: from, Type: "private"}, Text: "/test_server_bill"}
	}
	b.handleTestServerBill(command(serverPaymentTestPayer))
	if got := tg.texts(serverPaymentTestPayer); len(got) != 0 {
		t.Fatalf("не владельцу счёт не положен: %v", got)
	}
	b.handleTestServerBill(command(serverPaymentTestOwner))
	got := tg.texts(serverPaymentTestOwner)
	if len(got) != 1 || !strings.Contains(got[0], "за тест: 10 ₽ или 1 ⭐") || strings.Contains(got[0], "пауз") {
		t.Fatalf("тестовый счёт: %v", got)
	}

	month := serverPaymentTestMonthPrefix + "overdue"
	t.Cleanup(func() { _ = b.db.DeleteServerPaymentBill(month) })
	now := time.Now()
	if err := b.db.DeleteServerPaymentBill(month); err != nil {
		t.Fatal(err)
	}
	if _, err := b.db.InsertServerPaymentBill(database.ServerPaymentBill{Month: month, UserID: serverPaymentTestOwner, Stars: 1, Rub: 1, SentAt: now.Add(-72 * time.Hour)}); err != nil {
		t.Fatal(err)
	}
	b.refreshServerPaymentSuspension(now)
	if b.serverPaymentSuspended.Load() {
		t.Fatal("тестовый счёт не включает паузу")
	}
}

// Личка закрыта: счёт не теряется и пауза не включается, следующий тик пробует снова.
func TestServerPaymentRetriesIntegration(t *testing.T) {
	b, tg := newServerPaymentTestBot(t, func(c *config.Config) { c.ServerPaymentSuspendEnabled = true })
	now := time.Date(2026, 11, 1, 15, 0, 0, 0, moscowLocation())
	tg.fail = true
	if b.runServerPaymentBilling(now) {
		t.Fatal("отправка не удалась")
	}
	b.refreshServerPaymentSuspension(now.Add(72 * time.Hour))
	if b.serverPaymentSuspended.Load() {
		t.Fatal("счёт не дошёл — платить нечего, паузы нет")
	}
	tg.fail = false
	if !b.runServerPaymentBilling(now.Add(5 * time.Minute)) {
		t.Fatal("счёт должен уйти после восстановления")
	}
}

// Сутки без оплаты: Telegram на паузе, авто-кики стоят, звёздами оплатить можно, и пауза сразу снимается.
func TestServerPaymentSuspensionIntegration(t *testing.T) {
	b, tg := newServerPaymentTestBot(t, func(c *config.Config) { c.ServerPaymentSuspendEnabled = true })
	const member = int64(9005)
	now := time.Now().In(moscowLocation())
	month := now.Format("2006-01")
	if err := b.db.DeleteServerPaymentBill(month); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = b.db.DeleteServerPaymentBill(month) })
	if _, err := b.db.InsertServerPaymentBill(database.ServerPaymentBill{Month: month, UserID: serverPaymentTestPayer, Stars: 86, SentAt: now}); err != nil {
		t.Fatal(err)
	}

	b.refreshServerPaymentSuspension(now.Add(23 * time.Hour))
	if b.serverPaymentSuspended.Load() {
		t.Fatal("сутки ещё не прошли")
	}
	later := now.Add(serverPaymentSuspendGrace + time.Minute)
	b.refreshServerPaymentSuspension(later)
	b.refreshServerPaymentSuspension(later)
	if !b.serverPaymentSuspended.Load() {
		t.Fatal("счёт просрочен — бот на паузе")
	}
	if got := tg.texts(serverPaymentTestPayer); len(got) != 1 || !strings.Contains(got[0], "бот на паузе") {
		t.Fatalf("плательщик предупреждён один раз: %v", got)
	}
	if got := tg.texts(serverPaymentTestOwner); len(got) != 1 || !strings.Contains(got[0], month) {
		t.Fatalf("владелец предупреждён один раз: %v", got)
	}

	b.handleUpdate(tgbotapi.Update{Message: &tgbotapi.Message{From: &tgbotapi.User{ID: member}, Chat: &tgbotapi.Chat{ID: member, Type: "private"}, Text: "привет"}})
	if got := tg.texts(member); len(got) != 1 || !strings.Contains(got[0], "Бот на паузе") {
		t.Fatalf("ответ в паузе: %v", got)
	}
	if !b.dropUpdateWhileSuspended(tgbotapi.Update{PreCheckoutQuery: &tgbotapi.PreCheckoutQuery{InvoicePayload: "other"}}) {
		t.Error("чужой pre_checkout в паузе не обрабатываем")
	}
	if b.dropUpdateWhileSuspended(tgbotapi.Update{PreCheckoutQuery: &tgbotapi.PreCheckoutQuery{InvoicePayload: serverPaymentPayloadPrefix + month}}) {
		t.Error("оплата счёта проходит и в паузе")
	}
	b.removeUserForInactivity(member, 1, "member") // на паузе просто выходит, без обращения к базе и API

	b.handleUpdate(tgbotapi.Update{Message: &tgbotapi.Message{
		From: &tgbotapi.User{ID: serverPaymentTestPayer}, Chat: &tgbotapi.Chat{ID: serverPaymentTestPayer, Type: "private"},
		SuccessfulPayment: &tgbotapi.SuccessfulPayment{Currency: "XTR", TotalAmount: 86, InvoicePayload: serverPaymentPayloadPrefix + month, TelegramPaymentChargeID: "ch"},
	}})
	if b.serverPaymentSuspended.Load() {
		t.Fatal("оплата снимает паузу сразу")
	}
	if got := tg.texts(serverPaymentTestPayer); len(got) != 2 || !strings.Contains(got[1], "оплачен: 86 ⭐") {
		t.Fatalf("после оплаты: %v", got)
	}
	if got := tg.texts(serverPaymentTestOwner); len(got) != 2 || !strings.Contains(got[1], "снова работает") {
		t.Fatalf("владельцу о снятии паузы: %v", fmt.Sprint(got))
	}
}
