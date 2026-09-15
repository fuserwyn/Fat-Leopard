package bot

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	initdata "github.com/telegram-mini-apps/init-data-golang"

	"leo-bot/internal/database"
	"leo-bot/internal/domain"
	"leo-bot/internal/game/leopardmoney"
)

// ErrPackFeedForbidden — смотрящему нельзя видеть ленту (нет в стае / не оплачено).
var ErrPackFeedForbidden = errors.New("pack feed forbidden")

// PackVoter — кто отреагировал/лайкнул: имя + аватар (для поповера «кто лайкнул»).
type PackVoter struct {
	Name     string `json:"name"`
	PhotoURL string `json:"photo_url,omitempty"`
}

// packVoters — конвертация голосов в DTO с отрезолвленным аватаром: безопасный
// публичный photo_url из профиля или прокси через Bot API (как у авторов карточек).
func (b *Bot) packVoters(in []database.Voter, initDataRaw string) []PackVoter {
	if len(in) == 0 {
		return nil
	}
	publicBase := ""
	if b != nil && b.config != nil {
		publicBase = strings.TrimSpace(b.config.MiniappPublicBaseURL)
	}
	out := make([]PackVoter, len(in))
	for i, v := range in {
		if v.UserID == 0 || strings.TrimSpace(v.Name) == "Лео" {
			out[i] = PackVoter{
				Name:     v.Name,
				PhotoURL: packFeedLeoAvatarURL(publicBase),
			}
			continue
		}
		out[i] = PackVoter{
			Name:     v.Name,
			PhotoURL: packFeedResolveAuthorPhoto(v.PhotoURL, publicBase, v.UserID, initDataRaw),
		}
	}
	return out
}

// PackFeedReaction — агрегат реакций на отчёт в мини-аппе.
type PackFeedReaction struct {
	Emoji  string      `json:"emoji"`
	Count  int         `json:"count"`
	Me     bool        `json:"me"`
	Voters []PackVoter `json:"voters,omitempty"`
}

// PackFeedThreadReply — реплика в треде под training_done.
type PackFeedThreadReply struct {
	ID              int64  `json:"id"`
	UserID          int64  `json:"user_id"`
	Username        string `json:"username"`
	Text            string `json:"text"`
	CreatedAt       string `json:"created_at"`
	IsYou           bool   `json:"is_you"`
	IsLeo           bool   `json:"is_leo"`
	// IsAdmin — комментарий опубликован «от имени админов».
	IsAdmin bool `json:"is_admin,omitempty"`
	// AdminName — реальное имя автора-админа за голосом Лео/Админ. Заполняется
	// ТОЛЬКО для зрителей-админов (атрибуция); обычные юзеры его не получают.
	AdminName       string `json:"admin_name,omitempty"`
	AuthorPhotoURL  string `json:"author_photo_url,omitempty"`
	PhotoURL        string `json:"photo_url,omitempty"`
	ReplyToID       int64  `json:"reply_to_id,omitempty"`
	ReplyToUsername string `json:"reply_to_username,omitempty"`
	ReplyToText     string `json:"reply_to_text,omitempty"`
	ReplyToIsLeo    bool   `json:"reply_to_is_leo,omitempty"`
	ReplyToIsAdmin  bool   `json:"reply_to_is_admin,omitempty"`
	LikeCount       int         `json:"like_count,omitempty"`
	LikeMe          bool        `json:"like_me,omitempty"`
	LikeVoters      []PackVoter `json:"like_voters,omitempty"`
	EditedAt        string      `json:"edited_at,omitempty"`
}

type PackFeedPollOption struct {
	Label string `json:"label"`
	Votes int    `json:"votes"`
}

type PackFeedPoll struct {
	Question    string               `json:"question"`
	Options     []PackFeedPollOption `json:"options"`
	TotalVotes  int                  `json:"total_votes"`
	MyVoteIndex *int                 `json:"my_vote_index,omitempty"`
}

