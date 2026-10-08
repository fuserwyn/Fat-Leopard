package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"

	tgbotapi "github.com/go-telegram-bot-api/telegram-bot-api/v5"
)

// Раздел «Аналитика» в админ-панели — читаемый вид воронок и метрик из
// analytics_BT_v1.md поверх таблицы events. Только чтение, без записи.

// analyticsPeriods — окна, которые админ переключает кнопками. 0 = всё время.
var analyticsPeriods = []struct {
	Days  int
	Label string
}{
	{0, "Всё"},
	{30, "30д"},
	{7, "7д"},
}

func analyticsPeriodLabel(days int) string {
	for _, p := range analyticsPeriods {
		if p.Days == days {
			return p.Label
		}
	}
	if days <= 0 {
		return "Всё"
	}
	return strconv.Itoa(days) + "д"
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
	sb.WriteString("📈 Аналитика Fat Leopard\n\n")
	sb.WriteString("Воронки и метрики из БТ поверх таблицы events.\n")
	if last, ok := b.db.AnalyticsLastEventAt(); ok {
		sb.WriteString("Последнее событие: " + last.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04") + " (МСК)\n")
	} else {
		sb.WriteString("Событий пока нет — данные появятся после первых действий юзеров.\n")
	}
	sb.WriteString("\nВыбери раздел:")

	rows := [][]tgbotapi.InlineKeyboardButton{
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("1️⃣ Воронка: бот→оплата", "admin_an_f1_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("2️⃣ Воронка: активация", "admin_an_f2_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("3️⃣ Retention и серии", "admin_an_f3_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⭐ Сводка KPI", "admin_an_kpi_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("📡 Каналы", "admin_an_channels_0"),
			tgbotapi.NewInlineKeyboardButtonData("🗂 Все события", "admin_an_overview_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("🐆 Хочу не только спорт", "admin_an_nonsport_0"),
		),
		tgbotapi.NewInlineKeyboardRow(
			tgbotapi.NewInlineKeyboardButtonData("⬅️ К админке", "admin_open"),
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
			tgbotapi.NewInlineKeyboardButtonData("⬅️ К аналитике", "admin_analytics"),
		),
	}
}

// showAdminAnalyticsFunnel1 — воронка приобретения (§3).
func (b *Bot) showAdminAnalyticsFunnel1(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить воронку: "+err.Error()))
		return
	}

	stages := []struct {
		Event string
		Label string
	}{
		{database.EventBotStarted, "Старт бота"},
		{database.EventPaywallViewed, "Пэйвол"},
		{database.EventPaymentMethodSelected, "Выбор способа"},
		{database.EventPaymentInitiated, "К оплате"},
		{database.EventPaymentCompleted, "Оплатил"},
		{database.EventMiniappOpened, "Открыл миниапп"},
	}

	tbl := newAdminTable([]string{"Стадия", "Юзеры", "Конв"}, []int{14, 6, 5})
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
	subtitle := fmt.Sprintf("Период: %s · конверсия = к предыдущей стадии\n⭐ пэйвол→оплата: %s · старт→оплата: %s",
		analyticsPeriodLabel(days), analyticsPct(paid, paywall), analyticsPct(paid, started))

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_f1_", days)}
	b.sendAdminHTMLPreTable(chatID, "1️⃣ Воронка: бот → оплата", subtitle, tbl.render(), kb)
}

// showAdminAnalyticsFunnel2 — воронка активации (§4), включая kill-метрику.
func (b *Bot) showAdminAnalyticsFunnel2(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить воронку: "+err.Error()))
		return
	}

	stages := []struct {
		Event string
		Label string
	}{
		{database.EventMiniappOpened, "Открыл миниапп"},
		{database.EventWorkoutLogStarted, "Открыл форму"},
		{database.EventWorkoutLogged, "Залогал трен."},
		{database.EventLeoCommentReceived, "Коммент Лео"},
	}

	tbl := newAdminTable([]string{"Стадия", "Юзеры", "Конв"}, []int{14, 6, 5})
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
	subtitle := fmt.Sprintf("Период: %s\n⭐ kill-метрика (первая трен./оплата): %s\nТаргет ≥35%% · kill <20%% (BRD)",
		analyticsPeriodLabel(days), kill)

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_f2_", days)}
	b.sendAdminHTMLPreTable(chatID, "2️⃣ Воронка: активация", subtitle, tbl.render(), kb)
}

// showAdminAnalyticsFunnel3 — retention и серии (§5).
func (b *Bot) showAdminAnalyticsFunnel3(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить retention: "+err.Error()))
		return
	}

	rows := []struct {
		Event string
		Label string
	}{
		{database.EventStreakIncremented, "Стрик растёт"},
		{database.EventStreakAttemptUsed, "Попытка спасла"},
		{database.EventStreakBroken, "Стрик сгорел"},
		{database.EventLevelUp, "Новый уровень"},
		{database.EventMilestoneAchieved, "Ачивка стрика"},
		{database.EventBurnWarningSent, "Burn-алерт"},
		{database.EventBurnRecovered, "Спасся после"},
		{database.EventSickLeaveStarted, "Больничный вкл"},
		{database.EventSickLeaveEnded, "Больничный выкл"},
		{database.EventAccountDeletedInactivity, "Удалён (8д)"},
		{database.EventAccountReactivated, "Вернулся"},
		{database.EventChallengeJoined, "Взял челлендж"},
		{database.EventChallengeCompleted, "Прошёл челлендж"},
		{database.EventChallengeFailed, "Провалил челлендж"},
		{database.EventChallengeCreated, "Создал челлендж"},
	}

	tbl := newAdminTable([]string{"Событие", "Юзеры"}, []int{16, 6})
	for _, r := range rows {
		tbl.addRow(r.Label, strconv.FormatInt(counts[r.Event], 10))
	}

	burnRecovery := analyticsPct(counts[database.EventBurnRecovered], counts[database.EventBurnWarningSent])
	reactivation := analyticsPct(counts[database.EventAccountReactivated], counts[database.EventAccountDeletedInactivity])
	subtitle := fmt.Sprintf("Период: %s · уникальные юзеры\nBurn recovery: %s · Реактивация: %s\nNSM/D7/D30 — когортные, считаются отдельным SQL",
		analyticsPeriodLabel(days), burnRecovery, reactivation)

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_f3_", days)}
	b.sendAdminHTMLPreTable(chatID, "3️⃣ Retention и серии", subtitle, tbl.render(), kb)
}

