package bot

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"leo-bot/internal/database"
)

const trackerStandWait = 10 * time.Minute
const trackerStandPoll = 12 * time.Second
const trackerStandSkipGrace = 45 * time.Second
const trackerStandLogClip = 2000

var trackerStandInflight sync.Map

type standDeploy struct {
	ID        string
	Status    string
	Service   string
	CreatedAt time.Time
}

type standService struct {
	ID   string
	Name string
}

type standWaitOutcome struct {
	Done      bool
	Err       error
	FailedID  string
	FailedSvc string
	Status    string
}

type standBuildError struct {
	Service  string
	DeployID string
	Status   string
	Msg      string
}

func (e *standBuildError) Error() string {
	if e == nil {
		return "сборка не прошла"
	}
	if strings.TrimSpace(e.Msg) != "" {
		return e.Msg
	}
	svc := strings.TrimSpace(e.Service)
	if svc == "" {
		svc = "стенд"
	}
	st := strings.ToLower(strings.TrimSpace(e.Status))
	if st == "" {
		st = "failed"
	}
	return fmt.Sprintf("%s: деплой %s", svc, st)
}

// trackerFailLooksLikeStub — агент снова сдал обрезанный файл. Повторный
// заход в работу только усугубляет: Cursor ещё раз затирает профиль.
func trackerFailLooksLikeStub(reason, logs string) bool {
	text := strings.ToLower(reason + "\n" + logs)
	if strings.Contains(text, "обрезан") || strings.Contains(text, "заглушк") {
		return true
	}
	return strings.Contains(text, "undefined:") || strings.Contains(text, "imported and not used")
}

func tryBeginTrackerStand(taskID int64) bool {
	_, loaded := trackerStandInflight.LoadOrStore(taskID, struct{}{})
	return !loaded
}

func endTrackerStand(taskID int64) {
	trackerStandInflight.Delete(taskID)
}

// trackerStandBusy — по этой карточке уже идёт сборка.
func trackerStandBusy(taskID int64) bool {
	_, busy := trackerStandInflight.Load(taskID)
	return busy
}

func standDeployInFlight(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "BUILDING", "DEPLOYING", "INITIALIZING", "QUEUED", "WAITING", "PENDING", "NEEDS_APPROVAL":
		return true
	default:
		return false
	}
}

func standDeployFresh(d standDeploy, since time.Time) bool {
	return d.CreatedAt.IsZero() || !d.CreatedAt.Before(since)
}

func standDeployFailed(status string) bool {
	switch strings.ToUpper(strings.TrimSpace(status)) {
	case "FAILED", "CRASHED":
		return true
	default:
		return false
	}
}

// standNewestRelevant — самая поздняя сборка этой выкладки: заказанная нами
// плюс всё, что Railway успел стартовать после пуша (вебхук GitHub).
func standNewestRelevant(deploys []standDeploy, pinnedID string, since time.Time) *standDeploy {
	pinnedID = strings.TrimSpace(pinnedID)
	var best *standDeploy
	for i := range deploys {
		d := &deploys[i]
		isPinned := pinnedID != "" && strings.TrimSpace(d.ID) == pinnedID
		if !isPinned && !standDeployFresh(*d, since) {
			continue
		}
		if best == nil {
			best = d
			continue
		}
		if d.CreatedAt.After(best.CreatedAt) {
			best = d
			continue
		}
		if d.CreatedAt.Equal(best.CreatedAt) && standDeployInFlight(d.Status) && !standDeployInFlight(best.Status) {
			best = d
		}
	}
	return best
}

func standAnyInFlight(deploys []standDeploy, pinnedID string, since time.Time) bool {
	pinnedID = strings.TrimSpace(pinnedID)
	for _, d := range deploys {
		isPinned := pinnedID != "" && strings.TrimSpace(d.ID) == pinnedID
		if !isPinned && !standDeployFresh(d, since) {
			continue
		}
		if standDeployInFlight(d.Status) {
			return true
		}
	}
	return false
}

