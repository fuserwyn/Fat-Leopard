package bot

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
	"unicode/utf8"

	"leo-bot/internal/database"
	"leo-bot/internal/domain"
	"leo-bot/internal/moderation"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Челленджи: участник берёт N дней подряд с тренировкой.
//
// День засчитывается первой тренировкой в календарный день участника (его локальная
// дата, как в calculateTrainingDayOutcome). Пропуск дня проваливает челлендж, если
// стрик в этот день сгорел; попытка спасения и больничный стрик не жгут — тогда и
// челлендж продолжается. Сгорел ли стрик, решают те же функции, что считают стрик:
// sickAdjustedLastTrainingDate и ComputeStreakDays, — своей арифметики тут нет.

const (
	challengeCustomMinDays = 3
	challengeCustomMaxDays = 365
	challengeTitleMaxRunes = 40
	// challengeCreatorDays — прошедший челлендж такой длины открывает создание своих.
	challengeCreatorDays = 100
	// challengeStartPrefix — параметр ссылки t.me/<бот>?start=ch-<код>.
	challengeStartPrefix  = "ch-"
	challengeCodeAlphabet = "abcdefghijkmnpqrstuvwxyz23456789"
	challengeCodeLen      = 8
	challengeSweepEvery   = time.Hour
	challengeDateLayout   = "2006-01-02"
)

// StandardChallengeLengths — длины стандартных челленджей (заводятся миграцией 88).
var StandardChallengeLengths = []int{7, 14, 30, 60, 90, 100}

var (
	ErrChallengeNotFound        = errors.New("challenge_not_found")
	ErrChallengeAlreadyActive   = errors.New("challenge_already_active")
	ErrChallengeNotActive       = errors.New("challenge_not_active")
	ErrChallengeCreateForbidden = errors.New("challenge_create_forbidden")
	ErrChallengeBadLength       = errors.New("challenge_bad_length")
	ErrChallengeBadTitle        = errors.New("challenge_bad_title")
)

// ChallengeView — челлендж для мини-аппа.
type ChallengeView struct {
	Code       string `json:"code"`
	Title      string `json:"title"`
	LengthDays int    `json:"length_days"`
	Custom     bool   `json:"custom"`
	Link       string `json:"link"`
}

// ChallengeParticipationView — участие в челлендже.
type ChallengeParticipationView struct {
	Challenge       ChallengeView `json:"challenge"`
	Status          string        `json:"status"`
	StartDate       string        `json:"start_date"`
	DaysDone        int           `json:"days_done"`
	LastCountedDate string        `json:"last_counted_date,omitempty"`
	FinishedAt      string        `json:"finished_at,omitempty"`
	// AtRisk — день пропущен и стрик сгорел, но его ещё можно спасти попыткой:
	// тогда челлендж продолжится.
	AtRisk bool `json:"at_risk"`
}

// ChallengesState — экран челленджей: что можно взять, что идёт, что было.
type ChallengesState struct {
	Standard  []ChallengeView              `json:"standard"`
	Mine      []ChallengeView              `json:"mine"`
	Active    *ChallengeParticipationView  `json:"active"`
	History   []ChallengeParticipationView `json:"history"`
	Invite    *ChallengeView               `json:"invite"`
	CanCreate bool                         `json:"can_create"`
}

type challengeStep int

const (
	challengeStepKeep challengeStep = iota
	challengeStepCount
	challengeStepFail
)

func challengeDaysBetween(from, to string) int {
	a, err1 := time.Parse(challengeDateLayout, strings.TrimSpace(from))
	b, err2 := time.Parse(challengeDateLayout, strings.TrimSpace(to))
	if err1 != nil || err2 != nil {
		return 0
	}
	return int(b.Sub(a).Hours()/24 + 0.5)
}

