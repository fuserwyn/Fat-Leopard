package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Разделы админки, которые раньше жили только в чате (/admin): аналитика,
// посещения, оплаты, админы, отложенные посты, опросы и очистка ленты.
// Цифры и подписи считаются здесь же теми же запросами, что и для чата, —
// мини-апп только рисует, поэтому две админки не разъезжаются.

// MiniappAdminTable — таблица «как в чате»: заголовок, пояснение и строки.
type MiniappAdminTable struct {
	// Kind — что это за блок (kpi, funnel, retention, channels, events, visits):
	// мини-апп выбирает отрисовку по нему, а не по тексту заголовка.
	Kind     string     `json:"kind,omitempty"`
	Title    string     `json:"title"`
	Subtitle string     `json:"subtitle"`
	Columns  []string   `json:"columns"`
	Rows     [][]string `json:"rows"`
}

type MiniappAdminAnalytics struct {
	Period      string              `json:"period"`
	LastEventAt string              `json:"last_event_at"`
	Tables      []MiniappAdminTable `json:"tables"`
	// Dashboard — данные для раздела «Дашборды»; считаются только по запросу.
	Dashboard *MiniappAdminDashboard `json:"dashboard,omitempty"`
}

// MiniappAdminAnalyticsData — воронки, KPI, каналы и события за период.
// days<=0 — за всё время. withDashboard добавляет данные раздела «Дашборды».
func (b *Bot) MiniappAdminAnalyticsData(
	viewerUserID int64, initD initdata.InitData, days int, withDashboard bool,
) (MiniappAdminAnalytics, error) {
	var out MiniappAdminAnalytics
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	if days < 0 {
		days = 0
	}
	counts, err := b.db.EventUniqueCounts(days)
	if err != nil {
		return out, err
	}
	out.Period = analyticsPeriodLabel(days)
	if last, ok := b.db.AnalyticsLastEventAt(); ok {
		out.LastEventAt = last.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04")
	}

	kpis := b.dashKPIs(days)
	kpiValue := func(key string) string {
		for _, k := range kpis {
			if k.Key == key {
				return k.Value
			}
		}
		return "—"
	}

	funnel1, err := b.dashFunnelTable("1️⃣ Воронка: бот → оплата", days, [][2]string{
		{database.EventBotStarted, "Старт бота"},
		{database.EventPaywallViewed, "Пэйвол"},
		{database.EventPaymentMethodSelected, "Выбор способа"},
		{database.EventPaymentInitiated, "К оплате"},
		{database.EventPaymentCompleted, "Оплатил"},
		{database.EventMiniappOpened, "Открыл миниапп"},
	}, func(c []int64) string {
		return dashFunnelNote(fmt.Sprintf("⭐ пэйвол→оплата: %s · старт→оплата: %s", analyticsPct(c[4], c[1]), analyticsPct(c[4], c[0])))
	})
	if err != nil {
		return out, err
	}
	out.Tables = append(out.Tables, funnel1)

	funnel2, err := b.dashFunnelTable("2️⃣ Воронка: активация", days, [][2]string{
		{database.EventMiniappOpened, "Открыл миниапп"},
		{database.EventWorkoutLogStarted, "Открыл форму"},
		{database.EventWorkoutLogged, "Залогал трен."},
		{database.EventLeoCommentReceived, "Коммент Лео"},
	}, func([]int64) string {
		return dashFunnelNote(fmt.Sprintf("⭐ kill-метрика (первая трен./оплата): %s · таргет ≥35%%, kill <20%%", kpiValue("activation")))
	})
	if err != nil {
		return out, err
	}
	out.Tables = append(out.Tables, funnel2)

	retention := MiniappAdminTable{
		Kind:  "retention",
		Title: "3️⃣ Retention и серии",
		Subtitle: fmt.Sprintf("Burn recovery: %s · Реактивация: %s",
			kpiValue("burn_recovery"), kpiValue("reactivation")),
		Columns: []string{"Событие", "Юзеры"},
	}
	for _, r := range [][2]string{
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
	} {
		retention.Rows = append(retention.Rows, []string{r[1], strconv.FormatInt(counts[r[0]], 10)})
	}
	out.Tables = append(out.Tables, retention)

	kpi := MiniappAdminTable{
		Kind:     "kpi",
		Title:    "⭐ Сводка KPI",
		Subtitle: "Доля тех, кто вошёл в шаг в этом периоде и дошёл до следующего. Удержание по когортам — в «Дашбордах».",
		Columns:  []string{"Метрика", "Знач", "Цель", "Kill"},
		Rows: [][]string{
			{"Активация (трен/опл)", kpiValue("activation"), ">35%", "<20%"},
			{"Пэйвол→оплата", kpiValue("paywall_paid"), "—", "—"},
			{"Старт→пэйвол", kpiValue("start_paywall"), "—", "—"},
			{"Оплата→миниапп", kpiValue("paid_miniapp"), "—", "—"},
			{"Burn recovery", kpiValue("burn_recovery"), "—", "—"},
			{"Реактивация", kpiValue("reactivation"), "—", "—"},
		},
	}
	out.Tables = append(out.Tables, kpi)

	if channels, err := b.db.GetChannelAttribution(days); err == nil && len(channels) > 0 {
		tbl := MiniappAdminTable{Kind: "channels", Title: "📣 Каналы", Subtitle: "Откуда пришли и сколько оплатили", Columns: []string{"Канал", "Старты", "Оплат"}}
		for _, c := range channels {
			tbl.Rows = append(tbl.Rows, []string{c.Source, strconv.FormatInt(c.Started, 10), strconv.FormatInt(c.Paid, 10)})
		}
		out.Tables = append(out.Tables, tbl)
	}

	if overview, err := b.db.GetEventOverview(days); err == nil && len(overview) > 0 {
		tbl := MiniappAdminTable{Kind: "events", Title: "📋 События", Subtitle: "Все события периода", Columns: []string{"Событие", "Всего", "Юзеры"}}
		for _, e := range overview {
			tbl.Rows = append(tbl.Rows, []string{e.Name, strconv.FormatInt(e.Total, 10), strconv.FormatInt(e.UniqueUsers, 10)})
		}
		out.Tables = append(out.Tables, tbl)
	}

	if withDashboard {
		dash := b.buildAdminDashboard(days, kpis)
		out.Dashboard = &dash
	}
	return out, nil
}

