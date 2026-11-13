package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Раздел «Аналитика» в admin-панели — читаемый вид воронок и метрик из
// analytics_BT_v1.md поверх таблицы events. Только чтение, без записи.

// analyticsPeriods — окна, которые admin переключает кнопками. 0 = всё время.
var analyticsPeriods = []struct {
	Days  int
	Label string
}{
	{0, "All"},
	{30, "30d"},
	{7, "7d"},
}

func analyticsPeriodLabel(days int) string {
	for _, p := range analyticsPeriods {
		if p.Days == days {
			return p.Label
		}
	}
	if days <= 0 {
		return "All"
	}
	return strconv.Itoa(days) + "d"
}

// analyticsPeriodRow — ряд переключателей периода для конкретного экрана.
// prefix — базовый callback (напр. "admin_an_f1_"), к нему дописывается days.
func analyticsPeriodRow(prefix string, current int) []tgbotapi.InlineKeyboardButton {
	row := make([]tgbotapi.InlineKeyboardButton, 0, len(analyticsPeriods))
	for _, p := range analyticsPeriods {
		label := p.Label
		if p.Days == current {
			label = "• " + label
		}
		row = append(row, tgbotapi.NewInlineKeyboardButtonData(label, prefix+strconv.Itoa(p.Days)))
	}
	return row
}

// analyticsPct — конверсия num/den в проценты для таблиц.
func analyticsPct(num, den int64) string {
	if den <= 0 {
		return "—"
	}
	return fmt.Sprintf("%.0f%%", float64(num)/float64(den)*100)
}

// showAdminAnalyticsMenu — корневое меню аналитики.
func (b *Bot) showAdminAnalyticsMenu(chatID int64) {
	var sb strings.Builder
	sb.WriteString("📈 Analytics Fat Leopard\n\n")
	sb.WriteString("Funnels and metrics from the spec over the events table.\n")
	if last, ok := b.db.AnalyticsLastEventAt(); ok {
		sb.WriteString("Last event: " + last.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04") + " (MSK)\n")
	} else {
		sb.WriteString("No events yet — data appears after users start acting.\n")
	}
	sb.WriteString("\nChoose a section:")

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("1️⃣ Funnel: bot→pay", "admin_an_f1_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("2️⃣ Funnel: activation", "admin_an_f2_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("3️⃣ Retention & streaks", "admin_an_f3_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⭐ KPI summary", "admin_an_kpi_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📡 Channels", "admin_an_channels_0"),
			tgbotapi.NewInlineKeyboardButtonData("🗂 All events", "admin_an_overview_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🐆 Not just sport", "admin_an_nonsport_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Admin panel", "admin_open"),
		),
	}
	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ReplyMarkup = &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: rows}
	b.api.Send(msg)
}

// analyticsBottomRows — ряд периодов + навигация назад для экранов аналитики.
func analyticsBottomRows(prefix string, days int) [][]tgbotapi.InlineKeyboardButton {
	return [][]tgbotapi.InlineKeyboardButton{
		analyticsPeriodRow(prefix, days),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ Analytics", "admin_analytics"),
		),
	}
}

// showAdminAnalyticsFunnel1 — воронка приобретения (§3).
func (b *Bot) showAdminAnalyticsFunnel1(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load funnel: "+err.Error()))
		return
	}

	stages := []struct {
		Event string
		Label string
	}{
		{database.EventBotStarted, "Bot start"},
		{database.EventPaywallViewed, "Paywall"},
		{database.EventPaymentMethodSelected, "Method pick"},
		{database.EventPaymentInitiated, "To checkout"},
		{database.EventPaymentCompleted, "Paid"},
		{database.EventMiniappOpened, "Opened mini app"},
	}

	tbl := newAdminTable([]string{"Stage", "Users", "Conv"}, []int{14, 6, 5})
	var prev int64 = -1
	for _, s := range stages {
		n := counts[s.Event]
		conv := "—"
		if prev >= 0 {
			conv = analyticsPct(n, prev)
		}
		tbl.addRow(s.Label, strconv.FormatInt(n, 10), conv)
		prev = n
	}

	started := counts[database.EventBotStarted]
	paid := counts[database.EventPaymentCompleted]
	paywall := counts[database.EventPaywallViewed]
	subtitle := fmt.Sprintf("Period: %s · conversion vs previous stage\n⭐ paywall→paid: %s · start→paid: %s",
		analyticsPeriodLabel(days), analyticsPct(paid, paywall), analyticsPct(paid, started))

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_f1_", days)}
	b.sendAdminHTMLPreTable(chatID, "1️⃣ Funnel: bot → pay", subtitle, tbl.render(), kb)
}

