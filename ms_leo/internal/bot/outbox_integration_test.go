package bot

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"leo-bot/internal/yookassa"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

func TestOutboxBackoffAndAttempts(t *testing.T) {
	if got := outboxBackoffDuration("paywall_access_restore_requested", 1); got != 5*time.Second {
		t.Errorf("первая пауза выдачи доступа: %s", got)
	}
	if got := outboxBackoffDuration("refund_requested", 1); got != 10*time.Second {
		t.Errorf("первая пауза возврата: %s", got)
	}
	if got := outboxBackoffDuration("refund_requested", 3); got != 90*time.Second {
		t.Errorf("пауза растёт втрое: %s", got)
	}
	if got := outboxBackoffDuration("refund_requested", 20); got != 30*time.Minute {
		t.Errorf("пауза ограничена получасом: %s", got)
	}
	if outboxMaxAttemptsForType("paywall_access_restore_requested") != 4 || outboxMaxAttemptsForType("refund_requested") != 3 || outboxMaxAttemptsForType("прочее") != 8 {
		t.Error("число попыток по типам событий")
	}
	if !isNonRetryableOutboxErr(nonRetryableOutboxError{msg: "x"}) || isNonRetryableOutboxErr(fmt.Errorf("x")) || isNonRetryableOutboxErr(nil) {
		t.Error("какие ошибки не повторяем")
	}
}

type outboxRow struct {
	Type, Status string
	Attempts     int
}

func outboxRows(t *testing.T, db *sql.DB) []outboxRow {
	t.Helper()
	rows, err := db.Query(`SELECT event_type, status, attempts FROM outbox_events ORDER BY id`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []outboxRow
	for rows.Next() {
		var r outboxRow
		if err := rows.Scan(&r.Type, &r.Status, &r.Attempts); err != nil {
			t.Fatal(err)
		}
		out = append(out, r)
	}
	return out
}

// paidStarsRequestWithFailedDelivery — деньги списаны, а приветствие отправить
// не удалось: выдача доступа уходит в очередь на повтор.
func paidStarsRequestWithFailedDelivery(t *testing.T, b *Bot, tg *fakeTelegram, db *sql.DB) int64 {
	t.Helper()
	seedMember(t, db, itUser, "leopard", true)
	reqID, err := b.db.InsertPaywallAccessRequest(itUser, itPack)
	if err != nil {
		t.Fatal(err)
	}
	tg.fail["sendMessage"] = true
	b.handlePaywallSuccessfulPayment(&tgbotapi.Message{
		From: &tgbotapi.User{ID: itUser, UserName: "leopard"},
		SuccessfulPayment: &tgbotapi.SuccessfulPayment{
			Currency: "XTR", TotalAmount: 350, InvoicePayload: fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID), TelegramPaymentChargeID: "charge-77",
		},
	})
	rows := outboxRows(t, db)
	if len(rows) != 1 || rows[0].Type != "paywall_access_restore_requested" || rows[0].Status != "pending" {
		t.Fatalf("сбой выдачи должен поставить повтор в очередь: %+v", rows)
	}
	return reqID
}

// Telegram был недоступен в момент оплаты, потом ожил: очередь довыдаёт доступ.
func TestOutboxDeliversAccessWhenTelegramRecovers(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	paidStarsRequestWithFailedDelivery(t, b, tg, db)

	b.processOutboxBatch()
	if rows := outboxRows(t, db); rows[0].Status != "retry" || rows[0].Attempts != 1 {
		t.Fatalf("пока Telegram лежит — повтор позже: %+v", rows)
	}
	if !isKicked(t, db, itUser) {
		t.Fatal("без приветствия участника ещё не вернули")
	}

	tg.fail["sendMessage"] = false
	tg.reset()
	if _, err := db.Exec(`UPDATE outbox_events SET next_attempt_at = NOW()`); err != nil {
		t.Fatal(err)
	}
	b.processOutboxBatch()
	if rows := outboxRows(t, db); len(rows) != 1 || rows[0].Status != "done" {
		t.Fatalf("после восстановления Telegram событие закрыто: %+v", rows)
	}
	if isKicked(t, db, itUser) {
		t.Error("участник должен вернуться в стаю")
	}
	if texts := tg.textsTo(itUser); len(texts) != 1 || !strings.Contains(texts[0], "Ура, ты в стае") {
		t.Errorf("приветствие ровно одно: %q", texts)
	}
}

