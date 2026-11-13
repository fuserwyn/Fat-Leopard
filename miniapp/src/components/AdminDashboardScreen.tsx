import { useCallback, useEffect, useState } from "react";
import {
  fetchAdminAnalytics,
  type AdminDashboard,
  type AdminDashKpi,
  type AdminDashSeries,
  type AdminTable,
} from "../lib/adminApi";
import { dashBucketLabel, dashBarHeights } from "../lib/adminDashboard";
import "./AdminDashboardScreen.css";

type Props = {
  initData: string;
  showAlert: (text: string) => void;
};

const PERIODS = [
  { days: 7, label: "7 days" },
  { days: 30, label: "30 days" },
  { days: 90, label: "90 days" },
  { days: 0, label: "all time" },
];

function parseCount(raw: string): number {
  const n = Number.parseInt(raw.replace(/\s/g, ""), 10);
  return Number.isFinite(n) ? n : 0;
}

/** Заголовок без ведущего эмодзи-маркера («1️⃣ Воронка» → «Воронка»). */
function plainTitle(title: string) {
  return title.replace(/^[^\s]+\s/, "");
}

function Empty() {
  return <p className="dash-muted">No data for this period.</p>;
}

function KpiGrid({ kpis, compared }: { kpis: AdminDashKpi[]; compared: boolean }) {
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">KPI</h3>
      <p className="dash-panel__subtitle">
        {compared ? "Change vs previous period of the same length." : "All time — nothing to compare."}
      </p>
      <div className="dash-kpi">
        {kpis.map((k) => (
          <div key={k.key} className={`dash-kpi__cell dash-kpi__cell--${k.tone ?? "neutral"}`}>
            <small>{k.label}</small>
            <b>{k.value}</b>
            {k.delta ? <span className={`dash-kpi__delta dash-kpi__delta--${k.trend ?? "flat"}`}>{k.delta}</span> : null}
            {k.target ? <span className="dash-kpi__target">goal {k.target}</span> : null}
          </div>
        ))}
      </div>
    </section>
  );
}

function ActiveRow({ active }: { active: AdminDashboard["active"] }) {
  const cells = [
    { label: "last day", value: String(active.day) },
    { label: "last 7 days", value: String(active.week) },
    { label: "last 30 days", value: String(active.month) },
    { label: "D1 / M1", value: active.stickiness },
  ];
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">Active now</h3>
      <p className="dash-panel__subtitle">How many people logged a workout. Independent of selected period.</p>
      <div className="dash-stats dash-stats--four">
        {cells.map((c) => (
          <div key={c.label} className="dash-stats__cell">
            <b>{c.value}</b>
            <small>{c.label}</small>
          </div>
        ))}
      </div>
    </section>
  );
}

function SeriesChart({ series, weekly }: { series: AdminDashSeries; weekly: boolean }) {
  const heights = dashBarHeights(series.points.map((p) => p.value));
  const max = Math.max(0, ...series.points.map((p) => p.value));
  const first = series.points[0];
  const last = series.points[series.points.length - 1];
  return (
    <div className="dash-series">
      <div className="dash-series__head">
        <span>{series.label}</span>
        {max > 0 ? (
          <small>
            {series.total > 0 ? `total ${series.total} · ` : ""}
            max {max}
          </small>
        ) : null}
      </div>
      {max === 0 ? (
        <Empty />
      ) : (
        <>
          <div className="dash-series__bars" role="img" aria-label={`${series.label}: max ${max}`}>
            {series.points.map((p, i) => (
              <span
                key={p.bucket}
                className="dash-series__bar"
                style={{ height: `${heights[i]}%` }}
                title={`${dashBucketLabel(p.bucket, weekly)}: ${p.value}`}
              />
            ))}
          </div>
          <div className="dash-series__axis">
            <span>{first ? dashBucketLabel(first.bucket, weekly) : ""}</span>
            <span>{last ? dashBucketLabel(last.bucket, weekly) : ""}</span>
          </div>
        </>
      )}
    </div>
  );
}

function SeriesPanel({ dash }: { dash: AdminDashboard }) {
  const weekly = dash.series_bucket === "week";
  const series = dash.series ?? [];
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">{weekly ? "Trend by week" : "Trend by day"}</h3>
      {series.length === 0 ? (
        <Empty />
      ) : (
        <div className="dash-series-list">
          {series.map((s) => (
            <SeriesChart key={s.key} series={s} weekly={weekly} />
          ))}
        </div>
      )}
    </section>
  );
}

