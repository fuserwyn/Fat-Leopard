package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"leo-bot/internal/database"
)

// Автодеплой задачи на Railway.
//
// Раньше карточка после пуша в main просто ждала, что Railway сам заметит
// коммит и соберётся. Если вебхук GitHub не сработал (автосборка сервиса
// снята, сборка ждёт CI, репозиторий переподключали), ждать было нечего:
// карточка висела десять минут и уходила в «не уложилась», либо закрывалась
// по живому стенду — «выполнено» без выката. Теперь сборку заказываем сами и
// ждём именно тот деплой, который заказали.

// trackerAutoDeployKey — ключ настройки в pack_tracker_settings.
const trackerAutoDeployKey = "auto_deploy"

// trackerDeployOrder — что уже заказано у Railway по этой карточке.
//
// Заказ переживает рестарт бота, и это обязательное условие: пересборка ms_leo
// перезапускает нас самих, планировщик каждые 15 секунд возвращается к
// карточке в «Сборке» (resumeTrackerStandBuilds), и без записи о заказе он
// просил бы новую сборку в каждом цикле — вечный редеплой самого себя.
type trackerDeployOrder struct {
	// Ordered — сборку уже заказали, повторять не нужно, только ждать.
	Ordered bool `json:"ordered"`
	// Pinned — id заказанных деплоев по имени сервиса.
	Pinned map[string]string `json:"pinned,omitempty"`
}

func trackerDeployOrderKey(taskID int64) string {
	return fmt.Sprintf("deploy_order_%d", taskID)
}

func (b *Bot) loadTrackerDeployOrder(taskID int64) trackerDeployOrder {
	var order trackerDeployOrder
	if b == nil || b.db == nil || taskID <= 0 {
		return order
	}
	raw, err := b.db.GetTrackerSetting(trackerDeployOrderKey(taskID))
	if err != nil || strings.TrimSpace(raw) == "" {
		return order
	}
	if err := json.Unmarshal([]byte(raw), &order); err != nil {
		return trackerDeployOrder{}
	}
	return order
}

func (b *Bot) saveTrackerDeployOrder(taskID int64, order trackerDeployOrder) {
	if b == nil || b.db == nil || taskID <= 0 || !order.Ordered {
		return
	}
	raw, err := json.Marshal(order)
	if err != nil {
		return
	}
	if err := b.db.SetTrackerSetting(trackerDeployOrderKey(taskID), string(raw), 0); err != nil && b.logger != nil {
		b.logger.Warnf("трекер: не запомнить заказ сборки #%d: %v", taskID, err)
	}
}

// clearTrackerDeployOrder — сборка кончилась (собралась, упала или её просят
// заново): следующий заход закажет новую.
func (b *Bot) clearTrackerDeployOrder(taskID int64) {
	if b == nil || b.db == nil || taskID <= 0 {
		return
	}
	if err := b.db.SetTrackerSetting(trackerDeployOrderKey(taskID), "", 0); err != nil && b.logger != nil {
		b.logger.Warnf("трекер: не снять заказ сборки #%d: %v", taskID, err)
	}
}

// appendTrackerStepOnce — не повторять тот же шаг подряд. Планировщик
// возвращается к карточке каждые 15 секунд, и без этого лог «автодеплой не
// настроен» вытеснил бы всю историю задачи.
func appendTrackerStepOnce(t *database.TrackerTask, step string) {
	if t == nil {
		return
	}
	if n := len(t.Steps); n > 0 && strings.TrimSpace(t.Steps[n-1]) == strings.TrimSpace(step) {
		return
	}
	appendTrackerStep(t, step)
}

// TrackerDeployState — что показываем админу в трекере.
type TrackerDeployState struct {
	// Enabled — заказывать ли сборку самим после пуша в main.
	Enabled bool `json:"enabled"`
	// Configured — есть ли доступ к Railway API.
	Configured bool `json:"configured"`
	// Services — что именно пересобираем.
	Services []string `json:"services"`
	// Hint — почему автодеплой не сработает, если не сработает.
	Hint string `json:"hint"`
}

// trackerAutoDeployEnabled — включён ли автодеплой. По умолчанию включён:
// код и так уезжает в main, и оставлять его несобранным смысла нет.
func (b *Bot) trackerAutoDeployEnabled() bool {
	if b == nil || b.db == nil {
		return false
	}
	value, err := b.db.GetTrackerSetting(trackerAutoDeployKey)
	if err != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не прочитать настройку автодеплоя: %v", err)
		}
		return true
	}
	return !strings.EqualFold(strings.TrimSpace(value), "off")
}

