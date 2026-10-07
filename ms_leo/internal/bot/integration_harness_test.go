package bot

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"testing"

	"leo-bot/internal/config"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	_ "github.com/lib/pq"
)

// Сквозные тесты пакета идут на настоящем Postgres (LEO_TEST_PG_DSN) с
// поддельным Telegram: бот работает как в бою, а тест смотрит, что легло в
// базу и что ушло пользователю.

const (
	itPack  = int64(-1002000)
	itUser  = int64(555001)
	itAdmin = int64(555900)
)

type tgCall struct {
	Method string
	Form   url.Values
}

// fakeTelegram записывает все вызовы Bot API и отвечает успехом.
type fakeTelegram struct {
	mu    sync.Mutex
	calls []tgCall
	// fail — методы, на которые сервер отвечает ошибкой (например sendMessage).
	fail map[string]bool
}

func (f *fakeTelegram) handler(w http.ResponseWriter, r *http.Request) {
	method := r.URL.Path[strings.LastIndex(r.URL.Path, "/")+1:]
	_ = r.ParseMultipartForm(1 << 20)
	f.mu.Lock()
	f.calls = append(f.calls, tgCall{Method: method, Form: r.Form})
	failed := f.fail[method]
	f.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case failed:
		_, _ = w.Write([]byte(`{"ok":false,"error_code":400,"description":"Bad Request: test failure"}`))
	case method == "getMe":
		_, _ = w.Write([]byte(`{"ok":true,"result":{"id":1000002,"is_bot":true,"first_name":"Leo","username":"leo_it_bot"}}`))
	case method == "answerPreCheckoutQuery", method == "setChatMenuButton", method == "answerCallbackQuery", method == "refundStarPayment":
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	case method == "createInvoiceLink":
		_, _ = w.Write([]byte(`{"ok":true,"result":"https://t.me/$invoice-test"}`))
	default:
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1,"date":1,"chat":{"id":1,"type":"private"}}}`))
	}
}

// sent — вызовы одного метода Bot API (например "sendMessage").
func (f *fakeTelegram) sent(method string) []tgCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []tgCall
	for _, c := range f.calls {
		if c.Method == method {
			out = append(out, c)
		}
	}
	return out
}

// textsTo — тексты сообщений, отправленных пользователю chatID.
func (f *fakeTelegram) textsTo(chatID int64) []string {
	var out []string
	for _, c := range f.sent("sendMessage") {
		if c.Form.Get("chat_id") == itoa(chatID) {
			out = append(out, c.Form.Get("text"))
		}
	}
	return out
}

func (f *fakeTelegram) reset() {
	f.mu.Lock()
	f.calls = nil
	f.mu.Unlock()
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// newIntegrationBot — бот на отдельной пустой базе со всеми миграциями.
// tune правит конфиг до создания бота. Без LEO_TEST_PG_DSN тест пропускается.
func newIntegrationBot(t *testing.T, tune func(*config.Config)) (*Bot, *fakeTelegram, *sql.DB) {
	t.Helper()
	dsn := os.Getenv("LEO_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("LEO_TEST_PG_DSN не задан")
	}
	// Своя база: другие пакеты гоняют тесты параллельно на базе из DSN.
	admin, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	const name = "leo_bot_test"
	if _, err := admin.Exec(`DROP DATABASE IF EXISTS ` + name + ` WITH (FORCE)`); err != nil {
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
	sqlDB, err := sql.Open("postgres", u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	db := database.NewForTest(sqlDB)
	if err := db.CreateTables(); err != nil {
		t.Fatal(err)
	}
	if err := db.EnsureTrackerSchema(); err != nil {
		t.Fatal(err)
	}

	tg := &fakeTelegram{fail: map[string]bool{}}
	srv := httptest.NewServer(http.HandlerFunc(tg.handler))
	t.Cleanup(srv.Close)
	api, err := tgbotapi.NewBotAPIWithAPIEndpoint("1000002:IT-TOKEN", srv.URL+"/bot%s/%s")
	if err != nil {
		t.Fatal(err)
	}
	cfg := &config.Config{
		APIToken:         "1000002:IT-TOKEN",
		OwnerID:          itAdmin,
		MonetizedChatID:  itPack,
		PaywallEnabled:   true,
		PaywallEntryFree: true,
	}
	if tune != nil {
		tune(cfg)
	}
	b := NewForTest(cfg, db, logger.New("error"), api)
	// Отметки «Claude не ответил» живут в памяти процесса и привязаны к id
	// задачи, а id в каждой новой базе начинаются с единицы.
	trackerLeoVoteRetry.Range(func(k, _ any) bool {
		trackerLeoVoteRetry.Delete(k)
		return true
	})
	tg.reset()
	return b, tg, sqlDB
}

// seedMember заводит участника стаи; kicked — уже удалён за неактивность.
func seedMember(t *testing.T, db *sql.DB, userID int64, username string, kicked bool) {
	t.Helper()
	if _, err := db.Exec(`
		INSERT INTO training_state (user_id, chat_id, username, last_message, is_deleted)
		VALUES ($1, $2, $3, '', $4)`, userID, itPack, username, kicked); err != nil {
		t.Fatalf("участник: %v", err)
	}
}
