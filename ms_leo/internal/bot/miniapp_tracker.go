package bot

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Трекер задач Леопарда: карточки в отдельной БД (TRACKER_DATABASE_URL),
// не в Postgres стаи. Доску рисует TrackerScreen в админке.
//
// Раньше ходили на чужую доску по гостевой сессии. Своя доска не зависит от
// чужого секрета, а выкатить код на сервер человек делает сам («запушь»).
const trackerSessionTTL = 12 * time.Hour

// ErrTrackerNotConfigured — старый код гостевой сессии: доска теперь своя,
// этот код остаётся только для входящих уведомлений.
var ErrTrackerNotConfigured = fmt.Errorf("tracker not configured")

// MiniappTrackerAttach — приложить картинку к задаче.
// Мини-апп отдаёт готовое изображение из canvas base64-строкой.
func (b *Bot) MiniappTrackerAttach(
	viewerUserID int64,
	initD initdata.InitData,
	taskID int64,
	filename string,
	mime string,
	dataBase64 string,
) (json.RawMessage, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	if taskID <= 0 {
		return nil, ErrAdminActionInvalid
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(dataBase64))
	if err != nil {
		return nil, fmt.Errorf("картинка не разобралась")
	}
	att, err := b.db.AddTrackerAttachment(taskID, filename, mime, raw)
	if err != nil {
		return nil, err
	}
	return trackerJSON(map[string]any{"id": att.ID, "name": att.Name, "mime": att.Mime, "size": att.Size})
}

// MiniappTrackerAttachmentGet — байты приложенного к задаче фото.
func (b *Bot) MiniappTrackerAttachmentGet(
	viewerUserID int64, initD initdata.InitData, taskID int64, attID string,
) (mime string, data string, err error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return "", "", err
	}
	att, err := b.db.GetTrackerAttachment(taskID, attID)
	if err != nil {
		return "", "", err
	}
	mime = att.Mime
	if mime == "" {
		mime = "image/jpeg"
	}
	return mime, base64.StdEncoding.EncodeToString(att.Data), nil
}

// MiniappTrackerAttachmentDelete — снять фото с задачи: так работает «заменить».
func (b *Bot) MiniappTrackerAttachmentDelete(
	viewerUserID int64, initD initdata.InitData, taskID int64, attID string,
) error {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return err
	}
	return b.db.DeleteTrackerAttachment(taskID, attID)
}

// MiniappTrackerAuthors — кто ставил задачи: ник и имя по telegram_id.
// В трекере у задачи есть только author_id, а на доске нужен человек.
func (b *Bot) MiniappTrackerAuthors(
	viewerUserID int64, initD initdata.InitData, ids []int64,
) ([]database.AdminPersonRow, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	if len(ids) == 0 {
		return nil, nil
	}
	if len(ids) > 100 {
		ids = ids[:100]
	}
	return b.db.AdminPeopleByIDs(b.adminPackChatID(), ids)
}

// trackerSession — подписанная гостевая сессия MyVibeLab: тот же формат, что
// и у него самого (payload.signature, payload = base64url(JSON) без padding).
func (b *Bot) trackerSession(userID int64, name string) (string, error) {
	secret := strings.TrimSpace(b.config.BoardSecret)
	repo := strings.TrimSpace(b.config.BoardRepo)
	if secret == "" || repo == "" || strings.TrimSpace(b.config.BoardURL) == "" {
		return "", ErrTrackerNotConfigured
	}
	if userID <= 0 {
		return "", fmt.Errorf("не задан OWNER_ID для сессии доски")
	}
	// Пустую ветку в подпись не кладём: MyVibeLab тогда заводит отдельную
	// ветку задачи, помечает её «выполнено» и не пушит в основную — сборка
	// на сервере не стартует. Нет поля — работает ветка репозитория по умолчанию.
	sess := map[string]any{
		"k": "sess",
		"r": repo,
		"u": userID,
		"n": name,
		"e": time.Now().Add(trackerSessionTTL).Unix(),
	}
	if branch := strings.TrimSpace(b.config.BoardBranch); branch != "" {
		sess["b"] = branch
	}
	payload, err := json.Marshal(sess)
	if err != nil {
		return "", err
	}
	encoded := base64.RawURLEncoding.EncodeToString(payload)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(encoded))
	return encoded + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}

func trackerViewerName(initD initdata.InitData) string {
	name := strings.TrimSpace(initD.User.FirstName + " " + initD.User.LastName)
	if name == "" {
		name = initD.User.Username
	}
	return name
}