// streakContinues — по ответу ComputeStreakDays: тренировка в этот день продолжает стрик
// (он не сгорел) или это повтор того же дня.
func streakContinues(prevStreak, newStreak int, sameDay bool) bool {
	return sameDay || (prevStreak > 0 && newStreak == prevStreak+1)
}

// challengeStepOnTraining — что делать с активным челленджем, когда участник записал
// тренировку в день today. streakContinued — стрик этой тренировкой продолжился.
// Первый засчитанный день — первая тренировка после принятия челленджа.
func challengeStepOnTraining(lastCounted, today string, streakContinued bool) challengeStep {
	lastCounted = strings.TrimSpace(lastCounted)
	if lastCounted == "" {
		return challengeStepCount
	}
	gap := challengeDaysBetween(lastCounted, today)
	switch {
	case gap <= 0:
		return challengeStepKeep
	case gap == 1, streakContinued:
		return challengeStepCount
	}
	return challengeStepFail
}

// challengeStepIdle — проверка без тренировки (открыт экран или ежечасный обход).
// streakAlive — стрик не сгорел; saveOpen — сгорел, но попыткой его ещё можно спасти.
func challengeStepIdle(lastCounted, today string, streakAlive, saveOpen bool) challengeStep {
	lastCounted = strings.TrimSpace(lastCounted)
	if lastCounted == "" || challengeDaysBetween(lastCounted, today) <= 1 || streakAlive || saveOpen {
		return challengeStepKeep
	}
	return challengeStepFail
}

// challengeEffectiveLastTraining — дата последней тренировки с учётом больничного
// (дни болезни стрик не жгут).
func (b *Bot) challengeEffectiveLastTraining(ml *domain.MessageLog, today string) *string {
	if ml == nil || ml.LastTrainingDate == nil {
		return nil
	}
	last := strings.TrimSpace(*ml.LastTrainingDate)
	if ss, se, ok := b.sickWindowLocalDates(ml, today); ok {
		last = sickAdjustedLastTrainingDate(last, today, ss, se)
	}
	return &last
}

// challengeIdleStep решает судьбу активного челленджа без новой тренировки.
// atRisk — день пропущен, но стрик ещё можно спасти попыткой.
func (b *Bot) challengeIdleStep(p *database.ChallengeParticipant) (step challengeStep, atRisk bool) {
	if p == nil || strings.TrimSpace(p.LastCountedDate) == "" {
		return challengeStepKeep, false
	}
	ml, err := b.db.GetMessageLog(p.UserID, p.PackChatID)
	if err != nil || ml == nil {
		return challengeStepKeep, false
	}
	localNow := b.getUserLocalNow(ml.TimezoneOffsetFromMoscow)
	today := localNow.Format(challengeDateLayout)
	if challengeDaysBetween(p.LastCountedDate, today) <= 1 {
		return challengeStepKeep, false
	}
	last := b.challengeEffectiveLastTraining(ml, today)
	newStreak, sameDay := ComputeStreakDays(last, ml.StreakDays, localNow)
	alive := streakContinues(ml.StreakDays, newStreak, sameDay)
	saveOpen := false
	if !alive && last != nil {
		avail := b.GetMiniappProfileStatsForAPI(p.UserID, p.PackChatID).StreakSaveAttemptsAvail
		saveOpen = StreakSaveWindowError(challengeDaysBetween(*last, today), avail) == ""
	}
	step = challengeStepIdle(p.LastCountedDate, today, alive, saveOpen)
	return step, step == challengeStepKeep && saveOpen
}

