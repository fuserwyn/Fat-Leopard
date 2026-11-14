package bot

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Тестовый стенд (окружение leo-lab), который держит эксперименты Лео, стоит
// денег, пока крутится. Управлять им из Railway неудобно: админ и так сидит в
// мини-аппе прода. Поэтому здесь — статус и две кнопки.
//
// Выключение — это остановка приложений стенда, а не удаление окружения: базы
// и тома остаются на месте, поэтому включение возвращает стенд с теми же
// данными. Базы не трогаем сознательно: остановленный Postgres — это риск
// потерять данные ради пары центов.

const (
	standTimeout = 30 * time.Second
	// Стоп/старт делаем по одному сервису; список задаётся окружением, чтобы
	// добавление нового сервиса не требовало правки кода.
	standServicesEnvHint = "LAB_SERVICE_IDS"
)

// MiniappStandService — один сервис стенда для интерфейса.
type MiniappStandService struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status string `json:"status"`
}

// MiniappStandState — что показываем в админке.
type MiniappStandState struct {
	Configured bool                  `json:"configured"`
	Running    bool                  `json:"running"`
	MiniappURL string                `json:"miniapp_url"`
	Services   []MiniappStandService `json:"services"`
	// Для прогресса: сколько сервисов уже в нужном состоянии. Переключение не
	// мгновенное — контейнеры гаснут и поднимаются по одному, и без этих чисел
	// админ видит зависшую кнопку и жмёт её повторно.
	Total int  `json:"total"`
	Up    int  `json:"up"`
	Busy  bool `json:"busy"`
}

// MiniappLabStand — статус и управление тестовым стендом.
// action: status | start | stop.
func (b *Bot) MiniappLabStand(
	viewerUserID int64, initD initdata.InitData, action string,
) (MiniappStandState, error) {
	var out MiniappStandState
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	// Стенд — это деньги и инфраструктура, поэтому только настоящие админы.
	if b.config == nil || !b.config.IsAdminTelegramUser(viewerUserID) {
		return out, ErrPackFeedForbidden
	}
	if strings.TrimSpace(b.config.RailwayToken) == "" ||
		strings.TrimSpace(b.config.LabEnvironmentID) == "" ||
		len(b.config.LabServiceIDs()) == 0 {
		out.MiniappURL = strings.TrimSpace(b.config.LabMiniappURL)
		return out, nil // не настроен — просто не показываем блок
	}
	out.Configured = true
	out.MiniappURL = strings.TrimSpace(b.config.LabMiniappURL)

	switch strings.TrimSpace(action) {
	case "", "status":
	case "stop":
		if err := b.standSwitch(false); err != nil {
			return out, err
		}
		b.announceStand(false, trackerViewerName(initD))
	case "start":
		if err := b.standSwitch(true); err != nil {
			return out, err
		}
		b.announceStand(true, trackerViewerName(initD))
	default:
		return out, ErrAdminActionInvalid
	}

	services, err := b.standServices()
	if err != nil {
		return out, err
	}
	out.Services = services
	out.Total = len(services)
	for _, svc := range services {
		switch svc.Status {
		case "SUCCESS":
			out.Up++
		case "DEPLOYING", "BUILDING", "INITIALIZING", "QUEUED", "WAITING", "REMOVING":
			// Переходное состояние: прогресс ещё идёт, кнопку держим занятой.
			out.Busy = true
		}
	}
	out.Running = out.Up > 0
	return out, nil
}

// announceStand — сказать админам, что стенд включили или выключили.
//
// Стенд общий на троих: тот, кто его не трогал, должен понимать, почему он вдруг
// не отвечает или, наоборот, снова тратит ресурсы.
func (b *Bot) announceStand(on bool, who string) {
	who = strings.TrimSpace(who)
	if who == "" {
		who = "админ"
	}
	text := fmt.Sprintf("🧪 Тестовый стенд выключен (%s).\n\n"+
		"Контейнеры остановлены, автосборка снята — пуш в ветку стенда его больше "+
		"не поднимет. Данные лежат на томах, включение вернёт стенд как был.", who)
	if on {
		text = fmt.Sprintf("🧪 Тестовый стенд включён (%s).\n\n"+
			"Сервисы поднимаются, это занимает пару минут.", who)
	}
	if err := b.NotifyTrackerResult(0, text); err != nil {
		b.logger.Warnf("стенд: не сообщить админам: %v", err)
	}
}

