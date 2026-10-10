package bot

import (
	"database/sql"
	"fmt"
	"strings"
	"testing"
	"time"

	"leo-bot/internal/domain"
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
	// Стае об удалении не сообщаем: в ленте не появляется «я его съел».
	var feedRows int
	if err := db.QueryRow(`SELECT COUNT(*) FROM user_messages WHERE user_id = $1 AND chat_id = $2`, overdue, itPack).Scan(&feedRows); err != nil || feedRows != 0 {
		t.Errorf("в ленте стаи не должно быть записи об удалении: %d %v", feedRows, err)
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

// Админ исключает участника, у которого срок неактивности ещё не вышел:
// решение человека выполняется сразу, а автоматическое удаление по
// устаревшему таймеру такого участника по-прежнему не трогает.
func TestAdminKickRemovesMemberBeforeDeadline(t *testing.T) {
	b, tg, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "from_miniapp", false)
	seedMember(t, db, itUser+1, "from_chat", false)
	seedMember(t, db, itUser+2, "stale_timer", false)
	for _, id := range []int64{itUser, itUser + 1, itUser + 2} {
		setTimerStarted(t, db, id, 1)
	}

	// Сработал таймер, запланированный до того, как срок отодвинули.
	b.removeUser(itUser+2, itPack, "stale_timer")
	if isKicked(t, db, itUser+2) {
		t.Fatal("устаревший таймер не должен удалять участника с невышедшим сроком")
	}

	if err := b.kickUserFromPack(itUser); err != nil {
		t.Fatal(err)
	}
	if !isKicked(t, db, itUser) {
		t.Fatal("исключение из админки мини-аппа должно сработать сразу")
	}
	if err := b.kickUserFromPack(itUser); err == nil {
		t.Error("повторное исключение — ошибка «уже удалён»")
	}

	tg.reset()
	b.adminDeleteUser(itAdmin, itUser+1)
	if !isKicked(t, db, itUser+1) {
		t.Fatal("исключение из чат-админки должно сработать сразу")
	}
	if texts := tg.textsTo(itUser + 1); len(texts) != 1 || !strings.Contains(texts[0], "Лео удалил тебя из стаи") {
		t.Errorf("исключённому — сообщение с кнопкой возврата: %q", texts)
	}
}

// Карточки «я его съел», опубликованные до отключения, стая в ленте больше не видит.
func TestPackFeedHidesOldRemovalNotices(t *testing.T) {
	b, _, db := newIntegrationBot(t, starsConfig)
	seedMember(t, db, itUser, "leopard", false)
	for _, typ := range []string{"pack_removed", userMessageTypePackJoin} {
		if _, err := db.Exec(`INSERT INTO user_messages (user_id, chat_id, username, message_text, message_type) VALUES ($1, $2, 'leopard', 'текст', $3)`, itUser, itPack, typ); err != nil {
			t.Fatal(err)
		}
	}
	feeds := map[string]func() ([]*domain.PackActivityRow, error){
		"desc": func() ([]*domain.PackActivityRow, error) { return b.db.ListPackActivityFeedDesc(itPack, nil, 50) },
		"after": func() ([]*domain.PackActivityRow, error) {
			return b.db.ListPackActivityFeedAfterTS(itPack, time.Unix(0, 0), 50)
		},
		"since":   func() ([]*domain.PackActivityRow, error) { return b.db.ListPackActivityFeed(itPack, 50, nil) },
		"afterID": func() ([]*domain.PackActivityRow, error) { return b.db.ListPackActivityFeedAfterID(itPack, 1, 50) },
		"beforeID": func() ([]*domain.PackActivityRow, error) {
			return b.db.ListPackActivityFeedBeforeID(itPack, 1<<62, 50)
		},
	}
	for name, list := range feeds {
		rows, err := list()
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		join := false
		for _, r := range rows {
			if r.MessageType == "pack_removed" {
				t.Errorf("%s: запись об удалении видна в ленте", name)
			}
			join = join || r.MessageType == userMessageTypePackJoin
		}
		if !join {
			t.Errorf("%s: остальные записи ленты должны остаться", name)
		}
	}
}