// applyChallengeStep пишет шаг в базу, шлёт событие и сообщение участнику.
func (b *Bot) applyChallengeStep(p *database.ChallengeParticipant, step challengeStep, today string) {
	if p == nil {
		return
	}
	switch step {
	case challengeStepCount:
		days := p.DaysDone + 1
		status := database.ChallengeStatusActive
		if days >= p.Challenge.LengthDays {
			status = database.ChallengeStatusCompleted
		}
		ok, err := b.db.UpdateChallengeProgress(p.ID, days, today, status)
		if err != nil {
			b.logger.Warnf("challenge count user=%d: %v", p.UserID, err)
			return
		}
		if !ok {
			return
		}
		p.DaysDone, p.LastCountedDate, p.Status = days, today, status
		if status == database.ChallengeStatusCompleted {
			b.trackChallenge(database.EventChallengeCompleted, p, nil)
			text := fmt.Sprintf("🏆 Челлендж «%s» пройден: %d %s подряд!", p.Challenge.Title, days, daysWordForm(days))
			if p.Challenge.LengthDays >= challengeCreatorDays {
				text += "\n\nТеперь ты можешь создавать свои челленджи в мини-аппе."
			}
			b.notifyUserTextByID(p.UserID, p.UserID, text, "", 0)
		}
	case challengeStepFail:
		ok, err := b.db.UpdateChallengeProgress(p.ID, p.DaysDone, p.LastCountedDate, database.ChallengeStatusFailed)
		if err != nil {
			b.logger.Warnf("challenge fail user=%d: %v", p.UserID, err)
			return
		}
		if !ok {
			return
		}
		p.Status = database.ChallengeStatusFailed
		b.trackChallenge(database.EventChallengeFailed, p, map[string]any{"reason": "missed_day"})
		b.notifyUserTextByID(p.UserID, p.UserID, fmt.Sprintf(
			"Челлендж «%s» прерван: пропущен день. Засчитано %d из %d. Новый можно взять сразу — в мини-аппе.",
			p.Challenge.Title, p.DaysDone, p.Challenge.LengthDays), "", 0)
	}
}

func (b *Bot) trackChallenge(name string, p *database.ChallengeParticipant, extra map[string]any) {
	payload := map[string]any{
		"challenge_code": p.Challenge.Code,
		"length_days":    p.Challenge.LengthDays,
		"days_done":      p.DaysDone,
		"custom":         p.Challenge.AuthorUserID != 0,
	}
	for k, v := range extra {
		payload[k] = v
	}
	b.db.TrackEvent(database.AnalyticsEvent{Name: name, UserID: p.UserID, TelegramID: p.UserID, Payload: payload})
}

// challengeOnTraining — участник записал тренировку в свой день today.
// streakContinued — стрик ею продолжился (см. streakContinues).
func (b *Bot) challengeOnTraining(userID int64, today string, streakContinued bool) {
	if b == nil || b.db == nil || userID == 0 {
		return
	}
	p, err := b.db.GetActiveChallengeParticipant(userID)
	if err != nil {
		b.logger.Warnf("challenge on training user=%d: %v", userID, err)
		return
	}
	if p == nil {
		return
	}
	b.applyChallengeStep(p, challengeStepOnTraining(p.LastCountedDate, today, streakContinued), today)
}

// refreshActiveChallenge досчитывает пропуски и возвращает активное участие (nil — нет).
func (b *Bot) refreshActiveChallenge(userID int64) (*database.ChallengeParticipant, bool, error) {
	p, err := b.db.GetActiveChallengeParticipant(userID)
	if err != nil || p == nil {
		return nil, false, err
	}
	step, atRisk := b.challengeIdleStep(p)
	if step == challengeStepFail {
		b.applyChallengeStep(p, step, "")
		if p.Status != database.ChallengeStatusActive {
			return nil, false, nil
		}
	}
	return p, atRisk, nil
}

// sweepChallenges проваливает активные челленджи с пропущенным днём. Возвращает,
// сколько провалено.
func (b *Bot) sweepChallenges() int {
	if b == nil || b.db == nil {
		return 0
	}
	list, err := b.db.ListActiveChallengeParticipants()
	if err != nil {
		b.logger.Warnf("challenge sweep: %v", err)
		return 0
	}
	failed := 0
	for i := range list {
		p := &list[i]
		if step, _ := b.challengeIdleStep(p); step == challengeStepFail {
			b.applyChallengeStep(p, step, "")
			if p.Status == database.ChallengeStatusFailed {
				failed++
			}
		}
	}
	return failed
}

