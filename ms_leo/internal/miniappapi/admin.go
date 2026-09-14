package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"unicode/utf8"

	"leo-bot/internal/bot"
)

func parseJSONInt64(raw json.RawMessage) int64 {
	if len(raw) == 0 {
		return 0
	}
	var n int64
	if err := json.Unmarshal(raw, &n); err == nil {
		return n
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		n, _ = strconv.ParseInt(strings.TrimSpace(s), 10, 64)
		return n
	}
	return 0
}

func (s *Server) writeAdminErr(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, bot.ErrMiniAppChatMismatch):
		s.jsonErr(w, http.StatusConflict, "chat_mismatch")
	case errors.Is(err, bot.ErrPackFeedForbidden):
		s.jsonErr(w, http.StatusForbidden, "forbidden")
	case errors.Is(err, bot.ErrAdminNotFound):
		s.jsonErr(w, http.StatusNotFound, "not_found")
	case errors.Is(err, bot.ErrAdminActionInvalid):
		s.jsonErr(w, http.StatusBadRequest, "invalid_action")
	case errors.Is(err, bot.ErrTrackerNotConfigured):
		s.jsonErr(w, http.StatusServiceUnavailable, "tracker_not_configured")
	default:
		if s.jsonModerationErr(w, err) {
			return
		}
		msg := strings.TrimSpace(err.Error())
		if msg != "" && !strings.Contains(msg, "sql:") && !strings.Contains(strings.ToLower(msg), "pq:") {
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusBadRequest)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "admin_error", "message": msg})
			return
		}
		s.logger.Errorf("miniapp admin: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "admin_error")
	}
}

func (s *Server) handlePostAdminOverview(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	overview, err := s.bot.MiniappAdminOverview(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "overview": overview})
}

// handlePostAdminTracker — дверь мини-аппа к своей доске задач.
// op из белого списка, всё остальное делает bot.MiniappTrackerCall.
func (s *Server) handlePostAdminTracker(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string         `json:"init_data"`
		Op       string         `json:"op"`
		TaskID   int64          `json:"task_id"`
		Payload  map[string]any `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	data, err := s.bot.MiniappTrackerCall(parsed.User.ID, parsed, body.Op, body.TaskID, body.Payload)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "data": data})
}

func (s *Server) handlePostAdminSupportInbox(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	items, err := s.bot.MiniappAdminSupportInbox(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "conversations": items})
}

func (s *Server) handlePostAdminSupportThread(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData     string `json:"init_data"`
		TargetUserID int64  `json:"target_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	items, err := s.bot.MiniappAdminSupportThread(parsed.User.ID, parsed, body.TargetUserID)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

func (s *Server) handlePostAdminSupportReply(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData     string `json:"init_data"`
		TargetUserID int64  `json:"target_user_id"`
		Text         string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		s.jsonErr(w, http.StatusBadRequest, "empty_text")
		return
	}
	if utf8.RuneCountInString(text) > maxTextRunes {
		s.jsonErr(w, http.StatusBadRequest, "text_too_long")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminSupportReply(parsed.User.ID, parsed, body.TargetUserID, text, ""); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostAdminSupportReplyPhoto(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	if err := r.ParseMultipartForm(maxWorkoutPhotoBytes + 65536); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_multipart")
		return
	}
	initD := strings.TrimSpace(r.FormValue("init_data"))
	caption := strings.TrimSpace(r.FormValue("text"))
	targetRaw := strings.TrimSpace(r.FormValue("target_user_id"))
	if initD == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	targetUserID, err := strconv.ParseInt(targetRaw, 10, 64)
	if err != nil || targetUserID <= 0 {
		s.jsonErr(w, http.StatusBadRequest, "invalid_target_user_id")
		return
	}
	if utf8.RuneCountInString(caption) > maxTextRunes {
		s.jsonErr(w, http.StatusBadRequest, "text_too_long")
		return
	}
	parsed, ok := s.authMiniapp(w, initD)
	if !ok {
		return
	}
	fs := r.MultipartForm.File["photo"]
	if len(fs) == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_photo")
		return
	}
	file, err := fs[0].Open()
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "photo_open_error")
		return
	}
	defer file.Close()

	publicURL, ok := s.storeUploadedPhoto(w, r, file)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminSupportReply(parsed.User.ID, parsed, targetUserID, caption, publicURL); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "photo_url": publicURL})
}

