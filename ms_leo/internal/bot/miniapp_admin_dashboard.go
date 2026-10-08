package bot

import (
	"fmt"
	"strconv"
	"time"

	"leo-bot/internal/database"
	"leo-bot/internal/utils"
)

// Дашборды админки: KPI со сравнением с прошлым периодом, ряды по дням,
// когорты, деньги, недельная цель стаи и каналы. Считается на сервере,
// мини-апп только рисует.

const (
	dashCohortWeeks    = 8
	dashPackWeeks      = 8
	dashMaxWeekBuckets = 26
	dashDateLayout     = "2006-01-02"
)

type MiniappAdminDashKPI struct {
	Key    string `json:"key"`
	Label  string `json:"label"`
	Value  string `json:"value"`
	Delta  string `json:"delta,omitempty"`
	Trend  string `json:"trend,omitempty"` // up | down | flat
	Target string `json:"target,omitempty"`
	Tone   string `json:"tone,omitempty"` // good | bad
}

type MiniappAdminDashPoint struct {
	Bucket string `json:"bucket"`
	Value  int64  `json:"value"`
}

type MiniappAdminDashSeries struct {
	Key    string                  `json:"key"`
	Label  string                  `json:"label"`
	Total  int64                   `json:"total"`
	Points []MiniappAdminDashPoint `json:"points"`
}

type MiniappAdminDashActive struct {
	Day        int64  `json:"day"`
	Week       int64  `json:"week"`
	Month      int64  `json:"month"`
	Stickiness string `json:"stickiness"`
}

type MiniappAdminDashCohort struct {
	WeekStart string `json:"week_start"`
	Size      int64  `json:"size"`
	D1        string `json:"d1"`
	D7        string `json:"d7"`
	D30       string `json:"d30"`
}

type MiniappAdminDashMoney struct {
	Label  string `json:"label"`
	Count  string `json:"count"`
	Amount string `json:"amount"`
}

type MiniappAdminDashPackWeek struct {
	WeekStart string `json:"week_start"`
	Workouts  int    `json:"workouts"`
	Goal      int    `json:"goal"`
	Reached   bool   `json:"reached"`
	Current   bool   `json:"current"`
}

type MiniappAdminDashChannel struct {
	Source  string `json:"source"`
	Started int64  `json:"started"`
	Paid    int64  `json:"paid"`
	Conv    string `json:"conv"`
}

type MiniappAdminDashVisits struct {
	Visits int64 `json:"visits"`
	Unique int64 `json:"unique"`
}

type MiniappAdminDashboard struct {
	// Compared — есть ли сравнение с предыдущим периодом (для «всё время» нет).
	Compared     bool                       `json:"compared"`
	KPIs         []MiniappAdminDashKPI      `json:"kpis"`
	SeriesBucket string                     `json:"series_bucket"` // day | week
	Series       []MiniappAdminDashSeries   `json:"series"`
	Active       MiniappAdminDashActive     `json:"active"`
	Cohorts      []MiniappAdminDashCohort   `json:"cohorts"`
	Money        []MiniappAdminDashMoney    `json:"money"`
	PackWeeks    []MiniappAdminDashPackWeek `json:"pack_weeks"`
	Channels     []MiniappAdminDashChannel  `json:"channels"`
	Visits       MiniappAdminDashVisits     `json:"visits"`
}

// dashCountDelta — изменение счётчика к прошлому периоду в процентах.
func dashCountDelta(cur, prev int64) (delta, trend string) {
	switch {
	case prev <= 0 && cur <= 0:
		return "", ""
	case prev <= 0:
		return "новое", "up"
	}
	pct := float64(cur-prev) / float64(prev) * 100
	return dashSigned(pct, 0) + "%", dashTrend(pct)
}

// dashRateDelta — изменение доли к прошлому периоду в процентных пунктах.
func dashRateDelta(curNum, curDen, prevNum, prevDen int64) (delta, trend string) {
	if curDen <= 0 || prevDen <= 0 {
		return "", ""
	}
	pp := (float64(curNum)/float64(curDen) - float64(prevNum)/float64(prevDen)) * 100
	return dashSigned(pp, 1) + " п.п.", dashTrend(pp)
}

