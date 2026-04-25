import { useCallback, useEffect, useRef, useState } from "react";
import { timeAgoFromISO } from "../lib/timeAgo";
import "./PackGroupChatPanel.css";

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export type PackGroupMessage = {
  id: number;
  user_id: number;
  username: string;
  text: string;
  created_at: string;
  is_leo: boolean;
};

type Props = {
  initData: string;
  inTelegram: boolean;
  meId: number;
  showAlert: (m: string) => void;
  onHaptic?: () => void;
};

export function PackGroupChatPanel({ initData, inTelegram, meId, showAlert, onHaptic }: Props) {
  const [items, setItems] = useState<PackGroupMessage[]>([]);
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement | null>(null);

  const load = useCallback(async () => {
    if (!apiBase || !inTelegram || !initData) return;
    setErr(null);
    try {
      const res = await fetch(`${apiBase}/api/miniapp/pack-group/feed`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData }),
      });
      const j = (await res.json().catch(() => ({}))) as { ok?: boolean; messages?: PackGroupMessage[]; error?: string };
      if (!res.ok) {
        if (res.status === 403) {
          setErr("Нет доступа к чату стаи.");
          setItems([]);
          return;
        }
        setErr(j.error ?? `Ошибка ${res.status}`);
        return;
      }
      setItems(j.messages ?? []);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Сеть");
    }
  }, [inTelegram, initData]);

  useEffect(() => {
    void load();
  }, [load]);

  useEffect(() => {
    if (!apiBase || !inTelegram || !initData) return;
    const t = setInterval(() => void load(), 5000);
    return () => clearInterval(t);
  }, [load, inTelegram, initData]);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [items]);

  const send = useCallback(async () => {
    const t = text.trim();
    if (!t || sending) return;
    if (!apiBase) {
      showAlert("Сборка без VITE_MINIAPP_API_URL.");
      return;
    }
    if (!inTelegram || !initData) {
      showAlert("Открой мини-апп из Telegram.");
      return;
    }
    setSending(true);
    onHaptic?.();
    setText("");
    try {
      const res = await fetch(`${apiBase}/api/miniapp/pack-group/messages`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData, text: t }),
      });
      const j = (await res.json().catch(() => ({}))) as { error?: string; ok?: boolean; reply_text?: string };
      if (!res.ok) {
        showAlert(j.error ?? `Ошибка ${res.status}`);
        return;
      }
      await load();
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Сеть");
    } finally {
      setSending(false);
    }
  }, [text, sending, inTelegram, initData, showAlert, load, onHaptic]);

  if (!apiBase) {
    return <p className="packroom__warn muted">Нет API URL в билде.</p>;
  }

  return (
    <div className="packroom">
      <p className="packroom__hint">
        Пиши всем в стае. Чтобы ответил Лео, обрати к нему: <strong>@leo</strong> (или <strong>@…</strong> с username бота). Без
        @ — без ответа ИИ.
      </p>
      {err && <p className="packroom__err">{err}</p>}
      <div className="packroom__log" role="log" aria-label="Чат стаи">
        {items.map((m) => {
          const mine = !m.is_leo && m.user_id === meId;
          return (
            <div
              key={m.id}
              className={`packroom__row ${m.is_leo ? "packroom__row--leo" : mine ? "packroom__row--me" : "packroom__row--oth"}`}
            >
              <div className="packroom__meta">
                {m.is_leo ? "Лео" : m.username} · {timeAgoFromISO(m.created_at)}
              </div>
              <div className="packroom__bubble">{m.text}</div>
            </div>
          );
        })}
        <div ref={endRef} />
      </div>
      <form
        className="packroom__form"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <input
          className="packroom__input"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Сообщение… @leo — чтобы ответил бот"
          maxLength={4000}
          autoComplete="off"
        />
        <button type="submit" className="packroom__send" disabled={sending || !text.trim()}>
          {sending ? "…" : "➤"}
        </button>
      </form>
    </div>
  );
}
