package bot

import (
	"strings"
	"testing"
)

func joinedTexts(texts []string) string { return strings.Join(texts, "\n---\n") }

// Отметка в отчёте: участник стаи получает «совместную тренировку», знакомый боту
// не-участник — инвайт с личной ссылкой отметившего, незнакомый — ссылку получает
// сам отметивший. Повторная отметка инвайт не дублирует.
func TestJointTrainingMentionsNotifyAndInvite(t *testing.T) {
	b, tg, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)
	const (
		packmate = int64(555002)
		visitor  = int64(555003)
		kicked   = int64(555004)
	)
	seedMember(t, db, packmate, "vasya", false)
	seedMember(t, db, kicked, "olga_run", true)
	if err := b.db.RecordBotVisit(visitor, "Petya_Fit", "Петя", ""); err != nil {
		t.Fatal(err)
	}

	logWorkout(b, itUser, "#training_done пробежка с @Vasya, @petya_fit, @olga_run и @unknown_guy @leopard")

	if got := joinedTexts(tg.textsTo(packmate)); !strings.Contains(got, "на совместной тренировке") {
		t.Fatalf("участнику стаи: %q", got)
	}
	if got := joinedTexts(tg.textsTo(visitor)); !strings.Contains(got, "на совместной тренировке в стае") {
		t.Fatalf("инвайт знакомому боту: %q", got)
	}
	var markup string
	for _, c := range tg.sent("sendMessage") {
		if c.Form.Get("chat_id") == itoa(visitor) {
			markup = c.Form.Get("reply_markup")
		}
	}
	if !strings.Contains(markup, "https://t.me/leo_it_bot?start=ref-555001") {
		t.Fatalf("кнопка инвайта без ссылки: %q", markup)
	}
	if got := joinedTexts(tg.textsTo(kicked)); !strings.Contains(got, "Присоединяйся к стае") {
		t.Fatalf("инвайт выбывшему: %q", got)
	}
	author := joinedTexts(tg.textsTo(itUser))
	if !strings.Contains(author, "Отправил @petya_fit приглашение") ||
		!strings.Contains(author, "@unknown_guy пока нет в стае") ||
		!strings.Contains(author, "https://t.me/leo_it_bot?start=ref-555001") {
		t.Fatalf("отметившему: %q", author)
	}
	if strings.Contains(author, "совместной тренировке.") {
		t.Fatalf("себе уведомление о совместной тренировке не шлём: %q", author)
	}
	if n := count(t, db, `SELECT COUNT(*) FROM joint_training_invites WHERE inviter_user_id = $1`, itUser); n != 3 {
		t.Fatalf("инвайтов %d, ждали 3", n)
	}
	eventually(t, "событие joint_training_invite_sent", func() bool {
		return count(t, db, `SELECT COUNT(*) FROM events WHERE event_name = 'joint_training_invite_sent' AND telegram_id = $1`, itUser) == 3
	})

	// Повторная совместная тренировка: участнику снова уведомление, инвайты не дублируются.
	tg.reset()
	logWorkout(b, itUser, "#training_done ещё раз с @vasya и @petya_fit @unknown_guy")
	if len(tg.textsTo(packmate)) != 1 {
		t.Fatalf("участнику повторно: %v", tg.textsTo(packmate))
	}
	if len(tg.textsTo(visitor)) != 0 || len(tg.textsTo(itUser)) != 0 {
		t.Fatalf("инвайт продублирован: %v / %v", tg.textsTo(visitor), tg.textsTo(itUser))
	}

	// Друг пришёл по ссылке из инвайта — засчитан приглашением отметившего.
	startWith(b, visitor, "ref-555001")
	if v, _ := b.GetReferralForAPI(itUser); v.Joined != 1 {
		t.Fatalf("приглашение не засчитано: %+v", v)
	}
}

// Бот не смог доставить инвайт — ссылку получает отметивший.
func TestJointTrainingInviteFallbackWhenDMFails(t *testing.T) {
	b, tg, db := newIntegrationBot(t, nil)
	seedMember(t, db, itUser, "leopard", false)
	const visitor = int64(555013)
	if err := b.db.RecordBotVisit(visitor, "blocked_bot", "", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`UPDATE training_state SET username = '@leopard' WHERE user_id = $1`, itUser); err != nil {
		t.Fatal(err)
	}
	tg.fail["sendMessage"] = true
	b.notifyJointTrainingMentions(itPack, itUser, "@leopard", "#training_done зал с @blocked_bot")
	tg.fail["sendMessage"] = false
	if n := count(t, db, `SELECT COUNT(*) FROM joint_training_invites WHERE invitee_user_id = $1`, visitor); n != 1 {
		t.Fatalf("инвайт не записан: %d", n)
	}
	var toAuthor int
	for _, c := range tg.sent("sendMessage") {
		if c.Form.Get("chat_id") == itoa(itUser) && strings.Contains(c.Form.Get("text"), "@blocked_bot пока нет в стае") {
			toAuthor++
		}
	}
	if toAuthor != 1 {
		t.Fatalf("отметившему ссылка для пересылки: %d", toAuthor)
	}
}