func (b *Bot) startChallengeSweepScheduler(ctx context.Context) {
	t := time.NewTicker(challengeSweepEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			b.sweepChallenges()
		}
	}
}

// botUsername — имя бота из Telegram (getMe), для ссылок t.me/<бот>.
func (b *Bot) botUsername() string {
	if b == nil || b.api == nil {
		return ""
	}
	if name := b.api.Self.UserName; name != "" {
		return name
	}
	me, err := b.api.GetMe()
	if err != nil {
		b.logger.Warnf("getMe: %v", err)
		return ""
	}
	return me.UserName
}

// challengeLink — ссылка t.me/<бот>?start=ch-<код>, по которой друг приходит с челленджем.
func challengeLink(botName, code string) string {
	if botName == "" || code == "" {
		return ""
	}
	return "https://t.me/" + botName + "?start=" + challengeStartPrefix + code
}

func (b *Bot) challengeView(c database.Challenge, botName string) ChallengeView {
	return ChallengeView{
		Code: c.Code, Title: c.Title, LengthDays: c.LengthDays,
		Custom: c.AuthorUserID != 0, Link: challengeLink(botName, c.Code),
	}
}

func (b *Bot) participationView(p database.ChallengeParticipant, botName string, atRisk bool) ChallengeParticipationView {
	v := ChallengeParticipationView{
		Challenge:       b.challengeView(p.Challenge, botName),
		Status:          p.Status,
		StartDate:       p.StartDate,
		DaysDone:        p.DaysDone,
		LastCountedDate: p.LastCountedDate,
		AtRisk:          atRisk,
	}
	if p.FinishedAt != nil {
		v.FinishedAt = p.FinishedAt.UTC().Format(time.RFC3339)
	}
	return v
}

func (b *Bot) assertChallengeViewer(viewerUserID int64, initD initdata.InitData) error {
	if err := b.AssertMiniAppPackChatAligns(initD); err != nil {
		return err
	}
	return b.assertPackFeedSocialViewer(viewerUserID)
}

// GetChallengesStateForViewer — экран челленджей участника.
func (b *Bot) GetChallengesStateForViewer(viewerUserID int64, initD initdata.InitData) (ChallengesState, error) {
	out := ChallengesState{Standard: []ChallengeView{}, Mine: []ChallengeView{}, History: []ChallengeParticipationView{}}
	if err := b.assertChallengeViewer(viewerUserID, initD); err != nil {
		return out, err
	}
	botName := b.botUsername()
	active, atRisk, err := b.refreshActiveChallenge(viewerUserID)
	if err != nil {
		return out, err
	}
	if active != nil {
		v := b.participationView(*active, botName, atRisk)
		out.Active = &v
	}
	std, err := b.db.ListStandardChallenges()
	if err != nil {
		return out, err
	}
	for _, c := range std {
		out.Standard = append(out.Standard, b.challengeView(c, botName))
	}
	mine, err := b.db.ListChallengesByAuthor(viewerUserID)
	if err != nil {
		return out, err
	}
	for _, c := range mine {
		out.Mine = append(out.Mine, b.challengeView(c, botName))
	}
	history, err := b.db.ListChallengeHistory(viewerUserID, 20)
	if err != nil {
		return out, err
	}
	for _, p := range history {
		if p.Status != database.ChallengeStatusActive {
			out.History = append(out.History, b.participationView(p, botName, false))
		}
	}
	invite, err := b.db.GetChallengeInvite(viewerUserID)
	if err != nil {
		return out, err
	}
	if invite != nil {
		if active != nil && active.ChallengeID == invite.ID {
			_ = b.db.ClearChallengeInvite(viewerUserID)
		} else {
			v := b.challengeView(*invite, botName)
			out.Invite = &v
		}
	}
	out.CanCreate, err = b.db.HasCompletedChallengeOfLength(viewerUserID, challengeCreatorDays)
	if err != nil {
		return out, err
	}
	return out, nil
}