// standWaitLatest — «выполнено» только когда кончилась последняя сборка,
// не первый SUCCESS. Иначе вебхук GitHub ещё крутится, а карточка уже зелёная.
func standWaitLatest(deploys []standDeploy, pinnedID string, since time.Time) standWaitOutcome {
	latest := standNewestRelevant(deploys, pinnedID, since)
	if latest == nil {
		if strings.TrimSpace(pinnedID) != "" {
			return standWaitOutcome{} // заказ ещё не виден
		}
		return standWaitOutcome{Done: true} // этот сервис никто не пересобирал
	}
	if standAnyInFlight(deploys, pinnedID, since) {
		return standWaitOutcome{}
	}
	st := strings.ToUpper(strings.TrimSpace(latest.Status))
	if standDeployFailed(st) {
		return standWaitOutcome{
			Err:       fmt.Errorf("деплой %s", strings.ToLower(st)),
			FailedID:  latest.ID,
			FailedSvc: latest.Service,
			Status:    strings.ToLower(st),
		}
	}
	if st == "SUCCESS" || st == "SKIPPED" {
		return standWaitOutcome{Done: true}
	}
	if st == "REMOVED" {
		return standWaitOutcome{} // Railway сменил деплой — ждём следующий
	}
	return standWaitOutcome{}
}

func standWaitDecision(deploys []standDeploy, since, started, now time.Time) standWaitOutcome {
	_ = started
	_ = now
	return standWaitLatest(deploys, "", since)
}

// standWaitPinned — заказ плюс всё, что Railway стартовал после него.
func standWaitPinned(deploys []standDeploy, deployID string) standWaitOutcome {
	deployID = strings.TrimSpace(deployID)
	var since time.Time
	found := false
	for _, d := range deploys {
		if strings.TrimSpace(d.ID) != deployID {
			continue
		}
		found = true
		since = d.CreatedAt
		break
	}
	if !found {
		return standWaitOutcome{}
	}
	return standWaitLatest(deploys, deployID, since)
}

// standWaitServices — SUCCESS всех сервисов стенда. Свежий FAILED у Leo
// не прячется за SKIPPED MiniApp: иначе карточка «выполнена», а бот не собрался.
func standWaitServices(
	byService map[string][]standDeploy, pinned map[string]string, since, started, now time.Time,
) standWaitOutcome {
	if len(byService) == 0 {
		return standWaitOutcome{}
	}
	// Ждём последнюю сборку по каждому сервису: свой заказ и более новый
	// вебхук GitHub. Первый SUCCESS заказанного id карточку не закрывает.
	if len(pinned) > 0 {
		waiting := false
		for name, id := range pinned {
			if strings.TrimSpace(id) == "" {
				continue
			}
			out := standWaitLatest(byService[name], id, since)
			if out.Err != nil {
				if out.FailedSvc == "" {
					out.FailedSvc = name
				}
				out.Err = fmt.Errorf("%s: %w", name, out.Err)
				return out
			}
			if !out.Done {
				waiting = true
			}
		}
		for name, deploys := range byService {
			if _, tracked := pinned[name]; tracked {
				continue
			}
			out := standWaitLatest(deploys, "", since)
			if out.Err != nil {
				if out.FailedSvc == "" {
					out.FailedSvc = name
				}
				out.Err = fmt.Errorf("%s: %w", name, out.Err)
				return out
			}
			if !out.Done {
				waiting = true
			}
		}
		if waiting {
			return standWaitOutcome{}
		}
		return standWaitOutcome{Done: true}
	}
	waiting := false
	for name, deploys := range byService {
		var out standWaitOutcome
		out = standWaitDecision(deploys, since, started, now)
		if out.Err != nil {
			if out.FailedSvc == "" {
				out.FailedSvc = name
			}
			out.Err = fmt.Errorf("%s: %w", name, out.Err)
			return out
		}
		if !out.Done {
			waiting = true
		}
	}
	if waiting {
		return standWaitOutcome{}
	}
	return standWaitOutcome{Done: true}
}

func trackerTaskShippedToStand(t database.TrackerTask) bool {
	shipped := false
	for _, s := range t.Steps {
		low := strings.ToLower(s)
		if strings.Contains(low, "пуш в ") || strings.Contains(low, "ждём сборку") {
			shipped = true
		}
		if strings.Contains(low, "вернули в работу") {
			shipped = false
		}
	}
	return shipped
}

func trackerStandFailCount(t database.TrackerTask) int {
	n := 0
	for _, s := range t.Steps {
		low := strings.ToLower(s)
		if strings.Contains(low, "сборка на стенде не прошла") {
			n++
		}
	}
	return n
}

func standBuildDeployID(err error) string {
	var e *standBuildError
	if errors.As(err, &e) && e != nil {
		return e.DeployID
	}
	return ""
}