// PackFeedItem — JSON для мини-апpa.
type PackFeedItem struct {
	ID               int64  `json:"id"`
	UserID           int64  `json:"user_id"`
	Username         string `json:"username"`
	// AuthorTgUsername — настоящий TG-ник автора без «@». Фича «Написать в Telegram»
	// выключена: поле в ленту больше не заполняем.
	AuthorTgUsername string `json:"author_tg_username,omitempty"`
	Type             string `json:"type"`
	// Source — источник записи: "feed" (user_messages: тренировки/системные) или
	// "message" (miniapp_pack_group_chat). id уникален только внутри источника,
	// поэтому фронт ключует записи как `${source}:${id}` и роутит действия по source.
	Source           string `json:"source,omitempty"`
	Text             string `json:"text"`
	CreatedAt        string `json:"created_at"`
	StreakDays       int    `json:"streak_days"`
	IsYou            bool   `json:"is_you"`
	// IsFriend — viewer подписан на автора этой карточки (для фильтра «Друзья» в ленте).
	IsFriend         bool   `json:"is_friend,omitempty"`
	AuthorPhotoURL   string `json:"author_photo_url,omitempty"`
	TrainingPhotoURL string `json:"training_photo_url,omitempty"`
	// PackChatID — id группы «Стая» (MONETIZED_CHAT_ID), с которой синхронизирована лента.
	PackChatID int64                 `json:"pack_chat_id"`
	PackTitle  string                `json:"pack_title,omitempty"`
	Reactions  []PackFeedReaction    `json:"reactions,omitempty"`
	Thread     []PackFeedThreadReply `json:"thread,omitempty"`
	Poll       *PackFeedPoll         `json:"poll,omitempty"`
	// IsPinned — объявление закреплено админом (висит сверху ленты с пометкой «объявление»).
	IsPinned bool   `json:"is_pinned,omitempty"`
	PinnedAt string `json:"pinned_at,omitempty"`
	EditedAt string `json:"edited_at,omitempty"`
}

// PackWorkoutTypeCountsForViewer — число training_done по видам спорта за всё время стаи.
func (b *Bot) PackWorkoutTypeCountsForViewer(viewerUserID int64, initD initdata.InitData) (map[string]int, error) {
	if err := b.PackFeedAssertViewerAccess(viewerUserID, initD); err != nil {
		return nil, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return map[string]int{}, nil
	}
	texts, err := b.db.ListPackTrainingDoneTexts(chatID)
	if err != nil {
		return nil, err
	}
	return leopardmoney.AggregateTrainingCategoryCounts(texts), nil
}

