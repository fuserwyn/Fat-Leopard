package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"leo-tracker/internal/agent"
	"leo-tracker/internal/config"
	"leo-tracker/internal/railway"
	"leo-tracker/internal/store"
	"leo-tracker/internal/when"
)

type Server struct {
	cfg config.Config
	st  *store.Store
}

func New(cfg config.Config, st *store.Store) *Server {
	return &Server{cfg: cfg, st: st}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", s.health)
	mux.HandleFunc("GET /", s.index)
	mux.HandleFunc("GET /api/scheduled", s.auth(s.list))
	mux.HandleFunc("POST /api/scheduled", s.auth(s.create))
	mux.HandleFunc("GET /api/scheduled/{id}", s.auth(s.get))
	mux.HandleFunc("POST /api/scheduled/cancel", s.auth(s.cancel))
	mux.HandleFunc("POST /api/ship", s.auth(s.ship))
	mux.HandleFunc("POST /api/stamp", s.auth(s.stamp))
	mux.HandleFunc("GET /api/inspect", s.auth(s.inspect))
	return mux
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "ms_tracker",
		"wire": map[string]bool{
			"notify":  strings.TrimSpace(s.cfg.LeoNotifyURL) != "" && strings.TrimSpace(s.cfg.NotifySecret) != "",
			"railway": strings.TrimSpace(s.cfg.RailwayToken) != "" && strings.TrimSpace(s.cfg.RailwayProjectID) != "",
			"github":  strings.TrimSpace(s.cfg.GithubToken) != "",
			"cursor":  strings.TrimSpace(s.cfg.CursorAPIKey) != "",
			"claude":  strings.TrimSpace(s.cfg.AnthropicAPIKey) != "",
		},
	})
}

func (s *Server) index(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"service": "ms_tracker",
		"hint":    "Трекер Леопарда. Доска в админке, агент здесь.",
	})
}

func (s *Server) auth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.authorized(r) {
			writeJSON(w, http.StatusUnauthorized, map[string]any{"ok": false, "error": "unauthorized"})
			return
		}
		next(w, r)
	}
}

func (s *Server) authorized(r *http.Request) bool {
	want := strings.TrimSpace(s.cfg.TrackerSecret)
	if want == "" {
		return false
	}
	got := strings.TrimSpace(r.Header.Get("X-Tracker-Secret"))
	if got == "" {
		if a := strings.TrimSpace(r.Header.Get("Authorization")); strings.HasPrefix(strings.ToLower(a), "bearer ") {
			got = strings.TrimSpace(a[7:])
		}
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

func (s *Server) list(w http.ResponseWriter, _ *http.Request) {
	jobs, err := s.st.List(50)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	tasks := make([]map[string]any, 0, len(jobs))
	for _, j := range jobs {
		tasks = append(tasks, jobView(j))
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tasks": tasks})
}

func (s *Server) get(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	j, err := s.st.Get(id)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "task": jobView(j)})
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "непонятное тело"})
		return
	}
	body = unwrapScheduledBody(body)
	at, label, err := when.Parse(str(body, "when"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	j := store.Job{
		Prompt:       str(body, "prompt"),
		Phase:        str(body, "phase"),
		WhenAt:       at,
		WhenLabel:    label,
		Status:       "pending",
		Model:        str(body, "model_key"),
		AutoPush:     boolVal(body, "auto_push"),
		SourceTaskID: int64Val(body, "source_task_id"),
		SourceNum:    int(int64Val(body, "source_num")),
		AuthorID:     int64Val(body, "author_id"),
		Branch:       str(body, "branch"),
	}
	created, err := s.st.Create(j)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	log.Printf("трекер: задача #%d принята (source=%d)", created.ID, created.SourceTaskID)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":   true,
		"id":   created.ID,
		"when": created.WhenLabel,
	})
}

