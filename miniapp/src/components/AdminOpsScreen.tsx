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

/** Admin sections that used to live only in chat (/admin). */
export type AdminOpsSection = "analytics" | "visits" | "payments" | "admins" | "scheduled" | "poll" | "wipe";

type Props = {
  section: AdminOpsSection;
  initData: string;
  showAlert: (text: string) => void;
};

const PERIODS = [
  { days: 7, label: "7 days" },
  { days: 30, label: "30 days" },
  { days: 90, label: "90 days" },
  { days: 0, label: "all time" },
];

const PAYMENTS_PAGE = 20;

const PAYMENT_KINDS: { kind: AdminPaymentsKind; label: string }[] = [
  { kind: "", label: "Payments & donations" },
  { kind: "access", label: "Payments" },
  { kind: "donation", label: "Donations" },
];

const PAYMENT_ORDERS: { order: AdminPaymentsOrder; label: string }[] = [
  { order: "desc", label: "Newest first" },
  { order: "asc", label: "Oldest first" },
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
      {table.rows.length === 0 ? <p className="ops-muted">Empty</p> : null}
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
  const [options, setOptions] = useState("Yes | No");

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
            ? `Period: ${j.analytics.period} · last event ${j.analytics.last_event_at}`
            : `Period: ${j.analytics.period}`,
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
          paymentsKind === "access" ? "Payments" : paymentsKind === "donation" ? "Donations" : "Payments & donations";
        const empty = filtered ? `No ${what.toLowerCase()} for these filters` : `No ${what.toLowerCase()} yet`;
        setNote(p.total > 0 ? `Rows ${paymentsOffset + 1}–${paymentsOffset + shown} of ${p.total}` : empty);
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
      fail(e, "Couldn’t load section");
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
      showAlert("Enter @username or id.");
      return;
    }
    setBusy(true);
    try {
      const j = await addAdminPerson(initData, q);
      setAdminQuery("");
      showAlert(`Granted admin: ${j.user_id}`);
      await load();
    } catch (e) {
      fail(e, "Couldn’t grant admin");
    } finally {
      setBusy(false);
    }
  };

  const dropAdmin = async (person: AdminPerson) => {
    const label = person.username ? `@${person.username}` : String(person.user_id);
    if (!(await tgConfirm(`Remove admin rights from ${label}?`))) return;
    setBusy(true);
    try {
      await removeAdminPerson(initData, person.user_id);
      showAlert("Admin rights removed.");
      await load();
    } catch (e) {
      fail(e, "Couldn’t remove admin rights");
    } finally {
      setBusy(false);
    }
  };

  const schedulePost = async () => {
    const text = postText.trim();
    if (!text || !postAt) {
      showAlert("Need text and time.");
      return;
    }
    setBusy(true);
    try {
      await addAdminScheduledPost(initData, postAuthor, text, postAt);
      setPostText("");
      setPostAt("");
      showAlert("Post queued.");
      await load();
    } catch (e) {
      fail(e, "Couldn’t schedule post");
    } finally {
      setBusy(false);
    }
  };

  const dropPost = async (post: AdminScheduledPost) => {
    if (!(await tgConfirm(`Cancel post scheduled for ${post.scheduled_at}?`))) return;
    setBusy(true);
    try {
      await cancelAdminScheduledPost(initData, post.id);
      showAlert("Post canceled.");
      await load();
    } catch (e) {
      fail(e, "Couldn’t cancel post");
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
      setOptions("Yes | No");
      showAlert("Poll published to the feed.");
    } catch (e) {
      fail(e, "Couldn’t publish poll");
    } finally {
      setBusy(false);
    }
  };

  const doWipe = async () => {
    if (!(await tgConfirm("Delete the feed and pack chat? This can’t be undone."))) return;
    setBusy(true);
    try {
      const j = await wipePackFeed(initData, true);
      setWipe(j.counts);
      showAlert("Feed and chat cleared.");
    } catch (e) {
      fail(e, "Couldn’t clear");
    } finally {
      setBusy(false);
    }
  };

  if (loading) return <p className="ops-muted">Loading…</p>;

  if (section === "admins") {
    return (
      <div className="ops">
        <p className="ops-muted">
          Admins from environment variables can’t be removed here — they’re marked “from config”. Everyone else is
          granted and revoked on this screen.
        </p>
        <div className="ops-row">
          <input
            value={adminQuery}
            onChange={(e) => setAdminQuery(e.target.value)}
            placeholder="@username or telegram id"
          />
          <button type="button" className="ops-primary" disabled={busy} onClick={() => void addAdmin()}>
            Grant admin
          </button>
        </div>
        <ul className="ops-list">
          {admins.map((p) => (
            <li key={`${p.user_id}-${p.static ? "s" : "d"}`}>
              <span>
                <b>{p.username ? `@${p.username}` : p.user_id}</b>
                <small>{p.static ? "from config" : `added ${p.added_at}`}</small>
              </span>
              {p.static ? null : (
                <button type="button" className="ops-danger" disabled={busy} onClick={() => void dropAdmin(p)}>
                  Remove
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
            placeholder="Feed post text"
          />
          <div className="ops-row">
            <select value={postAuthor} onChange={(e) => setPostAuthor(e.target.value as "admin" | "leo")}>
              <option value="admin">From admin</option>
              <option value="leo">From Leo</option>
            </select>
            <input type="datetime-local" value={postAt} onChange={(e) => setPostAt(e.target.value)} />
            <button type="button" className="ops-primary" disabled={busy} onClick={() => void schedulePost()}>
              Schedule
            </button>
          </div>
          <p className="ops-muted">Moscow time. Same worker as chat-scheduled posts.</p>
        </div>
        <ul className="ops-list">
          {posts.length === 0 ? <li className="ops-muted">Queue is empty</li> : null}
          {posts.map((p) => (
            <li key={p.id}>
              <span>
                <b>{p.scheduled_at}</b>
                <small>
                  {p.author === "leo" ? "from Leo" : "from admin"} · {p.text.slice(0, 90)}
                </small>
              </span>
              <button type="button" className="ops-danger" disabled={busy} onClick={() => void dropPost(p)}>
                Remove
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
          <input value={question} onChange={(e) => setQuestion(e.target.value)} placeholder="Question" />
          <input
            value={options}
            onChange={(e) => setOptions(e.target.value)}
            placeholder="Options separated by | — e.g. Yes | No"
          />
          <p className="ops-muted">2–10 options, up to 100 characters each.</p>
          <button type="button" className="ops-primary" disabled={busy} onClick={() => void sendPoll()}>
            Publish poll
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
            This deletes feed posts ({wipe?.feed_posts ?? 0}), comments ({wipe?.feed_threads ?? 0}), reports (
            {wipe?.feed_reports ?? 0}), and pack chat messages ({wipe?.pack_chat_messages ?? 0}).
          </p>
          <p className="ops-muted">Irreversible — nothing to restore from.</p>
          <button type="button" className="ops-danger ops-danger--block" disabled={busy} onClick={() => void doWipe()}>
            Clear feed and chat
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
          <div className="ops-row">
            <select
              aria-label="Type"
              value={paymentsKind}
              onChange={(e) => {
                setPaymentsKind(e.target.value as AdminPaymentsKind);
                setPaymentsOffset(0);
              }}
            >
              {PAYMENT_KINDS.map((k) => (
                <option key={k.kind || "all"} value={k.kind}>
                  {k.label}
                </option>
              ))}
            </select>
            <select
              aria-label="Period"
              value={paymentsPeriodMode}
              onChange={(e) => {
                const mode = e.target.value as PaymentsPeriodMode;
                if (mode === "custom" && paymentsPeriodMode !== "custom") {
                  const r = paymentsPeriodRange(paymentsPeriodMode, paymentsAnchor);
                  setPaymentsCustomFrom(r.from);
                  setPaymentsCustomTo(r.to);
                }
                setPaymentsPeriodMode(mode);
                setPaymentsOffset(0);
              }}
            >
              {PAYMENTS_PERIOD_MODES.map((m) => (
                <option key={m.mode} value={m.mode}>
                  {m.label}
                </option>
              ))}
            </select>
          </div>
          {paymentsPeriodMode === "day" || paymentsPeriodMode === "week" || paymentsPeriodMode === "month" ? (
            <div className="ops-row ops-period-nav">
              <button
                type="button"
                aria-label="Previous period"
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
                aria-label="Next period"
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
                aria-label="From date"
                value={paymentsCustomFrom}
                max={paymentsCustomTo || undefined}
                onChange={(e) => {
                  setPaymentsCustomFrom(e.target.value);
                  setPaymentsOffset(0);
                }}
              />
              <input
                type="date"
                aria-label="To date"
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
              placeholder="@username, name, or id"
            />
            <button type="submit" className="ops-primary">
              Find
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
                Reset
              </button>
            ) : null}
          </form>
          <div className="ops-row">
            <select
              aria-label="Sort"
              value={paymentsOrder}
              onChange={(e) => {
                setPaymentsOrder(e.target.value as AdminPaymentsOrder);
                setPaymentsOffset(0);
              }}
            >
              {PAYMENT_ORDERS.map((o) => (
                <option key={o.order} value={o.order}>
                  {o.label}
                </option>
              ))}
            </select>
            <select
              aria-label="Status"
              value={paymentsPaidOnly ? "paid" : "any"}
              onChange={(e) => {
                setPaymentsPaidOnly(e.target.value === "paid");
                setPaymentsOffset(0);
              }}
            >
              <option value="any">Any status</option>
              <option value="paid">Paid only</option>
            </select>
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
            ◀ Back
          </button>
          <button
            type="button"
            disabled={paymentsOffset + PAYMENTS_PAGE >= paymentsTotal}
            onClick={() => setPaymentsOffset(paymentsOffset + PAYMENTS_PAGE)}
          >
            Forward ▶
          </button>
        </div>
      ) : null}
    </div>
  );
}
