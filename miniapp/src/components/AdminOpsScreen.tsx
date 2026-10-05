import { useCallback, useEffect, useState } from "react";
import {
  addAdminPerson,
  addAdminScheduledPost,
  cancelAdminScheduledPost,
  fetchAdminAdmins,
  fetchAdminAnalytics,
  fetchAdminPayments,
  fetchAdminScheduledPosts,
  fetchAdminVisits,
  publishAdminPoll,
  removeAdminPerson,
  wipePackFeed,
  type AdminPaymentsKind,
  type AdminPaymentsOrder,
  type AdminPerson,
  type AdminScheduledPost,
  type AdminTable,
  type AdminWipeCounts,
} from "../lib/adminApi";
import {
  PAYMENTS_PERIOD_MODES,
  canShiftPaymentsForward,
  paymentsPeriodRange,
  paymentsPeriodTitle,
  shiftPaymentsAnchor,
  todayMsk,
  type PaymentsPeriodMode,
} from "../lib/adminPaymentsPeriod";
import { tgConfirm } from "../lib/tgConfirm";
import "./AdminOpsScreen.css";

/** Разделы админки, которые раньше были только в чате (/admin). */
export type AdminOpsSection = "analytics" | "visits" | "payments" | "admins" | "scheduled" | "poll" | "wipe";

type Props = {
  section: AdminOpsSection;
  initData: string;
  showAlert: (text: string) => void;
};

const PERIODS = [
  { days: 7, label: "7 дней" },
  { days: 30, label: "30 дней" },
  { days: 90, label: "90 дней" },
  { days: 0, label: "всё время" },
];

const PAYMENTS_PAGE = 20;

const PAYMENT_KINDS: { kind: AdminPaymentsKind; label: string }[] = [
  { kind: "", label: "Все" },
  { kind: "access", label: "Платежи" },
  { kind: "donation", label: "Донаты" },
];

const PAYMENT_ORDERS: { order: AdminPaymentsOrder; label: string }[] = [
  { order: "desc", label: "Сначала новые" },
  { order: "asc", label: "Сначала старые" },
];