// MiniappAdminVisits — «Посещения бота»: сводка и топ визитёров.
func (b *Bot) MiniappAdminVisits(viewerUserID int64, initD initdata.InitData) ([]MiniappAdminTable, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	stats, err := b.db.GetBotVisitStats()
	if err != nil {
		return nil, err
	}
	summary := MiniappAdminTable{
		Kind:    "visits",
		Title:   "📊 Посещения бота",
		Columns: []string{"Метрика", "Значение"},
		Rows: [][]string{
			{"Всего визитов", strconv.FormatInt(stats.TotalVisits, 10)},
			{"Уникальных", strconv.FormatInt(stats.UniqueUsers, 10)},
			{"Сегодня", strconv.FormatInt(stats.TodayVisits, 10)},
			{"За неделю", strconv.FormatInt(stats.WeekVisits, 10)},
			{"За месяц", strconv.FormatInt(stats.MonthVisits, 10)},
		},
	}
	top := MiniappAdminTable{Title: "🏅 Кто заходит чаще", Columns: []string{"Юзер", "Визитов", "Последний"}}
	for _, u := range stats.TopVisitors {
		top.Rows = append(top.Rows, []string{
			adminVisitDisplayName(u.Username, u.FirstName, u.UserID),
			strconv.FormatInt(u.Visits, 10),
			u.LastVisit.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04"),
		})
	}
	if len(top.Rows) == 0 {
		return []MiniappAdminTable{summary}, nil
	}
	return []MiniappAdminTable{summary, top}, nil
}

type MiniappAdminPayments struct {
	Total         int               `json:"total"`
	Offset        int               `json:"offset"`
	Limit         int               `json:"limit"`
	Kind          string            `json:"kind"`           // "" | access | donation
	CompletedOnly bool              `json:"completed_only"` // только завершённые
	Query         string            `json:"query"`          // поиск по нику / имени / id
	From          string            `json:"from"`           // период с (ГГГГ-ММ-ДД, МСК), "" — без границы
	To            string            `json:"to"`             // период по (включительно)
	Order         string            `json:"order"`          // desc — новые сверху, asc — по хронологии
	Stats         MiniappAdminTable `json:"stats"`
	Payers        MiniappAdminTable `json:"payers"`
	Table         MiniappAdminTable `json:"table"`
}

