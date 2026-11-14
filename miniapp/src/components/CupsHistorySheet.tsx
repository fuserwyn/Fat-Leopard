import { useEffect, useMemo, useState } from "react";
import { createPortal } from "react-dom";
import {
  cupsHistoryCaption,
  CUPS_HISTORY_LIMIT,
  cupsHistorySortOptions,
  buildCupsHistoryRows,
  defaultCupsHistorySortDir,
  sortCupsHistoryRows,
  type CupsHistoryRow,
  type CupsHistorySortDir,
  type CupsHistorySortKey,
} from "../lib/cupsHistory";
import "./CupsHistorySheet.css";
import { pick } from "../lib/i18n";
import { useI18n } from "../lib/I18nContext";

type Props = {
  apiUrl: string;
  initData: string;
  onClose: () => void;
};

type HistoryResponse = {
  ok?: boolean;
  workouts?: Array<{ date?: string; message_text?: string; cups?: number; created_at?: string }>;
  pack_weekly?: Array<{ date?: string; cups?: number; created_at?: string }>;
};

export function CupsHistorySheet({ apiUrl, initData, onClose }: Props) {
  const { lang } = useI18n();
  const [rows, setRows] = useState<CupsHistoryRow[] | null>(null);
  const [error, setError] = useState("");
  const [sortKey, setSortKey] = useState<CupsHistorySortKey>("date");
  const [sortDir, setSortDir] = useState<CupsHistorySortDir>("desc");

  const sortedRows = useMemo(
    () => (rows == null ? null : sortCupsHistoryRows(rows, sortKey, sortDir)),
    [rows, sortKey, sortDir],
  );

  const onSortClick = (key: CupsHistorySortKey) => {
    if (key === sortKey) {
      setSortDir((d) => (d === "asc" ? "desc" : "asc"));
      return;
    }
    setSortKey(key);
    setSortDir(defaultCupsHistorySortDir(key));
  };

  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  useEffect(() => {
    const ac = new AbortController();
    if (!apiUrl || !initData.trim()) {
      setError(pick(lang, "История недоступна", "History unavailable"));
      setRows([]);
      return;
    }
    setRows(null);
    setError("");
    void (async () => {
      try {
        const res = await fetch(`${apiUrl}/api/miniapp/profile/cups-history`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData }),
          signal: ac.signal,
        });
        const j = (await res.json().catch(() => ({}))) as HistoryResponse;
        if (!res.ok || !j.ok) {
          setError(pick(lang, "Не получилось загрузить историю", "Couldn't load history"));
          setRows([]);
          return;
        }
        setRows(
          buildCupsHistoryRows(
            (j.workouts ?? []).map((w) => ({
              date: w.date ?? "",
              messageText: w.message_text ?? "",
              cups: typeof w.cups === "number" ? w.cups : 0,
              createdAt: w.created_at ?? "",
            })),
            (j.pack_weekly ?? []).map((w) => ({
              date: w.date ?? "",
              cups: typeof w.cups === "number" ? w.cups : 0,
              createdAt: w.created_at ?? "",
            })),
            CUPS_HISTORY_LIMIT,
            lang,
          ),
        );
      } catch (e) {
        if (ac.signal.aborted) return;
        setError(e instanceof Error ? e.message : pick(lang, "Не получилось загрузить историю", "Couldn't load history"));
        setRows([]);
      }
    })();
    return () => ac.abort();
  }, [apiUrl, initData, lang]);

  const body = (
    <>
      <div className="cups-history-backdrop" onClick={onClose} />
      <div className="cups-history" role="dialog" aria-modal="true" aria-label={pick(lang, "История кубков", "Cups history")}>
        <header className="cups-history__head">
          <h2 className="cups-history__title">{pick(lang, "История кубков", "Cups history")}</h2>
          <button type="button" className="cups-history__close" onClick={onClose} aria-label={pick(lang, "Закрыть", "Close")}>
            ✕
          </button>
        </header>
        <p className="cups-history__caption">{cupsHistoryCaption(lang)}</p>
        <div className="cups-history__sort" role="group" aria-label={pick(lang, "Сортировка", "Sort")}>
          {cupsHistorySortOptions(lang).map((opt) => {
            const active = opt.key === sortKey;
            return (
              <button
                key={opt.key}
                type="button"
                className={`cups-history__sort-btn${active ? " cups-history__sort-btn--active" : ""}`}
                aria-pressed={active}
                onClick={() => onSortClick(opt.key)}
              >
                {opt.label}
                {active ? (
                  <span className="cups-history__sort-arrow" aria-hidden>
                    {sortDir === "asc" ? "↑" : "↓"}
                  </span>
                ) : null}
              </button>
            );
          })}
        </div>
        <div className="cups-history__body">
          {sortedRows == null ? (
            <p className="cups-history__status">{pick(lang, "Загружаю…", "Loading…")}</p>
          ) : error ? (
            <p className="cups-history__status">{error}</p>
          ) : sortedRows.length === 0 ? (
            <p className="cups-history__status">{pick(lang, "Пока нет начислений кубков", "No cup awards yet")}</p>
          ) : (
            <ul className="cups-history__list">
              {sortedRows.map((row, i) => (
                <li key={`${row.kind}-${row.sortAt}-${i}`} className={`cups-history__row cups-history__row--${row.kind}`}>
                  <span className="cups-history__date">{row.dateLabel}</span>
                  <span className="cups-history__type">{row.workoutType}</span>
                  {row.intensity ? <span className="cups-history__intensity">{row.intensity}</span> : null}
                  {row.duration ? <span className="cups-history__duration">{row.duration}</span> : null}
                  <span className="cups-history__cups">{row.cupsLabel}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      </div>
    </>
  );

  if (typeof document === "undefined") return body;
  return createPortal(body, document.body);
}
