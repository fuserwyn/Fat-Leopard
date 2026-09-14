package bot

import (
	"context"
	"regexp"
	"strings"
	"time"

	"leo-bot/internal/ai"
	"leo-bot/internal/database"
)

const (
	releaseNotesPeriodDays = 14
	releaseNotesHour       = 10
	releaseNotesMinute     = 0
	releaseNotesTick       = 1 * time.Minute
)

var releaseNotesInternalPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(ms_tracker|cursor[\s-]?агент|зомби|конвейер|pipeline|планировщик.*трекер|tracker/notify|tracker_agent)`),
	regexp.MustCompile(`(?i)(telegram[\s-]?уведомлен|уведомлен.*админ|dm.*админ|личк.*админ)`),
	regexp.MustCompile(`(?i)(ревью|тест|сборк).{0,40}(трекер|агент|ms_leo\s+internal/bot/tracker)`),
	regexp.MustCompile(`(?i)(зависш|перезапуск.*агент|ожид.*очеред)`),
}

var releaseNotesUserFacingHints = []string{
	"miniapp", "мини-апп", "лента", "профиль", "чат стаи", "трениров",
	"реакц", "упомин", "@", "feed", "profile", "pack group", "общий чат",
	"стрик", "кубк", "опрос", "фото", "коммент", "поддержк", "мудрость",
	"доск", "аппрув", "кнопк",
}

// trackerTaskShippedToProd — задача доехала до main (см. trackerTaskShippedToStand + финальный статус).
func trackerTaskShippedToProd(t database.TrackerTask) bool {
	if t.Status != "done" || t.DevColumn != trackerColDone {
		return false
	}
	if trackerTaskShippedToStand(t) {
		return true
	}
	low := strings.ToLower(t.Result)
	return TrackerNotifyIsFullyShipped(t.Result) ||
		(strings.Contains(low, "выехал") && strings.Contains(low, "main"))
}

// trackerTaskAffectsUserExperience — грубый пре-фильтр: только то, что может касаться мини-аппа.
func trackerTaskAffectsUserExperience(t database.TrackerTask) bool {
	text := strings.ToLower(strings.TrimSpace(t.Prompt + "\n" + trackerDoneExecutionSummary(t)))
	if text == "" {
		return false
	}
	for _, re := range releaseNotesInternalPatterns {
		if re.MatchString(text) {
			return false
		}
	}
	for _, hint := range releaseNotesUserFacingHints {
		if strings.Contains(text, hint) {
			return true
		}
	}
	return false
}

func collectReleaseNoteFeatures(tasks []database.TrackerTask) []ai.ReleaseNoteFeature {
	out := make([]ai.ReleaseNoteFeature, 0, len(tasks))
	seen := map[int]bool{}
	for _, t := range tasks {
		if !trackerTaskShippedToProd(t) || !trackerTaskAffectsUserExperience(t) {
			continue
		}
		if t.Num > 0 && seen[t.Num] {
			continue
		}
		summary := trackerDoneExecutionSummary(t)
		if summary == "" {
			summary = trackerTaskTitle(t.Prompt)
		}
		summary = strings.TrimSpace(summary)
		if summary == "" {
			continue
		}
		if t.Num > 0 {
			seen[t.Num] = true
		}
		out = append(out, ai.ReleaseNoteFeature{Num: t.Num, Summary: summary})
	}
	return out
}

func releaseNotesShouldRunToday(now time.Time, lastPeriodEnd string) bool {
	if now.Weekday() != time.Monday {
		return false
	}
	if lastPeriodEnd == "" {
		return true
	}
	last, err := time.Parse("2006-01-02", lastPeriodEnd)
	if err != nil {
		return true
	}
	return now.Sub(last) >= releaseNotesPeriodDays*24*time.Hour
}

func releaseNotesPeriodEndDate(now time.Time) string {
	return now.Format("2006-01-02")
}

// startReleaseNotesScheduler — раз в две недели (понедельник 10:00 МСК) Лео публикует Release Notes.
func (b *Bot) startReleaseNotesScheduler(ctx context.Context) {
	if b == nil || b.aiClient == nil {
		if b != nil {
			b.logger.Warn("AI client not available, release notes scheduler disabled")
		}
		return
	}
	if b.config == nil || b.config.MonetizedChatID == 0 {
		b.logger.Info("release notes scheduler: skipped, MONETIZED_CHAT_ID не настроен")
		return
	}
	loc := moscowLocation()
	ticker := time.NewTicker(releaseNotesTick)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			now := time.Now().In(loc)
			if now.Hour() != releaseNotesHour || now.Minute() != releaseNotesMinute {
				continue
			}
			last, err := b.db.GetLatestReleaseNotesPeriodEnd()
			if err != nil {
				b.logger.Warnf("release notes: last period: %v", err)
				continue
			}
			if !releaseNotesShouldRunToday(now, last) {
				continue
			}
			periodEnd := releaseNotesPeriodEndDate(now)
			ok, err := b.db.HasReleaseNotesLog(periodEnd)
			if err != nil {
				b.logger.Warnf("release notes: has log: %v", err)
				continue
			}
			if ok {
				continue
			}
			b.logger.Infof("release notes: generating for period ending %s", periodEnd)
			b.generateAndPublishReleaseNotes(now, periodEnd)
		}
	}
}

func (b *Bot) generateAndPublishReleaseNotes(now time.Time, periodEnd string) {
	if b == nil || b.db == nil || b.aiClient == nil || b.config == nil {
		return
	}
	since := now.AddDate(0, 0, -releaseNotesPeriodDays)
	tasks, err := b.db.ListShippedTrackerTasksSince(since)
	if err != nil {
		b.logger.Warnf("release notes: list tasks: %v", err)
		return
	}
	features := collectReleaseNoteFeatures(tasks)
	if len(features) == 0 {
		b.logger.Info("release notes: no user-facing features, skip")
		return
	}
	text, err := b.aiClient.GenerateReleaseNotes(features)
	if err != nil {
		b.logger.Warnf("release notes: generate: %v", err)
		return
	}
	text = strings.TrimSpace(text)
	if text == "" || strings.EqualFold(text, "SKIP") {
		b.logger.Info("release notes: model skipped empty period")
		return
	}
	if _, err := b.publishAdminPackFeedPost(0, adminPostAuthorLeo, text); err != nil {
		b.logger.Warnf("release notes: publish: %v", err)
		return
	}
	if err := b.db.SaveReleaseNotesLog(periodEnd, text); err != nil {
		b.logger.Warnf("release notes: save log: %v", err)
	}
	b.logger.Infof("release notes published for period %s (%d features)", periodEnd, len(features))
}
