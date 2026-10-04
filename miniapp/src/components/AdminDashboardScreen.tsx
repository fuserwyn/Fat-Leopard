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
  { days: 7, label: "7 дней" },
  { days: 30, label: "30 дней" },
  { days: 90, label: "90 дней" },
  { days: 0, label: "всё время" },
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
  return <p className="dash-muted">Нет данных за этот период.</p>;
}

function KpiGrid({ kpis, compared }: { kpis: AdminDashKpi[]; compared: boolean }) {
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">KPI</h3>
      <p className="dash-panel__subtitle">
        {compared ? "Изменение — к предыдущему периоду такой же длины." : "За всё время — сравнивать не с чем."}
      </p>
      <div className="dash-kpi">
        {kpis.map((k) => (
          <div key={k.key} className={`dash-kpi__cell dash-kpi__cell--${k.tone ?? "neutral"}`}>
            <small>{k.label}</small>
            <b>{k.value}</b>
            {k.delta ? <span className={`dash-kpi__delta dash-kpi__delta--${k.trend ?? "flat"}`}>{k.delta}</span> : null}
            {k.target ? <span className="dash-kpi__target">цель {k.target}</span> : null}
          </div>
        ))}
      </div>
    </section>
  );
}

function ActiveRow({ active }: { active: AdminDashboard["active"] }) {
  const cells = [
    { label: "за сутки", value: String(active.day) },
    { label: "за 7 дней", value: String(active.week) },
    { label: "за 30 дней", value: String(active.month) },
    { label: "сутки / месяц", value: active.stickiness },
  ];
  return (
    <section className="dash-panel">
      <h3 className="dash-panel__title">Тренируются сейчас</h3>
      <p className="dash-panel__subtitle">Сколько человек записали тренировку. От выбранного периода не зависит.</p>
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
            {series.total > 0 ? `всего ${series.total} · ` : ""}
            макс {max}
          </small>
        ) : null}
      </div>
      {max === 0 ? (
        <Empty />
      ) : (
        <>
          <div className="dash-series__bars" role="img" aria-label={`${series.label}: максимум ${max}`}>
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
      <h3 className="dash-panel__title">{weekly ? "Динамика по неделям" : "Динамика по дням"}</h3>
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
      <h3 className="dash-panel__title">Удержание по когортам</h3>
      <p className="dash-panel__subtitle">
        Неделя первой оплаты → доля тех, кто тренировался спустя 1, 7 и 30 дней после неё или позже. «—» — срок ещё не
        прошёл.
      </p>
      {cohorts.length === 0 ? (
        <Empty />
      ) : (
        <table className="dash-table">
          <thead>
            <tr>
              <th>Неделя</th>
              <th>Оплатили</th>
              <th>1 д</th>
              <th>7 д</th>
              <th>30 д</th>
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
      <h3 className="dash-panel__title">Деньги</h3>
      <p className="dash-panel__subtitle">Завершённые оплаты доступа и донаты за период.</p>
      {!hasAny ? (
        <Empty />
      ) : (
        <table className="dash-table">
          <thead>
            <tr>
              <th>Тип</th>
              <th>Оплат</th>
              <th>Сумма</th>
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
      <h3 className="dash-panel__title">Недельная цель стаи</h3>
      <p className="dash-panel__subtitle">Тренировки стаи за неделю и цель. Залитые столбики — цель достигнута.</p>
      {!hasAny ? (
        <Empty />
      ) : (
        <div className="dash-pack">
          {ordered.map((w) => (
            <div key={w.week_start} className="dash-pack__col" title={`${w.workouts} из ${w.goal}`}>
              <b>{w.workouts}</b>
              <div className="dash-pack__track">
                <span className="dash-pack__goal" style={{ bottom: `${(w.goal / max) * 100}%` }} aria-hidden />
                <span
                  className={`dash-pack__bar ${w.reached ? "is-reached" : ""} ${w.current ? "is-current" : ""}`}
                  style={{ height: `${(w.workouts / max) * 100}%` }}
                />
              </div>
              <small>{dashBucketLabel(w.week_start, true)}</small>
              <small className="dash-pack__goal-label">из {w.goal}</small>
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
      <h3 className="dash-panel__title">Каналы</h3>
      <p className="dash-panel__subtitle">Откуда пришли и сколько из них оплатили.</p>
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
                    оплат {c.paid}
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
          ? `Период: ${analytics.period} · последнее событие ${analytics.last_event_at} (МСК)`
          : `Период: ${analytics.period}`,
      );
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Не удалось загрузить дашборды");
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
      {loading ? <p className="dash-muted">Загрузка…</p> : null}
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
            Посещения бота за период: {dash.visits.visits} · уникальных {dash.visits.unique}
          </p>
        </>
      ) : null}

      <p className="dash-muted dash-hint">Подробные таблицы — в разделе «Аналитика».</p>
    </div>
  );
}
