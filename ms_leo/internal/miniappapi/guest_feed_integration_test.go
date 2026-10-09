package miniappapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"leo-bot/internal/bot"
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"
)

// Гостевой просмотр: человек не в стае (платный вход, не оплатил), открывает
// мини-апп и видит несколько свежих тренировок и ссылку «Вступить». Обычная
// лента для него закрыта, а в гостевой нет id авторов, комментариев и
// системных карточек.
func TestGuestFeedShowsPackWithoutJoining(t *testing.T) {
	dsn := os.Getenv("LEO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("LEO_TEST_PG_DSN не задан")
	}
	const (
		pack     = int64(-1004000)
		member   = int64(710001)
		stranger = int64(710099)
	)
	sqlDB := openOwnTestDatabase(t, dsn, "leo_guest_feed_test")
	db := database.NewForTest(sqlDB)
	if err := db.CreateTables(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureTrackerSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO training_state (user_id, chat_id, username, last_message, streak_days) VALUES ($1, $2, 'anna', '', 4)`, member, pack); err != nil {
		t.Fatal(err)
	}
	// 7 тренировок участника (гостю — только 5 свежих), приветствие Лео и скрытый пост.
	for i := 0; i < 7; i++ {
		if _, err := sqlDB.Exec(`INSERT INTO user_messages (user_id, chat_id, username, message_text, message_type, created_at)
			VALUES ($1, $2, 'anna', $3, 'training_done', NOW() - make_interval(mins => $4))`,
			member, pack, "#training_done\nбег, 30 мин, интенсивность 3/5\nпост "+string(rune('A'+i)), 10*(i+1)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := sqlDB.Exec(`INSERT INTO user_messages (user_id, chat_id, username, message_text, message_type)
		VALUES ($1, $2, 'anna', 'Привет, стая', 'pack_join')`, member, pack); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO user_messages (user_id, chat_id, username, message_text, message_type, is_hidden)
		VALUES ($1, $2, 'anna', 'скрыто', 'training_done', TRUE)`, member, pack); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{
		APIToken: routesTestToken, OwnerID: 1, MonetizedChatID: pack,
		PaywallEnabled: true, PaymentCurrency: "XTR", PaymentAmountMinorUnits: 100,
	}
	log := logger.New("error")
	b := bot.NewForTest(cfg, db, log, fakeTelegram(t))
	h := New(b, routesTestToken, log, "https://example.test", t.TempDir(), nil)

	call := func(userID int64, name, path string) (int, map[string]any) {
		raw, _ := json.Marshal(map[string]any{"init_data": signedInit(userID, name)})
		req := httptest.NewRequest(http.MethodPost, "/api/miniapp"+path, bytes.NewReader(raw))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		out := map[string]any{}
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	// Обычная лента гостю закрыта — для этого и нужна гостевая.
	if code, _ := call(stranger, "guest", "/feed"); code != http.StatusForbidden {
		t.Fatalf("обычная лента для гостя: код %d", code)
	}
	if _, st := call(stranger, "guest", "/onboarding/ensure"); st["in_pack"] != false || st["deleted"] != false {
		t.Fatalf("гость не в стае и не выбыл: %v", st)
	}

	code, out := call(stranger, "guest", "/feed/guest")
	if code != http.StatusOK {
		t.Fatalf("гостевая лента: код %d %v", code, out)
	}
	if out["in_pack"] != false {
		t.Fatalf("гость не в стае: %v", out)
	}
	if out["join_url"] != "https://t.me/leo_test_bot?start=src-guest_feed" {
		t.Fatalf("ссылка «Вступить»: %v", out["join_url"])
	}
	posts := items(out, "items")
	if len(posts) != 5 {
		t.Fatalf("гостю 5 постов, а пришло %d: %v", len(posts), posts)
	}
	for i, p := range posts {
		if p["type"] != "training_done" || p["username"] != "anna" {
			t.Fatalf("пост %d: %v", i, p)
		}
		if _, ok := p["user_id"].(float64); ok && p["user_id"].(float64) != 0 {
			t.Fatalf("id автора гостю не показываем: %v", p)
		}
		if p["thread"] != nil || p["reactions"] != nil || p["author_photo_url"] != nil {
			t.Fatalf("комментарии/реакции/аватар гостю не нужны: %v", p)
		}
		if strings.Contains(p["text"].(string), "скрыто") {
			t.Fatalf("скрытый пост попал к гостю: %v", p)
		}
	}
	if !strings.HasSuffix(posts[0]["text"].(string), "пост A") || posts[0]["streak_days"] != float64(4) {
		t.Fatalf("сверху самый свежий пост со стриком автора: %v", posts[0])
	}

	// Тот, у кого доступ есть (владелец), тоже может открыть витрину — ответ скажет, что он уже внутри.
	if _, out := call(cfg.OwnerID, "owner", "/feed/guest"); out["in_pack"] != true {
		t.Fatalf("владелец в стае: %v", out)
	}
	// Без подписи Telegram витрину не отдаём.
	req := httptest.NewRequest(http.MethodPost, "/api/miniapp/feed/guest", strings.NewReader(`{"init_data":"user=%7B%22id%22%3A1%7D&hash=bad"}`))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("плохая подпись: код %d", rec.Code)
	}
}
