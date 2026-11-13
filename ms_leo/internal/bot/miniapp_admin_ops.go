package bot

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"leo-bot/internal/database"

	initdata "github.com/telegram-mini-apps/init-data-golang"
)

// Разделы adminки, которые раньше жили только в чате (/admin): аналитика,
// посещения, оплаты, adminы, отложенные посты, опросы и очистка ленты.
// Цифры и подписи считаются здесь же теми же запросами, что и для чата, —
// мини-апп только рисует, поэтому две adminки не разъезжаются.

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
// days<=0 — all time. withDashboard добавляет данные раздела «Дашборды».
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

	funnel1, err := b.dashFunnelTable("1️⃣ Funnel: bot → pay", days, [][2]string{
		{database.EventBotStarted, "Bot start"},
		{database.EventPaywallViewed, "Paywall"},
		{database.EventPaymentMethodSelected, "Method pick"},
		{database.EventPaymentInitiated, "To checkout"},
		{database.EventPaymentCompleted, "Paid"},
		{database.EventMiniappOpened, "Opened mini app"},
	}, func(c []int64) string {
		return dashFunnelNote(fmt.Sprintf("⭐ paywall→paid: %s · start→paid: %s", analyticsPct(c[4], c[1]), analyticsPct(c[4], c[0])))
	})
	if err != nil {
		return out, err
	}
	out.Tables = append(out.Tables, funnel1)

	funnel2, err := b.dashFunnelTable("2️⃣ Funnel: activation", days, [][2]string{
		{database.EventMiniappOpened, "Opened mini app"},
		{database.EventWorkoutLogStarted, "Opened form"},
		{database.EventWorkoutLogged, "Logged workout"},
		{database.EventLeoCommentReceived, "Leo comment"},
	}, func([]int64) string {
		return dashFunnelNote(fmt.Sprintf("⭐ kill metric (first workout/payment): %s · target ≥35%%, kill <20%%", kpiValue("activation")))
	})
	if err != nil {
		return out, err
	}
	out.Tables = append(out.Tables, funnel2)

	retention := MiniappAdminTable{
		Kind:  "retention",
		Title: "3️⃣ Retention & streaks",
		Subtitle: fmt.Sprintf("Burn recovery: %s · Reactivation: %s",
			kpiValue("burn_recovery"), kpiValue("reactivation")),
		Columns: []string{"Event", "Users"},
	}
	for _, r := range [][2]string{
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
	} {
		retention.Rows = append(retention.Rows, []string{r[1], strconv.FormatInt(counts[r[0]], 10)})
	}
	out.Tables = append(out.Tables, retention)

	kpi := MiniappAdminTable{
		Kind:     "kpi",
		Title:    "⭐ KPI summary",
		Subtitle: "Share who hit this step in the period and reached the next. Cohort retention is under Dashboards.",
		Columns:  []string{"Metric", "Val", "Target", "Kill"},
		Rows: [][]string{
			{"Activation (work/pay)", kpiValue("activation"), ">35%", "<20%"},
			{"Paywall→paid", kpiValue("paywall_paid"), "—", "—"},
			{"Start→paywall", kpiValue("start_paywall"), "—", "—"},
			{"Paid→mini app", kpiValue("paid_miniapp"), "—", "—"},
			{"Burn recovery", kpiValue("burn_recovery"), "—", "—"},
			{"Reactivation", kpiValue("reactivation"), "—", "—"},
		},
	}
	out.Tables = append(out.Tables, kpi)

	if channels, err := b.db.GetChannelAttribution(days); err == nil && len(channels) > 0 {
		tbl := MiniappAdminTable{Kind: "channels", Title: "📣 Channels", Subtitle: "Where users came from and how many paid", Columns: []string{"Channel", "Starts", "Payments"}}
		for _, c := range channels {
			tbl.Rows = append(tbl.Rows, []string{c.Source, strconv.FormatInt(c.Started, 10), strconv.FormatInt(c.Paid, 10)})
		}
		out.Tables = append(out.Tables, tbl)
	}

	if overview, err := b.db.GetEventOverview(days); err == nil && len(overview) > 0 {
		tbl := MiniappAdminTable{Kind: "events", Title: "📋 Events", Subtitle: "All events in period", Columns: []string{"Event", "Total", "Users"}}
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
		Title:   "📊 Bot visits",
		Columns: []string{"Metric", "Value"},
		Rows: [][]string{
			{"Total visits", strconv.FormatInt(stats.TotalVisits, 10)},
			{"Unique", strconv.FormatInt(stats.UniqueUsers, 10)},
			{"Today", strconv.FormatInt(stats.TodayVisits, 10)},
			{"This week", strconv.FormatInt(stats.WeekVisits, 10)},
			{"This month", strconv.FormatInt(stats.MonthVisits, 10)},
		},
	}
	top := MiniappAdminTable{Title: "🏅 Top visitors", Columns: []string{"User", "Visits", "Last"}}
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
	CompletedOnly bool              `json:"completed_only"` // completed only
	Query         string            `json:"query"`          // поиск по нику / имени / id
	From          string            `json:"from"`           // период с (ГГГГ-ММ-ДД, МСК), "" — без границы
	To            string            `json:"to"`             // период по (включительно)
	Order         string            `json:"order"`          // desc — новые сверху, asc — по хронологии
	Stats         MiniappAdminTable `json:"stats"`
	Payers        MiniappAdminTable `json:"payers"`
	Table         MiniappAdminTable `json:"table"`
}

