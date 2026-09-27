import { useEffect, useState } from "react";
import { createPortal } from "react-dom";
import {
  CUPS_HISTORY_CAPTION,
  CUPS_HISTORY_LIMIT,
  buildCupsHistoryRows,
  type CupsHistoryRow,
} from "../lib/cupsHistory";
import "./CupsHistorySheet.css";

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
  const [rows, setRows] = useState<CupsHistoryRow[] | null>(null);
  const [error, setError] = useState("");

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
      setError("История недоступна");
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
          setError("Не получилось загрузить историю");
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
          ),
        );
      } catch (e) {
        if (ac.signal.aborted) return;
        setError(e instanceof Error ? e.message : "Не получилось загрузить историю");
        setRows([]);
      }
    })();
    return () => ac.abort();
  }, [apiUrl, initData]);

  const body = (
    <>
      <div className="cups-history-backdrop" onClick={onClose} />
      <div className="cups-history" role="dialog" aria-modal="true" aria-label="История кубков">
        <header className="cups-history__head">
          <h2 className="cups-history__title">История кубков</h2>
          <button type="button" className="cups-history__close" onClick={onClose} aria-label="Закрыть">
            ✕
          </button>
        </header>
        <p className="cups-history__caption">{CUPS_HISTORY_CAPTION}</p>
        <div className="cups-history__body">
          {rows == null ? (
            <p className="cups-history__status">Загружаю…</p>
          ) : error ? (
            <p className="cups-history__status">{error}</p>
          ) : rows.length === 0 ? (
            <p className="cups-history__status">Пока нет начислений кубков</p>
          ) : (
            <ul className="cups-history__list">
              {rows.map((row, i) => (
                <li key={`${row.kind}-${row.sortAt}-${i}`} className={`cups-history__row cups-history__row--${row.kind}`}>
                  <span className="cups-history__date">{row.dateLabel}</span>
                  <span className="cups-history__sep" aria-hidden>
                    —
                  </span>
                  <span className="cups-history__type">{row.workoutType}</span>
                  <span className="cups-history__sep" aria-hidden>
                    —
                  </span>
                  <span className="cups-history__intensity">{row.intensity}</span>
                  <span className="cups-history__sep" aria-hidden>
                    —
                  </span>
                  <span className="cups-history__duration">{row.duration}</span>
                  <span className="cups-history__sep" aria-hidden>
                    —
                  </span>
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