func dashSigned(v float64, prec int) string {
	s := strconv.FormatFloat(v, 'f', prec, 64)
	if v > 0 && s != "0" && s != "0.0" {
		return "+" + s
	}
	if s == "-0" || s == "-0.0" {
		return s[1:]
	}
	// Типографский минус вместо дефиса.
	if len(s) > 0 && s[0] == '-' {
		return "−" + s[1:]
	}
	return s
}

func dashTrend(v float64) string {
	switch {
	case v >= 0.05:
		return "up"
	case v <= -0.05:
		return "down"
	}
	return "flat"
}

// dashSeriesBuckets — начала корзин ряда (YYYY-MM-DD) до сегодняшней включительно.
// До 30 дней — по дням, дольше и «всё время» — по неделям с понедельника.
func dashSeriesBuckets(now time.Time, days int) (weekly bool, buckets []string) {
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	if days > 0 && days <= 30 {
		for i := days - 1; i >= 0; i-- {
			buckets = append(buckets, today.AddDate(0, 0, -i).Format(dashDateLayout))
		}
		return false, buckets
	}
	monday := today.AddDate(0, 0, -((int(today.Weekday()) + 6) % 7))
	n := dashMaxWeekBuckets
	if days > 0 {
		n = (days + 6) / 7
	}
	for i := n - 1; i >= 0; i-- {
		buckets = append(buckets, monday.AddDate(0, 0, -7*i).Format(dashDateLayout))
	}
	return true, buckets
}

// dashCohortRate — доля когорты, тренировавшейся спустя gapDays после оплаты.
// «—», пока у последнего оплатившего в когорте этот срок ещё не прошёл.
func dashCohortRate(n, size int64, weekStart string, gapDays int, now time.Time) string {
	wk, err := time.ParseInLocation(dashDateLayout, weekStart, now.Location())
	if err != nil || size <= 0 {
		return "—"
	}
	if now.Before(wk.AddDate(0, 0, 7+gapDays)) {
		return "—"
	}
	return analyticsPct(n, size)
}

func (b *Bot) buildAdminDashboard(days int, kpis []MiniappAdminDashKPI) MiniappAdminDashboard {
	out := MiniappAdminDashboard{Compared: days > 0}
	now := utils.GetMoscowTime()

	out.KPIs = kpis
	out.SeriesBucket, out.Series = b.dashSeries(now, days)

	if day, week, month, err := b.db.ActiveUserCounts(); err == nil {
		out.Active = MiniappAdminDashActive{Day: day, Week: week, Month: month, Stickiness: analyticsPct(day, month)}
	} else {
		b.logger.Warnf("dashboard active users: %v", err)
	}

	thisMonday := utils.WeekStartMondayMSK(now)
	out.Cohorts = b.dashCohorts(now, thisMonday)
	out.PackWeeks = b.dashPackWeeks(now, thisMonday)

	packChatID := b.adminPackChatID()
	if sums, err := b.db.AdminSumCompletedMoney(packChatID, time.Now().AddDate(0, 0, -days), days > 0); err == nil {
		for _, row := range adminBuildMoneyStatsTable(sums).Rows {
			if len(row) >= 3 {
				out.Money = append(out.Money, MiniappAdminDashMoney{Label: row[0], Count: row[1], Amount: row[2]})
			}
		}
	} else {
		b.logger.Warnf("dashboard money: %v", err)
	}

	if channels, err := b.db.GetChannelAttribution(days); err == nil {
		for _, c := range channels {
			out.Channels = append(out.Channels, MiniappAdminDashChannel{
				Source: c.Source, Started: c.Started, Paid: c.Paid, Conv: analyticsPct(c.Paid, c.Started),
			})
		}
	}

	if visits, unique, err := b.db.BotVisitWindowStats(days); err == nil {
		out.Visits = MiniappAdminDashVisits{Visits: visits, Unique: unique}
	}
	return out
}