// MiniappTrackerCall — операция своей доски от имени админа мини-аппа.
func (b *Bot) MiniappTrackerCall(
	viewerUserID int64,
	initD initdata.InitData,
	op string,
	taskID int64,
	payload map[string]any,
) (json.RawMessage, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	name := trackerViewerName(initD)
	raw, err := b.trackerRequest(op, taskID, payload, viewerUserID, name)
	if err != nil {
		return raw, err
	}
	// QA «принять» = фича готова: помечаем к публикации, пуш делает человек.
	if trackerOpShouldShip(op, payload) {
		b.ShipTrackerTaskInBackground(trackerPayloadTaskID(taskID, payload), viewerUserID)
	}
	return raw, nil
}

// trackerOpShouldShip — после каких действий доски код должен уехать на сервер.
func trackerOpShouldShip(op string, payload map[string]any) bool {
	if op != "qa" || payload == nil {
		return false
	}
	action, _ := payload["action"].(string)
	return strings.EqualFold(strings.TrimSpace(action), "pass")
}

// trackerRequest — операция своей доски от имени userID.
//
// Отдельно от MiniappTrackerCall: автономный Лео ставит задачи сам, и права
// проверять не у кого — там решает состояние в базе, а не initData.
func (b *Bot) trackerRequest(
	op string, taskID int64, payload map[string]any, userID int64, name string,
) (json.RawMessage, error) {
	_ = name
	if b.db == nil {
		return nil, fmt.Errorf("база недоступна")
	}
	switch op {
	case "list":
		return b.localTrackerList()
	case "refresh":
		return b.localTrackerRefresh()
	case "create":
		return b.localTrackerCreate(payload, userID)
	case "task", "status":
		return b.localTrackerTask(taskID, payload)
	case "cancel":
		return b.localTrackerCancel(taskID, payload)
	case "delete":
		return b.localTrackerDelete(taskID, payload)
	case "qa":
		return b.localTrackerQa(taskID, payload)
	case "auto_qa":
		return b.localTrackerAutoQa(taskID, payload)
	case "review":
		return b.localTrackerComposer(taskID, payload, "review")
	case "auto_test":
		return b.localTrackerComposer(taskID, payload, "test")
	case "prompt":
		return b.localTrackerPrompt(taskID, payload)
	case "reschedule":
		return b.localTrackerReschedule(taskID, payload)
	case "restart":
		return b.localTrackerRestart(taskID, payload)
	case "move":
		return b.localTrackerMove(taskID, payload)
	case "promote":
		return b.localTrackerPromoteRevert("Перенос")
	case "revert":
		return b.localTrackerPromoteRevert("Откат")
	case "ship", "push", "deploy_refresh", "deploy_watch":
		return b.localTrackerShip(taskID, payload)
	case "deploy":
		return b.localTrackerDeployNow(taskID, payload)
	case "deploy_settings":
		return b.localTrackerDeploySettings(payload, userID)
	case "sprint_ideas":
		return b.localTrackerSprintIdeas(payload)
	case "sprint_generate":
		return b.localTrackerSprintGenerate(payload)
	case "sprint_apply":
		return b.localTrackerSprintApply(payload, userID)
	case "approve":
		return b.localTrackerApprove(taskID, payload, userID)
	default:
		return nil, ErrAdminActionInvalid
	}
}

// trackerTaskSnapshot — поля задачи, нужные чтобы понять, надо ли собирать на сервере.
type trackerTaskSnapshot struct {
	Status     string `json:"status"`
	Branch     string `json:"branch"`
	Commit     string `json:"commit"`
	Error      string `json:"error"`
	Done       bool   `json:"done"`
	DevColumn  string `json:"dev_column"`
	QaColumn   string `json:"qa_column"`
	QaStatus   string `json:"qa_status"`
	HandedToQa bool   `json:"handed_to_qa"`
}