// MiniappAdminPaymentsQuery — фильтры раздела «Оплаты».
type MiniappAdminPaymentsQuery struct {
	Offset        int
	Limit         int
	Kind          string // "" | access | donation
	CompletedOnly bool
	Query         string // @ник, имя или telegram id
	From          string // ГГГГ-ММ-ДД по Москве, включительно; "" — без границы
	To            string // ГГГГ-ММ-ДД по Москве, включительно; "" — без границы
	Order         string // desc (по умолчанию) | asc
}

// miniappAdminPayersLimit — сколько плательщиков показываем в таблице «Кто платил».
const miniappAdminPayersLimit = 50

var adminMoneyMSK = time.FixedZone("MSK", 3*3600)

// adminParseMoneyDay — день «ГГГГ-ММ-ДД» по Москве; пустая строка — нулевое время.
func adminParseMoneyDay(s string) (time.Time, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.ParseInLocation("2006-01-02", s, adminMoneyMSK)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: неверная дата %q, нужен формат ГГГГ-ММ-ДД", ErrAdminActionInvalid, s)
	}
	return t, nil
}

// adminMoneyPeriod — границы периода для фильтра: с from 00:00 до конца дня to (МСК).
// Если даты перепутаны местами — меняем их. Возвращает и нормализованные строки.
func adminMoneyPeriod(fromS, toS string) (from, to time.Time, fromOut, toOut string, err error) {
	from, err = adminParseMoneyDay(fromS)
	if err != nil {
		return
	}
	to, err = adminParseMoneyDay(toS)
	if err != nil {
		return
	}
	if !from.IsZero() && !to.IsZero() && to.Before(from) {
		from, to = to, from
	}
	if !from.IsZero() {
		fromOut = from.Format("2006-01-02")
	}
	if !to.IsZero() {
		toOut = to.Format("2006-01-02")
		to = to.AddDate(0, 0, 1)
	}
	return
}

// adminMoneyPeriodLabel — «за 01.09.26–30.09.26», «с 01.09.26», «по 30.09.26» или «за всё время».
func adminMoneyPeriodLabel(from, to string) string {
	f := func(s string) string {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return s
		}
		return t.Format("02.01.06")
	}
	switch {
	case from != "" && to != "" && from == to:
		return "за " + f(from)
	case from != "" && to != "":
		return "за " + f(from) + "–" + f(to)
	case from != "":
		return "с " + f(from)
	case to != "":
		return "по " + f(to)
	default:
		return "за всё время"
	}
}

// MiniappAdminPaymentsPage — «Оплаты»: сводка, кто платил и список платежей за доступ + донатов.
// Фильтры: вид (всё / доступ / донаты), только завершённые, человек (ник, имя, id),
// период по дате оплаты и порядок (новые сверху или по хронологии).
func (b *Bot) MiniappAdminPaymentsPage(
	viewerUserID int64, initD initdata.InitData, q MiniappAdminPaymentsQuery,
) (MiniappAdminPayments, error) {
	var out MiniappAdminPayments
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		return out, fmt.Errorf("не настроен MonetizedChatID")
	}
	offset, limit := q.Offset, q.Limit
	if offset < 0 {
		offset = 0
	}
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	kind := database.NormalizeAdminMoneyKind(q.Kind)
	from, to, fromS, toS, err := adminMoneyPeriod(q.From, q.To)
	if err != nil {
		return out, err
	}
	query := strings.TrimSpace(q.Query)
	if r := []rune(query); len(r) > 64 {
		query = string(r[:64])
	}
	oldestFirst := strings.EqualFold(strings.TrimSpace(q.Order), "asc")
	filter := database.AdminMoneyFilter{
		Kind:          kind,
		CompletedOnly: q.CompletedOnly,
		Query:         query,
		From:          from,
		To:            to,
		OldestFirst:   oldestFirst,
	}
	out.Kind, out.CompletedOnly, out.Query, out.From, out.To = kind, q.CompletedOnly, query, fromS, toS
	out.Order = "desc"
	if oldestFirst {
		out.Order = "asc"
	}

	period := adminMoneyPeriodLabel(fromS, toS)
	scope := period
	if query != "" {
		scope += " · по «" + query + "»"
	}

	sums, err := b.db.SumMoneyForAdminFiltered(packChatID, filter)
	if err != nil {
		return out, err
	}
	out.Stats = adminBuildMoneyStatsTableForKind(kind, sums)
	out.Stats.Subtitle = "Завершённые оплаты " + scope + " · звёзды и рубли"

	payers, err := b.db.ListMoneyPayersForAdmin(packChatID, filter, miniappAdminPayersLimit)
	if err != nil {
		return out, err
	}
	out.Payers = adminBuildMoneyPayersTable(kind, payers)
	out.Payers.Subtitle = "Завершённые оплаты " + scope + ", свежие сверху"

	total, err := b.db.CountMoneyPaymentsForAdminFiltered(packChatID, filter)
	if err != nil {
		return out, err
	}
	rows, err := b.db.ListMoneyPaymentsForAdminFiltered(packChatID, filter, offset, limit)
	if err != nil {
		return out, err
	}
	out.Total, out.Offset, out.Limit = total, offset, limit
	out.Table = MiniappAdminTable{
		Title:   adminMoneyListTitle(kind),
		Columns: []string{"№", "Тип", "Кто", "Статус", "Сумма", "Дата"},
		Rows:    [][]string{},
	}
	r := []rune(scope)
	sub := []string{strings.ToUpper(string(r[:1])) + string(r[1:])}
	if q.CompletedOnly {
		sub = append(sub, "только завершённые")
	}
	if oldestFirst {
		sub = append(sub, "сначала старые")
	} else {
		sub = append(sub, "сначала новые")
	}
	out.Table.Subtitle = strings.Join(sub, " · ")
	for i, p := range rows {
		cur := ""
		if p.Currency.Valid {
			cur = p.Currency.String
		}
		out.Table.Rows = append(out.Table.Rows, []string{
			strconv.Itoa(offset + i + 1),
			adminMoneyKindCurrencyLabel(p.Kind, cur),
			adminMoneyPersonLabel(p.Username, p.DisplayName, p.UserID),
			adminPaymentStatusForKind(p.Kind, p.Status, p.AccessActive),
			adminFormatPaymentAmount(p.AmountMinor, p.Currency),
			p.CreatedAt.In(adminMoneyMSK).Format("02.01.06 15:04"),
		})
	}
	return out, nil
}

