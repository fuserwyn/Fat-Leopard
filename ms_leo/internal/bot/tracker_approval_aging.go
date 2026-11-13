package bot

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"leo-bot/internal/database"
)

// Что происходит с карточкой, которая долго ждёт аппрувов:
//   - набран один голос из двух и карточка на аппруве больше суток — голосует
//     Лео: Claude (через Claude Agent SDK в ms_tracker) оценивает задачу и,
//     если она стоящая, ставит второй аппрув;
//   - за пять дней не набрано ни одного голоса — карточка отменяется сама.
const (
	trackerLeoVoteAfter        = 24 * time.Hour
	trackerApprovalExpireAfter = 5 * 24 * time.Hour
	// Если Claude не ответил, не дёргаем его на каждом проходе (раз в 5 минут).
	trackerLeoVoteRetryAfter = time.Hour

	trackerApprovalsResetStep  = "Approvals reset"
	trackerApprovalExpiredStep = "Auto-cancelled: 5 days without approvals"
)

const trackerLeoVotePrompt = `You are Leo — leopard coach and co-owner of Fat Leopard (Telegram Mini App:
pack, workouts, streaks, cups). On the task board a card awaits a second vote; an admin
already cast the first. Decide whether the task should be taken on.

Vote yes if the task is clear, doable in one code change, and helps pack members
or the team. Vote no if the wording is vague, harms users, breaks pack rules, needs
decisions a human must make (money, data deletion, legal), or isn't a dev task.

Reply with strict JSON only, no wrapper text:
{"approve": true|false, "reason": "one short sentence in English, no emoji"}`

// trackerApproverIDs — кто уже одобрил карточку, в порядке голосования.
// Всегда массив (не null), чтобы доске не проверять на пустоту.
func trackerApproverIDs(t database.TrackerTask) []int64 {
	out := make([]int64, 0, len(t.Approvals))
	return append(out, t.Approvals...)
}

// trackerApprovalWaitingSince — с какого момента карточка ждёт аппрувов.
func trackerApprovalWaitingSince(t database.TrackerTask) time.Time {
	if t.HasApprovalNotified && !t.ApprovalNotifiedAt.IsZero() {
		return t.ApprovalNotifiedAt
	}
	return t.CreatedAt
}

// trackerApprovalRound — номер круга голосования: растёт, когда описание
// меняют и аппрувы сбрасываются. В каждом круге Лео голосует один раз.
func trackerApprovalRound(t database.TrackerTask) int {
	round := 1
	for _, s := range t.Steps {
		trim := strings.TrimSpace(s)
		if strings.HasPrefix(trim, trackerApprovalsResetStep) ||
			strings.HasPrefix(trim, "Аппрувы сброшены") {
			round++
		}
	}
	return round
}

func trackerLeoVoteStep(round int) string {
	return fmt.Sprintf("Leo voted (round %d)", round)
}

func trackerHasStep(t database.TrackerTask, step string) bool {
	for _, s := range t.Steps {
		if strings.TrimSpace(s) == step {
			return true
		}
	}
	return false
}

func trackerAuthoredByLeo(t database.TrackerTask) bool {
	return t.Kind == "leo_task" || (t.HasAuthor && t.AuthorID == database.TrackerLeoAuthorID)
}

// trackerLeoVoteDue — пора ли Лео голосовать по этой карточке.
func trackerLeoVoteDue(t database.TrackerTask, now time.Time) bool {
	if !t.NeedsApproval || t.DevColumn != trackerColApprove {
		return false
	}
	// Ровно один чужой голос; свои задачи Лео не одобряет — как и любой автор.
	if len(t.Approvals) != trackerApprovalRequired-1 || trackerHasApproval(t, database.TrackerLeoAuthorID) || trackerAuthoredByLeo(t) {
		return false
	}
	if trackerHasStep(t, trackerLeoVoteStep(trackerApprovalRound(t))) {
		return false
	}
	since := trackerApprovalWaitingSince(t)
	return !since.IsZero() && !since.Add(trackerLeoVoteAfter).After(now)
}

// trackerApprovalExpired — за пять дней карточку никто не одобрил.
func trackerApprovalExpired(t database.TrackerTask, now time.Time) bool {
	if !t.NeedsApproval || t.DevColumn != trackerColApprove || len(t.Approvals) > 0 {
		return false
	}
	since := trackerApprovalWaitingSince(t)
	return !since.IsZero() && !since.Add(trackerApprovalExpireAfter).After(now)
}

