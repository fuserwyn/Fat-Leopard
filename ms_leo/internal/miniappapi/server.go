package miniappapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"leo-bot/internal/bot"
	"leo-bot/internal/database"
	"leo-bot/internal/logger"
)

const maxTextRunes = 4000

type Server struct {
	bot              *bot.Bot
	token            string
	logger           logger.Logger
	publicMediaBase  string
	mediaDirAbsolute string
	r2               *R2Storage // если != nil — фото грузятся в Cloudflare R2, иначе на локальный диск
}

// New — HTTP-оболочка для мини-апpa: валидация initData и тот же обработчик, что getUpdates.
// publicMediaBase — публичная база этого сервиса (HTTPS в проде), нужна для URL фото тренировки; mediaDirAbsolute — каталог для файлов.
// r2 — опциональное объектное хранилище (nil = локальный диск).
func New(b *bot.Bot, token string, log logger.Logger, publicMediaBase, mediaDirAbsolute string, r2 *R2Storage) http.Handler {
	s := &Server{
		bot:              b,
		token:            token,
		logger:           log,
		publicMediaBase:  strings.TrimRight(strings.TrimSpace(publicMediaBase), "/"),
		mediaDirAbsolute: strings.TrimSpace(mediaDirAbsolute),
		r2:               r2,
	}
	return withCORS(http.HandlerFunc(s.serve))
}