// Доступ выдать так и не удалось — звёзды возвращаются автоматически.
func TestOutboxRefundsStarsWhenAccessCannotBeDelivered(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	paidStarsRequestWithFailedDelivery(t, b, tg, db)

	// Три попытки уже были; четвёртая — последняя.
	if _, err := db.Exec(`UPDATE outbox_events SET attempts = 3, next_attempt_at = NOW()`); err != nil {
		t.Fatal(err)
	}
	tg.reset()
	b.processOutboxBatch()
	rows := outboxRows(t, db)
	if len(rows) != 2 || rows[0].Status != "dead" || rows[1].Type != "refund_requested" || rows[1].Status != "pending" {
		t.Fatalf("после последней попытки — запрос возврата: %+v", rows)
	}

	b.processOutboxBatch()
	if rows = outboxRows(t, db); rows[1].Status != "done" {
		t.Fatalf("возврат выполнен: %+v", rows)
	}
	refunds := tg.sent("refundStarPayment")
	if len(refunds) != 1 || refunds[0].Form.Get("telegram_payment_charge_id") != "charge-77" || refunds[0].Form.Get("user_id") != itoa(itUser) {
		t.Fatalf("возврат звёзд в Telegram: %+v", refunds)
	}
}

// Возврат рублёвой оплаты идёт в ЮKassa на всю сумму платежа.
func TestOutboxRefundsYookassaPayment(t *testing.T) {
	b, tg, db := newIntegrationBot(t, yookassaConfig)
	seedMember(t, db, itUser, "leopard", true)
	reqID, _ := b.db.InsertPaywallAccessRequest(itUser, itPack)
	// Как в бою: id платежа кассы пишется при создании ссылки, до оплаты.
	if err := b.db.SetPaywallYookassaPaymentID(reqID, "yk-9"); err != nil {
		t.Fatal(err)
	}
	if ok, err := b.db.CompletePaywallAccessRequest(reqID, itUser, itPack, "yk-9", 21000, "RUB", false); err != nil || !ok {
		t.Fatalf("закрыть заявку: %v %v", ok, err)
	}

	var refund struct {
		Amount struct {
			Value    string `json:"value"`
			Currency string `json:"currency"`
		} `json:"amount"`
		PaymentID string `json:"payment_id"`
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &refund)
		_, _ = w.Write([]byte(`{"id":"ref-1","status":"succeeded"}`))
	}))
	defer srv.Close()
	defer yookassa.SetAPIBase(srv.URL)()

	if err := b.db.EnqueueOutboxEvent("refund_requested", "refund_request:1", refundRequestedPayload{RequestID: reqID, UserID: itUser, Reason: "тест"}); err != nil {
		t.Fatal(err)
	}
	b.processOutboxBatch()

	if rows := outboxRows(t, db); len(rows) != 1 || rows[0].Status != "done" {
		t.Fatalf("возврат выполнен: %+v", rows)
	}
	if refund.PaymentID != "yk-9" || refund.Amount.Value != "210.00" || refund.Amount.Currency != "RUB" {
		t.Errorf("запрос возврата в кассу: %+v", refund)
	}
	if texts := tg.textsTo(itUser); len(texts) != 1 || !strings.Contains(texts[0], "Оплата возвращена") {
		t.Errorf("пользователю сообщили о возврате: %q", texts)
	}
}

// События, которые бессмысленно повторять, сразу уходят в «мёртвые»,
// владелец получает уведомление.
func TestOutboxDeadLettersBrokenEvents(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "leopard", false)
	reqID, _ := b.db.InsertPaywallAccessRequest(itUser, itPack)

	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(b.db.EnqueueOutboxEvent("refund_requested", "a", refundRequestedPayload{RequestID: 424242, UserID: itUser}))    // заявки нет
	must(b.db.EnqueueOutboxEvent("refund_requested", "b", refundRequestedPayload{RequestID: reqID, UserID: itUser + 1})) // чужой пользователь
	must(b.db.EnqueueOutboxEvent("refund_requested", "c", refundRequestedPayload{}))                                     // пустые поля
	must(b.db.EnqueueOutboxEvent("refund_requested", "d", refundRequestedPayload{RequestID: reqID, UserID: itUser}))     // оплата без данных для возврата
	must(b.db.EnqueueOutboxEvent("неизвестный_тип", "e", map[string]int{"x": 1}))

	b.processOutboxBatch()
	rows := outboxRows(t, db)
	for i := 0; i < 4; i++ {
		if rows[i].Status != "dead" {
			t.Errorf("событие %d должно быть мёртвым: %+v", i, rows[i])
		}
	}
	if rows[4].Status != "retry" {
		t.Errorf("неизвестный тип повторяем, пока не кончатся попытки: %+v", rows[4])
	}
	alerts := 0
	for _, text := range tg.textsTo(itAdmin) {
		if strings.Contains(text, "OUTBOX DEAD") {
			alerts++
		}
	}
	if alerts != 4 {
		t.Errorf("уведомлений владельцу: %d", alerts)
	}
	if len(tg.sent("refundStarPayment")) != 0 {
		t.Error("по сломанным событиям деньги не возвращаем")
	}
}