func adminMoneyListTitle(kind string) string {
	switch kind {
	case "access":
		return "💳 Платежи за доступ"
	case "donation":
		return "💛 Донаты"
	default:
		return "💳 Все платежи и донаты"
	}
}

// adminMoneyPersonLabel — кто платил: ник и имя из профиля, а если нет ни того,
// ни другого — telegram id, чтобы человека можно было найти.
func adminMoneyPersonLabel(username, displayName string, userID int64) string {
	nick := adminPaywallPersonLabel(username, "", userID)
	hasNick := strings.HasPrefix(nick, "@")
	name := strings.TrimSpace(displayName)
	if r := []rune(name); len(r) > 24 {
		name = string(r[:24]) + "…"
	}
	switch {
	case hasNick && name != "":
		return nick + " · " + name
	case hasNick:
		return nick
	case name != "":
		return name + " · id" + strconv.FormatInt(userID, 10)
	default:
		return "id" + strconv.FormatInt(userID, 10)
	}
}

// adminBuildMoneyPayersTable — «Кто платил»: по человеку, сколько раз и на какую сумму.
func adminBuildMoneyPayersTable(kind string, payers []database.AdminMoneyPayerRow) MiniappAdminTable {
	tbl := MiniappAdminTable{
		Title:    "👥 Кто платил",
		Subtitle: "Завершённые оплаты за всё время, свежие сверху",
		Columns:  []string{"Кто", "Доступ", "Донаты", "⭐", "₽", "Последний"},
		Rows:     [][]string{},
	}
	switch kind {
	case "access":
		tbl.Title = "👥 Кто платил за доступ"
		tbl.Columns = []string{"Кто", "Платежей", "⭐", "₽", "Последний"}
	case "donation":
		tbl.Title = "👥 Кто донатил"
		tbl.Columns = []string{"Кто", "Донатов", "⭐", "₽", "Последний"}
	}
	msk := time.FixedZone("MSK", 3*3600)
	for _, p := range payers {
		stars := "—"
		if p.StarsTotal > 0 {
			stars = strconv.FormatInt(p.StarsTotal, 10)
		}
		rub := "—"
		if p.RubMinorTotal > 0 {
			rub = fmt.Sprintf("%.0f", float64(p.RubMinorTotal)/100)
		}
		who := adminMoneyPersonLabel(p.Username, p.DisplayName, p.UserID)
		last := p.LastPaidAt.In(msk).Format("02.01.06")
		switch kind {
		case "access":
			tbl.Rows = append(tbl.Rows, []string{who, strconv.FormatInt(p.AccessCount, 10), stars, rub, last})
		case "donation":
			tbl.Rows = append(tbl.Rows, []string{who, strconv.FormatInt(p.DonationCount, 10), stars, rub, last})
		default:
			tbl.Rows = append(tbl.Rows, []string{
				who,
				strconv.FormatInt(p.AccessCount, 10),
				strconv.FormatInt(p.DonationCount, 10),
				stars, rub, last,
			})
		}
	}
	return tbl
}

