import { useCallback, useEffect, useRef, useState } from "react";
import { getTelegramUserId, loadLeoChat, saveLeoChat, type Msg } from "../lib/leoChatStorage";
import "./ChatScreen.css";

const envApi = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

const AVATAR_URL = `${import.meta.env.BASE_URL}leo-avatar.png`;

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
  const [items, setItems] = useState<Msg[]>(() => loadLeoChat(getTelegramUserId()));
  const endRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    endRef.current?.scrollIntoView({ behavior: "smooth" });
  }, [items, sending]);

  useEffect(() => {
    saveLeoChat(getTelegramUserId(), items);
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
      const j = (await res.json().catch(() => ({}))) as {
        error?: string;
        ok?: boolean;
        pending?: boolean;
        reply_text?: string;
      };
      if (!res.ok) {
        showAlert(j.error ?? `Ошибка ${res.status}`);
        return;
      }
      const replyNow = j.reply_text?.trim();
      if (replyNow) {
        setItems((prev) => [...prev, { id: nowId(), role: "system", time: Date.now(), text: replyNow }]);
        return;
      }
      if (j.pending) {
        const deadline = Date.now() + 4 * 60 * 1000;
        let gotAny = false;
        let idleEmpty = 0;
        while (Date.now() < deadline) {
          await new Promise((r) => setTimeout(r, 1500));
          const pr = await fetch(`${envApi}/api/miniapp/personal-reply/poll`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData }),
          });
          const pj = (await pr.json().catch(() => ({}))) as { reply_text?: string; error?: string; ok?: boolean };
          if (!pr.ok) {
            showAlert(pj.error ?? `Ошибка poll ${pr.status}`);
            break;
          }
          const chunk = pj.reply_text?.trim();
          if (chunk) {
            gotAny = true;
            idleEmpty = 0;
            setItems((prev) => [...prev, { id: nowId(), role: "system", time: Date.now(), text: chunk }]);
            continue;
          }
          idleEmpty++;
          if (gotAny && idleEmpty >= 2) break;
        }
        if (!gotAny) {
          setItems((prev) => [
            ...prev,
            {
              id: nowId(),
              role: "system",
              time: Date.now(),
              text: "Ответ задерживается. Загляни в личку с ботом в Telegram — там он уже может быть.",
            },
          ]);
        }
        return;
      }
      setItems((prev) => [
        ...prev,
        {
          id: nowId(),
          role: "system",
          time: Date.now(),
          text: "Сообщение ушло боту. Открой чат с ботом в Telegram, если ждёшь ответ там.",
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
        <div className="chat__head-avatarwrap">
          <img className="chat__head-avatar" src={AVATAR_URL} width={52} height={52} alt="Лео" loading="eager" />
          {sending && (
            <span className="chat__head-typing" aria-hidden="true">
              <span className="chat__head-typing-dots">
                <span className="chat__dot" />
                <span className="chat__dot" />
                <span className="chat__dot" />
              </span>
            </span>
          )}
        </div>
        <div className="chat__head-text">
          <h1 className="chat__title">Лео</h1>
          <p className="chat__sub">{sending ? "печатает…" : name}</p>
        </div>
      </header>
      <div className="chat__log" role="log" aria-label="Сообщения с ботом">
        {items.map((m) =>
          m.role === "user" ? (
            <div key={m.id} className="chat__row chat__row--user">
              <div className="chat__bubble chat__bubble--user">{m.text}</div>
            </div>
          ) : (
            <div key={m.id} className="chat__row chat__row--sys">
              <img className="chat__bubble-avatar" src={AVATAR_URL} width={36} height={36} alt="" aria-hidden="true" />
              <div className="chat__bubble chat__bubble--sys">{m.text}</div>
            </div>
          )
        )}
        {sending && (
          <div className="chat__row chat__row--sys" role="status" aria-live="polite" aria-label="Лео печатает">
            <img className="chat__bubble-avatar" src={AVATAR_URL} width={36} height={36} alt="" aria-hidden="true" />
            <div className="chat__bubble chat__bubble--sys chat__bubble--typing" aria-hidden="true">
              <span className="chat__typing-dots">
                <span className="chat__dot" />
                <span className="chat__dot" />
                <span className="chat__dot" />
              </span>
            </div>
          </div>
        )}
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