// dashKPIs — счётчики и конверсии периода с изменением к предыдущему периоду.
func (b *Bot) dashKPIs(days int) []MiniappAdminDashKPI {
	cur, err := b.db.EventWindowCounts(days, 0)
	if err != nil {
		b.logger.Warnf("dashboard kpi counts: %v", err)
		return nil
	}
	var prev map[string]database.EventWindowCount
	if days > 0 {
		prev, _ = b.db.EventWindowCounts(days, days)
	}

	var out []MiniappAdminDashKPI
	count := func(key, label string, c, p int64) {
		k := MiniappAdminDashKPI{Key: key, Label: label, Value: strconv.FormatInt(c, 10)}
		if prev != nil {
			k.Delta, k.Trend = dashCountDelta(c, p)
		}
		out = append(out, k)
	}
	count("active", "Тренировались", cur[database.EventWorkoutLogged].Unique, prev[database.EventWorkoutLogged].Unique)
	count("workouts", "Тренировок", cur[database.EventWorkoutLogged].Total, prev[database.EventWorkoutLogged].Total)
	count("starts", "Новых стартов", cur[database.EventBotStarted].Unique, prev[database.EventBotStarted].Unique)
	count("payments", "Оплат", cur[database.EventPaymentCompleted].Total, prev[database.EventPaymentCompleted].Total)

	rate := func(key, label, from, to string, firstEver bool, target string) {
		c, err := b.db.SequentialFunnelCounts([]string{from, to}, days, 0, firstEver)
		if err != nil || len(c) < 2 {
			return
		}
		k := MiniappAdminDashKPI{Key: key, Label: label, Value: analyticsPct(c[1], c[0]), Target: target}
		if days > 0 {
			if p, err := b.db.SequentialFunnelCounts([]string{from, to}, days, days, firstEver); err == nil && len(p) == 2 {
				k.Delta, k.Trend = dashRateDelta(c[1], c[0], p[1], p[0])
			}
		}
		if key == "activation" && c[0] > 0 {
			switch pct := float64(c[1]) / float64(c[0]) * 100; {
			case pct >= 35:
				k.Tone = "good"
			case pct < 20:
				k.Tone = "bad"
			}
		}
		out = append(out, k)
	}
	rate("activation", "Активация (трен/опл)", database.EventPaymentCompleted, database.EventWorkoutLogged, true, ">35%")
	rate("paywall_paid", "Пэйвол→оплата", database.EventPaywallViewed, database.EventPaymentCompleted, true, "")
	rate("start_paywall", "Старт→пэйвол", database.EventBotStarted, database.EventPaywallViewed, true, "")
	rate("paid_miniapp", "Оплата→миниапп", database.EventPaymentCompleted, database.EventMiniappOpened, true, "")
	rate("burn_recovery", "Burn recovery", database.EventBurnWarningSent, database.EventBurnRecovered, false, "")
	rate("reactivation", "Реактивация", database.EventAccountDeletedInactivity, database.EventAccountReactivated, false, "")
	return out
}

func (b *Bot) dashSeries(now time.Time, days int) (string, []MiniappAdminDashSeries) {
	weekly, buckets := dashSeriesBuckets(now, days)
	unit := "day"
	if weekly {
		unit = "week"
	}
	if len(buckets) == 0 {
		return unit, nil
	}
	rows, err := b.db.EventSeries(buckets[0], weekly)
	if err != nil {
		b.logger.Warnf("dashboard series: %v", err)
		return unit, nil
	}
	byBucket := make(map[string]database.EventSeriesRow, len(rows))
	for _, r := range rows {
		byBucket[r.Bucket] = r
	}
	series := []MiniappAdminDashSeries{
		{Key: "active", Label: "Тренировались"},
		{Key: "workouts", Label: "Тренировок"},
		{Key: "starts", Label: "Новых стартов"},
		{Key: "payments", Label: "Оплат"},
	}
	for _, bucket := range buckets {
		r := byBucket[bucket]
		for i, v := range []int64{r.ActiveUsers, r.Workouts, r.Starts, r.Payments} {
			series[i].Points = append(series[i].Points, MiniappAdminDashPoint{Bucket: bucket, Value: v})
			series[i].Total += v
		}
	}
	// «Тренировались» по корзинам не складывается в уникальных за период.
	series[0].Total = 0
	return unit, series
}