// PackFeedForViewer — объединённая лента стаи: тренировки/системные карточки
// (user_messages) + сообщения общего чата (miniapp_pack_group_chat), слитые по
// created_at (новые сверху). sinceTS != nil — только записи новее курсора (polling
// новых), beforeTS != nil — более старые (подгрузка вниз при скролле). sinceTS
// имеет приоритет. Старые сообщения чата (id <= baseline) в ленту не попадают.
// initD сверяется с MONETIZED_CHAT_ID; initDataRaw — для прокси аватаров.
func (b *Bot) PackFeedForViewer(viewerUserID int64, initD initdata.InitData, initDataRaw string, sinceTS *time.Time, beforeTS *time.Time) ([]PackFeedItem, error) {
	if err := b.PackFeedAssertViewerAccess(viewerUserID, initD); err != nil {
		return nil, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return []PackFeedItem{}, nil
	}
	packTitle := ""
	if initD.Chat.ID != 0 && (initD.Chat.Type == initdata.ChatTypeSupergroup || initD.Chat.Type == initdata.ChatTypeGroup) {
		packTitle = initD.Chat.Title
	}

	// Размер страницы: polling новых — меньше, обычная выдача/подгрузка — больше.
	limit := 50
	if sinceTS != nil {
		limit = 30
	}

	// --- Источник 1: тренировки и системные карточки (user_messages) ---
	var rows []*domain.PackActivityRow
	var err error
	switch {
	case sinceTS != nil:
		rows, err = b.db.ListPackActivityFeedAfterTS(chatID, *sinceTS, limit)
	default:
		rows, err = b.db.ListPackActivityFeedDesc(chatID, beforeTS, limit)
	}
	if err != nil {
		return nil, err
	}
	feedItems := make([]PackFeedItem, 0, len(rows))
	for _, r := range rows {
		if r.MessageType == "sick_leave" {
			continue
		}
		feedItems = append(feedItems, b.packFeedItemFromRow(r, viewerUserID, chatID, packTitle))
	}
	feedItems = b.enrichPackFeedTrainingSocial(feedItems, viewerUserID, chatID, initDataRaw)
	feedItems = b.enrichPackFeedPolls(feedItems, viewerUserID, chatID)
	feedItems = b.enrichPackFeedAuthorPhotos(feedItems, chatID, initDataRaw)

	// --- Источник 2: сообщения общего чата (miniapp_pack_group_chat) ---
	baseline, bErr := b.db.GetPackMessageBaselineID()
	if bErr != nil {
		// Нет строки baseline (миграция не накатилась) — сообщения в ленту пока не подмешиваем.
		b.logger.Warnf("pack message baseline: %v", bErr)
		baseline = -1
	}
	msgItems := []PackFeedItem{}
	if baseline >= 0 {
		var mrows []database.PackGroupChatRow
		var mErr error
		switch {
		case sinceTS != nil:
			mrows, mErr = b.db.ListPackGroupChatFeedAfterTS(chatID, baseline, *sinceTS, limit)
		default:
			mrows, mErr = b.db.ListPackGroupChatFeedDesc(chatID, baseline, beforeTS, limit)
		}
		if mErr != nil {
			return nil, mErr
		}
		msgs := b.packGroupRowsToMessages(mrows)
		for _, m := range msgs {
			if m != nil && m.PhotoURL != "" {
				m.PhotoURL = b.canonicalMiniappTrainingPhotoURL(m.PhotoURL)
			}
		}
		msgs = b.enrichPackGroupChatAuthorPhotos(msgs, chatID, initDataRaw)
		msgs = b.enrichPackGroupChatReactions(msgs, viewerUserID, chatID, initDataRaw)
		for _, m := range msgs {
			if m == nil {
				continue
			}
			msgItems = append(msgItems, b.packFeedItemFromMessage(m, viewerUserID, chatID, packTitle))
		}
		msgItems = b.enrichPackFeedMessageThreads(msgItems, viewerUserID, chatID, initDataRaw)
	}

	// --- Мёрж по времени (новые сверху) + ограничение страницы ---
	merged := mergePackFeedByCreatedAtDesc(feedItems, msgItems)
	// IsFriend проставляем на объединённом списке: раньше enrich шёл только по
	// тренировкам, поэтому сообщения друзей никогда не попадали в фильтр «Друзья»
	// (и «Друзья + Сообщения» всегда были пустыми).
	merged = b.enrichPackFeedFriends(merged, viewerUserID, chatID)
	if len(merged) > limit {
		merged = merged[:limit]
	}
	// «Написать в Telegram» выключено: TG-ник в ленту не отдаём.
	return merged, nil
}

// enrichPackFeedAuthorTgUsernames — проставляет настоящий TG-ник автора (без «@»)
// на карточках участников: Username карточки может быть display_name из анкеты,
// а для кнопки «Написать в Telegram» нужен именно @ник.
func (b *Bot) enrichPackFeedAuthorTgUsernames(items []PackFeedItem, chatID int64) []PackFeedItem {
	if b == nil || b.db == nil || chatID == 0 || len(items) == 0 {
		return items
	}
	idSet := make(map[int64]struct{}, len(items))
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		if it.UserID <= 0 {
			continue
		}
		if _, ok := idSet[it.UserID]; ok {
			continue
		}
		idSet[it.UserID] = struct{}{}
		ids = append(ids, it.UserID)
	}
	if len(ids) == 0 {
		return items
	}
	byID, err := b.db.PackMemberTgUsernames(chatID, ids)
	if err != nil {
		b.logger.Warnf("pack feed tg usernames enrich: %v", err)
		return items
	}
	for i := range items {
		if un, ok := byID[items[i].UserID]; ok {
			items[i].AuthorTgUsername = strings.TrimPrefix(un, "@")
		}
	}
	return items
}

// packFeedItemFromMessage — сообщение общего чата как карточка единой ленты.
func (b *Bot) packFeedItemFromMessage(m *domain.PackGroupChatMessage, viewerUserID, chatID int64, packTitle string) PackFeedItem {
	item := PackFeedItem{
		ID:               m.ID,
		UserID:           m.UserID,
		Username:         m.Username,
		Type:             "pack_message",
		Source:           "message",
		Text:             m.Text,
		CreatedAt:        m.CreatedAt,
		IsYou:            m.UserID == viewerUserID && !m.IsLeo,
		PackChatID:       chatID,
		PackTitle:        packTitle,
		AuthorPhotoURL:   m.AuthorPhotoURL,
		TrainingPhotoURL: m.PhotoURL,
		EditedAt:         m.EditedAt,
	}
	if len(m.Reactions) > 0 {
		item.Reactions = convertPackGroupReactions(m.Reactions)
	}
	return item
}

