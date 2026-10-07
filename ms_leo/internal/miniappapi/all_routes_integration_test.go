package miniappapi

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"leo-bot/internal/bot"
	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/lib/pq"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

const (
	routesTestToken = "1000001:TEST-TOKEN"
	routesTestPack  = int64(-1001000)
	routesTestUser  = int64(424242)
)

// fakeTelegram отвечает на любые методы Bot API: наружу тест не ходит.
func fakeTelegram(t *testing.T) *tgbotapi.BotAPI {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/getMe"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1000001,"is_bot":true,"first_name":"Leo","username":"leo_test_bot"}}`))
		case strings.HasSuffix(r.URL.Path, "/getChatMember"):
			_, _ = w.Write([]byte(`{"ok":true,"result":{"status":"member","user":{"id":424242,"first_name":"Тест"}}}`))
		default:
			_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1,"chat":{"id":424242,"type":"private"}}}`))
		}
	}))
	t.Cleanup(srv.Close)
	api, err := tgbotapi.NewBotAPIWithAPIEndpoint(routesTestToken, srv.URL+"/bot%s/%s")
	if err != nil {
		t.Fatalf("поддельный Telegram: %v", err)
	}
	return api
}

// routesFromSource читает маршруты прямо из switch в server.go: новый
// обработчик попадает в тест сам, без правки списка.
func routesFromSource(t *testing.T) [][2]string {
	t.Helper()
	src, err := os.ReadFile("server.go")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`path == "(/api/miniapp/[^"]+)" && r\.Method == http\.Method(Post|Get)`)
	var out [][2]string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		method := http.MethodPost
		if m[2] == "Get" {
			method = http.MethodGet
		}
		out = append(out, [2]string{method, m[1]})
	}
	return out
}

// Каждый маршрут API вызывается с настоящей подписью Telegram на настоящей
// базе. Аргументы — заглушки, поэтому отказы 4xx нормальны. Не нормально:
// паника обработчика и ответ 500 — это значит, что запрос к базе или код
// обработчика сломан.
func TestEveryRouteAnswersWithoutCrash(t *testing.T) {
	dsn := os.Getenv("LEO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("LEO_TEST_PG_DSN не задан")
	}
	// Своя база: пакет database гоняет тесты на базе из DSN и пересоздаёт в ней
	// схему, а пакеты тестируются параллельно.
	sqlDB := openOwnTestDatabase(t, dsn, "leo_routes_test")
	db := database.NewForTest(sqlDB)
	if err := db.CreateTables(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureTrackerSchema(); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO training_state (user_id, chat_id, username, last_message) VALUES ($1, $2, 'tester', NOW())`, routesTestUser, routesTestPack); err != nil {
		t.Fatal(err)
	}
	if _, err := sqlDB.Exec(`INSERT INTO miniapp_user_profile (user_id, pack_chat_id, display_name) VALUES ($1, $2, 'Тестер')`, routesTestUser, routesTestPack); err != nil {
		t.Fatal(err)
	}

	cfg := &config.Config{APIToken: routesTestToken, OwnerID: routesTestUser, MonetizedChatID: routesTestPack}
	log := logger.New("error")
	b := bot.NewForTest(cfg, db, log, fakeTelegram(t))
	handler := New(b, routesTestToken, log, "https://example.test", t.TempDir(), nil)

	// Подписываем так же, как Telegram: hash от полей и auth_date.
	now := time.Now()
	user := fmt.Sprintf(`{"id":%d,"first_name":"Тестер","username":"tester"}`, routesTestUser)
	hash := initdata.Sign(map[string]string{"user": user}, routesTestToken, now)
	init := url.Values{"user": {user}, "auth_date": {fmt.Sprint(now.Unix())}, "hash": {hash}}.Encode()

	body := map[string]any{
		"init_data": init, "text": "тест", "message": "тест", "prompt": "тест", "query": "тест", "q": "тест",
		"id": 1, "ids": []int64{routesTestUser}, "user_id": routesTestUser, "target_user_id": routesTestUser,
		"message_id": 1, "post_id": 1, "thread_id": 1, "reply_id": 1, "task_id": 1, "report_id": 1,
		"emoji": "🔥", "days": 7, "limit": 5, "offset": 0, "goal": 10, "amount_rub": 100,
		"table": "training_state", "author": "leo", "action": "noop", "filter": "all", "kind": "",
		"display_name": "Тестер", "option": 0, "enabled": true, "date": "2026-01-15",
	}
	payload, _ := json.Marshal(body)

	routes := routesFromSource(t)
	if len(routes) < 80 {
		t.Fatalf("найдено только %d маршрутов — разбор server.go сломался", len(routes))
	}
	var crashed []string
	byStatus := map[int]int{}
	for _, route := range routes {
		method, path := route[0], route[1]
		code, panicked := callRoute(handler, method, path, init, payload)
		switch {
		case panicked != "":
			crashed = append(crashed, fmt.Sprintf("%s %s: паника: %s", method, path, panicked))
		case code == 0:
			// Не ответил за отведённое время (долгий опрос) — не ошибка.
		case code == http.StatusInternalServerError:
			crashed = append(crashed, fmt.Sprintf("%s %s: ответ 500", method, path))
		}
		byStatus[code]++
	}
	sort.Strings(crashed)
	if len(crashed) > 0 {
		t.Errorf("маршруты упали (%d из %d):\n  %s", len(crashed), len(routes), strings.Join(crashed, "\n  "))
	}
	t.Logf("маршрутов: %d, ответы по кодам: %v", len(routes), byStatus)
}

// openOwnTestDatabase создаёт на том же сервере отдельную пустую базу name
// и подключается к ней.
func openOwnTestDatabase(t *testing.T, dsn, name string) *sql.DB {
	t.Helper()
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + name); err != nil {
		t.Fatal(err)
	}
	if _, err := admin.Exec(`CREATE DATABASE ` + name); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	u.Path = "/" + name
	db, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

// callRoute вызывает обработчик и возвращает код ответа; panicked — текст
// паники, если она была. Код 0 — обработчик не ответил за 10 секунд.
func callRoute(h http.Handler, method, path, init string, payload []byte) (code int, panicked string) {
	type result struct {
		code  int
		panic string
	}
	done := make(chan result, 1)
	go func() {
		rec := httptest.NewRecorder()
		defer func() {
			if r := recover(); r != nil {
				done <- result{panic: fmt.Sprint(r)}
			}
		}()
		var req *http.Request
		if method == http.MethodGet {
			req = httptest.NewRequest(method, path+"?init_data="+url.QueryEscape(init)+"&user_id=424242", nil)
		} else {
			req = httptest.NewRequest(method, path, bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
		}
		h.ServeHTTP(rec, req)
		done <- result{code: rec.Code}
	}()
	select {
	case r := <-done:
		return r.code, r.panic
	case <-time.After(10 * time.Second):
		return 0, ""
	}
}