func clipStandLogs(text string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	lines := strings.Split(text, "\n")
	var useful []string
	for _, line := range lines {
		low := strings.ToLower(line)
		if strings.Contains(low, "error") || strings.Contains(low, "undefined") ||
			strings.Contains(low, "failed") || strings.Contains(low, "fatal") ||
			strings.Contains(low, "cannot") || strings.Contains(low, "build failed") {
			useful = append(useful, line)
		}
	}
	if len(useful) > 0 {
		text = strings.Join(useful, "\n")
	}
	r := []rune(text)
	if len(r) <= trackerStandLogClip {
		return text
	}
	return string(r[len(r)-trackerStandLogClip:])
}

// waitStandBuild — после пуша в main карточка сидит в «Сборка», пока не
// кончится последняя свежая сборка MiniApp, Leo и того, что ещё крутит
// вебхук (например ms_payments). Первый SUCCESS заказанного деплоя — ещё
// не конец, если рядом уже стартовал более новый.
func (b *Bot) waitStandBuild(started time.Time, pinned map[string]string) error {
	if b == nil || b.config == nil {
		return fmt.Errorf("нет конфигурации Railway")
	}
	if len(pinned) == 0 {
		return fmt.Errorf("нет заказанной сборки Railway")
	}
	if strings.TrimSpace(b.config.RailwayToken) == "" || strings.TrimSpace(b.config.RailwayProjectID) == "" {
		return fmt.Errorf("нет RAILWAY_API_TOKEN или RAILWAY_PROJECT_ID")
	}
	return b.waitRailwayStand(started, pinned)
}

func (b *Bot) waitRailwayStand(started time.Time, pinned map[string]string) error {
	envID, svcs, err := b.lookupMainStandServices()
	if err != nil {
		return err
	}
	since := started.Add(-45 * time.Second)
	deadline := time.Now().Add(trackerStandWait)
	for time.Now().Before(deadline) {
		by := make(map[string][]standDeploy, len(svcs))
		apiFail := 0
		for _, svc := range svcs {
			deploys, lerr := b.listStandDeploys(svc.ID, envID)
			if lerr != nil {
				apiFail++
				continue
			}
			for i := range deploys {
				deploys[i].Service = svc.Name
			}
			by[svc.Name] = deploys
		}
		if len(by) == 0 && apiFail > 0 {
			time.Sleep(trackerStandPoll)
			continue
		}
		out := standWaitServices(by, pinned, since, started, time.Now())
		if out.Err != nil {
			return &standBuildError{
				Service:  out.FailedSvc,
				DeployID: out.FailedID,
				Status:   out.Status,
				Msg:      out.Err.Error(),
			}
		}
		if out.Done {
			return nil
		}
		time.Sleep(trackerStandPoll)
	}
	return fmt.Errorf("не уложилась в 10 минут")
}

func (b *Bot) listStandDeploys(svcID, envID string) ([]standDeploy, error) {
	raw, err := b.railwayCall(
		`query($sid:String!,$eid:String!){ deployments(first:10, input:{serviceId:$sid, environmentId:$eid}){ edges{ node{ id status createdAt } } } }`,
		map[string]any{"sid": svcID, "eid": envID},
	)
	if err != nil {
		return nil, err
	}
	var parsed struct {
		Deployments struct {
			Edges []struct {
				Node struct {
					ID        string `json:"id"`
					Status    string `json:"status"`
					CreatedAt string `json:"createdAt"`
				} `json:"node"`
			} `json:"edges"`
		} `json:"deployments"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, err
	}
	deploys := make([]standDeploy, 0, len(parsed.Deployments.Edges))
	for _, edge := range parsed.Deployments.Edges {
		at, _ := time.Parse(time.RFC3339, edge.Node.CreatedAt)
		deploys = append(deploys, standDeploy{
			ID:        edge.Node.ID,
			Status:    edge.Node.Status,
			CreatedAt: at,
		})
	}
	return deploys, nil
}

func (b *Bot) railwayDeployLogs(deployID string) string {
	if b == nil || strings.TrimSpace(deployID) == "" {
		return ""
	}
	if logs := b.railwayBuildLogsQuery(deployID); logs != "" {
		return logs
	}
	return b.railwayRuntimeLogsQuery(deployID)
}

func (b *Bot) railwayBuildLogsQuery(deployID string) string {
	raw, err := b.railwayCall(
		`query($id:String!){ buildLogs(deploymentId:$id) }`,
		map[string]any{"id": deployID},
	)
	if err != nil {
		return ""
	}
	var parsed struct {
		BuildLogs string `json:"buildLogs"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return ""
	}
	return clipStandLogs(parsed.BuildLogs)
}

