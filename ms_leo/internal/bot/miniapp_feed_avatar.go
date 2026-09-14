package bot

import (
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// ErrPackMemberAvatarNotFound — у пользователя нет фото профиля или Telegram не отдал файл.
var ErrPackMemberAvatarNotFound = errors.New("pack member avatar not found")

// PackFeedAssertViewerAccess — та же проверка доступа к ленте стаи, что и перед PackFeedForViewer.
func (b *Bot) PackFeedAssertViewerAccess(viewerUserID int64, initD initdata.InitData) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return ErrPackFeedForbidden
	}
	if b.config.IsAdminTelegramUser(viewerUserID) {
		return nil
	}
	ok, err := b.db.UserInPackOrPaid(viewerUserID, chatID, b.paywallEntryRequiresPayment())
	if err != nil {
		return err
	}
	if !ok {
		return ErrPackFeedForbidden
	}
	return nil
}

func packFeedTelegramPhotoURLIsSafePublic(raw string) bool {
	u := strings.TrimSpace(raw)
	if u == "" {
		return false
	}
	if strings.Contains(strings.ToLower(u), "api.telegram.org/file/bot") {
		return false
	}
	return strings.HasPrefix(u, "https://") || strings.HasPrefix(u, "http://")
}

// packFeedAvatarProxyURL — URL для <img>, сервер отдаёт байты через Bot API без утечки токена в клиент.
const leoAvatarStaticPath = "/leo-avatar.png"

// packFeedLeoAvatarURL — статичный аватар Лео из мини-аппа (public/leo-avatar.png).
func packFeedLeoAvatarURL(publicBase string) string {
	base := strings.TrimRight(strings.TrimSpace(publicBase), "/")
	if base != "" {
		return base + leoAvatarStaticPath
	}
	return leoAvatarStaticPath
}

func packFeedAvatarProxyURL(publicBase string, subjectUserID int64, initDataRaw string) string {
	if subjectUserID == 0 || strings.TrimSpace(initDataRaw) == "" {
		return ""
	}
	q := url.Values{}
	q.Set("init_data", initDataRaw)
	q.Set("user_id", strconv.FormatInt(subjectUserID, 10))
	suffix := "/api/miniapp/user-avatar?" + q.Encode()
	base := strings.TrimRight(strings.TrimSpace(publicBase), "/")
	if base != "" {
		return base + suffix
	}
	return suffix
}

func packFeedResolveAuthorPhoto(dbURL string, publicBase string, subjectUID int64, initDataRaw string) string {
	// Всегда отдаём прокси: прямые t.me/userpic из WebApp init часто 403 в WebView
	// (Referer / протухший CDN). Прокси сам тянет байты через Bot API или сохранённый URL.
	if proxy := packFeedAvatarProxyURL(publicBase, subjectUID, initDataRaw); proxy != "" {
		return proxy
	}
	if packFeedTelegramPhotoURLIsSafePublic(dbURL) {
		return strings.TrimSpace(dbURL)
	}
	return ""
}

// enrichPackFeedAuthorPhotos — URL аватаров: безопасный photo_url из профиля или прокси через Bot API.
func (b *Bot) enrichPackFeedAuthorPhotos(items []PackFeedItem, chatID int64, initDataRaw string) []PackFeedItem {
	if b == nil || b.db == nil || chatID == 0 || len(items) == 0 {
		return items
	}
	publicBase := ""
	if b.config != nil {
		publicBase = strings.TrimSpace(b.config.MiniappPublicBaseURL)
	}
	var ids []int64
	seen := map[int64]struct{}{}
	add := func(uid int64) {
		if uid == 0 {
			return
		}
		if _, ok := seen[uid]; ok {
			return
		}
		seen[uid] = struct{}{}
		ids = append(ids, uid)
	}
	for _, it := range items {
		for _, tr := range it.Thread {
			if !tr.IsLeo {
				add(tr.UserID)
			}
		}
		if !packFeedIsLeoNoticeType(it.Type) {
			add(it.UserID)
		}
	}
	if len(ids) == 0 {
		return items
	}
	m, err := b.db.MiniappTelegramPhotoURLMap(chatID, ids)
	if err != nil {
		b.logger.Warnf("miniapp feed author photos map: %v", err)
		m = map[int64]string{}
	}
	for i := range items {
		if !packFeedIsLeoNoticeType(items[i].Type) && items[i].UserID != 0 {
			items[i].AuthorPhotoURL = packFeedResolveAuthorPhoto(m[items[i].UserID], publicBase, items[i].UserID, initDataRaw)
		}
		for j := range items[i].Thread {
			if items[i].Thread[j].IsLeo || items[i].Thread[j].UserID == 0 {
				continue
			}
			u := items[i].Thread[j].UserID
			items[i].Thread[j].AuthorPhotoURL = packFeedResolveAuthorPhoto(m[u], publicBase, u, initDataRaw)
		}
	}
	return items
}