function FunnelChart({ table }: { table: AdminTable }) {
  const stages = table.rows.map((row) => ({
    label: row[0] ?? "",
    users: parseCount(row[1] ?? "0"),
    conv: row[2] ?? "—",
  }));
  const max = Math.max(1, ...stages.map((s) => s.users));

  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">{plainTitle(table.title)}</h3>
      {table.subtitle ? <p className="dash-panel__subtitle">{table.subtitle}</p> : null}
      {stages.every((s) => s.users === 0) ? (
        <Empty />
      ) : (
        <ul className="dash-funnel">
          {stages.map((s) => (
            <li key={s.label} className="dash-funnel__row">
              <div className="dash-funnel__head">
                <span className="dash-funnel__label">{s.label}</span>
                <span className="dash-funnel__meta">
                  <b>{s.users}</b>
                  {s.conv !== "—" ? <small>{s.conv}</small> : null}
                </span>
              </div>
              <div className="dash-funnel__track" aria-hidden>
                <div className="dash-funnel__fill" style={{ width: `${(s.users / max) * 100}%` }} />
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function CohortTable({ cohorts }: { cohorts: NonNullable<AdminDashboard["cohorts"]> }) {
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">Cohort retention</h3>
      <p className="dash-panel__subtitle">
        Week of first payment → share who worked out 1, 7, and 30 days after or later. «—» — period not over yet.
      </p>
      {cohorts.length === 0 ? (
        <Empty />
      ) : (
        <table className="dash-table">
          <thead>
            <tr>
              <th>Week</th>
              <th>Paid</th>
              <th>1d</th>
              <th>7d</th>
              <th>30d</th>
            </tr>
          </thead>
          <tbody>
            {cohorts.map((c) => (
              <tr key={c.week_start}>
                <td>{dashBucketLabel(c.week_start, true)}</td>
                <td>{c.size}</td>
                <td>{c.d1}</td>
                <td>{c.d7}</td>
                <td>{c.d30}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function RetentionBars({ table }: { table: AdminTable }) {
  const rows = table.rows.map((row) => ({
    label: row[0] ?? "",
    users: parseCount(row[1] ?? "0"),
  }));
  const max = Math.max(1, ...rows.map((r) => r.users));

  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">{plainTitle(table.title)}</h3>
      {table.subtitle ? <p className="dash-panel__subtitle">{table.subtitle}</p> : null}
      {rows.every((r) => r.users === 0) ? (
        <Empty />
      ) : (
        <ul className="dash-retention">
          {rows.map((r) => (
            <li key={r.label} className="dash-retention__row">
              <span className="dash-retention__label">{r.label}</span>
              <div className="dash-retention__track" aria-hidden>
                <div className="dash-retention__fill" style={{ width: `${(r.users / max) * 100}%` }} />
              </div>
              <span className="dash-retention__val">{r.users}</span>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

function MoneyPanel({ money }: { money: NonNullable<AdminDashboard["money"]> }) {
  const hasAny = money.some((m) => m.count !== "0");
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">Money</h3>
      <p className="dash-panel__subtitle">Completed access payments and donations for the period.</p>
      {!hasAny ? (
        <Empty />
      ) : (
        <table className="dash-table">
          <thead>
            <tr>
              <th>Type</th>
              <th>Payments</th>
              <th>Amount</th>
            </tr>
          </thead>
          <tbody>
            {money.map((m) => (
              <tr key={m.label}>
                <td>{m.label}</td>
                <td>{m.count}</td>
                <td>{m.amount}</td>
              </tr>
            ))}
          </tbody>
        </table>
      )}
    </section>
  );
}

function PackWeeksPanel({ weeks }: { weeks: NonNullable<AdminDashboard["pack_weeks"]> }) {
  // Сервер отдаёт от новых к старым — рисуем слева направо по времени.
  const ordered = [...weeks].reverse();
  const max = Math.max(1, ...ordered.map((w) => Math.max(w.workouts, w.goal)));
  const hasAny = ordered.some((w) => w.workouts > 0);
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">Pack weekly goal</h3>
      <p className="dash-panel__subtitle">Pack workouts per week vs goal. Filled bars = goal reached.</p>
      {!hasAny ? (
        <Empty />
      ) : (
        <div className="dash-pack">
          {ordered.map((w) => (
            <div key={w.week_start} className="dash-pack__col" title={`${w.workouts} of ${w.goal}`}>
              <b>{w.workouts}</b>
              <div className="dash-pack__track">
                <span className="dash-pack__goal" style={{ bottom: `${(w.goal / max) * 100}%` }} aria-hidden />
                <span
                  className={`dash-pack__bar ${w.reached ? "is-reached" : ""} ${w.current ? "is-current" : ""}`}
                  style={{ height: `${(w.workouts / max) * 100}%` }}
                />
              </div>
              <small>{dashBucketLabel(w.week_start, true)}</small>
              <small className="dash-pack__goal-label">of {w.goal}</small>
            </div>
          ))}
        </div>
      )}
    </section>
  );
}

function ChannelsPanel({ channels }: { channels: NonNullable<AdminDashboard["channels"]> }) {
  const max = Math.max(1, ...channels.map((c) => c.started));
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">Channels</h3>
      <p className="dash-panel__subtitle">Where users came from and how many paid.</p>
      {channels.length === 0 ? (
        <Empty />
      ) : (
        <ul className="dash-funnel">
          {channels.map((c) => (
            <li key={c.source} className="dash-funnel__row">
              <div className="dash-funnel__head">
                <span className="dash-funnel__label">{c.source}</span>
                <span className="dash-funnel__meta">
                  <b>{c.started}</b>
                  <small>
                    paid {c.paid}
                    {c.conv !== "—" ? ` · ${c.conv}` : ""}
                  </small>
                </span>
              </div>
              <div className="dash-funnel__track" aria-hidden>
                <div className="dash-funnel__fill" style={{ width: `${(c.started / max) * 100}%` }} />
              </div>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}

/** Визуальные дашборды: KPI со сравнением, динамика, воронки, когорты, деньги, цель стаи и каналы. */
export function AdminDashboardScreen({ initData, showAlert }: Props) {
  const [period, setPeriod] = useState(30);
  const [loading, setLoading] = useState(true);
  const [note, setNote] = useState("");
  const [tables, setTables] = useState<AdminTable[]>([]);
  const [dash, setDash] = useState<AdminDashboard | null>(null);

  const load = useCallback(async () => {
    setLoading(true);
    try {
      const { analytics } = await fetchAdminAnalytics(initData, period, true);
      setTables(analytics.tables ?? []);
      setDash(analytics.dashboard ?? null);
      setNote(
        analytics.last_event_at
          ? `Period: ${analytics.period} · last event ${analytics.last_event_at} (MSK)`
          : `Period: ${analytics.period}`,
      );
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Couldn't load dashboards");
    } finally {
      setLoading(false);
    }
  }, [initData, period, showAlert]);

  useEffect(() => {
    void load();
  }, [load]);

  const funnelTables = tables.filter((t) => t.kind === "funnel");
  const retentionTable = tables.find((t) => t.kind === "retention");

  return (
    <div className="dash">
      <div className="dash-periods">
        {PERIODS.map((p) => (
          <button
            key={p.days}
            type="button"
            className={period === p.days ? "on" : ""}
            onClick={() => setPeriod(p.days)}
          >
            {p.label}
          </button>
        ))}
      </div>
      {loading ? <p className="dash-muted">Loading…</p> : null}
      {!loading && note ? <p className="dash-muted">{note}</p> : null}

      {!loading && dash ? (
        <>
          {dash.kpis && dash.kpis.length > 0 ? <KpiGrid kpis={dash.kpis} compared={dash.compared} /> : null}
          <ActiveRow active={dash.active} />
          <SeriesPanel dash={dash} />
        </>
      ) : null}

      {!loading ? funnelTables.map((t) => <FunnelChart key={t.title} table={t} />) : null}

      {!loading && dash ? <CohortTable cohorts={dash.cohorts ?? []} /> : null}
      {!loading && retentionTable ? <RetentionBars table={retentionTable} /> : null}

      {!loading && dash ? (
        <>
          <MoneyPanel money={dash.money ?? []} />
          <PackWeeksPanel weeks={dash.pack_weeks ?? []} />
          <ChannelsPanel channels={dash.channels ?? []} />
          <p className="dash-muted">
            Bot visits in period: {dash.visits.visits} · unique {dash.visits.unique}
          </p>
        </>
      ) : null}

      <p className="dash-muted dash-hint">Detailed tables — in Analytics.</p>
    </div>
  );
}