func (s *Server) handlePostAdminReports(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	items, err := s.bot.MiniappAdminReports(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "reports": items})
}

func (s *Server) handlePostAdminReportAction(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		ReportID int64  `json:"report_id"`
		Action   string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminReportAction(parsed.User.ID, parsed, body.ReportID, body.Action); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostAdminHidden(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	items, err := s.bot.MiniappAdminHidden(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": items})
}

func (s *Server) handlePostAdminUnhide(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		Kind     string `json:"kind"`
		ID       int64  `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminUnhide(parsed.User.ID, parsed, body.Kind, body.ID); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostAdminUsers(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		Query    string `json:"query"`
		Offset   int    `json:"offset"`
		Filter   string `json:"filter"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	page, err := s.bot.MiniappAdminUsers(parsed.User.ID, parsed, body.Query, body.Offset, 20, body.Filter)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":           true,
		"users":        page.Users,
		"total":        page.Total,
		"offset":       page.Offset,
		"limit":        page.Limit,
		"users_active": page.UsersActive,
		"users_kicked": page.UsersKicked,
	})
}

func (s *Server) handlePostAdminUserCard(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData     string `json:"init_data"`
		TargetUserID int64  `json:"target_user_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	card, err := s.bot.MiniappAdminUserCard(parsed.User.ID, parsed, body.TargetUserID)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "user": card})
}

func (s *Server) handlePostAdminUserAction(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData     string `json:"init_data"`
		TargetUserID int64  `json:"target_user_id"`
		Action       string `json:"action"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminUserAction(parsed.User.ID, parsed, body.TargetUserID, body.Action); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostAdminPublish(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
		Author   string `json:"author"`
		Text     string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		s.jsonErr(w, http.StatusBadRequest, "empty_text")
		return
	}
	if utf8.RuneCountInString(text) > maxTextRunes {
		s.jsonErr(w, http.StatusBadRequest, "text_too_long")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminPublishPost(parsed.User.ID, parsed, body.Author, text); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostAdminPaywallPrice(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	price, err := s.bot.MiniappAdminPaywallPrice(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "price": price})
}

func (s *Server) handlePostAdminPaywallPriceSet(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		InitData  string `json:"init_data"`
		AmountRub int    `json:"amount_rub"`
		Reset     bool   `json:"reset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminSetPaywallPrice(parsed.User.ID, parsed, body.AmountRub, body.Reset); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	price, err := s.bot.MiniappAdminPaywallPrice(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "price": price})
}

// --- Разделы, переехавшие из чат-админки (bot/miniapp_admin_ops.go) --------

// handlePostAdminUserStat — правка показателей участника из карточки.
func (s *Server) handlePostAdminUserStat(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData     string `json:"init_data"`
		TargetUserID int64  `json:"target_user_id"`
		Field        string `json:"field"`
		Mode         string `json:"mode"`
		Value        int    `json:"value"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	err := s.bot.MiniappAdminSetStat(
		parsed.User.ID, parsed, body.TargetUserID,
		bot.MiniappAdminStatField(body.Field), body.Mode, body.Value,
	)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	card, err := s.bot.MiniappAdminUserCard(parsed.User.ID, parsed, body.TargetUserID)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"user": card})
}

func (s *Server) handlePostAdminAnalytics(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Days     int    `json:"days"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	data, err := s.bot.MiniappAdminAnalyticsData(parsed.User.ID, parsed, body.Days)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"analytics": data})
}

func (s *Server) handlePostAdminVisits(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	tables, err := s.bot.MiniappAdminVisits(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"tables": tables})
}

func (s *Server) handlePostAdminPayments(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Offset   int    `json:"offset"`
		Limit    int    `json:"limit"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	data, err := s.bot.MiniappAdminPaymentsPage(parsed.User.ID, parsed, body.Offset, body.Limit)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"payments": data})
}

func (s *Server) handlePostAdminAdmins(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	people, err := s.bot.MiniappAdminAdminsList(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"admins": people})
}

func (s *Server) handlePostAdminAdminsAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Query    string `json:"query"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	id, err := s.bot.MiniappAdminAddAdmin(parsed.User.ID, parsed, body.Query)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"user_id": id})
}

func (s *Server) handlePostAdminAdminsRemove(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		UserID   int64  `json:"user_id"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminRemoveAdmin(parsed.User.ID, parsed, body.UserID); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{})
}

func (s *Server) handlePostAdminScheduled(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	posts, err := s.bot.MiniappAdminScheduledPosts(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"posts": posts})
}

func (s *Server) handlePostAdminScheduledAdd(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Author   string `json:"author"`
		Text     string `json:"text"`
		At       string `json:"at"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	id, err := s.bot.MiniappAdminSchedulePost(parsed.User.ID, parsed, body.Author, body.Text, body.At)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"id": id})
}

func (s *Server) handlePostAdminScheduledCancel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		ID       int64  `json:"id"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminCancelScheduledPost(parsed.User.ID, parsed, body.ID); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{})
}

func (s *Server) handlePostAdminPoll(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string   `json:"init_data"`
		Question string   `json:"question"`
		Options  []string `json:"options"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	if err := s.bot.MiniappAdminPublishPoll(parsed.User.ID, parsed, body.Question, body.Options); err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{})
}

func (s *Server) handlePostAdminWipe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Confirm  bool   `json:"confirm"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	// Без confirm только считаем, что удалится: очистка необратима, поэтому
	// подтверждение приходит отдельным запросом.
	if !body.Confirm {
		counts, err := s.bot.MiniappAdminWipeCounts(parsed.User.ID, parsed)
		if err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{"counts": counts, "done": false})
		return
	}
	counts, err := s.bot.MiniappAdminWipeExecute(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"counts": counts, "done": true})
}

// handleDesktopPoll — приложение ждёт подтверждения входа в чате (GET или POST).
// Единственная ручка без авторизации: защита — неугадываемый nonce с коротким
// сроком жизни (bot/desktop_auth.go).
// handleBoardNotify — MyVibeLab сообщает автору о судьбе его задачи.
//
// Без авторизации мини-аппа: запрос приходит от сервера к серверу, поэтому
// подписан общим секретом доски. Пишем в личку тому, кто задачу ставил.
func (s *Server) handleBoardNotify(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	var body struct {
		Repo     string          `json:"repo"`
		AuthorID int64           `json:"author_id"`
		TaskID   json.RawMessage `json:"task_id"`
		Text     string          `json:"text"`
		Token    string          `json:"token"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	// Автор в токене и в теле могут не совпасть: задачу ставил Лео (id < 0),
	// из чата (0) или токен подписан владельцем доски. Репо и подпись — достаточны.
	repo, tokenUser, ok := s.bot.VerifyTrackerToken(body.Token, "notify")
	if !ok || (body.Repo != "" && repo != body.Repo) {
		s.jsonErr(w, http.StatusUnauthorized, "bad_signature")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		s.jsonErr(w, http.StatusBadRequest, "empty_text")
		return
	}
	notifyUser := body.AuthorID
	if notifyUser == 0 {
		notifyUser = tokenUser
	}
	taskID := parseJSONInt64(body.TaskID)
	// Сначала двигаем свою карточку: «выполнена» больше не должна висеть
	// в «В работе», пока человеку уже написали в личку.
	localID, ship, applyErr := s.bot.ApplyBoardNotify(taskID, text)
	if applyErr != nil && s.logger != nil {
		s.logger.Warnf("трекер: не обновить карточку по уведомлению: %v", applyErr)
	}
	shipID := localID
	if shipID <= 0 {
		shipID = taskID
	}
	// Сборку зовём только если карточка уже у публикации (fast-track / deploy).
	// Review и тест — следующий шаг человека, пуш там рано.
	if ship || localID <= 0 {
		s.bot.ShipTrackerTaskInBackground(shipID, notifyUser)
	}
	// В личку о выкате пишет только finishTrackerBuild после SUCCESS Railway.
	// Раньше здесь же звали NotifyTrackerShippedIfNeeded: вебхук и сборка
	// гонялись и автор получал два «задача выполнена».
	s.writeAdminOK(w, map[string]any{})
}

func (s *Server) handleDesktopPoll(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	// Приложение опрашивает GET-ом с ?nonce=, POST с телом оставлен для ручных
	// проверок: две формы одной и той же ручки дешевле, чем правка клиента.
	nonce := strings.TrimSpace(r.URL.Query().Get("nonce"))
	if nonce == "" && r.Method == http.MethodPost {
		var body struct {
			Nonce string `json:"nonce"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.jsonErr(w, http.StatusBadRequest, "invalid_json")
			return
		}
		nonce = strings.TrimSpace(body.Nonce)
	}
	if nonce == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_nonce")
		return
	}
	status, userID, token, err := s.bot.DesktopLoginPoll(nonce)
	if err != nil {
		s.jsonErr(w, http.StatusInternalServerError, "desktop_poll_error")
		return
	}
	out := map[string]any{"ok": true, "status": status}
	if status == "ok" {
		out["token"] = token
		name, username := s.bot.DesktopUserLabel(userID)
		out["user"] = map[string]any{"id": userID, "first_name": name, "username": username}
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(out)
}

