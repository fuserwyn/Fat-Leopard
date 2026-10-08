package miniappapi

import (
	"net/http"
	"strings"

	"leo-bot/internal/bot"
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
	case path == "/api/miniapp/profile/cups-history" && r.Method == http.MethodPost:
		s.handlePostProfileCupsHistory(w, r)
	case path == "/api/miniapp/challenges/state" && r.Method == http.MethodPost:
		s.handlePostChallengesState(w, r)
	case path == "/api/miniapp/challenges/accept" && r.Method == http.MethodPost:
		s.handlePostChallengesAccept(w, r)
	case path == "/api/miniapp/challenges/create" && r.Method == http.MethodPost:
		s.handlePostChallengesCreate(w, r)
	case path == "/api/miniapp/challenges/leave" && r.Method == http.MethodPost:
		s.handlePostChallengesLeave(w, r)
	case path == "/api/miniapp/challenges/invite/dismiss" && r.Method == http.MethodPost:
		s.handlePostChallengesInviteDismiss(w, r)
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
	case path == "/api/miniapp/contact-join-notifications/load" && r.Method == http.MethodPost:
		s.handlePostContactJoinNotificationsLoad(w, r)
	case path == "/api/miniapp/contact-join-notifications/save" && r.Method == http.MethodPost:
		s.handlePostContactJoinNotificationsSave(w, r)
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
	case path == "/api/miniapp/admin/pack-goal" && r.Method == http.MethodPost:
		s.handlePostAdminPackGoal(w, r)
	case path == "/api/miniapp/admin/pack-goal/set" && r.Method == http.MethodPost:
		s.handlePostAdminPackGoalSet(w, r)
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