// parseLeoVote разбирает ответ Claude. ok=false — ответ не разобрался.
func parseLeoVote(raw string) (approve bool, reason string, ok bool) {
	block := leoJSONBlock.FindString(raw)
	if block == "" {
		return false, "", false
	}
	var parsed struct {
		Approve *bool  `json:"approve"`
		Reason  string `json:"reason"`
	}
	if err := json.Unmarshal([]byte(block), &parsed); err != nil || parsed.Approve == nil {
		return false, "", false
	}
	reason = strings.TrimSpace(parsed.Reason)
	if r := []rune(reason); len(r) > 200 {
		reason = string(r[:200]) + "…"
	}
	return *parsed.Approve, reason, true
}

var trackerLeoVoteRetry sync.Map // id задачи → когда можно снова спросить Claude

func (b *Bot) runTrackerApprovalAgingSweep(now time.Time) {
	if b == nil || b.db == nil {
		return
	}
	tasks, err := b.db.ListTrackerTasksOnApproval()
	if err != nil {
		if b.logger != nil {
			b.logger.Errorf("трекер: аппрувы — список: %v", err)
		}
		return
	}
	for _, t := range tasks {
		switch {
		case trackerApprovalExpired(t, now):
			b.expireTrackerApproval(t)
		case trackerLeoVoteDue(t, now):
			if until, ok := trackerLeoVoteRetry.Load(t.ID); ok && now.Before(until.(time.Time)) {
				continue
			}
			if err := b.leoVoteOnTrackerTask(t); err != nil {
				trackerLeoVoteRetry.Store(t.ID, now.Add(trackerLeoVoteRetryAfter))
				if b.logger != nil {
					b.logger.Warnf("трекер: голос Лео по #%d: %v", trackerDueNum(t), err)
				}
			}
		}
	}
}

// leoVoteOnTrackerTask — Лео оценивает карточку через Claude и голосует.
// Запасной модели здесь нет намеренно: голос — это решение, и принимать его
// должен Claude; не ответил — попробуем позже.
func (b *Bot) leoVoteOnTrackerTask(t database.TrackerTask) error {
	model := ""
	if b.config != nil {
		model = strings.TrimSpace(b.config.LeoTasksModel)
	}
	prompt := strings.TrimSpace(t.Prompt)
	if r := []rune(prompt); len(r) > 4000 {
		prompt = string(r[:4000]) + "…"
	}
	raw, err := b.trackerAskClaude(trackerLeoVotePrompt, "Задача на доске:\n\n"+prompt, model)
	if err != nil {
		return err
	}
	approve, reason, ok := parseLeoVote(raw)
	if !ok {
		return fmt.Errorf("ответ Claude не разобрался: %s", strings.TrimSpace(raw))
	}
	// Отметку ставим в базе атомарно: при деплое старый и новый экземпляр бота
	// живут вместе, и без неё Лео проголосовал бы дважды.
	claimed, err := b.db.ClaimTrackerStep(t.ID, trackerLeoVoteStep(trackerApprovalRound(t)))
	if err != nil || !claimed {
		return err
	}
	if !approve {
		step := "Leo voted no"
		if reason != "" {
			step += ": " + reason
		}
		_, err := b.db.ClaimTrackerStep(t.ID, step)
		return err
	}
	label := "Approval from Leo"
	if reason != "" {
		label += ": " + reason
	}
	_, err = b.applyTrackerApproval(t.ID, database.TrackerLeoAuthorID, label)
	return err
}

// expireTrackerApproval — карточка пять дней без голосов уходит в отменённые.
func (b *Bot) expireTrackerApproval(t database.TrackerTask) {
	claimed, err := b.db.ClaimTrackerStep(t.ID, trackerApprovalExpiredStep)
	if err != nil || !claimed {
		return
	}
	fresh, err := b.db.GetTrackerTask(t.ID)
	if err != nil || fresh.DevColumn != trackerColApprove || len(fresh.Approvals) > 0 {
		return
	}
	if err := applyTrackerColumn(&fresh, trackerColCanceled); err != nil {
		return
	}
	if err := b.db.SaveTrackerTask(fresh); err != nil {
		if b.logger != nil {
			b.logger.Warnf("трекер: не отменить просроченную #%d: %v", trackerDueNum(fresh), err)
		}
		return
	}
	if fresh.HasAuthor && fresh.AuthorID > 0 {
		_ = b.NotifyTrackerAuthor(fresh.AuthorID,
			fmt.Sprintf("⛔ %s auto-cancelled: no approvals in 5 days.", trackerNotifyHeading(fresh)))
	}
}