// setTrackerAutoDeploy — переключить автодеплой из админки.
func (b *Bot) setTrackerAutoDeploy(on bool, by int64) error {
	if b == nil || b.db == nil {
		return fmt.Errorf("база недоступна")
	}
	value := "off"
	if on {
		value = "on"
	}
	return b.db.SetTrackerSetting(trackerAutoDeployKey, value, by)
}

// trackerRailwayReady — настроен ли доступ к Railway API.
func (b *Bot) trackerRailwayReady() bool {
	return b != nil && b.config != nil &&
		strings.TrimSpace(b.config.RailwayToken) != "" &&
		strings.TrimSpace(b.config.RailwayProjectID) != ""
}

// trackerDeployStatus — состояние автодеплоя для админки.
func (b *Bot) trackerDeployStatus() TrackerDeployState {
	out := TrackerDeployState{
		Enabled:    b.trackerAutoDeployEnabled(),
		Configured: b.trackerRailwayReady(),
	}
	if !out.Configured {
		out.Hint = "Нет доступа к Railway: задай сервису ms_leo переменные RAILWAY_API_TOKEN и RAILWAY_PROJECT_ID."
		return out
	}
	_, svcs, err := b.lookupMainStandServices()
	if err != nil {
		out.Hint = "Railway не отдал сервисы прода: " + err.Error()
		return out
	}
	for _, svc := range svcs {
		if !isStandWatchService(svc.Name) {
			continue
		}
		out.Services = append(out.Services, svc.Name)
	}
	return out
}

// startTrackerDeploy — заказать сборку прода на Railway.
//
// Возвращает заказ: заказали ли сборку и id деплоев по сервисам — их и ждём
// дальше, чтобы не принять за успех чужую старую сборку. Ordered=false —
// сборку не заказывали (выключено, не настроено или Railway не ответил);
// тогда ждём вебхук, как раньше, и пробуем снова на следующем заходе.
// Шаги пишем в карточку: админ должен видеть, почему выката нет.
func (b *Bot) startTrackerDeploy(t *database.TrackerTask) trackerDeployOrder {
	if b == nil || t == nil {
		return trackerDeployOrder{}
	}
	if !b.trackerAutoDeployEnabled() {
		appendTrackerStepOnce(t, "автодеплой выключен: ждём сборку по вебхуку")
		return trackerDeployOrder{}
	}
	if !b.trackerRailwayReady() {
		appendTrackerStepOnce(t, "автодеплой не настроен: нет RAILWAY_API_TOKEN или RAILWAY_PROJECT_ID")
		return trackerDeployOrder{}
	}
	envID, svcs, err := b.lookupMainStandServices()
	if err != nil {
		appendTrackerStepOnce(t, "Railway не отдал сервисы: "+err.Error())
		return trackerDeployOrder{}
	}
	order := trackerDeployOrder{Pinned: make(map[string]string, len(svcs))}
	for _, svc := range trackerDeployQueue(svcs) {
		if !isStandWatchService(svc.Name) {
			continue
		}
		deployID, derr := b.triggerRailwayDeploy(envID, svc.ID)
		if derr != nil {
			appendTrackerStepOnce(t, "Railway не собрал "+svc.Name+": "+derr.Error())
			continue
		}
		order.Ordered = true
		if deployID != "" {
			order.Pinned[svc.Name] = deployID
		}
		appendTrackerStep(t, "Railway: собираем "+svc.Name)
	}
	if !order.Ordered {
		appendTrackerStepOnce(t, "Railway не принял заказ сборки: ждём вебхук")
	}
	return order
}

// trackerDeployQueue — порядок заказа сборки: сам бот последним.
//
// Пересборка ms_leo убивает этот же процесс. Закажи мы её первой — Railway
// погасил бы нас раньше, чем дойдёт очередь до MiniApp, и фронт остался бы
// несобранным. Своя сборка идёт в конец, когда всё остальное уже заказано.
func trackerDeployQueue(svcs []standService) []standService {
	out := make([]standService, 0, len(svcs))
	var self []standService
	for _, svc := range svcs {
		if trackerDeploySelfService(svc.Name) {
			self = append(self, svc)
			continue
		}
		out = append(out, svc)
	}
	return append(out, self...)
}