type MiniappAdminPerson struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	Static   bool   `json:"static"`
	AddedAt  string `json:"added_at"`
}

// MiniappAdminAdminsList — кто админ: из переменных окружения и из базы.
func (b *Bot) MiniappAdminAdminsList(viewerUserID int64, initD initdata.InitData) ([]MiniappAdminPerson, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	out := make([]MiniappAdminPerson, 0, 8)
	if b.config.OwnerID != 0 {
		out = append(out, MiniappAdminPerson{UserID: b.config.OwnerID, Username: "владелец", Static: true})
	}
	for _, id := range b.config.AdminIDs {
		out = append(out, MiniappAdminPerson{UserID: id, Static: true})
	}
	dynamic, err := b.db.ListDynamicAdmins()
	if err != nil {
		return nil, err
	}
	for _, a := range dynamic {
		out = append(out, MiniappAdminPerson{
			UserID:   a.UserID,
			Username: a.Username,
			AddedAt:  a.AddedAt.In(time.FixedZone("MSK", 3*3600)).Format("02.01.2006"),
		})
	}
	return out, nil
}

// MiniappAdminAddAdmin — выдать права по @нику или id. Возвращает id новичка.
func (b *Bot) MiniappAdminAddAdmin(viewerUserID int64, initD initdata.InitData, query string) (int64, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return 0, err
	}
	q := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(query), "@"))
	if q == "" {
		return 0, ErrAdminActionInvalid
	}
	targetID, err := strconv.ParseInt(q, 10, 64)
	username := ""
	if err != nil {
		targetID, err = b.db.FindUserIDByUsername(q)
		if err != nil || targetID == 0 {
			return 0, fmt.Errorf("не нашёл @%s среди участников", q)
		}
		username = q
	}
	if err := b.db.AddDynamicAdmin(targetID, username, viewerUserID); err != nil {
		return 0, err
	}
	// Права проверяются по кэшу в памяти, а не по базе: без перезагрузки
	// добавленный из мини-аппа админ оставался обычным человеком до рестарта
	// сервиса — со стороны это выглядело как «добавление не работает».
	b.reloadDynamicAdmins()
	// Нижняя клавиатура в личке зависит от прав — как и при добавлении из чата,
	// сбрасываем её кэш, иначе кнопки админ-панели не появятся.
	b.privateBottomKeyboardKind.Delete(targetID)
	return targetID, nil
}

// MiniappAdminRemoveAdmin — снять права. Права из окружения так не снимаются.
func (b *Bot) MiniappAdminRemoveAdmin(viewerUserID int64, initD initdata.InitData, targetID int64) error {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return err
	}
	if targetID == b.config.OwnerID {
		return fmt.Errorf("владельца снять нельзя")
	}
	isDynamic, err := b.db.IsDynamicAdmin(targetID)
	if err != nil {
		return err
	}
	if !isDynamic {
		return fmt.Errorf("этот админ прописан в переменных окружения — снимается только там")
	}
	if _, err := b.db.RemoveDynamicAdmin(targetID); err != nil {
		return err
	}
	// Снятие прав тоже должно действовать сразу, а не после рестарта.
	b.reloadDynamicAdmins()
	b.privateBottomKeyboardKind.Delete(targetID)
	return nil
}

type MiniappScheduledPost struct {
	ID          int64  `json:"id"`
	Author      string `json:"author"`
	Text        string `json:"text"`
	ScheduledAt string `json:"scheduled_at"`
}

// MiniappAdminScheduledPosts — очередь отложенных постов в ленту.
func (b *Bot) MiniappAdminScheduledPosts(viewerUserID int64, initD initdata.InitData) ([]MiniappScheduledPost, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	posts, err := b.db.ListPendingScheduledAdminPosts(b.config.MonetizedChatID, 20)
	if err != nil {
		return nil, err
	}
	out := make([]MiniappScheduledPost, 0, len(posts))
	for _, p := range posts {
		out = append(out, MiniappScheduledPost{
			ID:          p.ID,
			Author:      p.Author,
			Text:        p.MessageText,
			ScheduledAt: p.ScheduledAt.In(time.FixedZone("MSK", 3*3600)).Format("02.01 15:04"),
		})
	}
	return out, nil
}

