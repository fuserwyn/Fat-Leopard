package bot

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"

	"leo-bot/internal/utils"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// setTimerStarted двигает начало отсчёта неактивности на daysAgo дней назад.
func setTimerStarted(t *testing.T, db *sql.DB, userID int64, daysAgo int) {
	t.Helper()
	start := utils.FormatMoscowTime(utils.GetMoscowTime().AddDate(0, 0, -daysAgo))
	if _, err := db.Exec(`UPDATE training_state SET timer_start_time = $3 WHERE user_id = $1 AND chat_id = $2`, userID, itPack, start); err != nil {
		t.Fatal(err)
	}
}

// Сторож неактивности удаляет только тех, у кого срок действительно вышел:
// не трогает активных, освобождённых от удаления и тех, кто на больничном.
func TestInactivitySweepRemovesOnlyOverdueMembers(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	const (
		overdue = itUser
		fresh   = itUser + 1
		exempt  = itUser + 2
		sick    = itUser + 3
	)
	for id, name := range map[int64]string{overdue: "overdue", fresh: "fresh", exempt: "exempt", sick: "sick"} {
		seedMember(t, db, id, name, false)
	}
	setTimerStarted(t, db, overdue, 9)
	setTimerStarted(t, db, fresh, 2)
	setTimerStarted(t, db, exempt, 30)
	setTimerStarted(t, db, sick, 30)
	if _, err := db.Exec(`UPDATE training_state SET is_exempt_from_deletion = TRUE WHERE user_id = $1`, exempt); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE training_state SET has_sick_leave = TRUE, has_healthy = FALSE WHERE user_id = $1`, sick); err != nil {
		t.Fatal(err)
	}

	b.runInactivityKickSweep()

	if !isKicked(t, db, overdue) {
		t.Fatal("просрочивший должен быть удалён")
	}
	for id, why := range map[int64]string{fresh: "активный", exempt: "освобождённый", sick: "на больничном"} {
		if isKicked(t, db, id) {
			t.Errorf("%s участник удалён зря", why)
		}
	}
	dms := tg.textsTo(overdue)
	if len(dms) != 1 || !strings.Contains(dms[0], "Лео удалил тебя из стаи") {
		t.Fatalf("удалённому — одно сообщение: %q", dms)
	}
	for _, c := range tg.sent("sendMessage") {
		if c.Form.Get("chat_id") == itoa(overdue) && !strings.Contains(c.Form.Get("reply_markup"), paywallCallbackReturnToPack) {
			t.Error("в сообщении об удалении должна быть кнопка «Вернуться в стаю»")
		}
	}
	var events int
	var status string
	if err := db.QueryRow(`SELECT COUNT(*), COALESCE(MAX(dm_status), '') FROM deletion_events WHERE user_id = $1`, overdue).Scan(&events, &status); err != nil || events != 1 || status != "dm_sent" {
		t.Errorf("запись об удалении: %d %q %v", events, status, err)
	}

	// Повторный проход никого не удаляет и не пишет второй раз.
	tg.reset()
	b.runInactivityKickSweep()
	if len(tg.textsTo(overdue)) != 0 {
		t.Error("повторное сообщение уже удалённому")
	}
}

// Полный круг единственной платной точки продукта: удалили за неактивность →
// закрыт доступ → оплатил возврат → снова в стае, счётчик неактивности с нуля.
func TestKickedMemberPaysAndReturns(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "leopard", false)
	// Раньше уже платил за возврат — доступ активен.
	oldReq, _ := b.db.InsertPaywallAccessRequest(itUser, itPack)
	if ok, err := b.db.CompletePaywallAccessRequest(oldReq, itUser, itPack, "old-charge", 350, "XTR", false); err != nil || !ok {
		t.Fatalf("прошлая оплата: %v %v", ok, err)
	}
	if !hasAccess(t, b, itUser) || b.paywallPrivateNeedsPayFirst(itUser) {
		t.Fatal("до удаления доступ есть")
	}

	setTimerStarted(t, db, itUser, 9)
	b.runInactivityKickSweep()
	if !isKicked(t, db, itUser) || hasAccess(t, b, itUser) || !b.paywallPrivateNeedsPayFirst(itUser) {
		t.Fatal("после удаления: выбыл, прошлая оплата больше не действует, нужен новый платёж")
	}

	reqID, err := b.paywallGetOrCreatePendingReqID(itUser)
	if err != nil || reqID == oldReq {
		t.Fatalf("на возврат заводится новая заявка: %d %v", reqID, err)
	}
	tg.reset()
	b.handlePaywallSuccessfulPayment(&tgbotapi.Message{
		From: &tgbotapi.User{ID: itUser, UserName: "leopard"},
		SuccessfulPayment: &tgbotapi.SuccessfulPayment{
			Currency: "XTR", TotalAmount: 350, InvoicePayload: fmt.Sprintf("%s%d", paywallPayloadPrefix, reqID), TelegramPaymentChargeID: "new-charge",
		},
	})
	if isKicked(t, db, itUser) || !hasAccess(t, b, itUser) || b.paywallPrivateNeedsPayFirst(itUser) {
		t.Fatal("после оплаты: снова в стае с доступом")
	}

	// Отсчёт неактивности начался заново — сторож вернувшегося не трогает.
	b.runInactivityKickSweep()
	if isKicked(t, db, itUser) {
		t.Fatal("вернувшегося нельзя удалять сразу")
	}
	var returns int
	if err := db.QueryRow(`SELECT COALESCE(return_count, 0) FROM training_state WHERE user_id = $1 AND chat_id = $2`, itUser, itPack).Scan(&returns); err != nil || returns != 1 {
		t.Errorf("счётчик возвратов: %d %v", returns, err)
	}
}

// Тренировка сбрасывает отсчёт: тот, кто отчитался, под удаление не попадает.
func TestWorkoutResetsInactivityTimer(t *testing.T) {
	b, _, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)
	setTimerStarted(t, db, itUser, 9)

	logWorkout(b, itUser, "#training_done успел")
	b.runInactivityKickSweep()

	if isKicked(t, db, itUser) {
		t.Fatal("после тренировки удалять нельзя")
	}
}