func (s *Server) stamp(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "непонятное тело"})
		return
	}
	job := store.Job{
		SourceTaskID: int64Val(body, "source_task_id"),
		SourceNum:    int(int64Val(body, "source_num")),
		Phase:        str(body, "phase"),
		Branch:       str(body, "branch"),
		Prompt:       str(body, "prompt"),
	}
	if job.Phase == "" {
		job.Phase = "review"
	}
	if job.Branch == "" && job.SourceTaskID > 0 {
		job.Branch = s.st.SourceBranch(job.SourceTaskID)
	}
	res, err := agent.Stamp(s.cfg, job, str(body, "note"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	if job.SourceTaskID > 0 && res.Committed {
		if job.Prompt == "" {
			job.Prompt = "коммит " + job.Phase
		}
		_, _ = s.st.Create(store.Job{
			SourceTaskID: job.SourceTaskID,
			SourceNum:    job.SourceNum,
			Prompt:       job.Prompt,
			Phase:        job.Phase,
			Status:       "done",
			Result:       res.Note,
			Branch:       res.Branch,
			Steps:        []string{"коммит " + res.Commit + " " + commitLabelRU(job.Phase)},
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":        true,
		"branch":    res.Branch,
		"commit":    res.Commit,
		"committed": res.Committed,
	})
}

func (s *Server) ship(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil && err.Error() != "EOF" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": "непонятное тело"})
		return
	}
	sourceID := int64Val(body, "source_task_id")
	num := int(int64Val(body, "source_num"))
	branch := str(body, "branch")
	if branch == "" && sourceID > 0 {
		branch = s.st.SourceBranch(sourceID)
	}
	info, ierr := agent.InspectBranch(s.cfg, branch)
	if reason := agent.ShipBlockReason(info, ierr); reason != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": reason})
		return
	}
	if reason := agent.CheckBranchImpl(s.cfg, branch, str(body, "prompt")); reason != "" {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": reason})
		return
	}
	base, err := agent.MergeToMain(s.cfg, branch, num)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	log.Printf("трекер: влили %s в %s (source=%d)", branch, base, sourceID)
	pinned, derr := railway.RedeployStand(s.cfg)
	if derr != nil || len(pinned) == 0 {
		if derr == nil {
			derr = fmt.Errorf("Railway не вернул id сборок")
		}
		log.Printf("трекер: Railway после %s: %v", branch, derr)
		writeJSON(w, http.StatusBadRequest, map[string]any{
			"ok":           false,
			"merged":       true,
			"base":         base,
			"head":         branch,
			"deployed":     false,
			"error":        derr.Error(),
			"deploy_error": derr.Error(),
		})
		return
	}
	log.Printf("трекер: заказали сборку Railway после %s: %v", branch, pinned)
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"merged":   true,
		"base":     base,
		"head":     branch,
		"deployed": true,
		"pinned":   pinned,
	})
}

func (s *Server) inspect(w http.ResponseWriter, r *http.Request) {
	branch := strings.TrimSpace(r.URL.Query().Get("branch"))
	info, err := agent.InspectBranch(s.cfg, branch)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":       true,
		"exists":   info.Exists,
		"has_impl": info.HasImpl,
		"head":     info.Head,
		"files":    info.Files,
		"branch":   branch,
	})
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	id := int64Val(body, "id")
	if id == 0 {
		id = int64Val(body, "task_id")
	}
	if err := s.st.Cancel(id); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func jobView(j store.Job) map[string]any {
	return map[string]any{
		"id":             j.ID,
		"source_task_id": j.SourceTaskID,
		"num":            j.SourceNum,
		"prompt":         j.Prompt,
		"status":         j.Status,
		"when":           j.WhenLabel,
		"error":          j.Error,
		"result":         j.Result,
		"steps":          j.Steps,
		"branch":         j.Branch,
	}
}

// unwrapScheduledBody — живой Leo ещё шлёт конверт MyVibeLab
// {op, session, payload:{when,prompt}}. Новый клиент кладёт поля сразу в корень.
func unwrapScheduledBody(body map[string]any) map[string]any {
	if body == nil || str(body, "op") == "" {
		return body
	}
	payload, ok := body["payload"].(map[string]any)
	if !ok || payload == nil {
		return body
	}
	return payload
}

func commitLabelRU(phase string) string {
	switch strings.ToLower(strings.TrimSpace(phase)) {
	case "review":
		return "ревью"
	case "test":
		return "тест"
	default:
		return "выполнение"
	}
}

func str(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	v, _ := m[key].(string)
	return strings.TrimSpace(v)
}

func boolVal(m map[string]any, key string) bool {
	if m == nil {
		return false
	}
	switch v := m[key].(type) {
	case bool:
		return v
	case string:
		return v == "1" || strings.EqualFold(v, "true")
	default:
		return false
	}
}

func int64Val(m map[string]any, key string) int64 {
	if m == nil {
		return 0
	}
	switch v := m[key].(type) {
	case float64:
		return int64(v)
	case int64:
		return v
	case int:
		return int64(v)
	case json.Number:
		n, _ := v.Int64()
		return n
	case string:
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		return n
	default:
		return 0
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