// handleDesktopLogout — отозвать токен, с которым пришёл запрос.
func (s *Server) handleDesktopLogout(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	// Приложение шлёт токен тем же заголовком, что мини-апп свой initData.
	token := strings.TrimSpace(r.Header.Get("X-Init-Data"))
	if token == "" {
		var body struct {
			InitData string `json:"init_data"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		token = strings.TrimSpace(body.InitData)
	}
	if token == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_token")
		return
	}
	if err := s.bot.DesktopSessionRevoke(token); err != nil {
		s.jsonErr(w, http.StatusInternalServerError, "desktop_logout_error")
		return
	}
	s.writeAdminOK(w, map[string]any{})
}

func (s *Server) handlePostAdminLeoPrompts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Action   string `json:"action"`
		Key      string `json:"key"`
		Body     string `json:"body"`
		Filename string `json:"filename"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	switch strings.ToLower(strings.TrimSpace(body.Action)) {
	case "", "list":
		list, err := s.bot.MiniappListLeoPrompts(parsed.User.ID, parsed)
		if err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{"prompts": list})
	case "save":
		if err := s.bot.MiniappSaveLeoPrompt(parsed.User.ID, parsed, body.Key, body.Body, body.Filename); err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{})
	case "reset":
		if err := s.bot.MiniappResetLeoPrompt(parsed.User.ID, parsed, body.Key); err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{})
	default:
		s.jsonErr(w, http.StatusBadRequest, "invalid_action")
	}
}

func (s *Server) handlePostAdminLeoLab(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Action   string `json:"action"` // prompt | ask | teach | memory
		System   string `json:"system"`
		Question string `json:"question"`
		Text     string `json:"text"`
		Days     int    `json:"days"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	switch body.Action {
	case "prompt":
		prompt, err := s.bot.MiniappLeoLabPrompt(parsed.User.ID, parsed)
		if err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{"prompt": prompt})
	case "ask":
		answer, usedDefault, err := s.bot.MiniappLeoLabAsk(parsed.User.ID, parsed, body.System, body.Question)
		if err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{"answer": answer, "used_default": usedDefault})
	case "teach":
		if err := s.bot.MiniappLeoLabTeach(parsed.User.ID, parsed, body.Text); err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{})
	case "memory":
		stats, err := s.bot.MiniappLeoLabMemory(parsed.User.ID, parsed, body.Days)
		if err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{"memory": stats})
	default:
		s.jsonErr(w, http.StatusBadRequest, "invalid_action")
	}
}

// handlePostAdminStand — включить, выключить или посмотреть тестовый стенд.
func (s *Server) handlePostAdminStand(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Action   string `json:"action"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	state, err := s.bot.MiniappLabStand(parsed.User.ID, parsed, body.Action)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"stand": state})
}