// AcceptChallengeForViewer — участник принимает челлендж (по коду). Если сегодня он уже
// тренировался, первый день засчитывается сразу.
func (b *Bot) AcceptChallengeForViewer(viewerUserID int64, initD initdata.InitData, code string) (ChallengeParticipationView, error) {
	if err := b.assertChallengeViewer(viewerUserID, initD); err != nil {
		return ChallengeParticipationView{}, err
	}
	c, err := b.db.GetChallengeByCode(strings.ToLower(strings.TrimSpace(code)))
	if err != nil {
		return ChallengeParticipationView{}, err
	}
	if c == nil {
		return ChallengeParticipationView{}, ErrChallengeNotFound
	}
	// Текущий мог провалиться, пока экран не открывали, — тогда новый брать можно.
	active, _, err := b.refreshActiveChallenge(viewerUserID)
	if err != nil {
		return ChallengeParticipationView{}, err
	}
	if active != nil {
		return ChallengeParticipationView{}, ErrChallengeAlreadyActive
	}
	packChatID := b.config.MonetizedChatID
	offset := 0
	ml, _ := b.db.GetMessageLog(viewerUserID, packChatID)
	if ml != nil {
		offset = ml.TimezoneOffsetFromMoscow
	}
	today := b.getUserLocalDate(offset)
	days, counted := 0, ""
	if ml != nil && ml.LastTrainingDate != nil && strings.TrimSpace(*ml.LastTrainingDate) == today {
		days, counted = 1, today
	}
	p, err := b.db.StartChallenge(c.ID, viewerUserID, packChatID, today, days, counted)
	if errors.Is(err, database.ErrChallengeAlreadyActive) {
		return ChallengeParticipationView{}, ErrChallengeAlreadyActive
	}
	if err != nil {
		return ChallengeParticipationView{}, err
	}
	if err := b.db.ClearChallengeInvite(viewerUserID); err != nil {
		b.logger.Warnf("clear challenge invite user=%d: %v", viewerUserID, err)
	}
	b.trackChallenge(database.EventChallengeJoined, p, nil)
	return b.participationView(*p, b.botUsername(), false), nil
}

// LeaveChallengeForViewer — участник сам бросает активный челлендж (статус failed).
func (b *Bot) LeaveChallengeForViewer(viewerUserID int64, initD initdata.InitData) error {
	if err := b.assertChallengeViewer(viewerUserID, initD); err != nil {
		return err
	}
	p, err := b.db.GetActiveChallengeParticipant(viewerUserID)
	if err != nil {
		return err
	}
	if p == nil {
		return ErrChallengeNotActive
	}
	ok, err := b.db.UpdateChallengeProgress(p.ID, p.DaysDone, p.LastCountedDate, database.ChallengeStatusFailed)
	if err != nil {
		return err
	}
	if !ok {
		return ErrChallengeNotActive
	}
	b.trackChallenge(database.EventChallengeFailed, p, map[string]any{"reason": "left"})
	return nil
}

// DismissChallengeInviteForViewer — участник отказался от челленджа из ссылки.
func (b *Bot) DismissChallengeInviteForViewer(viewerUserID int64, initD initdata.InitData) error {
	if err := b.assertChallengeViewer(viewerUserID, initD); err != nil {
		return err
	}
	return b.db.ClearChallengeInvite(viewerUserID)
}

// normalizeChallengeTitle — название в одну строку; ok=false — пустое или длиннее 40 символов.
func normalizeChallengeTitle(title string) (string, bool) {
	title = strings.Join(strings.Fields(title), " ")
	n := utf8.RuneCountInString(title)
	return title, n > 0 && n <= challengeTitleMaxRunes
}

