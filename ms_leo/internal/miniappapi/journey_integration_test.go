package miniappapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"leo-bot/internal/bot"
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// journey — мини-апп глазами пользователей: настоящие обработчики, настоящая
// база, поддельный Telegram. Каждый шаг — запрос от имени конкретного человека.
type journey struct {
	t       *testing.T
	handler http.Handler
	db      *sql.DB
}

const (
	jPack  = int64(-1003000)
	jAnna  = int64(700001)
	jBoris = int64(700002)
	jOwner = int64(700900)
)

func newJourney(t *testing.T) *journey {
	t.Helper()
	dsn := os.Getenv("LEO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("LEO_TEST_PG_DSN не задан")
	}
	sqlDB := openOwnTestDatabase(t, dsn, "leo_journey_test")
	db := database.NewForTest(sqlDB)
	if err := db.CreateTables(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureTrackerSchema(); err != nil {
		t.Fatal(err)
	}
	for id, name := range map[int64]string{jAnna: "anna", jBoris: "boris", jOwner: "owner"} {
		if _, err := sqlDB.Exec(`INSERT INTO training_state (user_id, chat_id, username, last_message) VALUES ($1, $2, $3, '')`, id, jPack, name); err != nil {
			t.Fatal(err)
		}
	}
	cfg := &config.Config{
		APIToken: routesTestToken, OwnerID: jOwner, MonetizedChatID: jPack,
		PaywallEnabled: true, PaywallEntryFree: true,
	}
	log := logger.New("error")
	b := bot.NewForTest(cfg, db, log, fakeTelegram(t))
	return &journey{t: t, handler: New(b, routesTestToken, log, "https://example.test", t.TempDir(), nil), db: sqlDB}
}

func signedInit(userID int64, name string) string {
	now := time.Now()
	user := fmt.Sprintf(`{"id":%d,"first_name":"%s","username":"%s"}`, userID, name, name)
	hash := initdata.Sign(map[string]string{"user": user}, routesTestToken, now)
	return url.Values{"user": {user}, "auth_date": {fmt.Sprint(now.Unix())}, "hash": {hash}}.Encode()
}

func userName(id int64) string {
	switch id {
	case jAnna:
		return "anna"
	case jBoris:
		return "boris"
	}
	return "owner"
}

// post — JSON-запрос от имени userID; возвращает код и разобранный ответ.
func (j *journey) post(userID int64, path string, fields map[string]any) (int, map[string]any) {
	j.t.Helper()
	body := map[string]any{"init_data": signedInit(userID, userName(userID))}
	for k, v := range fields {
		body[k] = v
	}
	raw, _ := json.Marshal(body)
	req := httptest.NewRequest(http.MethodPost, "/api/miniapp"+path, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	return j.do(req)
}

// form — multipart-запрос (так мини-апп шлёт тренировку и фото).
func (j *journey) form(userID int64, path string, fields map[string]string) (int, map[string]any) {
	j.t.Helper()
	var buf bytes.Buffer
	w := multipart.NewWriter(&buf)
	_ = w.WriteField("init_data", signedInit(userID, userName(userID)))
	for k, v := range fields {
		_ = w.WriteField(k, v)
	}
	_ = w.Close()
	req := httptest.NewRequest(http.MethodPost, "/api/miniapp"+path, &buf)
	req.Header.Set("Content-Type", w.FormDataContentType())
	return j.do(req)
}

func (j *journey) do(req *http.Request) (int, map[string]any) {
	rec := httptest.NewRecorder()
	j.handler.ServeHTTP(rec, req)
	out := map[string]any{}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if os.Getenv("LEO_JOURNEY_TRACE") != "" {
		raw := rec.Body.String()
		if len(raw) > 400 {
			raw = raw[:400] + "…"
		}
		j.t.Logf("%s → %d %s", req.URL.Path, rec.Code, raw)
	}
	return rec.Code, out
}

// ok — запрос обязан пройти.
func (j *journey) ok(userID int64, path string, fields map[string]any) map[string]any {
	j.t.Helper()
	code, out := j.post(userID, path, fields)
	if code != http.StatusOK {
		j.t.Fatalf("%s от %s: код %d, ответ %v", path, userName(userID), code, out)
	}
	return out
}

func (j *journey) scalar(query string, args ...any) int64 {
	j.t.Helper()
	var n sql.NullInt64
	if err := j.db.QueryRow(query, args...).Scan(&n); err != nil {
		j.t.Fatalf("%v\n%s", err, query)
	}
	return n.Int64
}

func (j *journey) text(query string, args ...any) string {
	j.t.Helper()
	var s sql.NullString
	if err := j.db.QueryRow(query, args...).Scan(&s); err != nil && err != sql.ErrNoRows {
		j.t.Fatalf("%v\n%s", err, query)
	}
	return strings.TrimSpace(s.String)
}

// waitWorkout ждёт, пока фоновый обработчик запишет тренировку, и возвращает
// id её поста в ленте.
func (j *journey) waitWorkout(userID int64) int64 {
	j.t.Helper()
	for i := 0; i < 100; i++ {
		if id := j.scalar(`SELECT MAX(id) FROM user_messages WHERE user_id = $1 AND message_type = 'training_done'`, userID); id > 0 &&
			j.scalar(`SELECT COUNT(*) FROM training_sessions WHERE user_id = $1`, userID) > 0 {
			return id
		}
		time.Sleep(50 * time.Millisecond)
	}
	j.t.Fatalf("тренировка %s не записалась", userName(userID))
	return 0
}

// refused — запрос обязан быть отклонён (не 200) и ничего не изменить.
func (j *journey) refused(userID int64, path string, fields map[string]any) int {
	j.t.Helper()
	code, out := j.post(userID, path, fields)
	if code == http.StatusOK {
		j.t.Fatalf("%s от %s должен быть отклонён, а прошёл: %v", path, userName(userID), out)
	}
	return code
}

func items(out map[string]any, key string) []map[string]any {
	raw, _ := out[key].([]any)
	res := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if m, ok := r.(map[string]any); ok {
			res = append(res, m)
		}
	}
	return res
}

const journeyReport = "бег, 30 мин, интенсивность 3/5"

// Тренировка и её обсуждение в ленте: пост, реакция, комментарии, лайк,
// правка только автором, жалоба и её разбор админом.
func TestJourneyWorkoutAndFeedDiscussion(t *testing.T) {
	j := newJourney(t)

	j.ok(jAnna, "/onboarding/ensure", nil)
	saved := j.ok(jAnna, "/profile/save", map[string]any{"gender": "f", "display_name": "Анна", "age": 30, "timezone_offset": 0, "theme": "dark"})
	if saved["display_name"] != "Анна" {
		t.Fatalf("профиль сохранился: %v", saved)
	}

	reply := j.ok(jAnna, "/messages", map[string]any{"text": journeyReport})
	if text, _ := reply["reply_text"].(string); !strings.Contains(text, "Отчёт принят") || !strings.Contains(text, "Стрик: 1 день") {
		t.Fatalf("ответ на тренировку: %v", reply)
	}
	postID := j.waitWorkout(jAnna)
	profile := j.ok(jAnna, "/profile/load", nil)
	if profile["streak_days"] != float64(1) || profile["last_training_date"] == "" {
		t.Errorf("профиль после тренировки: стрик %v, дата %v", profile["streak_days"], profile["last_training_date"])
	}
	history := items(j.ok(jAnna, "/profile/cups-history", nil), "workouts")
	if len(history) != 1 || history[0]["message_text"] != journeyReport || history[0]["cups"].(float64) <= 0 {
		t.Errorf("история кубков: %v", history)
	}

	// Борис видит пост в ленте.
	var seen map[string]any
	for _, it := range items(j.ok(jBoris, "/feed", nil), "items") {
		if it["id"] == float64(postID) {
			seen = it
		}
	}
	if seen == nil || seen["type"] != "training_done" || seen["username"] != "Анна" || seen["is_you"] != false || seen["streak_days"] != float64(1) {
		t.Fatalf("пост в ленте у другого участника: %v", seen)
	}

	j.ok(jBoris, "/feed/training/react", map[string]any{"user_message_id": postID, "emoji": "🔥"})
	j.refused(jBoris, "/feed/training/react", map[string]any{"user_message_id": postID, "emoji": "💩"})
	if n := j.scalar(`SELECT COUNT(*) FROM miniapp_training_feed_reactions WHERE user_message_id = $1 AND user_id = $2`, postID, jBoris); n != 1 {
		t.Errorf("реакций Бориса: %d", n)
	}

	j.ok(jBoris, "/feed/training/thread", map[string]any{"user_message_id": postID, "text": "Красота!"})
	replyID := j.scalar(`SELECT MAX(id) FROM miniapp_training_feed_thread WHERE from_user_id = $1`, jBoris)
	if unread := j.ok(jAnna, "/feed/training/thread/unread-count", nil); unread["count"] != float64(1) {
		t.Errorf("автору поста — один непрочитанный комментарий: %v", unread)
	}
	j.ok(jAnna, "/feed/training/thread/unread-clear", map[string]any{"user_message_id": postID})
	if unread := j.ok(jAnna, "/feed/training/thread/unread-count", nil); unread["count"] != float64(0) {
		t.Errorf("после прочтения непрочитанных нет: %v", unread)
	}
	j.ok(jAnna, "/feed/training/thread/like", map[string]any{"thread_reply_id": replyID})

	// Править и удалять комментарий может только автор.
	j.refused(jAnna, "/feed/training/thread/edit", map[string]any{"thread_reply_id": replyID, "text": "чужая правка"})
	j.ok(jBoris, "/feed/training/thread/edit", map[string]any{"thread_reply_id": replyID, "text": "Красота, так держать!"})
	if got := j.text(`SELECT message_text FROM miniapp_training_feed_thread WHERE id = $1`, replyID); got != "Красота, так держать!" {
		t.Errorf("текст комментария после правки: %q", got)
	}
	j.refused(jAnna, "/feed/training/thread/delete", map[string]any{"thread_reply_id": replyID})
	j.refused(jBoris, "/feed/edit", map[string]any{"user_message_id": postID, "text": "чужой пост"})
	j.refused(jBoris, "/feed/delete", map[string]any{"user_message_id": postID})

	// Жалоба доходит до админа; он скрывает комментарий, и из ленты тот пропадает.
	j.ok(jAnna, "/feed/report", map[string]any{"user_message_id": postID, "thread_reply_id": replyID})
	reports := items(j.ok(jOwner, "/admin/reports", nil), "reports")
	if len(reports) != 1 || reports[0]["status"] != "open" || reports[0]["target_user_id"] != float64(jBoris) || reports[0]["reporter_name"] != "Анна" {
		t.Fatalf("жалоба у админа: %v", reports)
	}
	if ov := j.ok(jOwner, "/admin/overview", nil)["overview"].(map[string]any); ov["reports_open"] != float64(1) {
		t.Errorf("счётчик открытых жалоб: %v", ov)
	}
	j.refused(jOwner, "/admin/reports/action", map[string]any{"report_id": reports[0]["id"], "action": "нет такого"})
	j.ok(jOwner, "/admin/reports/action", map[string]any{"report_id": reports[0]["id"], "action": "hide"})
	if hidden := j.scalar(`SELECT COUNT(*) FROM miniapp_training_feed_thread WHERE id = $1 AND is_hidden`, replyID); hidden != 1 {
		t.Fatal("комментарий должен быть скрыт")
	}
	hiddenList := items(j.ok(jOwner, "/admin/hidden", nil), "items")
	if len(hiddenList) != 1 {
		t.Fatalf("скрытое у админа: %v", hiddenList)
	}
	j.ok(jOwner, "/admin/hidden/restore", map[string]any{"kind": "thread_reply", "id": replyID})
	if hidden := j.scalar(`SELECT COUNT(*) FROM miniapp_training_feed_thread WHERE id = $1 AND is_hidden`, replyID); hidden != 0 {
		t.Error("после восстановления комментарий снова виден")
	}

	// Автор удаляет свой комментарий и правит свой пост; убрать пост из ленты
	// может только админ.
	j.ok(jBoris, "/feed/training/thread/delete", map[string]any{"thread_reply_id": replyID})
	j.ok(jAnna, "/feed/edit", map[string]any{"user_message_id": postID, "text": "бег, 35 мин, интенсивность 3/5"})
	j.refused(jAnna, "/feed/delete", map[string]any{"user_message_id": postID})
	j.ok(jOwner, "/feed/delete", map[string]any{"user_message_id": postID})
	for _, it := range items(j.ok(jBoris, "/feed", nil), "items") {
		if it["id"] == float64(postID) {
			t.Error("удалённый пост не должен оставаться в ленте")
		}
	}
}

// Общий чат стаи: сообщение, поиск, реакция, непрочитанное, правка и
// удаление только автором.
func TestJourneyPackChat(t *testing.T) {
	j := newJourney(t)
	j.ok(jAnna, "/pack-group/messages", map[string]any{"text": "Всем привет"})
	msgID := j.scalar(`SELECT MAX(id) FROM miniapp_pack_group_chat`)
	if msgID == 0 {
		t.Fatal("сообщение не сохранилось")
	}

	feed := items(j.ok(jBoris, "/pack-group/feed", nil), "messages")
	if len(feed) != 1 || feed[0]["text"] != "Всем привет" || feed[0]["user_id"] != float64(jAnna) {
		t.Fatalf("чат у Бориса: %v", feed)
	}

	// Обычное сообщение бейдж не зажигает, ответ — зажигает у того, кому ответили.
	if unread := j.ok(jBoris, "/pack-group/unread-count", nil); unread["count"] != float64(0) {
		t.Errorf("от обычного сообщения непрочитанных нет: %v", unread)
	}
	j.ok(jBoris, "/pack-group/messages", map[string]any{"text": "И тебе привет", "reply_to_id": msgID})
	replyMsgID := j.scalar(`SELECT MAX(id) FROM miniapp_pack_group_chat`)
	if unread := j.ok(jAnna, "/pack-group/unread-count", nil); unread["count"] != float64(1) {
		t.Errorf("Анне ответили — одно непрочитанное: %v", unread)
	}
	j.ok(jAnna, "/pack-group/unread-clear", nil)
	if unread := j.ok(jAnna, "/pack-group/unread-count", nil); unread["count"] != float64(0) {
		t.Errorf("после прочтения непрочитанных нет: %v", unread)
	}
	j.ok(jBoris, "/pack-group/messages/delete", map[string]any{"message_id": replyMsgID})

	if found := items(j.ok(jBoris, "/pack-group/search", map[string]any{"query": "Всем", "limit": 5}), "messages"); len(found) != 1 {
		t.Errorf("поиск по слову: %v", found)
	}
	if found := items(j.ok(jBoris, "/pack-group/search", map[string]any{"query": "абракадабра", "limit": 5}), "messages"); len(found) != 0 {
		t.Errorf("поиск без совпадений: %v", found)
	}

	j.ok(jBoris, "/pack-group/react", map[string]any{"message_id": msgID, "emoji": "🔥"})
	j.refused(jBoris, "/pack-group/react", map[string]any{"message_id": msgID, "emoji": "💩"})
	if n := j.scalar(`SELECT COUNT(*) FROM miniapp_pack_group_reactions WHERE pack_message_id = $1`, msgID); n != 1 {
		t.Errorf("реакций на сообщение: %d", n)
	}

	j.refused(jBoris, "/pack-group/messages/edit", map[string]any{"message_id": msgID, "text": "чужая правка"})
	j.ok(jAnna, "/pack-group/messages/edit", map[string]any{"message_id": msgID, "text": "Всем привет, стая"})
	if got := j.text(`SELECT message_text FROM miniapp_pack_group_chat WHERE id = $1`, msgID); got != "Всем привет, стая" {
		t.Errorf("текст после правки: %q", got)
	}
	j.refused(jBoris, "/pack-group/messages/delete", map[string]any{"message_id": msgID})

	j.ok(jBoris, "/pack-group/report", map[string]any{"message_id": msgID})
	if reports := items(j.ok(jOwner, "/admin/reports", nil), "reports"); len(reports) != 1 || reports[0]["target_type"] != "pack_group_message" {
		t.Errorf("жалоба на сообщение чата: %v", reports)
	}

	j.ok(jAnna, "/pack-group/messages/delete", map[string]any{"message_id": msgID})
	if left := items(j.ok(jBoris, "/pack-group/feed", nil), "messages"); len(left) != 0 {
		t.Errorf("после удаления чат пуст: %v", left)
	}
	j.refused(jAnna, "/pack-group/messages", map[string]any{"text": "   "})
}

// Поддержка, подписки на друзей и личные настройки.
func TestJourneySupportFriendsAndSettings(t *testing.T) {
	j := newJourney(t)

	j.ok(jAnna, "/support/send", map[string]any{"text": "Не вижу кубки"})
	inbox := items(j.ok(jOwner, "/admin/support/inbox", nil), "conversations")
	if len(inbox) != 1 || inbox[0]["user_id"] != float64(jAnna) || inbox[0]["needs_reply"] != true || inbox[0]["last_text"] != "Не вижу кубки" {
		t.Fatalf("обращение у админа: %v", inbox)
	}
	if thread := items(j.ok(jOwner, "/admin/support/thread", map[string]any{"target_user_id": jAnna}), "messages"); len(thread) != 1 {
		t.Errorf("переписка у админа: %v", thread)
	}
	j.ok(jOwner, "/admin/support/reply", map[string]any{"target_user_id": jAnna, "text": "Сейчас посмотрим"})
	chat := items(j.ok(jAnna, "/support/feed", nil), "messages")
	if len(chat) != 2 || chat[0]["role"] != "user" || chat[1]["role"] != "support" || chat[1]["text"] != "Сейчас посмотрим" {
		t.Fatalf("переписка у пользователя: %v", chat)
	}
	if inbox = items(j.ok(jOwner, "/admin/support/inbox", nil), "conversations"); len(inbox) != 1 || inbox[0]["needs_reply"] != false {
		t.Errorf("после ответа обращение не ждёт: %v", inbox)
	}
	j.refused(jBoris, "/admin/support/reply", map[string]any{"target_user_id": jAnna, "text": "я не админ"})

	// Друзья.
	if out := j.ok(jBoris, "/friends/follow", map[string]any{"target_id": jAnna}); out["following"] != true {
		t.Fatalf("подписка: %v", out)
	}
	var anna map[string]any
	for _, m := range items(j.ok(jBoris, "/friends/list", nil), "members") {
		if m["user_id"] == float64(jAnna) {
			anna = m
		}
	}
	if anna == nil || anna["following"] != true || anna["notify_workouts"] != true {
		t.Fatalf("Анна в списке друзей Бориса: %v", anna)
	}
	j.ok(jBoris, "/friends/notify", map[string]any{"target_id": jAnna, "enabled": false})
	if n := j.scalar(`SELECT COUNT(*) FROM miniapp_friend_subscriptions WHERE subscriber_id = $1 AND target_id = $2`, jBoris, jAnna); n != 1 {
		t.Errorf("подписок в базе: %d", n)
	}
	j.ok(jBoris, "/friends/notify-all", map[string]any{"enabled": true})
	j.ok(jBoris, "/friends/unfollow", map[string]any{"target_id": jAnna})
	if n := j.scalar(`SELECT COUNT(*) FROM miniapp_friend_subscriptions WHERE subscriber_id = $1 AND target_id = $2`, jBoris, jAnna); n != 0 {
		t.Errorf("после отписки подписок: %d", n)
	}
	j.refused(jBoris, "/friends/follow", map[string]any{"target_id": jBoris})

	// Настройки сохраняются и читаются обратно.
	j.ok(jBoris, "/reminders/save", map[string]any{"enabled": true, "remind_hour": 9})
	if got := j.ok(jBoris, "/reminders/load", nil); got["enabled"] != true || got["remind_hour"] != float64(9) {
		t.Errorf("напоминания: %v", got)
	}
	j.ok(jBoris, "/reminders/save", map[string]any{"enabled": false, "remind_hour": 9})
	if got := j.ok(jBoris, "/reminders/load", nil); got["enabled"] != false {
		t.Errorf("напоминания после выключения: %v", got)
	}
	// У «мудрости дня» час фиксированный: присланный клиентом не учитывается.
	j.ok(jBoris, "/wisdom-sub/save", map[string]any{"enabled": true, "remind_hour": 9})
	wisdom := j.ok(jBoris, "/wisdom-sub/load", nil)
	if wisdom["enabled"] != true || wisdom["remind_hour"] == float64(9) {
		t.Errorf("мудрость дня: %v", wisdom)
	}
	for _, path := range []string{"/like-notifications", "/contact-join-notifications"} {
		j.ok(jBoris, path+"/save", map[string]any{"enabled": false})
		if got := j.ok(jBoris, path+"/load", nil); got["enabled"] != false {
			t.Errorf("%s: %v", path, got)
		}
	}
	if st := j.ok(jAnna, "/health/status", nil); st["on_sick"] != false {
		t.Errorf("статус здоровья: %v", st)
	}
}

// Админка: ни один админский маршрут не открыт обычному участнику, а владелец
// управляет участниками, объявлениями, ценой и целью.
func TestJourneyAdmin(t *testing.T) {
	j := newJourney(t)
	j.ok(jAnna, "/messages", map[string]any{"text": journeyReport})
	j.waitWorkout(jAnna)

	// Каждый /admin/* маршрут из server.go закрыт для не-админа.
	admin := 0
	for _, route := range routesFromSource(t) {
		path := strings.TrimPrefix(route[1], "/api/miniapp")
		if !strings.HasPrefix(path, "/admin/") || route[0] != http.MethodPost {
			continue
		}
		admin++
		if code, out := j.post(jAnna, path, map[string]any{"target_user_id": jBoris, "text": "x", "id": 1}); code == http.StatusOK {
			t.Errorf("%s открыт не-админу: %v", path, out)
		}
	}
	if admin < 40 {
		t.Fatalf("админских маршрутов найдено только %d", admin)
	}
	if n := j.scalar(`SELECT COUNT(*) FROM training_state WHERE chat_id = $1 AND NOT COALESCE(is_deleted, FALSE)`, jPack); n != 3 {
		t.Fatalf("попытки не-админа ничего не сломали: участников %d", n)
	}

	ov := j.ok(jOwner, "/admin/overview", nil)["overview"].(map[string]any)
	if ov["users"] != float64(3) || ov["users_active"] != float64(3) || ov["users_kicked"] != float64(0) {
		t.Errorf("сводка: %v", ov)
	}
	if found := j.ok(jOwner, "/admin/users", map[string]any{"query": "anna", "offset": 0, "filter": "all"}); found["total"] != float64(1) {
		t.Errorf("поиск участника: %v", found)
	}
	card := j.ok(jOwner, "/admin/users/card", map[string]any{"target_user_id": jAnna})["user"].(map[string]any)
	if card["streak_days"] != float64(1) || card["cups"].(float64) <= 0 {
		t.Errorf("карточка участника: %v", card)
	}

	// Админ правит кубки — участник видит новое число и уровень.
	j.ok(jOwner, "/admin/users/stat", map[string]any{"target_user_id": jAnna, "field": "cups", "mode": "set", "value": 500})
	if p := j.ok(jAnna, "/profile/load", nil); p["level"] != float64(2) {
		t.Errorf("уровень после 500 кубков: %v", p["level"])
	}
	j.refused(jOwner, "/admin/users/action", map[string]any{"target_user_id": jAnna, "action": "нет такого"})

	// Исключение и возврат участника.
	j.ok(jOwner, "/admin/users/action", map[string]any{"target_user_id": jBoris, "action": "kick"})
	if ov = j.ok(jOwner, "/admin/overview", nil)["overview"].(map[string]any); ov["users_kicked"] != float64(1) {
		t.Errorf("после исключения: %v", ov)
	}
	if st := j.ok(jBoris, "/onboarding/ensure", nil); st["deleted"] != true {
		t.Errorf("исключённый видит, что выбыл: %v", st)
	}
	j.ok(jOwner, "/admin/users/action", map[string]any{"target_user_id": jBoris, "action": "restore_full"})
	if st := j.ok(jBoris, "/onboarding/ensure", nil); st["deleted"] != false {
		t.Errorf("после восстановления снова в стае: %v", st)
	}

	// Объявление и опрос попадают в ленту; голос учитывается.
	j.ok(jOwner, "/admin/publish", map[string]any{"author": "leo", "text": "Объявление стае"})
	j.refused(jOwner, "/admin/publish", map[string]any{"author": "leo", "text": "  "})
	j.ok(jOwner, "/admin/poll", map[string]any{"question": "Бег или йога?", "options": []string{"Бег", "Йога"}})
	pollID := j.scalar(`SELECT MAX(id) FROM user_messages WHERE message_text LIKE '%Бег или йога?%'`)
	texts := ""
	for _, it := range items(j.ok(jAnna, "/feed", nil), "items") {
		texts += fmt.Sprint(it["text"]) + "\n"
	}
	if !strings.Contains(texts, "Объявление стае") || !strings.Contains(texts, "Бег или йога?") {
		t.Errorf("объявление и опрос в ленте: %s", texts)
	}
	j.ok(jAnna, "/feed/poll/vote", map[string]any{"user_message_id": pollID, "option_index": 1})
	if n := j.scalar(`SELECT COUNT(*) FROM miniapp_feed_poll_votes WHERE user_message_id = $1 AND user_id = $2`, pollID, jAnna); n != 1 {
		t.Errorf("голосов Анны: %d", n)
	}
	j.refused(jAnna, "/feed/poll/vote", map[string]any{"user_message_id": pollID, "option_index": 9})

	// Отложенный пост: создать, увидеть, отменить.
	at := time.Now().Add(48 * time.Hour).Format("2006-01-02T15:04")
	sched := j.ok(jOwner, "/admin/scheduled/add", map[string]any{"author": "leo", "text": "Позже", "at": at})
	if posts := items(j.ok(jOwner, "/admin/scheduled", nil), "posts"); len(posts) != 1 || posts[0]["text"] != "Позже" {
		t.Errorf("отложенные посты: %v", posts)
	}
	j.ok(jOwner, "/admin/scheduled/cancel", map[string]any{"id": sched["id"]})
	if posts := items(j.ok(jOwner, "/admin/scheduled", nil), "posts"); len(posts) != 0 {
		t.Errorf("после отмены отложенных нет: %v", posts)
	}

	// Цена доступа и цель стаи.
	price := j.ok(jOwner, "/admin/paywall-price/set", map[string]any{"amount_rub": 499})["price"].(map[string]any)
	if price["amount_rub"] != float64(499) || price["is_custom"] != true {
		t.Errorf("цена: %v", price)
	}
	if p := j.ok(jAnna, "/profile/load", nil); p["access_price_rub"] != float64(499) {
		t.Errorf("цена у участника: %v", p["access_price_rub"])
	}
	j.refused(jOwner, "/admin/paywall-price/set", map[string]any{"amount_rub": 0})
	if price = j.ok(jOwner, "/admin/paywall-price/set", map[string]any{"reset": true})["price"].(map[string]any); price["is_custom"] != false {
		t.Errorf("сброс цены: %v", price)
	}
	goal := j.ok(jOwner, "/admin/pack-goal/set", map[string]any{"goal": 120})["pack_goal"].(map[string]any)
	if goal["goal"] != float64(120) || goal["is_custom"] != true || goal["workouts_week"] != float64(1) {
		t.Errorf("цель стаи: %v", goal)
	}
	j.refused(jOwner, "/admin/pack-goal/set", map[string]any{"goal": -1})

	// Права админа: выдать и забрать.
	j.ok(jOwner, "/admin/admins/add", map[string]any{"query": "boris"})
	j.ok(jBoris, "/admin/overview", nil)
	// Выданные через панель права не открывают сырые данные базы.
	j.refused(jBoris, "/admin/db/tables", nil)
	j.ok(jOwner, "/admin/db/tables", nil)
	if res := j.ok(jOwner, "/admin/db/query", map[string]any{"sql": "SELECT COUNT(*) FROM training_state"}); res["result"] == nil && res["rows"] == nil {
		t.Errorf("запрос на чтение: %v", res)
	}
	j.refused(jOwner, "/admin/db/query", map[string]any{"sql": "DELETE FROM training_state"})
	j.ok(jOwner, "/admin/admins/remove", map[string]any{"user_id": jBoris})
	j.refused(jBoris, "/admin/overview", nil)

	// Аналитика и оплаты отвечают на настоящих данных.
	an := j.ok(jOwner, "/admin/analytics", map[string]any{"days": 30, "dashboard": true})["analytics"].(map[string]any)
	if an["dashboard"] == nil || len(items(an, "tables")) < 4 {
		t.Errorf("аналитика с дашбордом: %v", an)
	}
	j.ok(jOwner, "/admin/payments", map[string]any{"offset": 0, "limit": 20})
	j.ok(jOwner, "/admin/visits", nil)
}

// Челленджи через API: список, принятие, второй активный, бросить, свой — только
// после 100 дней, приглашение из ссылки можно отклонить.
func TestJourneyChallenges(t *testing.T) {
	j := newJourney(t)

	st := j.ok(jAnna, "/challenges/state", nil)
	std := items(st, "standard")
	if len(std) != 6 || std[0]["length_days"] != float64(7) || std[5]["length_days"] != float64(100) ||
		!strings.HasPrefix(std[0]["link"].(string), "https://t.me/leo_test_bot?start=ch-") {
		t.Fatalf("стандартные челленджи: %v", std)
	}
	if st["active"] != nil || st["can_create"] != false {
		t.Fatalf("новичок: %v", st)
	}

	if code, _ := j.post(jAnna, "/challenges/accept", map[string]any{"code": "nope"}); code != http.StatusNotFound {
		t.Errorf("неизвестный код: %d", code)
	}
	accepted := j.ok(jAnna, "/challenges/accept", map[string]any{"code": "days7"})
	if a, _ := accepted["active"].(map[string]any); a == nil || a["status"] != "active" {
		t.Fatalf("принят: %v", accepted)
	}
	if code, out := j.post(jAnna, "/challenges/accept", map[string]any{"code": "days14"}); code != http.StatusConflict || out["error"] != "challenge_already_active" {
		t.Errorf("второй активный: %d %v", code, out)
	}
	if a, _ := j.ok(jAnna, "/challenges/state", nil)["active"].(map[string]any); a == nil || a["challenge"].(map[string]any)["code"] != "days7" {
		t.Errorf("активный на экране: %v", a)
	}
	if code, out := j.post(jAnna, "/challenges/create", map[string]any{"title": "Свой", "length_days": 10}); code != http.StatusForbidden || out["error"] != "challenge_create_forbidden" {
		t.Errorf("свой без 100 дней: %d %v", code, out)
	}

	j.ok(jAnna, "/challenges/leave", nil)
	if code, out := j.post(jAnna, "/challenges/leave", nil); code != http.StatusConflict || out["error"] != "challenge_not_active" {
		t.Errorf("бросить нечего: %d %v", code, out)
	}

	// Прошла 100 дней — можно свой; плохая длина и название отклоняются.
	if _, err := j.db.Exec(`INSERT INTO challenge_participants (challenge_id, user_id, pack_chat_id, start_date, status, days_done, finished_at)
		SELECT id, $1, $2, CURRENT_DATE - 100, 'completed', 100, NOW() FROM challenges WHERE code = 'days100'`, jAnna, jPack); err != nil {
		t.Fatal(err)
	}
	if code, out := j.post(jAnna, "/challenges/create", map[string]any{"title": "Свой", "length_days": 400}); code != http.StatusBadRequest || out["error"] != "challenge_bad_length" {
		t.Errorf("длина 400: %d %v", code, out)
	}
	if code, out := j.post(jAnna, "/challenges/create", map[string]any{"title": "", "length_days": 10}); code != http.StatusBadRequest || out["error"] != "challenge_bad_title" {
		t.Errorf("пустое название: %d %v", code, out)
	}
	if code, out := j.post(jAnna, "/challenges/create", map[string]any{"title": "ты блять крут", "length_days": 10}); code == http.StatusOK || out["error"] == nil {
		t.Errorf("модерация названия: %d %v", code, out)
	}
	created := j.ok(jAnna, "/challenges/create", map[string]any{"title": "Планка", "length_days": 21})
	c, _ := created["challenge"].(map[string]any)
	if c == nil || c["custom"] != true || c["length_days"] != float64(21) {
		t.Fatalf("свой челлендж: %v", created)
	}

	// Борис пришёл по ссылке Анны: челлендж предложен, он отказался.
	if _, err := j.db.Exec(`INSERT INTO challenge_invites (user_id, challenge_id) SELECT $1, id FROM challenges WHERE code = $2`, jBoris, c["code"]); err != nil {
		t.Fatal(err)
	}
	if inv, _ := j.ok(jBoris, "/challenges/state", nil)["invite"].(map[string]any); inv == nil || inv["title"] != "Планка" {
		t.Errorf("приглашение: %v", inv)
	}
	j.ok(jBoris, "/challenges/invite/dismiss", nil)
	if st := j.ok(jBoris, "/challenges/state", nil); st["invite"] != nil {
		t.Errorf("отклонённое приглашение: %v", st["invite"])
	}
}

// Итоги недели стаи: модалка приходит участнику прошлой недели один раз,
// не участвовавшему — нет; закрыть можно только существующие итоги.
func TestJourneyPackWeekSummary(t *testing.T) {
	j := newJourney(t)
	msk, _ := time.LoadLocation("Europe/Moscow")
	now := time.Now().In(msk)
	monday := now.AddDate(0, 0, -((int(now.Weekday()) + 6) % 7))
	prev := monday.AddDate(0, 0, -7).Format("2006-01-02")
	if _, err := j.db.Exec(`INSERT INTO training_sessions (user_id, chat_id, session_date, trainings_count) VALUES ($1, $2, $3, 1)`, jAnna, jPack, prev); err != nil {
		t.Fatal(err)
	}
	if _, err := j.db.Exec(`INSERT INTO pack_week_summaries (pack_chat_id, week_start_date, workouts, goal, goal_reached, next_goal, participants)
		VALUES ($1, $2::date, 80, 75, TRUE, 80, 1)`, jPack, prev); err != nil {
		t.Fatal(err)
	}

	got, _ := j.ok(jAnna, "/pack/week-summary", nil)["summary"].(map[string]any)
	if got == nil || got["week_start"] != prev || got["goal_reached"] != true || got["next_goal"] != float64(80) || got["my_workouts"] != float64(1) {
		t.Fatalf("итоги Анне: %v", got)
	}
	if out := j.ok(jBoris, "/pack/week-summary", nil); out["summary"] != nil {
		t.Errorf("Борис не тренировался — модалки нет: %v", out)
	}
	if code := j.refused(jAnna, "/pack/week-summary/seen", map[string]any{"week_start": "2020-01-06"}); code != http.StatusNotFound {
		t.Errorf("чужая неделя: %d", code)
	}
	j.ok(jAnna, "/pack/week-summary/seen", map[string]any{"week_start": prev})
	if out := j.ok(jAnna, "/pack/week-summary", nil); out["summary"] != nil {
		t.Errorf("после просмотра модалки нет: %v", out)
	}
}