func parseTrackerTaskSnapshot(raw json.RawMessage) (trackerTaskSnapshot, error) {
	var wrap struct {
		Task trackerTaskSnapshot `json:"task"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return trackerTaskSnapshot{}, err
	}
	if wrap.Task.Status != "" || wrap.Task.Commit != "" || wrap.Task.Done || wrap.Task.DevColumn != "" {
		return wrap.Task, nil
	}
	var flat trackerTaskSnapshot
	if err := json.Unmarshal(raw, &flat); err != nil {
		return trackerTaskSnapshot{}, err
	}
	return flat, nil
}

// normalizeTrackerReschedule — mode=now без when превращаем в «сейчас»:
// карточка сразу уходит в работу, а не сидит минуту в «Ожидает».
func normalizeTrackerReschedule(op string, payload map[string]any) {
	if op != "reschedule" || payload == nil {
		return
	}
	when, _ := payload["when"].(string)
	if strings.TrimSpace(when) != "" {
		return
	}
	mode, _ := payload["mode"].(string)
	if !strings.EqualFold(strings.TrimSpace(mode), "now") {
		return
	}
	payload["when"] = "сейчас"
	delete(payload, "mode")
}

func trackerPayloadTaskID(taskID int64, payload map[string]any) int64 {
	if taskID > 0 {
		return taskID
	}
	if payload == nil {
		return 0
	}
	switch id := payload["id"].(type) {
	case float64:
		return int64(id)
	case int64:
		return id
	case int:
		return int64(id)
	case json.Number:
		n, _ := id.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(id), 10, 64)
		return n
	}
	return 0
}

// trackerTaskReadyToShip — агент закончил (или QA принял), можно пушить.
// Коммит не обязателен: с сервера проекта git push часто закрыт, SHA тогда
// пустой, а код уже есть у агента / на GitHub — его забирает /api/push.
func trackerTaskReadyToShip(t trackerTaskSnapshot) bool {
	status := strings.ToLower(strings.TrimSpace(t.Status))
	if status == "running" || status == "reviewing" || status == "pending" || status == "scheduled" {
		return false
	}
	if trackerErrorBlocksShip(t.Error) {
		return false
	}
	if t.Done {
		return true
	}
	if status == "done" || status == "completed" || status == "holding" {
		return true
	}
	col := strings.ToLower(strings.TrimSpace(t.DevColumn))
	if col == "done" || col == "deploy" || col == "test" {
		return true
	}
	qa := strings.ToLower(strings.TrimSpace(t.QaColumn))
	if t.HandedToQa && (qa == "done" || strings.EqualFold(strings.TrimSpace(t.QaStatus), "pass")) {
		return true
	}
	return strings.TrimSpace(t.Commit) != ""
}

// trackerErrorBlocksShip — настоящий срыв задачи. «Git push недоступен»
// как раз тот случай, ради которого мы сами зовём /api/push.
func trackerErrorBlocksShip(err string) bool {
	e := strings.TrimSpace(err)
	if e == "" {
		return false
	}
	low := strings.ToLower(e)
	if strings.Contains(low, "push") || strings.Contains(low, "пуш") || strings.Contains(low, "git") {
		return false
	}
	// Сорванная сборка чинится повтором выката, а не возвратом в работу.
	// Иначе карточка запиралась: «Задеплоить» отвечало skipped и молчало.
	if strings.Contains(low, "деплой") || strings.Contains(low, "сборка") ||
		strings.Contains(low, "стенд") || strings.Contains(low, "railway") {
		return false
	}
	return true
}

// shipCompletedTrackerTask — отметить задачу готовой к публикации.
// Сами мы git не трогаем: выкатить код человек делает через «запушь».
func (b *Bot) shipCompletedTrackerTask(taskID int64, payload map[string]any, userID int64, name string) (json.RawMessage, error) {
	_ = userID
	_ = name
	return b.localTrackerShip(taskID, payload)
}

// ShipTrackerTaskInBackground — после уведомления доски довести сдачу до
// сервера, не держа вебхук. Пуш и сборка занимают минуты, автору ответ уже ушёл.
func (b *Bot) ShipTrackerTaskInBackground(taskID, authorID int64) {
	if b == nil || taskID <= 0 {
		return
	}
	go func() {
		defer func() {
			if rec := recover(); rec != nil && b.logger != nil {
				b.logger.Errorf("трекер: паника при сборке #%d: %v", taskID, rec)
			}
		}()
		userID := authorID
		if userID <= 0 {
			userID = b.leoBoardUserID()
		}
		var err error
		delays := []time.Duration{3 * time.Second, 6 * time.Second, 12 * time.Second, 20 * time.Second, 30 * time.Second, 45 * time.Second, 60 * time.Second}
		for attempt := 0; attempt <= len(delays); attempt++ {
			if attempt > 0 {
				time.Sleep(delays[attempt-1])
			}
			var raw json.RawMessage
			raw, err = b.shipCompletedTrackerTask(taskID, nil, userID, "Стая")
			if err != nil {
				continue
			}
			var res struct {
				Skipped  bool   `json:"skipped"`
				Pushed   bool   `json:"pushed"`
				Deployed bool   `json:"deployed"`
				Promoted bool   `json:"promoted"`
				Error    string `json:"error"`
			}
			_ = json.Unmarshal(raw, &res)
			if res.Skipped {
				err = fmt.Errorf("ещё не готова")
				continue
			}
			if res.Pushed || res.Deployed || res.Promoted {
				if res.Error != "" && b.logger != nil {
					b.logger.Warnf("трекер: задача #%d на сервере с оговоркой: %s", taskID, res.Error)
				}
				return
			}
			if res.Error != "" {
				err = fmt.Errorf("%s", res.Error)
				continue
			}
			err = fmt.Errorf("пуш и сборка не стартовали")
		}
		if err != nil {
			if b.logger != nil {
				b.logger.Warnf("трекер: не собрать задачу #%d на сервере: %v", taskID, err)
			}
		}
	}()
}

// VerifyTrackerToken — проверить подпись, которой MyVibeLab метит свои запросы
// к нам (уведомления о задачах). Формат тот же, что у ссылки на доску.
// Возвращает repo и id человека, для которого запрос.
func (b *Bot) VerifyTrackerToken(token, kind string) (repo string, userID int64, ok bool) {
	secret := strings.TrimSpace(b.config.BoardSecret)
	if secret == "" || token == "" || !strings.Contains(token, ".") {
		return "", 0, false
	}
	parts := strings.SplitN(token, ".", 2)
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(parts[0]))
	want := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(want), []byte(parts[1])) {
		return "", 0, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", 0, false
	}
	var payload struct {
		Kind string `json:"k"`
		Repo string `json:"r"`
		User int64  `json:"u"`
		Exp  int64  `json:"e"`
	}
	if err := json.Unmarshal(raw, &payload); err != nil {
		return "", 0, false
	}
	if payload.Kind != kind || payload.Exp < time.Now().Unix() {
		return "", 0, false
	}
	return payload.Repo, payload.User, true
}

// NotifyTrackerAuthor — написать автору задачи в личку от имени бота стаи.
func (b *Bot) NotifyTrackerAuthor(userID int64, text string) error {
	if b == nil || b.api == nil || userID == 0 {
		return fmt.Errorf("некому писать")
	}
	msg := tgbotapi.NewMessage(userID, text)
	msg.DisableWebPagePreview = true
	_, err := b.api.Send(msg)
	return err
}

// trackerAdminNotifyIDs — все админы стаи: из env и динамические.
func (b *Bot) trackerAdminNotifyIDs() []int64 {
	if b == nil || b.config == nil {
		return nil
	}
	seen := make(map[int64]struct{})
	out := make([]int64, 0, len(b.config.AdminIDs)+2)
	add := func(id int64) {
		if id <= 0 {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	add(b.config.OwnerID)
	for _, id := range b.config.AdminIDs {
		add(id)
	}
	b.dynamicAdminsMu.RLock()
	for id := range b.dynamicAdmins {
		add(id)
	}
	b.dynamicAdminsMu.RUnlock()
	return out
}

// trackerResultNotifyTargets — кому писать о судьбе задачи.
func (b *Bot) trackerResultNotifyTargets(authorID int64) []int64 {
	if authorID <= 0 {
		return b.trackerAdminNotifyIDs()
	}
	if !b.isAdminTelegramUser(authorID) {
		return []int64{authorID}
	}
	seen := map[int64]struct{}{authorID: {}}
	out := []int64{authorID}
	for _, id := range b.trackerAdminNotifyIDs() {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// NotifyTrackerResult — сообщить о судьбе задачи: автору, а если его нет
// (ставили из чата) или автор не человек (задачу придумал Лео — id меньше
// нуля) — админам стаи. Если задачу ставил админ — остальные админы тоже
// получат уведомление о выкате.
func (b *Bot) NotifyTrackerResult(authorID int64, text string) error {
	targets := b.trackerResultNotifyTargets(authorID)
	if len(targets) == 0 {
		return fmt.Errorf("некому писать: админы не заданы")
	}
	var firstErr error
	for _, id := range targets {
		if err := b.NotifyTrackerAuthor(id, text); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}