// showAdminAnalyticsFunnel2 — воронка активации (§4), включая kill-метрику.
func (b *Bot) showAdminAnalyticsFunnel2(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load funnel: "+err.Error()))
		return
	}

	stages := []struct {
		Event string
		Label string
	}{
		{database.EventMiniappOpened, "Opened mini app"},
		{database.EventWorkoutLogStarted, "Opened form"},
		{database.EventWorkoutLogged, "Logged workout"},
		{database.EventLeoCommentReceived, "Leo comment"},
	}

	tbl := newAdminTable([]string{"Stage", "Users", "Conv"}, []int{14, 6, 5})
	var prev int64 = -1
	for _, s := range stages {
		n := counts[s.Event]
		conv := "—"
		if prev >= 0 {
			conv = analyticsPct(n, prev)
		}
		tbl.addRow(s.Label, strconv.FormatInt(n, 10), conv)
		prev = n
	}

	logged := counts[database.EventWorkoutLogged]
	paid := counts[database.EventPaymentCompleted]
	kill := analyticsPct(logged, paid)
	subtitle := fmt.Sprintf("Period: %s\n⭐ kill metric (first workout/payment): %s\nTarget ≥35%% · kill <20%% (BRD)",
		analyticsPeriodLabel(days), kill)

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_f2_", days)}
	b.sendAdminHTMLPreTable(chatID, "2️⃣ Funnel: activation", subtitle, tbl.render(), kb)
}

// showAdminAnalyticsFunnel3 — retention и серии (§5).
func (b *Bot) showAdminAnalyticsFunnel3(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load retention: "+err.Error()))
		return
	}

	rows := []struct {
		Event string
		Label string
	}{
		{database.EventStreakIncremented, "Streak up"},
		{database.EventStreakAttemptUsed, "Attempt saved"},
		{database.EventStreakBroken, "Streak broke"},
		{database.EventLevelUp, "Level up"},
		{database.EventMilestoneAchieved, "Streak milestone"},
		{database.EventBurnWarningSent, "Burn alert"},
		{database.EventBurnRecovered, "Recovered after"},
		{database.EventSickLeaveStarted, "Sick leave on"},
		{database.EventSickLeaveEnded, "Sick leave off"},
		{database.EventAccountDeletedInactivity, "Removed (8d)"},
		{database.EventAccountReactivated, "Came back"},
		{database.EventChallengeJoined, "Joined challenge"},
		{database.EventChallengeCompleted, "Finished challenge"},
		{database.EventChallengeFailed, "Failed challenge"},
		{database.EventChallengeCreated, "Created challenge"},
		{database.EventJointTrainingInvite, "Invited to workout"},
	}

	tbl := newAdminTable([]string{"Event", "Users"}, []int{16, 6})
	for _, r := range rows {
		tbl.addRow(r.Label, strconv.FormatInt(counts[r.Event], 10))
	}

	burnRecovery := analyticsPct(counts[database.EventBurnRecovered], counts[database.EventBurnWarningSent])
	reactivation := analyticsPct(counts[database.EventAccountReactivated], counts[database.EventAccountDeletedInactivity])
	subtitle := fmt.Sprintf("Period: %s · unique users\nBurn recovery: %s · Reactivation: %s\nNSM/D7/D30 — cohort SQL",
		analyticsPeriodLabel(days), burnRecovery, reactivation)

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_f3_", days)}
	b.sendAdminHTMLPreTable(chatID, "3️⃣ Retention & streaks", subtitle, tbl.render(), kb)
}

// showAdminAnalyticsKPI — сводка ключевых метрик с таргетами (§7).
func (b *Bot) showAdminAnalyticsKPI(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load KPI: "+err.Error()))
		return
	}

	started := counts[database.EventBotStarted]
	paywall := counts[database.EventPaywallViewed]
	paid := counts[database.EventPaymentCompleted]
	miniapp := counts[database.EventMiniappOpened]
	logged := counts[database.EventWorkoutLogged]

	tbl := newAdminTable([]string{"Metric", "Val", "Target", "Kill"}, []int{15, 5, 5, 4})
	tbl.addRow("Activation (work/pay)", analyticsPct(logged, paid), ">35%", "<20%")
	tbl.addRow("Paywall→paid", analyticsPct(paid, paywall), "—", "—")
	tbl.addRow("Start→paywall", analyticsPct(paywall, started), "—", "—")
	tbl.addRow("Paid→mini app", analyticsPct(miniapp, paid), "—", "—")
	tbl.addRow("Burn recovery", analyticsPct(counts[database.EventBurnRecovered], counts[database.EventBurnWarningSent]), "—", "—")
	tbl.addRow("Reactivation", analyticsPct(counts[database.EventAccountReactivated], counts[database.EventAccountDeletedInactivity]), "—", "—")

	subtitle := fmt.Sprintf("Period: %s · unique users\nNSM (streak≥7 by day 14), D7/D30 retention, Sean Ellis — cohort metrics, not in events directly",
		analyticsPeriodLabel(days))

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_kpi_", days)}
	b.sendAdminHTMLPreTable(chatID, "⭐ KPI summary", subtitle, tbl.render(), kb)
}