// handlePostAdminTrackerAttachment — посмотреть или снять фото задачи.
// action: get — вернуть картинку base64 (мини-апп рисует её в карточке);
// delete — убрать вложение, на этом строится «заменить фото».
func (s *Server) handlePostAdminTrackerAttachment(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Action   string `json:"action"`
		TaskID   int64  `json:"task_id"`
		AttID    string `json:"att_id"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	switch body.Action {
	case "delete":
		if err := s.bot.MiniappTrackerAttachmentDelete(
			parsed.User.ID, parsed, body.TaskID, body.AttID,
		); err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{})
	default:
		mime, data, err := s.bot.MiniappTrackerAttachmentGet(
			parsed.User.ID, parsed, body.TaskID, body.AttID,
		)
		if err != nil {
			s.writeAdminErr(w, err)
			return
		}
		s.writeAdminOK(w, map[string]any{"mime": mime, "data": data})
	}
}

// handlePostAdminLeoAutonomy — включить/выключить режим, когда Лео сам ставит
// задачи, и посмотреть, когда он возьмётся за следующий спринт.
func (s *Server) handlePostAdminLeoAutonomy(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData    string `json:"init_data"`
		Action      string `json:"action"`
		Days        int    `json:"days"`
		EveryHours  int    `json:"every_hours"`
		TasksPerRun int    `json:"tasks_per_run"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	state, err := s.bot.MiniappLeoAutonomy(
		parsed.User.ID, parsed, body.Action, body.Days, body.EveryHours, body.TasksPerRun,
	)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"autonomy": state})
}

func (s *Server) handlePostAdminLeoPropose(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string   `json:"init_data"`
		Hint     string   `json:"hint"`
		Busy     []string `json:"busy"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	reply, title, task, err := s.bot.MiniappLeoProposeTask(parsed.User.ID, parsed, body.Hint, body.Busy)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"reply": reply, "title": title, "task": task})
}

func (s *Server) handlePostAdminLeoSprint(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Hint     string `json:"hint"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	reply, theme, tasks, err := s.bot.MiniappLeoSprint(parsed.User.ID, parsed, body.Hint)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"reply": reply, "theme": theme, "tasks": tasks})
}

func (s *Server) handlePostAdminAskLeo(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Question string `json:"question"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	reply, task, err := s.bot.MiniappAskLeoTask(parsed.User.ID, parsed, body.Question)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"reply": reply, "task": task})
}

func (s *Server) handlePostAdminTrackerAuthors(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string  `json:"init_data"`
		IDs      []int64 `json:"ids"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	people, err := s.bot.MiniappTrackerAuthors(parsed.User.ID, parsed, body.IDs)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"people": people})
}

func (s *Server) handlePostAdminTrackerAttach(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		TaskID   int64  `json:"task_id"`
		Filename string `json:"filename"`
		Mime     string `json:"mime"`
		Data     string `json:"data"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	data, err := s.bot.MiniappTrackerAttach(parsed.User.ID, parsed, body.TaskID, body.Filename, body.Mime, body.Data)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"data": data})
}

func (s *Server) handlePostAdminDBTables(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	tables, err := s.bot.MiniappAdminDBTables(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"tables": tables})
}

func (s *Server) handlePostAdminDBTable(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Table    string `json:"table"`
		Limit    int    `json:"limit"`
		Offset   int    `json:"offset"`
		OrderBy  string `json:"order_by"`
		Desc     bool   `json:"desc"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	res, err := s.bot.MiniappAdminDBTable(parsed.User.ID, parsed, body.Table, body.Limit, body.Offset, body.OrderBy, body.Desc)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"result": res})
}

func (s *Server) handlePostAdminDBColumns(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		Table    string `json:"table"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	cols, err := s.bot.MiniappAdminDBColumns(parsed.User.ID, parsed, body.Table)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"columns": cols})
}

func (s *Server) handlePostAdminDBQuery(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
		SQL      string `json:"sql"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	res, err := s.bot.MiniappAdminDBQuery(parsed.User.ID, parsed, body.SQL)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"result": res})
}

func (s *Server) handlePostAdminResources(w http.ResponseWriter, r *http.Request) {
	var body struct {
		InitData string `json:"init_data"`
	}
	corsWriteHeaders(w, r)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	parsed, ok := s.authMiniapp(w, body.InitData)
	if !ok {
		return
	}
	res, err := s.bot.MiniappAdminResources(parsed.User.ID, parsed)
	if err != nil {
		s.writeAdminErr(w, err)
		return
	}
	s.writeAdminOK(w, map[string]any{"resources": res})
}

func (s *Server) writeAdminOK(w http.ResponseWriter, payload map[string]any) {
	if payload == nil {
		payload = map[string]any{}
	}
	payload["ok"] = true
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(payload)
}