// applyMessageThreadQuote — цитата «ответ на…», если комментарий отвечает не на
// карточку-родителя, а на другое сообщение треда (Лео или участник).
func applyMessageThreadQuote(pr *PackFeedThreadReply, m *domain.PackGroupChatMessage, rootParentID int64) {
	if pr == nil || m == nil || m.ReplyToID <= 0 || m.ReplyToID == rootParentID {
		return
	}
	pr.ReplyToID = m.ReplyToID
	pr.ReplyToUsername = m.ReplyToUsername
	pr.ReplyToText = m.ReplyToText
	pr.ReplyToIsLeo = m.ReplyToIsLeo
}

// enrichPackFeedMessageThreads — подтягивает комментарии (ответы) к сообщениям ленты
// как тред, включая ответы на комментарии. Комментарий = сообщение чата с reply_to_id;
// поэтому переиспользуем хранилище общего чата (вставка/редакт/удаление/фото).
func (b *Bot) enrichPackFeedMessageThreads(items []PackFeedItem, viewerUserID, chatID int64, initDataRaw string) []PackFeedItem {
	if b == nil || b.db == nil || chatID == 0 || len(items) == 0 {
		return items
	}
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		if it.Source == "message" && it.ID > 0 {
			ids = append(ids, it.ID)
		}
	}
	if len(ids) == 0 {
		return items
	}
	repliesByParent, err := b.db.ListPackGroupRepliesForMessages(chatID, ids)
	if err != nil {
		b.logger.Warnf("pack feed message threads: %v", err)
		return items
	}
	if len(repliesByParent) == 0 {
		return items
	}
	var allRows []database.PackGroupChatRow
	for _, rows := range repliesByParent {
		allRows = append(allRows, rows...)
	}
	// packGroupRowsToMessages заполняет цитату (reply_to_username/text) по родителю.
	allMsgs := b.packGroupRowsToMessages(allRows)
	for _, m := range allMsgs {
		if m != nil && m.PhotoURL != "" {
			m.PhotoURL = b.canonicalMiniappTrainingPhotoURL(m.PhotoURL)
		}
	}
	allMsgs = b.enrichPackGroupChatAuthorPhotos(allMsgs, chatID, initDataRaw)
	msgByID := make(map[int64]*domain.PackGroupChatMessage, len(allMsgs))
	for _, m := range allMsgs {
		if m != nil {
			msgByID[m.ID] = m
		}
	}
	for i := range items {
		if items[i].Source != "message" {
			continue
		}
		rows := repliesByParent[items[i].ID]
		if len(rows) == 0 {
			continue
		}
		thread := make([]PackFeedThreadReply, 0, len(rows))
		for _, r := range rows {
			m := msgByID[r.ID]
			if m == nil {
				continue
			}
			pr := PackFeedThreadReply{
				ID:             m.ID,
				UserID:         m.UserID,
				Username:       m.Username,
				Text:           m.Text,
				CreatedAt:      m.CreatedAt,
				IsYou:          m.UserID == viewerUserID && !m.IsLeo,
				IsLeo:          m.IsLeo,
				AuthorPhotoURL: m.AuthorPhotoURL,
				PhotoURL:       m.PhotoURL,
				EditedAt:       m.EditedAt,
			}
			applyMessageThreadQuote(&pr, m, items[i].ID)
			thread = append(thread, pr)
		}
		items[i].Thread = thread
	}
	return items
}

// convertPackGroupReactions — реакции чата → реакции ленты (одинаковая форма).
func convertPackGroupReactions(in []domain.PackGroupChatReaction) []PackFeedReaction {
	out := make([]PackFeedReaction, 0, len(in))
	for _, r := range in {
		var voters []PackVoter
		if len(r.Voters) > 0 {
			voters = make([]PackVoter, len(r.Voters))
			for i, v := range r.Voters {
				voters[i] = PackVoter{Name: v.Name, PhotoURL: v.PhotoURL}
			}
		}
		out = append(out, PackFeedReaction{Emoji: r.Emoji, Count: r.Count, Me: r.Me, Voters: voters})
	}
	return out
}