func (s *Server) serve(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	switch {
	case path == "/healthz" && r.Method == http.MethodGet:
		s.handleHealthz(w, r)
	case path == "/api/miniapp/messages/photo" && r.Method == http.MethodPost:
		s.handlePostLeoMessageWithPhoto(w, r)
	case path == "/api/miniapp/messages" && r.Method == http.MethodPost:
		s.handlePostMessage(w, r)
	case path == "/api/miniapp/personal-reply/pending-count" && r.Method == http.MethodPost:
		s.handlePostPersonalPendingCount(w, r)
	case path == "/api/miniapp/personal-reply/drain" && r.Method == http.MethodPost:
		s.handlePostPersonalReplyDrain(w, r)
	case path == "/api/miniapp/personal-reply/poll" && r.Method == http.MethodPost:
		s.handlePostPersonalReplyPoll(w, r)
	case path == "/api/miniapp/personal-chat/feed" && r.Method == http.MethodPost:
		s.handlePostPersonalChatFeed(w, r)
	case path == "/api/miniapp/personal-chat/like" && r.Method == http.MethodPost:
		s.handlePostPersonalChatLike(w, r)
	case path == "/api/miniapp/support/feed" && r.Method == http.MethodPost:
		s.handlePostSupportChatFeed(w, r)
	case path == "/api/miniapp/support/send" && r.Method == http.MethodPost:
		s.handlePostSupportChatSend(w, r)
	case path == "/api/miniapp/support/send/photo" && r.Method == http.MethodPost:
		s.handlePostSupportChatSendPhoto(w, r)
	case path == "/api/miniapp/feed" && r.Method == http.MethodPost:
		s.handlePostFeed(w, r)
	case path == "/api/miniapp/user-avatar" && r.Method == http.MethodGet:
		s.handleGetUserAvatar(w, r)
	case path == "/api/miniapp/feed/training/react" && r.Method == http.MethodPost:
		s.handlePostFeedTrainingReact(w, r)
	case path == "/api/miniapp/feed/poll/vote" && r.Method == http.MethodPost:
		s.handlePostFeedPollVote(w, r)
	case path == "/api/miniapp/feed/training/thread" && r.Method == http.MethodPost:
		s.handlePostFeedTrainingThread(w, r)
	case path == "/api/miniapp/feed/training/thread/delete" && r.Method == http.MethodPost:
		s.handlePostFeedTrainingThreadDelete(w, r)
	case path == "/api/miniapp/feed/training/thread/edit" && r.Method == http.MethodPost:
		s.handlePostFeedTrainingThreadEdit(w, r)
	case path == "/api/miniapp/feed/edit" && r.Method == http.MethodPost:
		s.handlePostFeedEdit(w, r)
	case path == "/api/miniapp/feed/delete" && r.Method == http.MethodPost:
		s.handlePostFeedDelete(w, r)
	case path == "/api/miniapp/feed/photo" && r.Method == http.MethodPost:
		s.handlePostFeedPhoto(w, r)
	case path == "/api/miniapp/feed/photo/delete" && r.Method == http.MethodPost:
		s.handlePostFeedPhotoDelete(w, r)
	case path == "/api/miniapp/feed/pin" && r.Method == http.MethodPost:
		s.handlePostFeedPin(w, r)
	case path == "/api/miniapp/feed/training/thread/like" && r.Method == http.MethodPost:
		s.handlePostFeedTrainingThreadLike(w, r)
	case path == "/api/miniapp/feed/report" && r.Method == http.MethodPost:
		s.handlePostFeedReport(w, r)
	case path == "/api/miniapp/feed/training/thread/unread-count" && r.Method == http.MethodPost:
		s.handlePostFeedTrainingThreadUnreadCount(w, r)
	case path == "/api/miniapp/feed/training/thread/unread-clear" && r.Method == http.MethodPost:
		s.handlePostFeedTrainingThreadUnreadClear(w, r)
	case path == "/api/miniapp/pack-group/feed" && r.Method == http.MethodPost:
		s.handlePostPackGroupFeed(w, r)
	case path == "/api/miniapp/pack-group/messages/photo" && r.Method == http.MethodPost:
		s.handlePostPackGroupMessageWithPhoto(w, r)
	case path == "/api/miniapp/pack-group/messages" && r.Method == http.MethodPost:
		s.handlePostPackGroupMessage(w, r)
	case path == "/api/miniapp/pack-group/messages/delete" && r.Method == http.MethodPost:
		s.handlePostPackGroupMessageDelete(w, r)
	case path == "/api/miniapp/pack-group/messages/edit" && r.Method == http.MethodPost:
		s.handlePostPackGroupMessageEdit(w, r)
	case path == "/api/miniapp/pack-group/report" && r.Method == http.MethodPost:
		s.handlePostPackGroupReport(w, r)
	case path == "/api/miniapp/pack-group/unread-count" && r.Method == http.MethodPost:
		s.handlePostPackGroupUnreadCount(w, r)
	case path == "/api/miniapp/pack-group/unread-clear" && r.Method == http.MethodPost:
		s.handlePostPackGroupUnreadClear(w, r)
	case path == "/api/miniapp/pack-group/react" && r.Method == http.MethodPost:
		s.handlePostPackGroupReact(w, r)
	case path == "/api/miniapp/pack-group/search" && r.Method == http.MethodPost:
		s.handlePostPackGroupSearch(w, r)
	case path == "/api/miniapp/onboarding/ensure" && r.Method == http.MethodPost:
		s.handlePostOnboardingEnsure(w, r)
	case path == "/api/miniapp/donate/options" && r.Method == http.MethodPost:
		s.handlePostDonateOptions(w, r)
	case path == "/api/miniapp/donate/stars" && r.Method == http.MethodPost:
		s.handlePostDonateStars(w, r)
	case path == "/api/miniapp/donate/card" && r.Method == http.MethodPost:
		s.handlePostDonateCard(w, r)
	case path == "/api/miniapp/donate/status" && r.Method == http.MethodPost:
		s.handlePostDonateStatus(w, r)
	case path == "/api/miniapp/profile/load" && r.Method == http.MethodPost:
		s.handlePostProfileLoad(w, r)
	case path == "/api/miniapp/profile/save" && r.Method == http.MethodPost:
		s.handlePostProfileSave(w, r)
	case path == "/api/miniapp/reminders/load" && r.Method == http.MethodPost:
		s.handlePostReminderLoad(w, r)
	case path == "/api/miniapp/reminders/save" && r.Method == http.MethodPost:
		s.handlePostReminderSave(w, r)
	case path == "/api/miniapp/wisdom-sub/load" && r.Method == http.MethodPost:
		s.handlePostWisdomSubLoad(w, r)
	case path == "/api/miniapp/wisdom-sub/save" && r.Method == http.MethodPost:
		s.handlePostWisdomSubSave(w, r)
	case path == "/api/miniapp/like-notifications/load" && r.Method == http.MethodPost:
		s.handlePostLikeNotificationsLoad(w, r)
	case path == "/api/miniapp/like-notifications/save" && r.Method == http.MethodPost:
		s.handlePostLikeNotificationsSave(w, r)
	case path == "/api/miniapp/friends/list" && r.Method == http.MethodPost:
		s.handlePostFriendsList(w, r)
	case path == "/api/miniapp/friends/follow" && r.Method == http.MethodPost:
		s.handlePostFriendsFollow(w, r)
	case path == "/api/miniapp/friends/unfollow" && r.Method == http.MethodPost:
		s.handlePostFriendsUnfollow(w, r)
	case path == "/api/miniapp/friends/notify" && r.Method == http.MethodPost:
		s.handlePostFriendsNotify(w, r)
	case path == "/api/miniapp/friends/notify-all" && r.Method == http.MethodPost:
		s.handlePostFriendsNotifyAll(w, r)
	case path == "/api/miniapp/health/status" && r.Method == http.MethodPost:
		s.handlePostHealthStatus(w, r)
	case path == "/api/miniapp/streak/save-use" && r.Method == http.MethodPost:
		s.handlePostStreakSaveUse(w, r)
	case path == "/api/miniapp/workout" && r.Method == http.MethodPost:
		s.handlePostWorkoutWithPhoto(w, r)
	case path == "/api/miniapp/diag/init-source" && r.Method == http.MethodPost:
		s.handlePostDiagInitSource(w, r)
	case path == "/api/miniapp/analytics/leo-comment-displayed" && r.Method == http.MethodPost:
		s.handlePostLeoCommentDisplayed(w, r)
	case path == "/api/miniapp/analytics/event" && r.Method == http.MethodPost:
		s.handlePostAnalyticsEvent(w, r)
	case path == "/api/miniapp/admin/overview" && r.Method == http.MethodPost:
		s.handlePostAdminOverview(w, r)
	case path == "/api/miniapp/admin/tracker" && r.Method == http.MethodPost:
		s.handlePostAdminTracker(w, r)
	case path == "/api/miniapp/admin/tracker/attach" && r.Method == http.MethodPost:
		s.handlePostAdminTrackerAttach(w, r)
	case path == "/api/miniapp/admin/tracker/authors" && r.Method == http.MethodPost:
		s.handlePostAdminTrackerAuthors(w, r)
	case path == "/api/miniapp/admin/tracker/leo" && r.Method == http.MethodPost:
		s.handlePostAdminAskLeo(w, r)
	case path == "/api/miniapp/admin/tracker/leo-sprint" && r.Method == http.MethodPost:
		s.handlePostAdminLeoSprint(w, r)
	case path == "/api/miniapp/admin/tracker/leo-propose" && r.Method == http.MethodPost:
		s.handlePostAdminLeoPropose(w, r)
	case path == "/api/miniapp/admin/leo-lab" && r.Method == http.MethodPost:
		s.handlePostAdminLeoLab(w, r)
	case path == "/api/miniapp/admin/leo-prompts" && r.Method == http.MethodPost:
		s.handlePostAdminLeoPrompts(w, r)
	case path == "/api/miniapp/admin/tracker/leo-autonomy" && r.Method == http.MethodPost:
		s.handlePostAdminLeoAutonomy(w, r)
	case path == "/api/miniapp/admin/tracker/attachment" && r.Method == http.MethodPost:
		s.handlePostAdminTrackerAttachment(w, r)
	case path == "/api/miniapp/admin/stand" && r.Method == http.MethodPost:
		s.handlePostAdminStand(w, r)
	case path == "/api/miniapp/board/notify" && r.Method == http.MethodPost:
		s.handleBoardNotify(w, r)
	case path == "/api/miniapp/auth/desktop/poll" && (r.Method == http.MethodGet || r.Method == http.MethodPost):
		s.handleDesktopPoll(w, r)
	case path == "/api/miniapp/auth/desktop/logout" && r.Method == http.MethodPost:
		s.handleDesktopLogout(w, r)
	case path == "/api/miniapp/admin/analytics" && r.Method == http.MethodPost:
		s.handlePostAdminAnalytics(w, r)
	case path == "/api/miniapp/admin/visits" && r.Method == http.MethodPost:
		s.handlePostAdminVisits(w, r)
	case path == "/api/miniapp/admin/payments" && r.Method == http.MethodPost:
		s.handlePostAdminPayments(w, r)
	case path == "/api/miniapp/admin/admins" && r.Method == http.MethodPost:
		s.handlePostAdminAdmins(w, r)
	case path == "/api/miniapp/admin/admins/add" && r.Method == http.MethodPost:
		s.handlePostAdminAdminsAdd(w, r)
	case path == "/api/miniapp/admin/admins/remove" && r.Method == http.MethodPost:
		s.handlePostAdminAdminsRemove(w, r)
	case path == "/api/miniapp/admin/scheduled" && r.Method == http.MethodPost:
		s.handlePostAdminScheduled(w, r)
	case path == "/api/miniapp/admin/scheduled/add" && r.Method == http.MethodPost:
		s.handlePostAdminScheduledAdd(w, r)
	case path == "/api/miniapp/admin/scheduled/cancel" && r.Method == http.MethodPost:
		s.handlePostAdminScheduledCancel(w, r)
	case path == "/api/miniapp/admin/poll" && r.Method == http.MethodPost:
		s.handlePostAdminPoll(w, r)
	case path == "/api/miniapp/admin/wipe" && r.Method == http.MethodPost:
		s.handlePostAdminWipe(w, r)
	case path == "/api/miniapp/admin/db/tables" && r.Method == http.MethodPost:
		s.handlePostAdminDBTables(w, r)
	case path == "/api/miniapp/admin/db/table" && r.Method == http.MethodPost:
		s.handlePostAdminDBTable(w, r)
	case path == "/api/miniapp/admin/db/columns" && r.Method == http.MethodPost:
		s.handlePostAdminDBColumns(w, r)
	case path == "/api/miniapp/admin/db/query" && r.Method == http.MethodPost:
		s.handlePostAdminDBQuery(w, r)
	case path == "/api/miniapp/admin/resources" && r.Method == http.MethodPost:
		s.handlePostAdminResources(w, r)
	case path == "/api/miniapp/admin/support/inbox" && r.Method == http.MethodPost:
		s.handlePostAdminSupportInbox(w, r)
	case path == "/api/miniapp/admin/support/thread" && r.Method == http.MethodPost:
		s.handlePostAdminSupportThread(w, r)
	case path == "/api/miniapp/admin/support/reply" && r.Method == http.MethodPost:
		s.handlePostAdminSupportReply(w, r)
	case path == "/api/miniapp/admin/support/reply/photo" && r.Method == http.MethodPost:
		s.handlePostAdminSupportReplyPhoto(w, r)
	case path == "/api/miniapp/admin/reports" && r.Method == http.MethodPost:
		s.handlePostAdminReports(w, r)
	case path == "/api/miniapp/admin/reports/action" && r.Method == http.MethodPost:
		s.handlePostAdminReportAction(w, r)
	case path == "/api/miniapp/admin/hidden" && r.Method == http.MethodPost:
		s.handlePostAdminHidden(w, r)
	case path == "/api/miniapp/admin/hidden/restore" && r.Method == http.MethodPost:
		s.handlePostAdminUnhide(w, r)
	case path == "/api/miniapp/admin/users" && r.Method == http.MethodPost:
		s.handlePostAdminUsers(w, r)
	case path == "/api/miniapp/admin/users/card" && r.Method == http.MethodPost:
		s.handlePostAdminUserCard(w, r)
	case path == "/api/miniapp/admin/users/stat" && r.Method == http.MethodPost:
		s.handlePostAdminUserStat(w, r)
	case path == "/api/miniapp/admin/users/action" && r.Method == http.MethodPost:
		s.handlePostAdminUserAction(w, r)
	case path == "/api/miniapp/admin/publish" && r.Method == http.MethodPost:
		s.handlePostAdminPublish(w, r)
	case path == "/api/miniapp/admin/paywall-price" && r.Method == http.MethodPost:
		s.handlePostAdminPaywallPrice(w, r)
	case path == "/api/miniapp/admin/paywall-price/set" && r.Method == http.MethodPost:
		s.handlePostAdminPaywallPriceSet(w, r)
	case strings.HasPrefix(path, "/api/miniapp/media/") && r.Method == http.MethodGet:
		s.handleGetMiniappMedia(w, r)
	case path == "/" && r.Method == http.MethodGet:
		s.handleRoot(w, r)
	default:
		http.NotFound(w, r)
	}
}