func (b *Bot) dashCohorts(now time.Time, thisMonday string) []MiniappAdminDashCohort {
	monday, err := time.ParseInLocation(dashDateLayout, thisMonday, now.Location())
	if err != nil {
		return nil
	}
	from := monday.AddDate(0, 0, -7*(dashCohortWeeks-1)).Format(dashDateLayout)
	rows, err := b.db.PaidCohortRetention(from)
	if err != nil {
		b.logger.Warnf("dashboard cohorts: %v", err)
		return nil
	}
	out := make([]MiniappAdminDashCohort, 0, len(rows))
	for _, r := range rows {
		out = append(out, MiniappAdminDashCohort{
			WeekStart: r.WeekStart,
			Size:      r.Size,
			D1:        dashCohortRate(r.D1, r.Size, r.WeekStart, 1, now),
			D7:        dashCohortRate(r.D7, r.Size, r.WeekStart, 7, now),
			D30:       dashCohortRate(r.D30, r.Size, r.WeekStart, 30, now),
		})
	}
	return out
}

// dashPackWeeks — последние недели стаи: сколько тренировок, цель, достигнута ли.
// Для прошлых недель цель берётся из итогов недели (pack_week_summaries), а если
// итогов нет (недели до их появления) — цель по умолчанию для той недели.
func (b *Bot) dashPackWeeks(now time.Time, thisMonday string) []MiniappAdminDashPackWeek {
	packChatID := b.adminPackChatID()
	monday, err := time.ParseInLocation(dashDateLayout, thisMonday, now.Location())
	if err != nil || packChatID == 0 {
		return nil
	}
	from := monday.AddDate(0, 0, -7*(dashPackWeeks-1)).Format(dashDateLayout)
	counts, err := b.db.PackWeeklyWorkoutHistory(packChatID, from)
	if err != nil {
		b.logger.Warnf("dashboard pack weeks: %v", err)
		return nil
	}
	bonus, _ := b.db.PackWeeklyGoalBonusWeeks(packChatID, from)
	summaries, _ := b.db.ListPackWeekSummaries(packChatID, from)
	summaryGoal := make(map[string]int, len(summaries))
	for _, s := range summaries {
		summaryGoal[s.WeekStart] = s.Goal
	}
	out := make([]MiniappAdminDashPackWeek, 0, dashPackWeeks)
	for i := 0; i < dashPackWeeks; i++ {
		wk := monday.AddDate(0, 0, -7*i).Format(dashDateLayout)
		goal := PackWeeklyWorkoutGoalForWeek(wk)
		if g := summaryGoal[wk]; g > 0 {
			goal = g
		}
		if i == 0 {
			goal = b.packWeeklyWorkoutGoal(packChatID, wk)
		}
		out = append(out, MiniappAdminDashPackWeek{
			WeekStart: wk,
			Workouts:  counts[wk],
			Goal:      goal,
			Reached:   bonus[wk] || counts[wk] >= goal,
			Current:   i == 0,
		})
	}
	return out
}

// dashFunnelTable — последовательная воронка в виде таблицы «как в чате».
func (b *Bot) dashFunnelTable(title string, days int, stages [][2]string, subtitle func(counts []int64) string) (MiniappAdminTable, error) {
	names := make([]string, len(stages))
	for i, s := range stages {
		names[i] = s[0]
	}
	counts, err := b.db.SequentialFunnelCounts(names, days, 0, true)
	if err != nil {
		return MiniappAdminTable{}, err
	}
	tbl := MiniappAdminTable{Kind: "funnel", Title: title, Subtitle: subtitle(counts), Columns: []string{"Стадия", "Юзеры", "Конв"}}
	for i, s := range stages {
		conv := "—"
		if i > 0 {
			conv = analyticsPct(counts[i], counts[i-1])
		}
		tbl.Rows = append(tbl.Rows, []string{s[1], strconv.FormatInt(counts[i], 10), conv})
	}
	return tbl, nil
}

func dashFunnelNote(text string) string {
	return fmt.Sprintf("%s · считаем тех, кто вошёл в воронку в этом периоде", text)
}