function Table({ table }: { table: AdminTable }) {
  return (
    <section className="ops-table">
      {table.title ? <h3 className="ops-table__title">{table.title}</h3> : null}
      {table.subtitle ? <p className="ops-table__subtitle">{table.subtitle}</p> : null}
      <div className="ops-table__scroll">
        <table>
          <thead>
            <tr>
              {table.columns.map((c) => (
                <th key={c}>{c}</th>
              ))}
            </tr>
          </thead>
          <tbody>
            {table.rows.map((row, i) => (
              <tr key={i}>
                {row.map((cell, j) => (
                  <td key={j}>{cell}</td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      {table.rows.length === 0 ? <p className="ops-muted">Пусто</p> : null}
    </section>
  );
}

export function AdminOpsScreen({ section, initData, showAlert }: Props) {
  const [loading, setLoading] = useState(true);
  const [busy, setBusy] = useState(false);
  const [tables, setTables] = useState<AdminTable[]>([]);
  const [period, setPeriod] = useState(30);
  const [note, setNote] = useState("");

  const [paymentsOffset, setPaymentsOffset] = useState(0);
  const [paymentsTotal, setPaymentsTotal] = useState(0);
  const [paymentsKind, setPaymentsKind] = useState<AdminPaymentsKind>("");
  const [paymentsPaidOnly, setPaymentsPaidOnly] = useState(false);
  const [paymentsOrder, setPaymentsOrder] = useState<AdminPaymentsOrder>("desc");
  const [paymentsPeriodMode, setPaymentsPeriodMode] = useState<PaymentsPeriodMode>("all");
  const [paymentsAnchor, setPaymentsAnchor] = useState(() => todayMsk());
  const [paymentsCustomFrom, setPaymentsCustomFrom] = useState("");
  const [paymentsCustomTo, setPaymentsCustomTo] = useState("");
  const [paymentsQueryDraft, setPaymentsQueryDraft] = useState("");
  const [paymentsQuery, setPaymentsQuery] = useState("");

  const paymentsRange =
    paymentsPeriodMode === "custom"
      ? { from: paymentsCustomFrom, to: paymentsCustomTo }
      : paymentsPeriodRange(paymentsPeriodMode, paymentsAnchor);
  const paymentsFrom = paymentsRange.from;
  const paymentsTo = paymentsRange.to;

  const [admins, setAdmins] = useState<AdminPerson[]>([]);
  const [adminQuery, setAdminQuery] = useState("");

  const [posts, setPosts] = useState<AdminScheduledPost[]>([]);
  const [postText, setPostText] = useState("");
  const [postAuthor, setPostAuthor] = useState<"admin" | "leo">("admin");
  const [postAt, setPostAt] = useState("");

  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState("Да | Нет");

  const [wipe, setWipe] = useState<AdminWipeCounts | null>(null);

  const fail = useCallback(
    (e: unknown, fallback: string) => showAlert(e instanceof Error ? e.message : fallback),
    [showAlert],
  );

  const load = useCallback(async () => {
    setLoading(true);
    try {
      if (section === "analytics") {
        const j = await fetchAdminAnalytics(initData, period);
        setTables(j.analytics.tables ?? []);
        setNote(
          j.analytics.last_event_at
            ? `Период: ${j.analytics.period} · последнее событие ${j.analytics.last_event_at}`
            : `Период: ${j.analytics.period}`,
        );
      } else if (section === "visits") {
        const j = await fetchAdminVisits(initData);
        setTables(j.tables ?? []);
      } else if (section === "payments") {
        const j = await fetchAdminPayments(initData, paymentsOffset, PAYMENTS_PAGE, {
          kind: paymentsKind,
          completedOnly: paymentsPaidOnly,
          query: paymentsQuery,
          from: paymentsFrom,
          to: paymentsTo,
          order: paymentsOrder,
        });
        const p = j.payments;
        const shown = p.table?.rows?.length ?? 0;
        setTables(
          [p.stats, p.payers, p.table]
            .filter((t): t is AdminTable => Boolean(t))
            .map((t) => ({ ...t, rows: t.rows ?? [] })),
        );
        setPaymentsTotal(p.total);
        const filtered = Boolean(paymentsQuery || paymentsFrom || paymentsTo);
        const what =
          paymentsKind === "access" ? "Платежей" : paymentsKind === "donation" ? "Донатов" : "Оплат и донатов";
        const empty = filtered ? `${what} по этим фильтрам нет` : `${what} пока нет`;
        setNote(p.total > 0 ? `Строки ${paymentsOffset + 1}–${paymentsOffset + shown} из ${p.total}` : empty);
      } else if (section === "admins") {
        const j = await fetchAdminAdmins(initData);
        setAdmins(j.admins ?? []);
      } else if (section === "scheduled") {
        const j = await fetchAdminScheduledPosts(initData);
        setPosts(j.posts ?? []);
      } else if (section === "wipe") {
        const j = await wipePackFeed(initData, false);
        setWipe(j.counts);
      }
    } catch (e) {
      fail(e, "Не удалось загрузить раздел");
    } finally {
      setLoading(false);
    }
  }, [
    section,
    initData,
    period,
    paymentsOffset,
    paymentsKind,
    paymentsPaidOnly,
    paymentsQuery,
    paymentsFrom,
    paymentsTo,
    paymentsOrder,
    fail,
  ]);

  useEffect(() => {
    void load();
  }, [load]);

  const addAdmin = async () => {
    const q = adminQuery.trim();
    if (!q) {
      showAlert("Укажи @ник или id.");
      return;
    }
    setBusy(true);
    try {
      const j = await addAdminPerson(initData, q);
      setAdminQuery("");
      showAlert(`Выдал права: ${j.user_id}`);
      await load();
    } catch (e) {
      fail(e, "Не удалось выдать права");
    } finally {
      setBusy(false);
    }
  };

  const dropAdmin = async (person: AdminPerson) => {
    const label = person.username ? `@${person.username}` : String(person.user_id);
    if (!(await tgConfirm(`Снять права у ${label}?`))) return;
    setBusy(true);
    try {
      await removeAdminPerson(initData, person.user_id);
      showAlert("Права сняты.");
      await load();
    } catch (e) {
      fail(e, "Не удалось снять права");
    } finally {
      setBusy(false);
    }
  };

  const schedulePost = async () => {
    const text = postText.trim();
    if (!text || !postAt) {
      showAlert("Нужен текст и время.");
      return;
    }
    setBusy(true);
    try {
      await addAdminScheduledPost(initData, postAuthor, text, postAt);
      setPostText("");
      setPostAt("");
      showAlert("Пост поставлен в очередь.");
      await load();
    } catch (e) {
      fail(e, "Не удалось запланировать пост");
    } finally {
      setBusy(false);
    }
  };

  const dropPost = async (post: AdminScheduledPost) => {
    if (!(await tgConfirm(`Снять пост на ${post.scheduled_at}?`))) return;
    setBusy(true);
    try {
      await cancelAdminScheduledPost(initData, post.id);
      showAlert("Пост снят.");
      await load();
    } catch (e) {
      fail(e, "Не удалось снять пост");
    } finally {
      setBusy(false);
    }
  };

  const sendPoll = async () => {
    const opts = options
      .split("|")
      .map((o) => o.trim())
      .filter(Boolean);
    setBusy(true);
    try {
      await publishAdminPoll(initData, question.trim(), opts);
      setQuestion("");
      setOptions("Да | Нет");
      showAlert("Опрос опубликован в ленте.");
    } catch (e) {
      fail(e, "Не удалось опубликовать опрос");
    } finally {
      setBusy(false);
    }
  };

  const doWipe = async () => {
    if (!(await tgConfirm("Удалить ленту и переписку стаи? Отменить будет нельзя."))) return;
    setBusy(true);
    try {
      const j = await wipePackFeed(initData, true);
      setWipe(j.counts);
      showAlert("Лента и чат очищены.");
    } catch (e) {
      fail(e, "Не удалось очистить");
    } finally {
      setBusy(false);
    }
  };

  if (loading) return <p className="ops-muted">Загрузка…</p>;

  if (section === "admins") {
    return (
      <div className="ops">
        <p className="ops-muted">
          Права из переменных окружения снять нельзя — они помечены как «из настроек». Остальным права выдаются и
          снимаются здесь.
        </p>
        <div className="ops-row">
          <input
            value={adminQuery}
            onChange={(e) => setAdminQuery(e.target.value)}
            placeholder="@ник или telegram id"
          />
          <button type="button" className="ops-primary" disabled={busy} onClick={() => void addAdmin()}>
            Выдать права
          </button>
        </div>
        <ul className="ops-list">
          {admins.map((p) => (
            <li key={`${p.user_id}-${p.static ? "s" : "d"}`}>
              <span>
                <b>{p.username ? `@${p.username}` : p.user_id}</b>
                <small>{p.static ? "из настроек" : `добавлен ${p.added_at}`}</small>
              </span>
              {p.static ? null : (
                <button type="button" className="ops-danger" disabled={busy} onClick={() => void dropAdmin(p)}>
                  Снять
                </button>
              )}
            </li>
          ))}
        </ul>
      </div>
    );
  }

  if (section === "scheduled") {
    return (
      <div className="ops">
        <div className="ops-card">
          <textarea
            value={postText}
            onChange={(e) => setPostText(e.target.value)}
            placeholder="Текст поста в ленту"
          />
          <div className="ops-row">
            <select value={postAuthor} onChange={(e) => setPostAuthor(e.target.value as "admin" | "leo")}>
              <option value="admin">От админа</option>
              <option value="leo">От Лео</option>
            </select>
            <input type="datetime-local" value={postAt} onChange={(e) => setPostAt(e.target.value)} />
            <button type="button" className="ops-primary" disabled={busy} onClick={() => void schedulePost()}>
              Запланировать
            </button>
          </div>
          <p className="ops-muted">Время по Москве. Публикует тот же воркер, что и посты из чата.</p>
        </div>
        <ul className="ops-list">
          {posts.length === 0 ? <li className="ops-muted">Очередь пуста</li> : null}
          {posts.map((p) => (
            <li key={p.id}>
              <span>
                <b>{p.scheduled_at}</b>
                <small>
                  {p.author === "leo" ? "от Лео" : "от админа"} · {p.text.slice(0, 90)}
                </small>
              </span>
              <button type="button" className="ops-danger" disabled={busy} onClick={() => void dropPost(p)}>
                Снять
              </button>
            </li>
          ))}
        </ul>
      </div>
    );
  }

  if (section === "poll") {
    return (
      <div className="ops">
        <div className="ops-card">
          <input value={question} onChange={(e) => setQuestion(e.target.value)} placeholder="Вопрос" />
          <input
            value={options}
            onChange={(e) => setOptions(e.target.value)}
            placeholder="Варианты через | — например: Да | Нет"
          />
          <p className="ops-muted">От 2 до 10 вариантов, каждый до 100 символов.</p>
          <button type="button" className="ops-primary" disabled={busy} onClick={() => void sendPoll()}>
            Опубликовать опрос
          </button>
        </div>
      </div>
    );
  }

  if (section === "wipe") {
    return (
      <div className="ops">
        <div className="ops-card ops-card--danger">
          <p>
            Удалятся посты ленты ({wipe?.feed_posts ?? 0}), комментарии ({wipe?.feed_threads ?? 0}), жалобы (
            {wipe?.feed_reports ?? 0}) и сообщения чата стаи ({wipe?.pack_chat_messages ?? 0}).
          </p>
          <p className="ops-muted">Действие необратимо: восстановить содержимое будет нечем.</p>
          <button type="button" className="ops-danger ops-danger--block" disabled={busy} onClick={() => void doWipe()}>
            Очистить ленту и чат
          </button>
        </div>
      </div>
    );
  }

  return (
    <div className="ops">
      {section === "analytics" ? (
        <div className="ops-periods">
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
      ) : null}
      {section === "payments" ? (
        <>
          <div className="ops-periods">
            {PAYMENT_KINDS.map((k) => (
              <button
                key={k.kind || "all"}
                type="button"
                className={paymentsKind === k.kind ? "on" : ""}
                onClick={() => {
                  setPaymentsKind(k.kind);
                  setPaymentsOffset(0);
                }}
              >
                {k.label}
              </button>
            ))}
          </div>
          <div className="ops-periods">
            {PAYMENTS_PERIOD_MODES.map((m) => (
              <button
                key={m.mode}
                type="button"
                className={paymentsPeriodMode === m.mode ? "on" : ""}
                onClick={() => {
                  if (m.mode === "custom" && paymentsPeriodMode !== "custom") {
                    // Свой период начинаем с того, что было выбрано, — его удобно подправить.
                    const r = paymentsPeriodRange(paymentsPeriodMode, paymentsAnchor);
                    setPaymentsCustomFrom(r.from);
                    setPaymentsCustomTo(r.to);
                  }
                  setPaymentsPeriodMode(m.mode);
                  setPaymentsOffset(0);
                }}
              >
                {m.label}
              </button>
            ))}
          </div>
          {paymentsPeriodMode === "day" || paymentsPeriodMode === "week" || paymentsPeriodMode === "month" ? (
            <div className="ops-row ops-period-nav">
              <button
                type="button"
                aria-label="Предыдущий период"
                onClick={() => {
                  setPaymentsAnchor(shiftPaymentsAnchor(paymentsPeriodMode, paymentsAnchor, -1));
                  setPaymentsOffset(0);
                }}
              >
                ◀
              </button>
              <span className="ops-period-nav__title">{paymentsPeriodTitle(paymentsPeriodMode, paymentsAnchor)}</span>
              <button
                type="button"
                aria-label="Следующий период"
                disabled={!canShiftPaymentsForward(paymentsPeriodMode, paymentsAnchor, todayMsk())}
                onClick={() => {
                  setPaymentsAnchor(shiftPaymentsAnchor(paymentsPeriodMode, paymentsAnchor, 1));
                  setPaymentsOffset(0);
                }}
              >
                ▶
              </button>
            </div>
          ) : null}
          {paymentsPeriodMode === "custom" ? (
            <div className="ops-row">
              <input
                type="date"
                aria-label="С даты"
                value={paymentsCustomFrom}
                max={paymentsCustomTo || undefined}
                onChange={(e) => {
                  setPaymentsCustomFrom(e.target.value);
                  setPaymentsOffset(0);
                }}
              />
              <input
                type="date"
                aria-label="По дату"
                value={paymentsCustomTo}
                min={paymentsCustomFrom || undefined}
                onChange={(e) => {
                  setPaymentsCustomTo(e.target.value);
                  setPaymentsOffset(0);
                }}
              />
            </div>
          ) : null}
          <form
            className="ops-row"
            onSubmit={(e) => {
              e.preventDefault();
              setPaymentsQuery(paymentsQueryDraft.trim());
              setPaymentsOffset(0);
            }}
          >
            <input
              type="search"
              value={paymentsQueryDraft}
              onChange={(e) => setPaymentsQueryDraft(e.target.value)}
              placeholder="@ник, имя или id"
            />
            <button type="submit" className="ops-primary">
              Найти
            </button>
            {paymentsQuery ? (
              <button
                type="button"
                onClick={() => {
                  setPaymentsQueryDraft("");
                  setPaymentsQuery("");
                  setPaymentsOffset(0);
                }}
              >
                Сбросить
              </button>
            ) : null}
          </form>
          <div className="ops-periods">
            {PAYMENT_ORDERS.map((o) => (
              <button
                key={o.order}
                type="button"
                className={paymentsOrder === o.order ? "on" : ""}
                onClick={() => {
                  setPaymentsOrder(o.order);
                  setPaymentsOffset(0);
                }}
              >
                {o.label}
              </button>
            ))}
            <button
              type="button"
              className={paymentsPaidOnly ? "on" : ""}
              onClick={() => {
                setPaymentsPaidOnly(!paymentsPaidOnly);
                setPaymentsOffset(0);
              }}
            >
              {paymentsPaidOnly ? "✓ Только оплаченные" : "Только оплаченные"}
            </button>
          </div>
        </>
      ) : null}
      {note ? <p className="ops-muted">{note}</p> : null}
      {tables.map((t, i) => (
        <Table key={`${t.title}-${i}`} table={t} />
      ))}
      {section === "payments" && paymentsTotal > PAYMENTS_PAGE ? (
        <div className="ops-row">
          <button
            type="button"
            disabled={paymentsOffset === 0}
            onClick={() => setPaymentsOffset(Math.max(0, paymentsOffset - PAYMENTS_PAGE))}
          >
            ◀ Назад
          </button>
          <button
            type="button"
            disabled={paymentsOffset + PAYMENTS_PAGE >= paymentsTotal}
            onClick={() => setPaymentsOffset(paymentsOffset + PAYMENTS_PAGE)}
          >
            Вперёд ▶
          </button>
        </div>
      ) : null}
    </div>
  );
}