// mergePackFeedByCreatedAtDesc — слить два уже-DESC списка в один по created_at
// (новые сверху). Тайбрейк — id DESC внутри одного created_at (стабильный порядок).
func mergePackFeedByCreatedAtDesc(a, b []PackFeedItem) []PackFeedItem {
	out := make([]PackFeedItem, 0, len(a)+len(b))
	out = append(out, a...)
	out = append(out, b...)
	sort.SliceStable(out, func(i, j int) bool {
		ti := parsePackFeedTime(out[i].CreatedAt)
		tj := parsePackFeedTime(out[j].CreatedAt)
		if ti.Equal(tj) {
			return out[i].ID > out[j].ID
		}
		return ti.After(tj)
	})
	return out
}

func parsePackFeedTime(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// packFeedItemFromRow конвертирует строку ленты в PackFeedItem (с пометкой закрепа).
func (b *Bot) packFeedItemFromRow(r *domain.PackActivityRow, viewerUserID, chatID int64, packTitle string) PackFeedItem {
	item := PackFeedItem{
		ID:               r.ID,
		UserID:           r.UserID,
		Username:         r.Username,
		Type:             r.MessageType,
		Source:           "feed",
		Text:             r.MessageText,
		CreatedAt:        r.CreatedAt.UTC().Format("2006-01-02T15:04:05Z07:00"),
		StreakDays:       r.StreakDays,
		IsYou:            r.UserID == viewerUserID,
		PackChatID:       chatID,
		PackTitle:        packTitle,
		TrainingPhotoURL: b.canonicalMiniappTrainingPhotoURL(r.TrainingPhotoURL),
	}
	if r.PinnedAt != nil {
		item.IsPinned = true
		item.PinnedAt = r.PinnedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if r.EditedAt.Valid {
		item.EditedAt = r.EditedAt.Time.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return item
}

// PackFeedPinnedForViewer — текущие закреплённые объявления стаи (свежезакреплённые сверху).
// Возвращается отдельно от хронологической ленты как авторитетный набор для фронта:
// фронт по нему синхронизирует флаг закрепа (в т.ч. снимает закреп при открепе).
func (b *Bot) PackFeedPinnedForViewer(viewerUserID int64, initD initdata.InitData, initDataRaw string) ([]PackFeedItem, error) {
	if err := b.PackFeedAssertViewerAccess(viewerUserID, initD); err != nil {
		return nil, err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return []PackFeedItem{}, nil
	}
	packTitle := ""
	if initD.Chat.ID != 0 && (initD.Chat.Type == initdata.ChatTypeSupergroup || initD.Chat.Type == initdata.ChatTypeGroup) {
		packTitle = initD.Chat.Title
	}
	rows, err := b.db.ListPinnedAdminPosts(chatID)
	if err != nil {
		return nil, err
	}
	out := make([]PackFeedItem, 0, len(rows))
	for _, r := range rows {
		out = append(out, b.packFeedItemFromRow(r, viewerUserID, chatID, packTitle))
	}
	out = b.enrichPackFeedTrainingSocial(out, viewerUserID, chatID, initDataRaw)
	out = b.enrichPackFeedPolls(out, viewerUserID, chatID)
	out = b.enrichPackFeedAuthorPhotos(out, chatID, initDataRaw)
	return out, nil
}

// PackFeedAdminSetPin — админ закрепляет/открепляет объявление в ленте стаи.
// Только админ; только admin_post/admin_poll. Открепление оставляет пост в ленте.
func (b *Bot) PackFeedAdminSetPin(viewerUserID int64, initD initdata.InitData, userMessageID int64, pin bool) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	if b == nil || b.db == nil {
		return fmt.Errorf("bot unavailable")
	}
	if !b.isAdminTelegramUser(viewerUserID) {
		return ErrPackFeedForbidden
	}
	if userMessageID <= 0 {
		return ErrTrainingFeedParentNotFound
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return ErrTrainingFeedParentNotFound
	}
	ok, err := b.db.SetUserMessagePinned(chatID, userMessageID, pin)
	if err != nil {
		return err
	}
	if !ok {
		return ErrTrainingFeedParentNotFound
	}
	return nil
}

func packFeedIsLeoNoticeType(t string) bool {
	switch t {
	case "pack_join", "pack_rejoin", "daily_wisdom", "pack_removed", "admin_post", "admin_poll":
		return true
	default:
		return false
	}
}

