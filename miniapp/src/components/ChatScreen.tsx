import { useCallback, useEffect, useRef, useState } from "react";
import "./ChatScreen.css";

type Msg = { id: string; role: "user" | "system"; text: string; time: number };

const envApi = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

type Props = {
  name: string;
  initData: string;
  inTelegram: boolean;
  showAlert: (m: string) => void;
};

function nowId() {
  return `${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

export function ChatScreen({ name, initData, inTelegram, showAlert }: Props) {
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  const [items, setItems] = useState<Msg[]>(() => [
    {
      id: "h",
      role: "system",
      time: Date.now(),
      text:
        "Пиши Лео как в личке: #training_done, /start, вопросы с @ботом. Ответ — в Telegram. Во вкладке «Стая» видны отчёты других.",
    },
  ]);
  const endRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [items]);

  const send = useCallback(async () => {
    const t = text.trim();
    if (!t || sending) return;
    if (!envApi) {
      showAlert(
        "Сборка без API: в Railway у сервиса мини-аппа задай Build Variable VITE_MINIAPP_API_URL = публичный https URL сервиса с ботом (ms_leo), затем Redeploy."
      );
      return;
    }
    if (!inTelegram || !initData) {
      showAlert("Открой мини-апп из Telegram (нужен initData).");
      return;
    }
    setSending(true);
    setItems((prev) => [...prev, { id: nowId(), role: "user", text: t, time: Date.now() }]);
    setText("");
    try {
      const w = window.Telegram?.WebApp;
      w?.HapticFeedback?.impactOccurred?.("light");
      const res = await fetch(`${envApi}/api/miniapp/messages`, {
        method: "POST",
        headers: { "Content-Type": "application/json" },
        body: JSON.stringify({ init_data: initData, text: t }),
      });
      if (!res.ok) {
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        showAlert(j.error ?? `Ошибка ${res.status}`);
        return;
      }
      setItems((prev) => [
        ...prev,
        {
          id: nowId(),
          role: "system",
          time: Date.now(),
          text: "Сообщение ушло боту. Открой чат с ботом в Telegram — ответ там же.",
        },
      ]);
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Сеть");
    } finally {
      setSending(false);
    }
  }, [text, sending, inTelegram, initData, showAlert]);

  return (
    <div className="chat">
      {!import.meta.env.VITE_MINIAPP_API_URL && (
        <div className="chat__configwarn" role="status">
          Нет <code className="chat__code">VITE_MINIAPP_API_URL</code> при сборке. В Railway → сервис
          <strong> miniapp</strong> → Variables → <strong>Build</strong> → укажи https URL сервиса
          <strong> бота</strong> (ms_leo), Redeploy.
        </div>
      )}
      <header className="chat__head">
        <h1 className="chat__title">Лео</h1>
        <p className="chat__sub">{name}</p>
      </header>
      <div className="chat__log" role="log" aria-label="Сообщения с ботом">
        {items.map((m) => (
          <div
            key={m.id}
            className={`chat__bubble ${m.role === "user" ? "chat__bubble--user" : "chat__bubble--sys"}`}
          >
            {m.text}
          </div>
        ))}
        <div ref={endRef} />
      </div>
      <form
        className="chat__form"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <input
          className="chat__input"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder="Сообщение…"
          maxLength={4000}
          autoComplete="off"
          enterKeyHint="send"
        />
        <button type="submit" className="chat__send" disabled={sending || !text.trim()}>
          {sending ? "…" : "➤"}
        </button>
      </form>
    </div>
  );
}