// showAdminAnalyticsKPI — сводка ключевых метрик с таргетами (§7).
func (b *Bot) showAdminAnalyticsKPI(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить KPI: "+err.Error()))
		return
	}

	started := counts[database.EventBotStarted]
	paywall := counts[database.EventPaywallViewed]
	paid := counts[database.EventPaymentCompleted]
	miniapp := counts[database.EventMiniappOpened]
	logged := counts[database.EventWorkoutLogged]

	tbl := newAdminTable([]string{"Метрика", "Знач", "Цель", "Kill"}, []int{15, 5, 5, 4})
	tbl.addRow("Активация (трен/опл)", analyticsPct(logged, paid), ">35%", "<20%")
	tbl.addRow("Пэйвол→оплата", analyticsPct(paid, paywall), "—", "—")
	tbl.addRow("Старт→пэйвол", analyticsPct(paywall, started), "—", "—")
	tbl.addRow("Оплата→миниапп", analyticsPct(miniapp, paid), "—", "—")
	tbl.addRow("Burn recovery", analyticsPct(counts[database.EventBurnRecovered], counts[database.EventBurnWarningSent]), "—", "—")
	tbl.addRow("Реактивация", analyticsPct(counts[database.EventAccountReactivated], counts[database.EventAccountDeletedInactivity]), "—", "—")

	subtitle := fmt.Sprintf("Период: %s · по уникальным юзерам\nNSM (стрик≥7 на 14д), D7/D30 retention, Sean Ellis — когортные, не в events напрямую",
		analyticsPeriodLabel(days))

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_kpi_", days)}
	b.sendAdminHTMLPreTable(chatID, "⭐ Сводка KPI", subtitle, tbl.render(), kb)
}

// showAdminAnalyticsChannels — атрибуция каналов (§8).
func (b *Bot) showAdminAnalyticsChannels(chatID int64, days int) {
	channels, err := b.db.GetChannelAttribution(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить каналы: "+err.Error()))
		return
	}

	subtitle := fmt.Sprintf("Период: %s · source из deep-link", analyticsPeriodLabel(days))
	tableText := "Данных по каналам пока нет."
	if len(channels) > 0 {
		tbl := newAdminTable([]string{"Канал", "Старты", "Опл", "CR"}, []int{16, 6, 4, 5})
		for _, c := range channels {
			tbl.addRow(c.Source, strconv.FormatInt(c.Started, 10), strconv.FormatInt(c.Paid, 10), analyticsPct(c.Paid, c.Started))
		}
		tableText = tbl.render()
	}

	kb := &tgbotapi.InlineKeyboardMarkup{InlineKeyboard: analyticsBottomRows("admin_an_channels_", days)}
	b.sendAdminHTMLPreTable(chatID, "📡 Каналы приобретения", subtitle, tableText, kb)
}

// showAdminAnalyticsOverview — все типы событий разом (§2/§7): что и сколько трекается.
func (b *Bot) showAdminAnalyticsOverview(chatID int64, days int) {
	overview, err := b.db.GetEventOverview(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить события: "+err.Error()))
		return
	}

	subtitle := fmt.Sprintf("Период: %s · всего типов: %d", analyticsPeriodLabel(days), len(overview))
	tableText := "Событий пока нет."
	if len(overview) > 0 {
		tbl := newAdminTable([]string{"Событие", "Всего", "Юзер", "Послед"}, []int{24, 6, 5, 8})
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
	b.sendAdminHTMLPreTable(chatID, "🗂 Все события", subtitle, tableText, kb)
}

// showAdminAnalyticsNonSport — счётчик заявок на фичу «хочу вносить не только спорт».
// Событие non_sport_interest дедупится по пользователю, поэтому число = количество людей.
func (b *Bot) showAdminAnalyticsNonSport(chatID int64, days int) {
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		b.api.Send(tgbotapi.NewMessage(chatID, "❌ Не удалось загрузить счётчик: "+err.Error()))
		return
	}
	n := counts[database.EventNonSportInterest]

	var sb strings.Builder
	sb.WriteString("🐆 Хочу вносить не только спорт\n\n")
	sb.WriteString(fmt.Sprintf("Заявок на фичу: %d\n", n))
	sb.WriteString("Период: " + analyticsPeriodLabel(days) + " · уникальные люди (по одной заявке на юзера)")

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