// trackerDeploySelfService — это сервис с этим самым ботом.
func trackerDeploySelfService(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "_", "-")
	if strings.Contains(n, "miniapp") || strings.Contains(n, "tracker") {
		return false
	}
	return n == "leo" || strings.Contains(n, "ms-leo") || strings.HasPrefix(n, "leo-") ||
		strings.Contains(n, "fat-leopard")
}

// triggerRailwayDeploy — попросить Railway собрать сервис заново.
// Мутация та же, которой поднимаем тестовый стенд; в ответе — id деплоя.
func (b *Bot) triggerRailwayDeploy(envID, svcID string) (string, error) {
	envID = strings.TrimSpace(envID)
	svcID = strings.TrimSpace(svcID)
	if envID == "" || svcID == "" {
		return "", fmt.Errorf("нет сервиса или окружения")
	}
	vars := map[string]any{"s": svcID, "e": envID}
	// Как myvibelab: latestCommit тянет HEAD с GitHub. V2 без этого часто
	// пересобирает старый образ, а вебхук мог быть снят.
	mutations := []string{
		`mutation($s:String!,$e:String!){ serviceInstanceDeploy(serviceId:$s, environmentId:$e, latestCommit:true) }`,
		`mutation($s:String!,$e:String!){ serviceInstanceDeployV2(serviceId:$s, environmentId:$e) }`,
		`mutation($s:String!,$e:String!){ serviceInstanceRedeploy(serviceId:$s, environmentId:$e) }`,
	}
	var last error
	for _, q := range mutations {
		raw, err := b.railwayCall(q, vars)
		if err != nil {
			last = err
			continue
		}
		return railwayDeployID(raw), nil
	}
	if last != nil {
		return "", last
	}
	return "", fmt.Errorf("Railway не принял деплой")
}

func railwayDeployID(raw []byte) string {
	var parsed struct {
		Deploy   json.RawMessage `json:"serviceInstanceDeploy"`
		DeployV2 json.RawMessage `json:"serviceInstanceDeployV2"`
		Redeploy json.RawMessage `json:"serviceInstanceRedeploy"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return ""
	}
	for _, field := range []json.RawMessage{parsed.Deploy, parsed.DeployV2, parsed.Redeploy} {
		if id := railwayDeployIDField(field); id != "" {
			return id
		}
	}
	return ""
}

func railwayDeployIDField(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 {
		return ""
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		return strings.TrimSpace(s)
	}
	var obj struct {
		ID string `json:"id"`
	}
	if json.Unmarshal(raw, &obj) == nil {
		return strings.TrimSpace(obj.ID)
	}
	return ""
}

// localTrackerDeployNow — кнопка «Задеплоить» в админке: пересобрать прод
// для этой карточки прямо сейчас, даже если она уже «Выполнено».
func (b *Bot) localTrackerDeployNow(taskID int64, payload map[string]any) (json.RawMessage, error) {
	t, err := b.localTrackerLoad(taskID, payload)
	if err != nil {
		return nil, err
	}
	if !trackerTaskHasCode(t) {
		return nil, fmt.Errorf("нечего выкатывать: у задачи нет коммита")
	}
	if trackerStandBusy(t.ID) {
		return trackerJSON(map[string]any{"ok": true, "deploy": false, "busy": true})
	}
	_ = applyTrackerColumn(&t, trackerColDeploy)
	t.Error = ""
	// Снимаем прошлый заказ: админ просит именно новую сборку.
	b.clearTrackerDeployOrder(t.ID)
	appendTrackerStep(&t, "Ручной деплой из админки")
	if err := b.db.SaveTrackerTask(t); err != nil {
		return nil, err
	}
	b.kickTrackerPipeline(t)
	return trackerJSON(map[string]any{"ok": true, "deploy": true, "busy": false})
}

// localTrackerDeploySettings — прочитать или переключить автодеплой.
// action: пусто/status — только показать, on / off — переключить.
func (b *Bot) localTrackerDeploySettings(payload map[string]any, userID int64) (json.RawMessage, error) {
	switch strings.ToLower(payloadString(payload, "action")) {
	case "", "status":
	case "on":
		if err := b.setTrackerAutoDeploy(true, userID); err != nil {
			return nil, err
		}
	case "off":
		if err := b.setTrackerAutoDeploy(false, userID); err != nil {
			return nil, err
		}
	default:
		return nil, ErrAdminActionInvalid
	}
	return trackerJSON(map[string]any{"ok": true, "deploy": b.trackerDeployStatus()})
}