// standSwitch — поднять или остановить приложения стенда.
func (b *Bot) standSwitch(on bool) error {
	var firstErr error
	for _, serviceID := range b.config.LabServiceIDs() {
		var err error
		if on {
			if e := b.standAutoDeploy(serviceID, true); e != nil && firstErr == nil {
				firstErr = e
			}
			_, err = b.railwayCall(
				`mutation($s:String!,$e:String!){ serviceInstanceDeployV2(serviceId:$s, environmentId:$e) }`,
				map[string]any{"s": serviceID, "e": strings.TrimSpace(b.config.LabEnvironmentID)},
			)
		} else {
			// Сначала снимаем автосборку, потом гасим: иначе сборка, запущенная
			// в этот момент, поднимет сервис сразу после остановки.
			if e := b.standAutoDeploy(serviceID, false); e != nil && firstErr == nil {
				firstErr = e
			}
			err = b.standStopService(serviceID)
		}
		if err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// standAutoDeploy — включить или выключить автосборку сервиса.
//
// Без этого выключенный стенд оживал сам: любой пуш в его ветку запускал
// сборку, и мини-апп снова оказывался онлайн — со стороны это выглядело как
// «выключение не сработало».
func (b *Bot) standAutoDeploy(serviceID string, enabled bool) error {
	_, err := b.railwayCall(
		`mutation($in:ServiceInstanceAutoDeployUpdateInput!){ serviceInstanceAutoDeployUpdate(input:$in){ enabled } }`,
		map[string]any{"in": map[string]any{
			"serviceId":     serviceID,
			"environmentId": strings.TrimSpace(b.config.LabEnvironmentID),
			"projectId":     strings.TrimSpace(b.config.RailwayProjectID),
			"enabled":       enabled,
		}},
	)
	return err
}

// standStopService — остановить текущий деплой сервиса стенда.
func (b *Bot) standStopService(serviceID string) error {
	raw, err := b.railwayCall(
		`query($sid:String!,$eid:String!){ deployments(first:1, input:{serviceId:$sid, environmentId:$eid}){ edges{ node{ id status } } } }`,
		map[string]any{"sid": serviceID, "eid": strings.TrimSpace(b.config.LabEnvironmentID)},
	)
	if err != nil {
		return err
	}
	var parsed struct {
		Deployments struct {
			Edges []struct {
				Node struct {
					ID     string `json:"id"`
					Status string `json:"status"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return err
	}
	if len(parsed.Deployments.Edges) == 0 {
		return nil // нечего останавливать
	}
	node := parsed.Deployments.Edges[0].Node
	if node.Status == "REMOVED" || node.Status == "CRASHED" || node.Status == "FAILED" {
		return nil
	}
	// deploymentRemove, а не deploymentStop: stop гасит контейнер, но в API
	// деплой продолжает числиться успешным — прогресс и подпись кнопки на этом
	// врали бы. После remove статус становится REMOVED, и состояние видно честно.
	_, err = b.railwayCall(
		`mutation($id:String!){ deploymentRemove(id:$id) }`,
		map[string]any{"id": node.ID},
	)
	return err
}

// standServices — имя и состояние каждого сервиса стенда.
func (b *Bot) standServices() ([]MiniappStandService, error) {
	out := make([]MiniappStandService, 0, len(b.config.LabServiceIDs()))
	for _, serviceID := range b.config.LabServiceIDs() {
		raw, err := b.railwayCall(
			`query($sid:String!,$eid:String!){ service(id:$sid){ name } deployments(first:5, input:{serviceId:$sid, environmentId:$eid}){ edges{ node{ status } } } }`,
			map[string]any{"sid": serviceID, "eid": strings.TrimSpace(b.config.LabEnvironmentID)},
		)
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Service struct {
				Name string `json:"name"`
			} `json:"service"`
			Deployments struct {
				Edges []struct {
					Node struct {
						Status string `json:"status"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"deployments"`
		}
		if err := json.Unmarshal(raw, &parsed); err != nil {
			return nil, err
		}
		// Берём свежий значимый выкат: SKIPPED — это пуш, который сервиса не
		// касался, и о состоянии контейнера он ничего не говорит.
		status := "нет деплоя"
		for _, e := range parsed.Deployments.Edges {
			if e.Node.Status == "SKIPPED" {
				continue
			}
			status = e.Node.Status
			break
		}
		name := parsed.Service.Name
		if name == "" {
			name = serviceID[:8]
		}
		out = append(out, MiniappStandService{ID: serviceID, Name: name, Status: status})
	}
	return out, nil
}

// railwayCall — запрос к Railway API от имени проекта.
func (b *Bot) railwayCall(query string, variables map[string]any) (json.RawMessage, error) {
	body, err := json.Marshal(map[string]any{"query": query, "variables": variables})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(http.MethodPost, railwayGraphQL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+strings.TrimSpace(b.config.RailwayToken))
	res, err := (&http.Client{Timeout: standTimeout}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("Railway недоступен: %w", err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		return nil, fmt.Errorf("Railway ответил %d", res.StatusCode)
	}
	var envelope struct {
		Data   json.RawMessage `json:"data"`
		Errors []struct {
			Message string `json:"message"`
		} `json:"errors"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if len(envelope.Errors) > 0 {
		return nil, fmt.Errorf("%s", envelope.Errors[0].Message)
	}
	return envelope.Data, nil
}