func newChallengeCode() (string, error) {
	var sb strings.Builder
	alphabet := big.NewInt(int64(len(challengeCodeAlphabet)))
	for i := 0; i < challengeCodeLen; i++ {
		n, err := rand.Int(rand.Reader, alphabet)
		if err != nil {
			return "", err
		}
		sb.WriteByte(challengeCodeAlphabet[n.Int64()])
	}
	return sb.String(), nil
}

// CreateChallengeForViewer — свой челлендж. Доступен тем, кто прошёл 100-дневный;
// длина 3–365 дней, название до 40 символов и та же модерация, что у комментариев.
func (b *Bot) CreateChallengeForViewer(viewerUserID int64, initD initdata.InitData, title string, lengthDays int) (ChallengeView, error) {
	if err := b.assertChallengeViewer(viewerUserID, initD); err != nil {
		return ChallengeView{}, err
	}
	can, err := b.db.HasCompletedChallengeOfLength(viewerUserID, challengeCreatorDays)
	if err != nil {
		return ChallengeView{}, err
	}
	if !can {
		return ChallengeView{}, ErrChallengeCreateForbidden
	}
	if lengthDays < challengeCustomMinDays || lengthDays > challengeCustomMaxDays {
		return ChallengeView{}, ErrChallengeBadLength
	}
	title, ok := normalizeChallengeTitle(title)
	if !ok {
		return ChallengeView{}, ErrChallengeBadTitle
	}
	if _, err := b.enforceUGC(title, moderation.SurfaceFeedComment, viewerUserID); err != nil {
		return ChallengeView{}, err
	}
	var c *database.Challenge
	for attempt := 0; attempt < 5; attempt++ {
		code, err := newChallengeCode()
		if err != nil {
			return ChallengeView{}, err
		}
		c, err = b.db.CreateChallenge(code, title, lengthDays, viewerUserID)
		if errors.Is(err, database.ErrChallengeCodeTaken) {
			continue
		}
		if err != nil {
			return ChallengeView{}, err
		}
		break
	}
	if c == nil {
		return ChallengeView{}, database.ErrChallengeCodeTaken
	}
	b.db.TrackEvent(database.AnalyticsEvent{
		Name: database.EventChallengeCreated, UserID: viewerUserID, TelegramID: viewerUserID,
		Payload: map[string]any{"challenge_code": c.Code, "length_days": c.LengthDays},
	})
	return b.challengeView(*c, b.botUsername()), nil
}

// parseChallengeStartPayload — код челленджа из параметра /start вида ch-<код>; пусто — не он.
func parseChallengeStartPayload(arg string) string {
	arg = strings.TrimSpace(arg)
	if !strings.HasPrefix(arg, challengeStartPrefix) {
		return ""
	}
	code := strings.ToLower(arg[len(challengeStartPrefix):])
	if code == "" || len(code) > 32 {
		return ""
	}
	for _, r := range code {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return ""
		}
	}
	return code
}

// rememberChallengeInviteFromStart запоминает челлендж из ссылки за пользователем:
// мини-апп предложит его принять. Возвращает челлендж или nil.
func (b *Bot) rememberChallengeInviteFromStart(userID int64, arg string) *database.Challenge {
	code := parseChallengeStartPayload(arg)
	if code == "" || b == nil || b.db == nil || userID == 0 {
		return nil
	}
	c, err := b.db.GetChallengeByCode(code)
	if err != nil {
		b.logger.Warnf("challenge from start user=%d code=%s: %v", userID, code, err)
		return nil
	}
	if c == nil {
		return nil
	}
	if err := b.db.SetChallengeInvite(userID, c.ID); err != nil {
		b.logger.Warnf("remember challenge invite user=%d: %v", userID, err)
		return nil
	}
	return c
}