// showAdminAnalyticsChannels — атрибуция каналов (§8).
func (b *Bot) showAdminAnalyticsChannels(chatID int64, days int) {
	channels, err := b.db.GetChannelAttribution(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load channels: "+err.Error()))
		return
	}

	subtitle := fmt.Sprintf("Period: %s · source from deep link", analyticsPeriodLabel(days))
	tableText := "No channel data yet."
	if len(channels) > 0 {
		tbl := newAdminTable([]string{"Channel", "Starts", "Paid", "CR"}, []int{16, 6, 4, 5})
		for _, c := range channels {
			tbl.addRow(c.Source, strconv.FormatInt(c.Started, 10), strconv.FormatInt(c.Paid, 10), analyticsPct(c.Paid, c.Started))
		}
		tableText = tbl.render()
	}

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_channels_", days)}
	b.sendAdminHTMLPreTable(chatID, "📡 Acquisition channels", subtitle, tableText, kb)
}

// showAdminAnalyticsOverview — все типы событий разом (§2/§7): что и сколько трекается.
func (b *Bot) showAdminAnalyticsOverview(chatID int64, days int) {
	overview, err := b.db.GetEventOverview(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load events: "+err.Error()))
		return
	}

	subtitle := fmt.Sprintf("Period: %s · event types: %d", analyticsPeriodLabel(days), len(overview))
	tableText := "No events yet."
	if len(overview) > 0 {
		tbl := newAdminTable([]string{"Event", "Total", "User", "Last"}, []int{24, 6, 5, 8})
		for _, e := range overview {
			last := "—"
			if e.LastAt.Valid {
				last = e.LastAt.Time.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04")
			}
			tbl.addRow(e.Name, strconv.FormatInt(e.Total, 10), strconv.FormatInt(e.UniqueUsers, 10), last)
		}
		tableText = tbl.render()
	}

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_overview_", days)}
	b.sendAdminHTMLPreTable(chatID, "🗂 All events", subtitle, tableText, kb)
}

// showAdminAnalyticsNonSport — счётчик заявок на фичу «хочу вносить не только спорт».
// Event non_sport_interest дедупится по пользователю, поэтому число = количество людей.
func (b *Bot) showAdminAnalyticsNonSport(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Could not load counter: "+err.Error()))
		return
	}
	n := counts[database.EventNonSportInterest]

	var sb strings.Builder
	sb.WriteString("🐆 Not just sport\n\n")
	sb.WriteString(fmt.Sprintf("Feature requests: %d\n", n))
	sb.WriteString("Period: " + analyticsPeriodLabel(days) + " · unique users (one request per user)")

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_nonsport_", days)}
	msg := tgbotapi.NewMessage(chatID, sb.String())
	msg.ReplyMarkup = kb
	b.api.Send(msg)
}

// handleAdminAnalyticsCallback — роутер callback'ов раздела аналитики.
// Возвращает true, если callback относится к аналитике и обработан.
func (b *Bot) handleAdminAnalyticsCallback(callback *tgbotapi.CallbackQuery) bool {
	if callback == nil || callback.Message == nil {
		return false
	}
	data := callback.Data
	chatID := callback.Message.Chat.ID

	if data == "admin_analytics" {
		b.showAdminAnalyticsMenu(chatID)
		return true
	}

	routes := []struct {
		prefix string
		render func(int64, int)
	}{
		{"admin_an_nonsport_", b.showAdminAnalyticsNonSport},
		{"admin_an_overview_", b.showAdminAnalyticsOverview},
		{"admin_an_channels_", b.showAdminAnalyticsChannels},
		{"admin_an_kpi_", b.showAdminAnalyticsKPI},
		{"admin_an_f1_", b.showAdminAnalyticsFunnel1},
		{"admin_an_f2_", b.showAdminAnalyticsFunnel2},
		{"admin_an_f3_", b.showAdminAnalyticsFunnel3},
	}
	for _, r := range routes {
		if strings.HasPrefix(data, r.prefix) {
			days, err := strconv.Atoi(strings.TrimPrefix(data, r.prefix))
			if err != nil || days < 0 {
				days = 0
			}
			r.render(chatID, days)
			return true
		}
	}
	return false
}