// MiniappAdminPaymentsQuery — фильтры раздела «Payments».
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
		return time.Time{}, fmt.Errorf("%w: invalid date %q, use YYYY-MM-DD", ErrAdminActionInvalid, s)
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

// adminMoneyPeriodLabel — «за 01.09.26–30.09.26», «с 01.09.26», «по 30.09.26» или «all time».
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
		return "for " + f(from)
	case from != "" && to != "":
		return "for " + f(from) + "–" + f(to)
	case from != "":
		return "from " + f(from)
	case to != "":
		return "through " + f(to)
	default:
		return "all time"
	}
}

// MiniappAdminPaymentsPage — «Payments»: сводка, кто платил и список платежей за access + donations.
// Фильтры: вид (всё / access / donations), completed only, человек (ник, имя, id),
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
		return out, fmt.Errorf("MonetizedChatID not configured")
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
		scope += " · matching «" + query + "»"
	}

	sums, err := b.db.SumMoneyForAdminFiltered(packChatID, filter)
	if err != nil {
		return out, err
	}
	out.Stats = adminBuildMoneyStatsTableForKind(kind, sums)
	out.Stats.Subtitle = "Completed payments " + scope + " · stars and rubles"

	payers, err := b.db.ListMoneyPayersForAdmin(packChatID, filter, miniappAdminPayersLimit)
	if err != nil {
		return out, err
	}
	out.Payers = adminBuildMoneyPayersTable(kind, payers)
	out.Payers.Subtitle = "Completed payments " + scope + ", newest first"

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
		Columns: []string{"No.", "Type", "Who", "Status", "Amount", "Date"},
		Rows:    [][]string{},
	}
	r := []rune(scope)
	sub := []string{strings.ToUpper(string(r[:1])) + string(r[1:])}
	if q.CompletedOnly {
		sub = append(sub, "completed only")
	}
	if oldestFirst {
		sub = append(sub, "oldest first")
	} else {
		sub = append(sub, "newest first")
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
		return "💳 Access payments"
	case "donation":
		return "💛 Donations"
	default:
		return "💳 All payments & donations"
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
		Title:    "👥 Who paid",
		Subtitle: "Completed payments all time, newest first",
		Columns:  []string{"Who", "Access", "Donations", "⭐", "₽", "Last"},
		Rows:     [][]string{},
	}
	switch kind {
	case "access":
		tbl.Title = "👥 Access payers"
		tbl.Columns = []string{"Who", "Payments", "⭐", "₽", "Last"}
	case "donation":
		tbl.Title = "👥 Donors"
		tbl.Columns = []string{"Who", "Donations", "⭐", "₽", "Last"}
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

// MiniappAdminAdminsList — кто admin: из переменных окружения и из базы.
func (b *Bot) MiniappAdminAdminsList(viewerUserID int64, initD initdata.InitData) ([]MiniappAdminPerson, error) {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return nil, err
	}
	out := make([]MiniappAdminPerson, 0, 8)
	if b.config.OwnerID != 0 {
		out = append(out, MiniappAdminPerson{UserID: b.config.OwnerID, Username: "owner", Static: true})
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
			return 0, fmt.Errorf("could not find @%s among members", q)
		}
		username = q
	}
	if err := b.db.AddDynamicAdmin(targetID, username, viewerUserID); err != nil {
		return 0, err
	}
	// Права проверяются по кэшу в памяти, а не по базе: без перезагрузки
	// добавленный из мини-аппа admin оставался обычным человеком до рестарта
	// сервиса — со стороны это выглядело как «добавление не работает».
	b.reloadDynamicAdmins()
	// Нижняя клавиатура в личке зависит от прав — как и при добавлении из чата,
	// сбрасываем её кэш, иначе кнопки admin-панели не появятся.
	b.privateBottomKeyboardKind.Delete(targetID)
	return targetID, nil
}

// MiniappAdminRemoveAdmin — снять права. Права из окружения так не снимаются.
func (b *Bot) MiniappAdminRemoveAdmin(viewerUserID int64, initD initdata.InitData, targetID int64) error {
	if _, err := b.requireMiniappAdmin(viewerUserID, initD); err != nil {
		return err
	}
	if targetID == b.config.OwnerID {
		return fmt.Errorf("cannot remove owner")
	}
	isDynamic, err := b.db.IsDynamicAdmin(targetID)
	if err != nil {
		return err
	}
	if !isDynamic {
		return fmt.Errorf("this admin is in environment variables — remove there only")
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
		return 0, fmt.Errorf("could not parse time: use 2026-08-20T09:00")
	}
	if when.Before(time.Now()) {
		return 0, fmt.Errorf("time is in the past")
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
		return fmt.Errorf("enter a question")
	}
	clean := make([]string, 0, len(options))
	for _, opt := range options {
		v := strings.TrimSpace(opt)
		if v == "" {
			continue
		}
		if len([]rune(v)) > 100 {
			return fmt.Errorf("option %q is longer than 100 characters", string([]rune(v)[:20]))
		}
		clean = append(clean, v)
	}
	if len(clean) < 2 {
		return fmt.Errorf("need at least 2 options")
	}
	if len(clean) > 10 {
		return fmt.Errorf("maximum 10 options")
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
		return out, fmt.Errorf("MonetizedChatID not configured")
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
		return out, fmt.Errorf("MonetizedChatID not configured")
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