func (s *Server) handleRoot(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) handleHealthz(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) handlePostMessage(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}

	var body struct {
		InitData string `json:"init_data"`
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
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp init_data invalid: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp assert pack chat: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	miniRes := s.bot.ProcessMiniAppPrivateText(parsed, text)
	if miniRes.Blocked {
		code := miniRes.BlockCode
		if code == "" {
			code = "moderation_blocked"
		}
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		w.WriteHeader(bot.ModerationHTTPStatus(code))
		_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": miniRes.ReplyText})
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if miniRes.Pending {
		out["pending"] = true
	}
	if miniRes.ReplyText != "" {
		out["reply_text"] = miniRes.ReplyText
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostPersonalReplyPoll(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp personal poll: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp personal poll assert chat: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	txt, has := s.bot.PopMiniappPersonalReply(parsed.User.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if has && txt != "" {
		out["reply_text"] = txt
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostPersonalReplyDrain(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp personal drain: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp personal drain assert chat: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	cleared := s.bot.ClearMiniappPersonalReplyQueue(parsed.User.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "cleared": cleared})
}

func (s *Server) handlePostPersonalPendingCount(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pending count: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp pending count assert chat: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	n := s.bot.MiniappPersonalQueueLen(parsed.User.ID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":    true,
		"count": n,
	})
}

func (s *Server) handlePostFeedTrainingThreadUnreadCount(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed thread unread count: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	n, cardIDs, err := s.bot.MiniappTrainingThreadUnreadSummary(parsed, parsed.User.ID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("feed thread unread count: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "unread_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "count": n, "user_message_ids": cardIDs})
}

func (s *Server) handlePostFeedTrainingThreadUnreadClear(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed thread unread clear: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.MiniappTrainingThreadUnreadClear(parsed, parsed.User.ID, body.UserMessageID); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("feed thread unread clear: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "clear_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// parseFeedCursorTS — курсор единой ленты из RFC3339-строки. Пусто/битое → nil (без курсора).
func parseFeedCursorTS(s string) *time.Time {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}
	tu := t.UTC()
	return &tu
}

func (s *Server) handlePostFeed(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		// Курсоры единой ленты — по времени (RFC3339). since_ts: только новее (polling),
		// before_ts: более старые (подгрузка вниз). since_ts имеет приоритет.
		SinceTS  string `json:"since_ts"`
		BeforeTS string `json:"before_ts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed init_data invalid: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	sinceTS := parseFeedCursorTS(body.SinceTS)
	beforeTS := parseFeedCursorTS(body.BeforeTS)
	items, err := s.bot.PackFeedForViewer(parsed.User.ID, parsed, body.InitData, sinceTS, beforeTS)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		s.logger.Errorf("pack feed: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_error")
		return
	}
	// Закреплённые объявления — авторитетный текущий набор (в т.ч. при incremental polling),
	// чтобы фронт синхронизировал закреп/откреп даже для старых постов вне окна выдачи.
	// Для подгрузки вниз (before_ts) закрепы уже есть на фронте — не дёргаем повторно.
	var pinned []bot.PackFeedItem
	if beforeTS == nil {
		p, pErr := s.bot.PackFeedPinnedForViewer(parsed.User.ID, parsed, body.InitData)
		if pErr != nil {
			s.logger.Warnf("pack feed pinned: %v", pErr)
			p = nil
		}
		pinned = p
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "items": items, "pinned": pinned})
}

func (s *Server) handlePostFeedPin(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		Pin           bool   `json:"pin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed pin: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.PackFeedAdminSetPin(parsed.User.ID, parsed, body.UserMessageID, body.Pin); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed pin: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_pin_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostFeedPollVote(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		OptionIndex   int    `json:"option_index"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed poll vote invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.PackFeedPollVote(parsed.User.ID, parsed, body.UserMessageID, body.OptionIndex); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrPackFeedPollNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if errors.Is(err, bot.ErrPackFeedPollInvalidOption) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_option")
			return
		}
		s.logger.Errorf("pack feed poll vote: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "poll_vote_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handleGetUserAvatar(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		http.Error(w, "server_unavailable", http.StatusServiceUnavailable)
		return
	}
	initData := strings.TrimSpace(r.URL.Query().Get("init_data"))
	if initData == "" {
		http.Error(w, "missing_init_data", http.StatusBadRequest)
		return
	}
	uidStr := strings.TrimSpace(r.URL.Query().Get("user_id"))
	subjectUID, err := strconv.ParseInt(uidStr, 10, 64)
	if err != nil || subjectUID <= 0 {
		http.Error(w, "invalid_user_id", http.StatusBadRequest)
		return
	}
	if err := s.validateInit(initData); err != nil {
		s.logger.Warnf("miniapp user-avatar init_data invalid: %v", err)
		http.Error(w, "invalid_init_data", http.StatusUnauthorized)
		return
	}
	parsed, err := s.parseInit(initData)
	if err != nil || parsed.User.ID == 0 {
		http.Error(w, "parse_init_data", http.StatusBadRequest)
		return
	}
	aerr := s.bot.WritePackMemberAvatarHTTP(w, r, parsed.User.ID, subjectUID, parsed)
	if aerr == nil {
		return
	}
	if errors.Is(aerr, bot.ErrPackFeedForbidden) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if errors.Is(aerr, bot.ErrMiniAppChatMismatch) {
		http.Error(w, "chat_mismatch", http.StatusConflict)
		return
	}
	if errors.Is(aerr, bot.ErrPackMemberAvatarNotFound) {
		http.NotFound(w, r)
		return
	}
	s.logger.Warnf("miniapp user-avatar user=%d subject=%d: %v", parsed.User.ID, subjectUID, aerr)
	http.Error(w, "avatar_error", http.StatusInternalServerError)
}

func (s *Server) handlePostFeedTrainingReact(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		Emoji         string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed training react: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.PackTrainingFeedReact(parsed.User.ID, parsed, body.UserMessageID, body.Emoji); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedInvalidEmoji) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_emoji")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed training react: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "react_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostFeedTrainingThread(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}

	// Комментарий может прийти JSON'ом (только текст) или multipart'ом (текст + фото).
	var (
		initDataRaw   string
		userMessageID int64
		text          string
		replyToID     int64
		photoURL      string
		postAs        string
	)
	isMultipart := strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/form-data")
	if isMultipart {
		if err := r.ParseMultipartForm(maxWorkoutPhotoBytes + 65536); err != nil {
			s.jsonErr(w, http.StatusBadRequest, "invalid_multipart")
			return
		}
		initDataRaw = strings.TrimSpace(r.FormValue("init_data"))
		text = r.FormValue("text")
		if v, perr := strconv.ParseInt(strings.TrimSpace(r.FormValue("user_message_id")), 10, 64); perr == nil {
			userMessageID = v
		}
		if rid := strings.TrimSpace(r.FormValue("reply_to_id")); rid != "" {
			if v, perr := strconv.ParseInt(rid, 10, 64); perr == nil {
				replyToID = v
			}
		}
		postAs = strings.TrimSpace(r.FormValue("post_as"))
		if postAs == "" {
			// legacy: as_admin=1 раньше означало голос Лео.
			switch strings.TrimSpace(r.FormValue("as_admin")) {
			case "1", "true", "yes":
				postAs = "leo"
			}
		}
	} else {
		var body struct {
			InitData      string `json:"init_data"`
			UserMessageID int64  `json:"user_message_id"`
			Text          string `json:"text"`
			ReplyToID     int64  `json:"reply_to_id"`
			PostAs        string `json:"post_as"`
			AsAdmin       bool   `json:"as_admin"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			s.jsonErr(w, http.StatusBadRequest, "invalid_json")
			return
		}
		initDataRaw = body.InitData
		userMessageID = body.UserMessageID
		text = body.Text
		replyToID = body.ReplyToID
		postAs = strings.TrimSpace(body.PostAs)
		if postAs == "" && body.AsAdmin {
			postAs = "leo"
		}
	}

	if initDataRaw == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if userMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(initDataRaw); err != nil {
		s.logger.Warnf("miniapp feed training thread: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(initDataRaw)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}

	// Фото загружаем в хранилище ПОСЛЕ валидации initData (не тратим запись на анонимов).
	if isMultipart && r.MultipartForm != nil {
		if fs := r.MultipartForm.File["photo"]; len(fs) > 0 {
			file, ferr := fs[0].Open()
			if ferr != nil {
				s.jsonErr(w, http.StatusBadRequest, "photo_open_error")
				return
			}
			defer file.Close()
			url, ok := s.storeUploadedPhoto(w, r, file)
			if !ok {
				return // storeUploadedPhoto уже записал ошибку
			}
			photoURL = url
		}
	}

	if err := s.bot.PackTrainingFeedThreadPost(parsed.User.ID, parsed, userMessageID, text, replyToID, photoURL, postAs); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadEmpty) {
			s.jsonErr(w, http.StatusBadRequest, "empty_text")
			return
		}
		if s.jsonModerationErr(w, err) {
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadInvalidReply) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_reply")
			return
		}
		s.logger.Errorf("feed training thread: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "thread_error")
		return
	}
	replies, rerr := s.bot.PackFeedThreadRepliesForViewer(parsed.User.ID, userMessageID, initDataRaw)
	if rerr != nil {
		s.logger.Warnf("feed training thread: list after insert: %v", rerr)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if rerr == nil {
		out["thread"] = replies
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeedTrainingThreadDelete(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		ThreadReplyID int64  `json:"thread_reply_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.ThreadReplyID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_thread_reply_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed training thread delete: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	parentID, err := s.bot.PackTrainingFeedThreadDelete(parsed.User.ID, parsed, body.ThreadReplyID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadDeleteNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed training thread delete: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "thread_delete_error")
		return
	}
	replies, rerr := s.bot.PackFeedThreadRepliesForViewer(parsed.User.ID, parentID, body.InitData)
	if rerr != nil {
		s.logger.Warnf("feed training thread delete: list after delete: %v", rerr)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if rerr == nil {
		out["thread"] = replies
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeedTrainingThreadEdit(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		ThreadReplyID int64  `json:"thread_reply_id"`
		Text          string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.ThreadReplyID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_thread_reply_id")
		return
	}
	text := strings.TrimSpace(body.Text)
	if text == "" {
		s.jsonErr(w, http.StatusBadRequest, "empty_text")
		return
	}
	if utf8.RuneCountInString(text) > 500 {
		s.jsonErr(w, http.StatusBadRequest, "text_too_long")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed training thread edit: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	parentID, err := s.bot.PackTrainingFeedThreadEdit(parsed.User.ID, parsed, body.ThreadReplyID, text)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadEmpty) {
			s.jsonErr(w, http.StatusBadRequest, "empty_text")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadDeleteNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if s.jsonModerationErr(w, err) {
			return
		}
		s.logger.Errorf("feed training thread edit: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "thread_edit_error")
		return
	}
	replies, rerr := s.bot.PackFeedThreadRepliesForViewer(parsed.User.ID, parentID, body.InitData)
	if rerr != nil {
		s.logger.Warnf("feed training thread edit: list after edit: %v", rerr)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if rerr == nil {
		out["thread"] = replies
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeedEdit(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		Text          string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
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
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed edit: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.PackFeedPostEdit(parsed.User.ID, parsed, body.UserMessageID, text); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadEmpty) {
			s.jsonErr(w, http.StatusBadRequest, "empty_text")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if s.jsonModerationErr(w, err) {
			return
		}
		s.logger.Errorf("feed edit: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_edit_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostFeedDelete(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed delete: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	deletedPhotoURL, err := s.bot.PackFeedAdminDeletePost(parsed.User.ID, parsed, body.UserMessageID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed hide: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_delete_error")
		return
	}
	_ = deletedPhotoURL
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostFeedTrainingThreadLike(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		ThreadReplyID int64  `json:"thread_reply_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.ThreadReplyID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_thread_reply_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed training thread like: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	parentID, err := s.bot.PackTrainingFeedThreadLikeToggle(parsed.User.ID, parsed, body.ThreadReplyID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedThreadDeleteNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("feed training thread like: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "thread_like_error")
		return
	}
	replies, rerr := s.bot.PackFeedThreadRepliesForViewer(parsed.User.ID, parentID, body.InitData)
	if rerr != nil {
		s.logger.Warnf("feed training thread like: list after toggle: %v", rerr)
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if rerr == nil {
		out["thread"] = replies
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostFeedReport(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData      string `json:"init_data"`
		UserMessageID int64  `json:"user_message_id"`
		ThreadReplyID int64  `json:"thread_reply_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.UserMessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_user_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp feed report: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.PackFeedReport(parsed.User.ID, parsed, body.UserMessageID, body.ThreadReplyID); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedParentNotFound) || errors.Is(err, bot.ErrTrainingFeedThreadDeleteNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if errors.Is(err, bot.ErrFeedReportSelf) {
			s.jsonErr(w, http.StatusBadRequest, "cannot_report_self")
			return
		}
		if errors.Is(err, bot.ErrFeedReportLeo) {
			s.jsonErr(w, http.StatusBadRequest, "cannot_report_leo")
			return
		}
		if errors.Is(err, bot.ErrFeedReportAlreadyExists) {
			s.jsonErr(w, http.StatusConflict, "already_reported")
			return
		}
		s.logger.Errorf("feed report: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "report_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupReport(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		MessageID int64  `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.MessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group report: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.PackGroupChatReport(parsed.User.ID, parsed, body.MessageID); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedSocialForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrPackGroupMessageNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		if errors.Is(err, bot.ErrFeedReportSelf) {
			s.jsonErr(w, http.StatusBadRequest, "cannot_report_self")
			return
		}
		if errors.Is(err, bot.ErrFeedReportLeo) {
			s.jsonErr(w, http.StatusBadRequest, "cannot_report_leo")
			return
		}
		if errors.Is(err, bot.ErrFeedReportAlreadyExists) {
			s.jsonErr(w, http.StatusConflict, "already_reported")
			return
		}
		s.logger.Errorf("pack group report: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "report_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupFeed(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group feed: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	items, err := s.bot.PackGroupChatForViewer(parsed.User.ID, parsed, body.InitData)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		s.logger.Errorf("pack group feed: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

func (s *Server) handlePostPackGroupSearch(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		Query    string `json:"query"`
		Limit    int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	items, err := s.bot.PackGroupChatSearch(parsed.User.ID, parsed, body.Query, body.Limit)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		s.logger.Errorf("pack group search: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

func (s *Server) handlePostPackGroupMessage(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		Text      string `json:"text"`
		ReplyToID int64  `json:"reply_to_id"`
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
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group msg: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	miniRes, perr := s.bot.ProcessMiniAppPackGroupMessage(parsed, text, body.ReplyToID, "")
	if perr != nil {
		if errors.Is(perr, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(perr, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(perr, bot.ErrPackGroupInvalidReply) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_reply")
			return
		}
		if s.jsonModerationErr(w, perr) {
			return
		}
		s.logger.Errorf("pack group message: %v", perr)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{"ok": true}
	if miniRes.ReplyText != "" {
		out["reply_text"] = miniRes.ReplyText
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostPackGroupMessageDelete(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		MessageID int64  `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.MessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group delete: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	deleted, derr := s.bot.DeleteMiniAppPackGroupMessage(parsed.User.ID, parsed, body.MessageID)
	if derr != nil {
		if errors.Is(derr, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(derr, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		s.logger.Errorf("pack group delete: %v", derr)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_delete_error")
		return
	}
	if !deleted {
		s.jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupMessageEdit(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		MessageID int64  `json:"message_id"`
		Text      string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.MessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_message_id")
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
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group edit: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	updated, uerr := s.bot.EditMiniAppPackGroupMessage(parsed.User.ID, parsed, body.MessageID, text)
	if uerr != nil {
		if errors.Is(uerr, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(uerr, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if s.jsonModerationErr(w, uerr) {
			return
		}
		s.logger.Errorf("pack group edit: %v", uerr)
		s.jsonErr(w, http.StatusInternalServerError, "pack_group_edit_error")
		return
	}
	if !updated {
		s.jsonErr(w, http.StatusNotFound, "not_found")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupUnreadCount(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group unread count: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	summary, err := s.bot.MiniappPackGroupUnreadSummary(parsed, parsed.User.ID)
	if err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("pack group unread summary: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "unread_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "count": summary.Count, "pack_message_ids": summary.MessageIDs})
}

func (s *Server) handlePostPackGroupUnreadClear(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group unread clear: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.MiniappPackGroupUnreadClear(parsed, parsed.User.ID); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("pack group unread clear: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "unread_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostPackGroupReact(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		MessageID int64  `json:"message_id"`
		Emoji     string `json:"emoji"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.MessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp pack group react: invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.PackGroupChatReact(parsed.User.ID, parsed, body.MessageID, body.Emoji); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		if errors.Is(err, bot.ErrPackFeedForbidden) {
			s.jsonErr(w, http.StatusForbidden, "forbidden")
			return
		}
		if errors.Is(err, bot.ErrTrainingFeedInvalidEmoji) {
			s.jsonErr(w, http.StatusBadRequest, "invalid_emoji")
			return
		}
		if errors.Is(err, bot.ErrPackGroupMessageNotFound) {
			s.jsonErr(w, http.StatusNotFound, "not_found")
			return
		}
		s.logger.Errorf("pack group react: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "react_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handlePostPersonalChatFeed — серверная история приватного чата юзера с Лео.
// Источник правды для синхронизации между устройствами: localStorage заменён БД.
// Если sinceID > 0 — отдаём только записи новее (incremental polling); иначе —
// последние N сообщений (для первого открытия экрана).
func (s *Server) handlePostPersonalChatFeed(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		SinceID  int64  `json:"since_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp personal chat feed invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp personal chat feed assert: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	items, err := s.bot.MiniappPersonalChatHistory(parsed.User.ID, body.SinceID)
	if err != nil {
		s.logger.Errorf("miniapp personal chat feed load: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

func (s *Server) handlePostPersonalChatLike(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData  string `json:"init_data"`
		MessageID int64  `json:"message_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if body.MessageID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "missing_message_id")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp personal chat like invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp personal chat like assert: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	if err := s.bot.MiniappPersonalChatLikeToggle(parsed.User.ID, body.MessageID); err != nil {
		s.logger.Errorf("miniapp personal chat like: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "like_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

// handlePostSupportChatFeed — серверная история отдельного чата поддержки.
func (s *Server) handlePostSupportChatFeed(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
		SinceID  int64  `json:"since_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp support feed invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp support feed assert: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	// Первая загрузка ленты (since_id=0) = открытие экрана «Сообщить о проблеме».
	if body.SinceID == 0 {
		s.bot.TrackEvent(database.AnalyticsEvent{
			Name:       database.EventSupportButtonClicked,
			TelegramID: parsed.User.ID,
			Payload:    map[string]any{"from": "miniapp"},
		})
	}
	items, err := s.bot.MiniappSupportChatHistory(parsed.User.ID, body.SinceID)
	if err != nil {
		s.logger.Errorf("miniapp support feed load: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "feed_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "messages": items})
}

// handlePostSupportChatSend — пользователь пишет в поддержку, не в Лео.
func (s *Server) handlePostSupportChatSend(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
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
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.logger.Warnf("miniapp support send invalid init: %v", err)
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.logger.Errorf("miniapp support send assert: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	if err := s.bot.MiniappSupportSendFromUser(parsed.User.ID, text, ""); err != nil {
		s.logger.Errorf("miniapp support send: %v", err)
		s.jsonErr(w, http.StatusInternalServerError, "send_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func (s *Server) handlePostHealthStatus(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":      true,
		"on_sick": s.bot.MiniappHealthStatus(parsed.User.ID),
	})
}

// handlePostOnboardingEnsure — идемпотентный хендшейк мини-аппа: на первом открытии
// после оплаты стартуем таймер неактивности и пишем pack_join/pack_rejoin в ленту стаи.
func (s *Server) handlePostOnboardingEnsure(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	res, err := s.bot.EnsureMiniAppOnboarding(parsed)
	if err != nil {
		s.logger.Errorf("miniapp onboarding ensure user=%d: %v", parsed.User.ID, err)
		s.jsonErr(w, http.StatusInternalServerError, "onboarding_failed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":             true,
		"in_pack":        res.InPack,
		"deleted":        res.Deleted,
		"access_state":   res.AccessState,
		"just_onboarded": res.JustOnboarded,
		"is_rejoin":      res.IsRejoin,
	})
}

func (s *Server) handlePostProfileLoad(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	packID := s.bot.MonetizedChatID()
	if packID == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return
	}
	g, d, a := s.bot.GetMiniappUserProfileJSONForAPI(parsed.User.ID, packID)
	tz := s.bot.GetTimezoneOffsetForAPI(parsed.User.ID, packID)
	stats := s.bot.GetMiniappProfileStatsForAPI(parsed.User.ID, packID)
	workoutsByDay := s.bot.GetMiniappWorkoutsByDayForAPI(parsed.User.ID, packID, 90)
	suggestedWorkoutTypes := s.bot.GetSuggestedWorkoutTypesForAPI(parsed.User.ID, packID, tz, stats.DaysSinceLastTraining)
	kickAt := s.bot.GetMiniappInactivityRemovalDeadlineRFC3339(parsed.User.ID, packID)
	theme := s.bot.GetMiniappThemeForAPI(parsed.User.ID, packID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{
		"ok":                         true,
		"gender":                     g,
		"display_name":               d,
		"theme":                      theme,
		"timezone_offset":            tz,
		"xp":                         stats.XP,
		"level":                      stats.Level,
		"level_name":                 stats.LevelName,
		"streak_days":                stats.StreakDays,
		"max_streak_days":            stats.MaxStreakDays,
		"achievement_count":          stats.AchievementCount,
		"achievements_max":           stats.AchievementsMax,
		"workouts_total":             stats.WorkoutsTotal,
		"workouts_week":              stats.WorkoutsWeek,
		"days_since_last_training":   stats.DaysSinceLastTraining,
		"last_training_date":         stats.LastTrainingDate,
		"streak_save_attempts_used":  stats.StreakSaveAttemptsUsed,
		"streak_save_attempts_max":   stats.StreakSaveAttemptsMax,
		"streak_save_attempts_avail": stats.StreakSaveAttemptsAvail,
		"is_admin":                   s.bot.IsMiniappViewerAdmin(parsed.User.ID),
		"access_price_rub":           s.bot.AccessPriceRub(),
		"workouts_by_day":            workoutsByDay,
		"suggested_workout_types":    suggestedWorkoutTypes,
	}
	if kickAt != "" {
		out["inactivity_removal_at"] = kickAt
	}
	if a != nil {
		out["age"] = *a
	} else {
		out["age"] = nil
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostProfileSave(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData       string  `json:"init_data"`
		Gender         *string `json:"gender"`
		DisplayName    *string `json:"display_name"`
		Age            *int    `json:"age"`
		TimezoneOffset *int    `json:"timezone_offset"`
		Theme          *string `json:"theme"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	packID := s.bot.MonetizedChatID()
	if packID == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return
	}
	if body.Gender != nil || body.DisplayName != nil || body.Age != nil {
		gv := ""
		if body.Gender != nil {
			gv = *body.Gender
		}
		dn := ""
		if body.DisplayName != nil {
			dn = *body.DisplayName
		}
		if err := s.bot.SaveMiniappUserProfileFromMiniapp(parsed.User.ID, packID, gv, dn, body.Age); err != nil {
			s.logger.Errorf("miniapp profile save: %v", err)
			s.jsonErr(w, http.StatusInternalServerError, "save_failed")
			return
		}
	}
	if body.Theme != nil {
		if err := s.bot.SaveMiniappThemeFromMiniapp(parsed.User.ID, packID, *body.Theme); err != nil {
			s.logger.Errorf("miniapp theme save: %v", err)
			s.jsonErr(w, http.StatusBadRequest, "invalid_theme")
			return
		}
	}
	if body.TimezoneOffset != nil {
		if err := s.bot.SaveTimezoneOffsetFromMiniapp(parsed.User.ID, packID, *body.TimezoneOffset); err != nil {
			s.logger.Errorf("miniapp tz save: %v", err)
			s.jsonErr(w, http.StatusBadRequest, "invalid_timezone_offset")
			return
		}
	}
	g, d, a := s.bot.GetMiniappUserProfileJSONForAPI(parsed.User.ID, packID)
	tz := s.bot.GetTimezoneOffsetForAPI(parsed.User.ID, packID)
	stats := s.bot.GetMiniappProfileStatsForAPI(parsed.User.ID, packID)
	kickAt := s.bot.GetMiniappInactivityRemovalDeadlineRFC3339(parsed.User.ID, packID)
	theme := s.bot.GetMiniappThemeForAPI(parsed.User.ID, packID)
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	out := map[string]any{
		"ok":                         true,
		"gender":                     g,
		"display_name":               d,
		"theme":                      theme,
		"timezone_offset":            tz,
		"xp":                         stats.XP,
		"level":                      stats.Level,
		"level_name":                 stats.LevelName,
		"streak_days":                stats.StreakDays,
		"max_streak_days":            stats.MaxStreakDays,
		"achievement_count":          stats.AchievementCount,
		"achievements_max":           stats.AchievementsMax,
		"workouts_total":             stats.WorkoutsTotal,
		"workouts_week":              stats.WorkoutsWeek,
		"days_since_last_training":   stats.DaysSinceLastTraining,
		"last_training_date":         stats.LastTrainingDate,
		"streak_save_attempts_used":  stats.StreakSaveAttemptsUsed,
		"streak_save_attempts_max":   stats.StreakSaveAttemptsMax,
		"streak_save_attempts_avail": stats.StreakSaveAttemptsAvail,
	}
	if kickAt != "" {
		out["inactivity_removal_at"] = kickAt
	}
	if a != nil {
		out["age"] = *a
	} else {
		out["age"] = nil
	}
	_ = json.NewEncoder(w).Encode(out)
}

func (s *Server) handlePostStreakSaveUse(w http.ResponseWriter, r *http.Request) {
	corsWriteHeaders(w, r)
	if s.bot == nil || s.token == "" {
		s.jsonErr(w, http.StatusServiceUnavailable, "server_unavailable")
		return
	}
	var body struct {
		InitData string `json:"init_data"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.jsonErr(w, http.StatusBadRequest, "invalid_json")
		return
	}
	if body.InitData == "" {
		s.jsonErr(w, http.StatusBadRequest, "missing_init_data")
		return
	}
	if err := s.validateInit(body.InitData); err != nil {
		s.jsonErr(w, http.StatusUnauthorized, "invalid_init_data")
		return
	}
	parsed, err := s.parseInit(body.InitData)
	if err != nil {
		s.jsonErr(w, http.StatusBadRequest, "parse_init_data")
		return
	}
	if parsed.User.ID == 0 {
		s.jsonErr(w, http.StatusBadRequest, "user_missing")
		return
	}
	if err := s.bot.AssertMiniAppPackChatAligns(parsed); err != nil {
		if errors.Is(err, bot.ErrMiniAppChatMismatch) {
			s.jsonErr(w, http.StatusConflict, "chat_mismatch")
			return
		}
		s.jsonErr(w, http.StatusInternalServerError, "assert_chat_error")
		return
	}
	packID := s.bot.MonetizedChatID()
	if packID == 0 {
		s.jsonErr(w, http.StatusServiceUnavailable, "pack_not_configured")
		return
	}
	used, max, avail, restoredStreak, err := s.bot.UseStreakSaveAttemptForAPI(parsed.User.ID, packID)
	if err != nil {
		switch err.Error() {
		case "no_attempts", "not_needed", "too_late", "nothing_to_save", "no_training_history":
			w.Header().Set("Content-Type", "application/json; charset=utf-8")
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error":                      err.Error(),
				"streak_days":                restoredStreak,
				"streak_save_attempts_used":  used,
				"streak_save_attempts_max":   max,
				"streak_save_attempts_avail": avail,
			})
			return
		}
		s.logger.Errorf("streak save use user=%d pack=%d: %v", parsed.User.ID, packID, err)
		s.jsonErr(w, http.StatusInternalServerError, "save_failed")
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":                         true,
		"streak_days":                restoredStreak,
		"streak_save_attempts_used":  used,
		"streak_save_attempts_max":   max,
		"streak_save_attempts_avail": avail,
	})
}

func (s *Server) jsonModerationErr(w http.ResponseWriter, err error) bool {
	var mod *bot.ModerationBlockedError
	if !errors.As(err, &mod) || mod == nil {
		return false
	}
	code := mod.APICode
	if code == "" {
		code = "moderation_blocked"
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(bot.ModerationHTTPStatus(code))
	_ = json.NewEncoder(w).Encode(map[string]string{"error": code, "message": mod.Message})
	return true
}

func (s *Server) jsonErr(w http.ResponseWriter, code int, err string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err})
}

func withCORS(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			corsWriteHeaders(w, r)
			w.WriteHeader(http.StatusNoContent)
			return
		}
		corsWriteHeaders(w, r)
		h.ServeHTTP(w, r)
	})
}

func corsWriteHeaders(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
	acc := "Content-Type"
	if rh := r.Header.Get("Access-Control-Request-Headers"); strings.TrimSpace(rh) != "" {
		acc = rh
	}
	w.Header().Set("Access-Control-Allow-Headers", acc)
}