func (b *Bot) railwayRuntimeLogsQuery(deployID string) string {
	raw, err := b.railwayCall(
		`query($id:String!,$n:Int){ deploymentLogs(deploymentId:$id, limit:$n){ message } }`,
		map[string]any{"id": deployID, "n": 80},
	)
	if err != nil {
		return ""
	}
	var parsed struct {
		DeploymentLogs []struct {
			Message string `json:"message"`
		} `json:"deploymentLogs"`
	}
	if json.Unmarshal(raw, &parsed) != nil {
		return ""
	}
	var bld strings.Builder
	for _, line := range parsed.DeploymentLogs {
		if msg := strings.TrimSpace(line.Message); msg != "" {
			bld.WriteString(msg)
			bld.WriteByte('\n')
		}
	}
	return clipStandLogs(bld.String())
}

func isStandWatchService(name string) bool {
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "_", "-")
	if n == "" || strings.Contains(n, "tracker") || strings.Contains(n, "postgres") ||
		strings.Contains(n, "redis") || strings.Contains(n, "qdrant") || strings.Contains(n, "payment") {
		return false
	}
	return n == "miniapp" || n == "leo" ||
		strings.Contains(n, "miniapp") ||
		strings.Contains(n, "ms-leo") ||
		strings.HasPrefix(n, "leo-") ||
		strings.Contains(n, "fat-leopard")
}

// isStandObserveService — смотрим и платежный сервис: он на том же GitHub,
// вебхук крутит третью сборку, хотя мы её не заказываем.
func isStandObserveService(name string) bool {
	if isStandWatchService(name) {
		return true
	}
	n := strings.ToLower(strings.TrimSpace(name))
	n = strings.ReplaceAll(n, "_", "-")
	return strings.Contains(n, "payment") && !strings.Contains(n, "postgres")
}

func (b *Bot) lookupMainStandServices() (envID string, svcs []standService, err error) {
	raw, err := b.railwayCall(
		`query($id:String!){ project(id:$id){ environments{ edges{ node{ id name } } } services{ edges{ node{ id name } } } } }`,
		map[string]any{"id": strings.TrimSpace(b.config.RailwayProjectID)},
	)
	if err != nil {
		return "", nil, err
	}
	var parsed struct {
		Project struct {
			Environments struct {
				Edges []struct {
					Node struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"environments"`
			Services struct {
				Edges []struct {
					Node struct {
						ID   string `json:"id"`
						Name string `json:"name"`
					} `json:"node"`
				} `json:"edges"`
			} `json:"services"`
		} `json:"project"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", nil, err
	}
	if id := strings.TrimSpace(b.config.RailwayEnvironmentID); id != "" {
		envID = id
	} else {
		want := []string{}
		if br := strings.TrimSpace(b.config.BoardBranch); br != "" {
			want = append(want, br)
		}
		want = append(want, "production", "prod", "main")
		seen := map[string]bool{}
		for _, name := range want {
			key := strings.ToLower(name)
			if seen[key] {
				continue
			}
			seen[key] = true
			for _, e := range parsed.Project.Environments.Edges {
				if strings.EqualFold(e.Node.Name, name) {
					envID = e.Node.ID
					break
				}
			}
			if envID != "" {
				break
			}
		}
		if envID == "" && len(parsed.Project.Environments.Edges) > 0 {
			envID = parsed.Project.Environments.Edges[0].Node.ID
		}
	}
	for _, s := range parsed.Project.Services.Edges {
		if isStandObserveService(s.Node.Name) {
			svcs = append(svcs, standService{ID: s.Node.ID, Name: s.Node.Name})
		}
	}
	if envID == "" || len(svcs) == 0 {
		return "", nil, fmt.Errorf("сервисы стенда не найдены")
	}
	return envID, svcs, nil
}

func (b *Bot) waitMiniappHTTP() error {
	url := strings.TrimRight(strings.TrimSpace(b.config.MiniappWebAppURL), "/")
	if url == "" {
		url = strings.TrimRight(strings.TrimSpace(b.config.MiniappPublicBaseURL), "/")
	}
	if url == "" {
		url = "https://fat-leopard-main.up.railway.app"
	}
	deadline := time.Now().Add(3 * time.Minute)
	client := &http.Client{Timeout: 15 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url + "/")
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode < 500 {
				return nil
			}
		}
		time.Sleep(trackerStandPoll)
	}
	return fmt.Errorf("стенд не ответил")
}