// MiniappAdminSchedulePost — поставить пост в ленту на время (МСК).
func (b *Bot) MiniappAdminSchedulePost(
	viewerUserID int64, initD initdata.InitData, author, text, at string,
) (int64, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return 0, err
	}
	body := strings.TrimSpace(text)
	if body == "" {
		return 0, ErrAdminActionInvalid
	}
	msk := time.FixedZone("MSK", 3*3600)
	when, err := time.ParseInLocation("2006-01-02T15:04", strings.TrimSpace(at), msk)
	if err != nil {
		return 0, fmt.Errorf("не понял время: нужен формат 2026-08-20T09:00")
	}
	if when.Before(time.Now()) {
		return 0, fmt.Errorf("время уже прошло")
	}
	return b.db.InsertScheduledAdminPost(
		b.config.MonetizedChatID, normalizeAdminPostAuthor(author), body, when, viewerUserID,
	)
}

// MiniappAdminCancelScheduledPost — снять пост из очереди.
func (b *Bot) MiniappAdminCancelScheduledPost(viewerUserID int64, initD initdata.InitData, postID int64) error {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return err
	}
	ok, err := b.db.CancelScheduledAdminPost(b.config.MonetizedChatID, postID)
	if err != nil {
		return err
	}
	if !ok {
		return ErrAdminNotFound
	}
	return nil
}

// MiniappAdminPublishPoll — опрос в ленту: вопрос и 2–10 вариантов.
func (b *Bot) MiniappAdminPublishPoll(
	viewerUserID int64, initD initdata.InitData, question string, options []string,
) error {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return err
	}
	q := strings.TrimSpace(question)
	if q == "" {
		return fmt.Errorf("напиши вопрос")
	}
	clean := make([]string, 0, len(options))
	for _, opt := range options {
		v := strings.TrimSpace(opt)
		if v == "" {
			continue
		}
		if len([]rune(v)) > 100 {
			return fmt.Errorf("вариант «%s…» длиннее 100 символов", string([]rune(v)[:20]))
		}
		clean = append(clean, v)
	}
	if len(clean) < 2 {
		return fmt.Errorf("нужно минимум 2 варианта")
	}
	if len(clean) > 10 {
		return fmt.Errorf("максимум 10 вариантов")
	}
	return b.saveAdminPollPackFeed(viewerUserID, q, clean)
}

type MiniappWipeCounts struct {
	FeedPosts        int64 `json:"feed_posts"`
	FeedThreads      int64 `json:"feed_threads"`
	FeedReports      int64 `json:"feed_reports"`
	PackChatMessages int64 `json:"pack_chat_messages"`
}

// MiniappAdminWipeCounts — что именно удалится при очистке ленты и чата.
func (b *Bot) MiniappAdminWipeCounts(viewerUserID int64, initD initdata.InitData) (MiniappWipeCounts, error) {
	var out MiniappWipeCounts
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		return out, fmt.Errorf("не настроен MonetizedChatID")
	}
	counts, err := b.db.AdminCountPackFeedAndChat(packChatID)
	if err != nil {
		return out, err
	}
	return MiniappWipeCounts{
		FeedPosts:        counts.FeedPosts,
		FeedThreads:      counts.FeedThreads,
		FeedReports:      counts.FeedReports,
		PackChatMessages: counts.PackChatMessages,
	}, nil
}

// MiniappAdminWipeExecute — необратимая очистка ленты и чата стаи.
func (b *Bot) MiniappAdminWipeExecute(viewerUserID int64, initD initdata.InitData) (MiniappWipeCounts, error) {
	var out MiniappWipeCounts
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return out, err
	}
	packChatID := b.adminPackChatID()
	if packChatID == 0 {
		return out, fmt.Errorf("не настроен MonetizedChatID")
	}
	deleted, err := b.db.AdminClearPackFeedAndChat(packChatID)
	if err != nil {
		return out, err
	}
	return MiniappWipeCounts{
		FeedPosts:        deleted.FeedPosts,
		FeedThreads:      deleted.FeedThreads,
		FeedReports:      deleted.FeedReports,
		PackChatMessages: deleted.PackChatMessages,
	}, nil
}