// WritePackMemberAvatarHTTP — GET-аватар участника стаи. Байты всегда отдаём сами
// (без 302 на t.me): редирект ломает картинку в WebView из‑за Referer и не даёт
// откатиться на Bot API, если CDN-ссылка протухла.
func (b *Bot) WritePackMemberAvatarHTTP(w http.ResponseWriter, r *http.Request, viewerUserID int64, subjectUserID int64, initD initdata.InitData) error {
	if b == nil || b.api == nil {
		return errors.New("bot unavailable")
	}
	if subjectUserID <= 0 {
		return errors.New("invalid user_id")
	}
	if err := b.PackFeedAssertViewerAccess(viewerUserID, initD); err != nil {
		return err
	}
	chatID := b.config.MonetizedChatID
	if chatID == 0 {
		return ErrPackFeedForbidden
	}

	if err := b.streamTelegramUserProfilePhoto(w, r, subjectUserID); err == nil {
		return nil
	}
	if err := b.streamTelegramChatPhoto(w, r, subjectUserID); err == nil {
		return nil
	}

	if prof, err := b.db.GetMiniappUserProfile(subjectUserID, chatID); err == nil && packFeedTelegramPhotoURLIsSafePublic(prof.TelegramPhotoURL) {
		if ferr := streamPublicImageURL(w, r, strings.TrimSpace(prof.TelegramPhotoURL)); ferr == nil {
			return nil
		} else {
			b.logger.Warnf("miniapp avatar stored url user=%d: %v", subjectUserID, ferr)
		}
	} else if err != nil && !errors.Is(err, sql.ErrNoRows) {
		b.logger.Warnf("miniapp avatar profile load user=%d: %v", subjectUserID, err)
	}
	return ErrPackMemberAvatarNotFound
}

func (b *Bot) streamTelegramUserProfilePhoto(w http.ResponseWriter, r *http.Request, subjectUserID int64) error {
	cfg := tgbotapi.NewUserProfilePhotos(subjectUserID)
	cfg.Limit = 1
	photos, err := b.api.GetUserProfilePhotos(cfg)
	if err != nil {
		b.logger.Warnf("miniapp GetUserProfilePhotos user=%d: %v", subjectUserID, err)
		return ErrPackMemberAvatarNotFound
	}
	if photos.TotalCount == 0 || len(photos.Photos) == 0 || len(photos.Photos[0]) == 0 {
		return ErrPackMemberAvatarNotFound
	}
	row := photos.Photos[0]
	sz := row[len(row)-1]
	return b.streamTelegramFileID(w, r, sz.FileID)
}

func (b *Bot) streamTelegramChatPhoto(w http.ResponseWriter, r *http.Request, subjectUserID int64) error {
	ch, err := b.api.GetChat(tgbotapi.ChatInfoConfig{ChatConfig: tgbotapi.ChatConfig{ChatID: subjectUserID}})
	if err != nil {
		b.logger.Warnf("miniapp GetChat avatar user=%d: %v", subjectUserID, err)
		return ErrPackMemberAvatarNotFound
	}
	if ch.Photo == nil {
		return ErrPackMemberAvatarNotFound
	}
	fileID := strings.TrimSpace(ch.Photo.BigFileID)
	if fileID == "" {
		fileID = strings.TrimSpace(ch.Photo.SmallFileID)
	}
	if fileID == "" {
		return ErrPackMemberAvatarNotFound
	}
	return b.streamTelegramFileID(w, r, fileID)
}

func (b *Bot) streamTelegramFileID(w http.ResponseWriter, r *http.Request, fileID string) error {
	fileID = strings.TrimSpace(fileID)
	if fileID == "" {
		return ErrPackMemberAvatarNotFound
	}
	file, err := b.api.GetFile(tgbotapi.FileConfig{FileID: fileID})
	if err != nil || strings.TrimSpace(file.FilePath) == "" {
		b.logger.Warnf("miniapp GetFile avatar: %v", err)
		return ErrPackMemberAvatarNotFound
	}
	return streamPublicImageURL(w, r, file.Link(b.api.Token))
}

func streamPublicImageURL(w http.ResponseWriter, r *http.Request, link string) error {
	link = strings.TrimSpace(link)
	if link == "" {
		return ErrPackMemberAvatarNotFound
	}
	client := &http.Client{Timeout: 18 * time.Second}
	req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, link, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return ErrPackMemberAvatarNotFound
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" || ct == "application/octet-stream" {
		ct = "image/jpeg"
	}
	if !strings.HasPrefix(ct, "image/") {
		return ErrPackMemberAvatarNotFound
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=600")
	_, copyErr := io.Copy(w, resp.Body)
	return copyErr
}
