import { useCallback, useEffect, useLayoutEffect, useRef, useState, type ChangeEvent } from "react";
import { clearLeoPersonalInbox } from "../lib/leoPersonalInbox";
import { LeoTranslatableText } from "./LeoTranslatableText";
import {
  createChatScrollScheduler,
  isChatLogNearBottom,
  scrollChatLogToEnd,
} from "../lib/chatLogScroll";
import { formatChatTime } from "../lib/timeAgo";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { resolveTrainingPhotoUrl } from "../lib/packFeed";
import { CameraButton } from "./CameraButton";
import { PhotoLightbox } from "./PhotoLightbox";
import "./ChatScreen.css";

const envApi = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

type Props = {
  name: string;
  initData: string;
  inTelegram: boolean;
  showAlert: (m: string) => void;
  /** После открытия чата — сразу сбросить бейдж в UI. */
  onInboxOpened?: () => void;
  /** После забора очереди лички с сервера (синхронизация с сервером). */
  onInboxDrained?: () => void;
  /** Вкладка видима (keep-alive: false — без поллинга и body--lock). */
  active?: boolean;
};

// Сообщение в локальном UI: серверное (с числовым id и ISO created_at) и
// оптимистичное (id вида "local-..." без серверного id; ждёт следующий polling,
// после которого замещается серверным дубликатом). Источник правды — сервер
// (/api/miniapp/personal-chat/feed): локальный кеш живёт только до replace.
type ChatMsg = {
  uiKey: string;
  serverID?: number;
  role: "user" | "leo";
  text: string;
  /** Публичный URL фото (или локальный blob у оптимистичного сообщения). */
  photoUrl?: string;
  // ISO-строка от сервера или ISO от Date.now() для оптимистичных.
  createdAt: string;
  likeCount?: number;
  likeMe?: boolean;
};

type ServerMsg = { id: number; role: "user" | "leo"; text: string; photo_url?: string; created_at: string; like_count?: number; like_me?: boolean };

function nowId() {
  return `local-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

// Подмерживает серверную пачку в текущий список:
//   • удаляет локальные оптимистичные user-сообщения, чьи (text, role) совпали с пришедшими;
//   • добавляет новые серверные.
function mergeFromServer(prev: ChatMsg[], incoming: ServerMsg[]): ChatMsg[] {
  if (incoming.length === 0) return prev;
  const haveServerIDs = new Set<number>();
  for (const m of prev) {
    if (m.serverID) haveServerIDs.add(m.serverID);
  }
  const fresh = incoming.filter((m) => !haveServerIDs.has(m.id));
  const updated = prev.map((p) => {
    if (!p.serverID) return p;
    const sm = incoming.find((x) => x.id === p.serverID);
    if (!sm) return p;
    return { ...p, likeCount: sm.like_count ?? 0, likeMe: Boolean(sm.like_me) };
  });
  if (fresh.length === 0) return updated;
  // Сматчим оптимистичные user-сообщения по тексту (порядок прилёта совпадает).
  const out = updated.slice();
  for (const sm of fresh) {
    if (sm.role === "user") {
      const idx = out.findIndex(
        (m) => !m.serverID && m.role === "user" && m.text.trim() === sm.text.trim()
      );
      if (idx >= 0) {
        out[idx] = {
          uiKey: `s-${sm.id}`,
          serverID: sm.id,
          role: "user",
          text: sm.text,
          photoUrl: resolveTrainingPhotoUrl(sm.photo_url),
          createdAt: sm.created_at,
          likeCount: sm.like_count ?? 0,
          likeMe: Boolean(sm.like_me),
        };
        continue;
      }
    }
    out.push({
      uiKey: `s-${sm.id}`,
      serverID: sm.id,
      role: sm.role,
      text: sm.text,
      photoUrl: resolveTrainingPhotoUrl(sm.photo_url),
      createdAt: sm.created_at,
      likeCount: sm.like_count ?? 0,
      likeMe: Boolean(sm.like_me),
    });
  }
  // Сортировка по серверному id (где есть), потом по времени; локальные без id — в конец.
  out.sort((a, b) => {
    if (a.serverID && b.serverID) return a.serverID - b.serverID;
    if (a.serverID && !b.serverID) return -1;
    if (!a.serverID && b.serverID) return 1;
    return new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime();
  });
  return out;
}

// Максимальный серверный id в списке — для since_id-инкрементального запроса.
function maxServerID(items: ChatMsg[]): number {
  let max = 0;
  for (const m of items) {
    if (m.serverID && m.serverID > max) max = m.serverID;
  }
  return max;
}

export function ChatScreen({ name, initData, inTelegram, showAlert, onInboxOpened, onInboxDrained, active = true }: Props) {
  const [text, setText] = useState("");
  const [sending, setSending] = useState(false);
  /** После POST /messages: ответ ИИ пишется асинхронно — «печатает» до появления строки Лео в фиде. */
  const [leoTyping, setLeoTyping] = useState(false);
  const [items, setItems] = useState<ChatMsg[]>([]);
  const [loaded, setLoaded] = useState(false);
  /** Прикреплённое фото (для рекомендаций Лео) и его локальное превью. */
  const [photo, setPhoto] = useState<File | null>(null);
  const [photoPreview, setPhotoPreview] = useState<string | null>(null);
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null);
  /** Читает историю выше — показать кнопку «к последним». */
  const [showJumpLatest, setShowJumpLatest] = useState(false);
  /** Лента уже прокручена вниз — иначе при открытии вкладки сначала фон, потом бабблы. */
  const [logSettled, setLogSettled] = useState(false);
  const photoInputRef = useRef<HTMLInputElement | null>(null);
  /** blob-URL оптимистичных превью — ревокаем на размонтировании, чтобы не текла память. */
  const optimisticBlobsRef = useRef<string[]>([]);
  const chatRef = useRef<HTMLDivElement | null>(null);
  const logRef = useRef<HTMLDivElement | null>(null);
  const formRef = useRef<HTMLFormElement | null>(null);
  const endRef = useRef<HTMLDivElement | null>(null);
  const didInitialScrollRef = useRef(false);
  /** true — после отправки сообщения пользователем: всегда скроллим вниз, даже если был наверху */
  const forceScrollRef = useRef(false);
  /** Пользователь читает историю выше — не дёргаем scrollTop при поллинге. */
  const userScrolledUpRef = useRef(false);
  const userScrollingRef = useRef(false);
  const scrollGestureTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const tailMessageKeyRef = useRef<string | null>(null);
  /** max(server_id) перед отправкой пользователя — новый ответ Лео с id выше этого. */
  const baselineMaxForPendingLeoRef = useRef(0);

  // iOS WebKit: при rubber-band-скролле документа каретка фокусированного position:fixed
  // инпута визуально уезжает вверх. Лочим body только пока вкладка «Лео» активна.
  // Снимаем lock в render / useLayoutEffect (не в useEffect), чтобы TabKeepAlive
  // вернул scrollY ленты уже после unlock — иначе #root padding прыгает после restore.
  const prevLockActiveRef = useRef(active);
  if (prevLockActiveRef.current && !active) {
    document.body.classList.remove("body--lock");
  }
  prevLockActiveRef.current = active;
  useLayoutEffect(() => {
    if (!active) {
      document.body.classList.remove("body--lock");
      return;
    }
    document.body.classList.add("body--lock");
    return () => {
      document.body.classList.remove("body--lock");
    };
  }, [active]);

  const isNearLogBottom = useCallback((threshold = 80) => {
    const el = logRef.current;
    if (!el) return true;
    return isChatLogNearBottom(el, threshold);
  }, []);

  const scrollLogToEnd = useCallback(() => {
    const el = logRef.current;
    if (!el) return;
    scrollChatLogToEnd(el);
  }, []);

  const scrollLogToEndIfAllowed = useCallback(() => {
    if (userScrollingRef.current) return;
    if (userScrolledUpRef.current && !isNearLogBottom(120)) return;
    scrollLogToEnd();
  }, [isNearLogBottom, scrollLogToEnd]);

  // Пока вкладка была display:none, WebKit обнулил scrollTop. Крутим вниз
  // и только потом показываем ленту — иначе сначала фон, потом сообщения.
  useLayoutEffect(() => {
    if (!active) {
      setLogSettled(false);
      return;
    }
    if (!loaded) return;
    if (items.length > 0) scrollLogToEnd();
    setLogSettled(true);
  }, [active, loaded, items.length, scrollLogToEnd]);

  // Отслеживаем жест скролла и «читаю историю».
  useEffect(() => {
    const el = logRef.current;
    if (!el) return;
    const onScroll = () => {
      const nearBottom = isNearLogBottom();
      userScrolledUpRef.current = !nearBottom;
      setShowJumpLatest((prev) => (prev === !nearBottom ? prev : !nearBottom));
      userScrollingRef.current = true;
      if (scrollGestureTimerRef.current) clearTimeout(scrollGestureTimerRef.current);
      scrollGestureTimerRef.current = setTimeout(() => {
        userScrollingRef.current = false;
      }, 180);
    };
    el.addEventListener("scroll", onScroll, { passive: true });
    return () => {
      el.removeEventListener("scroll", onScroll);
      if (scrollGestureTimerRef.current) clearTimeout(scrollGestureTimerRef.current);
    };
  }, [isNearLogBottom]);

  // Рост ленты — подтягиваем низ только если пользователь внизу.
  useEffect(() => {
    const el = logRef.current;
    if (!el) return;
    const scheduleScroll = createChatScrollScheduler(() => {
      if (userScrollingRef.current) return;
      if (forceScrollRef.current || !userScrolledUpRef.current) {
        scrollLogToEnd();
      }
    });
    const ro = new ResizeObserver(scheduleScroll);
    ro.observe(el);
    return () => ro.disconnect();
  }, [scrollLogToEnd]);

  /** Пока клавиатура анимируется — держим низ ленты у поля ввода. */
  useEffect(() => {
    if (!active) return;
    const vv = window.visualViewport;
    const onViewportChange = () => {
      const input = document.activeElement;
      if (!(input instanceof HTMLInputElement) || !input.classList.contains("chat__input")) return;
      if (userScrolledUpRef.current) return;
      scrollLogToEnd();
    };
    vv?.addEventListener("resize", onViewportChange);
    vv?.addEventListener("scroll", onViewportChange);
    return () => {
      vv?.removeEventListener("resize", onViewportChange);
      vv?.removeEventListener("scroll", onViewportChange);
    };
  }, [active, scrollLogToEnd]);

  /** После закрытия клавиатуры (тап по ленте) — не оставлять низ под полем ввода. */
  useEffect(() => {
    if (!active) return;
    const root = document.documentElement;
    let wasOpen = root.classList.contains("app-keyboard-open");
    const mo = new MutationObserver(() => {
      const open = root.classList.contains("app-keyboard-open");
      if (wasOpen && !open) {
        requestAnimationFrame(scrollLogToEndIfAllowed);
        window.setTimeout(scrollLogToEndIfAllowed, 120);
      }
      wasOpen = open;
    });
    mo.observe(root, { attributes: true, attributeFilter: ["class"] });
    return () => mo.disconnect();
  }, [active, scrollLogToEndIfAllowed]);

  useEffect(() => {
    const el = logRef.current;
    if (!el || !loaded) return;
    if (!didInitialScrollRef.current) {
      if (items.length === 0) return;
      didInitialScrollRef.current = true;
      userScrolledUpRef.current = false;
      setShowJumpLatest(false);
      tailMessageKeyRef.current = items[items.length - 1]?.uiKey ?? null;
      requestAnimationFrame(scrollLogToEnd);
      return;
    }

    const tailKey = items[items.length - 1]?.uiKey ?? null;
    const tailChanged = tailKey !== tailMessageKeyRef.current;
    tailMessageKeyRef.current = tailKey;

    if (forceScrollRef.current) {
      forceScrollRef.current = false;
      userScrolledUpRef.current = false;
      setShowJumpLatest(false);
      scrollLogToEnd();
      return;
    }

    if (
      tailChanged &&
      !userScrollingRef.current &&
      (!userScrolledUpRef.current || isNearLogBottom())
    ) {
      scrollLogToEnd();
    }
  }, [items, sending, leoTyping, loaded, isNearLogBottom, scrollLogToEnd]);

  useEffect(() => {
    if (!leoTyping) return;
    const baseline = baselineMaxForPendingLeoRef.current;
    const hasNewLeo = items.some(
      (m) => m.role === "leo" && m.serverID !== undefined && m.serverID > baseline
    );
    if (hasNewLeo) setLeoTyping(false);
  }, [items, leoTyping]);

  /** Если ответ так и не пришёл из БД — не держать «печатает» бесконечно. */
  useEffect(() => {
    if (!leoTyping) return;
    const timer = window.setTimeout(() => setLeoTyping(false), 180_000);
    return () => window.clearTimeout(timer);
  }, [leoTyping]);

  // Первая загрузка истории + инкрементальный polling каждые 3 секунды.
  // Источник правды — БД на сервере (см. /api/miniapp/personal-chat/feed).
  // Это даёт синхронизацию между всеми устройствами одного юзера (Telegram
  // Desktop / iPhone и т.д.).
  useEffect(() => {
    if (!active || !envApi || !inTelegram || !initData?.trim()) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const fetchSlice = async (sinceID: number) => {
      try {
        const res = await fetch(`${envApi}/api/miniapp/personal-chat/feed`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, since_id: sinceID }),
        });
        if (!res.ok) return;
        const j = (await res.json().catch(() => ({}))) as { ok?: boolean; messages?: ServerMsg[] };
        if (cancelled) return;
        const incoming = j.messages ?? [];
        if (incoming.length > 0) {
          setItems((prev) => mergeFromServer(prev, incoming));
        }
      } catch {
        // тихо: следующая итерация цикла исправит, если сеть моргнула
      }
    };

    const loop = async () => {
      // Первое чтение — since_id=0 (последние ~200 сообщений).
      // Дальше — since_id = max(server_id) уже виденных, инкрементально.
      let sinceID = 0;
      while (!cancelled) {
        await fetchSlice(sinceID);
        if (cancelled) return;
        setLoaded(true);
        // Берём актуальный max через функциональный setState.
        await new Promise<void>((r) => {
          setItems((prev) => {
            sinceID = Math.max(sinceID, maxServerID(prev));
            return prev;
          });
          // даём React отрисовать
          timer = setTimeout(() => r(), 0);
        });
        await new Promise<void>((r) => {
          timer = setTimeout(r, 3000);
        });
      }
    };
    void loop();
    return () => {
      cancelled = true;
      if (timer) clearTimeout(timer);
    };
  }, [active, inTelegram, initData]);

  /** Сброс бейджа «Лео»: сразу в UI, очередь на сервере — параллельно. */
  useEffect(() => {
    if (!active || !envApi || !inTelegram || !initData?.trim()) return;
    onInboxOpened?.();
    let cancelled = false;
    void clearLeoPersonalInbox(initData).then(() => {
      if (!cancelled) onInboxDrained?.();
    });
    return () => {
      cancelled = true;
    };
  }, [active, inTelegram, initData, onInboxOpened, onInboxDrained]);

  const onPickPhoto = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
      const f = e.target.files?.[0] ?? null;
      if (!f) return;
      if (!/^image\//.test(f.type)) {
        showAlert("Only images allowed (JPG, PNG, WEBP, GIF).");
        if (photoInputRef.current) photoInputRef.current.value = "";
        return;
      }
      if (f.size > 6 * 1024 * 1024) {
        showAlert("Photo too large. Max 6 MB.");
        if (photoInputRef.current) photoInputRef.current.value = "";
        return;
      }
      setPhoto(f);
      setPhotoPreview((prev) => {
        if (prev) URL.revokeObjectURL(prev);
        return URL.createObjectURL(f);
      });
    },
    [showAlert],
  );

  const clearPhoto = useCallback(() => {
    setPhoto(null);
    setPhotoPreview((prev) => {
      if (prev) URL.revokeObjectURL(prev);
      return null;
    });
    if (photoInputRef.current) photoInputRef.current.value = "";
  }, []);

  // Подчищаем blob превью и оптимистичные blob-URL на размонтировании.
  useEffect(() => {
    return () => {
      if (photoPreview) URL.revokeObjectURL(photoPreview);
      for (const u of optimisticBlobsRef.current) URL.revokeObjectURL(u);
      optimisticBlobsRef.current = [];
    };
  }, [photoPreview]);

  const send = useCallback(async () => {
    const t = text.trim();
    const sentPhoto = photo;
    if ((!t && !sentPhoto) || sending) return;
    if (!envApi) {
      showAlert(
        "Build missing API: in Railway → miniapp service → Variables → Build → set VITE_MINIAPP_API_URL to the public https URL of the bot service (ms_leo), then Redeploy."
      );
      return;
    }
    if (!inTelegram || !initData) {
      showAlert("Open the miniapp from Telegram (initData required).");
      return;
    }

    // Локальное превью для оптимистичного пузыря (фото видно сразу).
    const localPhotoUrl = sentPhoto ? URL.createObjectURL(sentPhoto) : undefined;
    if (localPhotoUrl) optimisticBlobsRef.current.push(localPhotoUrl);

    forceScrollRef.current = true;
    /** Макс. server id до оптимистичного сообщения — так отличим новый ответ Лео от старых. */
    let baselineMaxBeforeSend = 0;
    setItems((prev) => {
      baselineMaxBeforeSend = maxServerID(prev);
      return [
        ...prev,
        { uiKey: nowId(), role: "user", text: t, photoUrl: localPhotoUrl, createdAt: new Date().toISOString() },
      ];
    });

    setSending(true);
    setText("");
    clearPhoto();
    try {
      const w = window.Telegram?.WebApp;
      w?.HapticFeedback?.impactOccurred?.("light");
      let res: Response;
      if (sentPhoto) {
        const fd = new FormData();
        fd.append("init_data", initData);
        fd.append("text", t);
        fd.append("photo", sentPhoto, sentPhoto.name || "photo.jpg");
        res = await fetch(`${envApi}/api/miniapp/messages/photo`, {
          method: "POST",
          body: fd,
        });
      } else {
        res = await fetch(`${envApi}/api/miniapp/messages`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, text: t }),
        });
      }
      const j = (await res.json().catch(() => ({}))) as {
        error?: string;
        message?: string;
        ok?: boolean;
        pending?: boolean;
        reply_text?: string;
      };
      if (!res.ok) {
        const errMap: Record<string, string> = {
          media_not_configured: "Photo upload isn't configured on the server.",
          unsupported_image: "Couldn't read the photo. Try JPG, PNG, WEBP, or GIF.",
          photo_too_large: "Photo too large. Max 6 MB.",
          invalid_multipart: "Couldn't send the photo. Pick the image again.",
          missing_photo: "No photo attached.",
          chat_mismatch: "Open the miniapp from the pack chat",
        };
        showAlert(j.message ?? errMap[j.error ?? ""] ?? j.error ?? `Error ${res.status}`);
        setLeoTyping(false);
        return;
      }
      baselineMaxForPendingLeoRef.current = baselineMaxBeforeSend;
      setLeoTyping(true);
      // Основной поллер фида подхватит юзер-сообщение (с серверным id) и ответ Лео из БД — тогда items → useEffect выше снимает leoTyping.
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Network error");
      setLeoTyping(false);
    } finally {
      setSending(false);
    }
  }, [text, photo, sending, inTelegram, initData, showAlert, clearPhoto]);

  const toggleLike = useCallback(
    (messageID?: number) => {
      if (!messageID || !envApi || !inTelegram || !initData) return;

      let snapshot: ChatMsg[] | null = null;
      setItems((prev) => {
        snapshot = prev;
        return prev.map((m) => {
          if (m.serverID !== messageID) return m;
          const was = Boolean(m.likeMe);
          return {
            ...m,
            likeMe: !was,
            likeCount: Math.max(0, (m.likeCount ?? 0) + (was ? -1 : 1)),
          };
        });
      });

      void (async () => {
        try {
          const res = await fetch(`${envApi}/api/miniapp/personal-chat/like`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData, message_id: messageID }),
          });
          if (!res.ok && snapshot) setItems(snapshot);
        } catch {
          if (snapshot) setItems(snapshot);
        }
      })();
    },
    [inTelegram, initData],
  );

  const showTypingCue = sending || leoTyping;

  return (
    <div className={`chat${active && logSettled ? " chat--ready" : ""}`} ref={chatRef}>
      {!import.meta.env.VITE_MINIAPP_API_URL && (
        <div className="chat__configwarn" role="status">
          Missing <code className="chat__code">VITE_MINIAPP_API_URL</code> at build time. In Railway →{" "}
          <strong>miniapp</strong> service → Variables → <strong>Build</strong> → set the https URL of the{" "}
          <strong>bot</strong> service (ms_leo), then Redeploy.
        </div>
      )}
      <header className="chat__head">
        <div className="chat__head-avatarwrap">
          <img className="chat__head-avatar" src={LEO_AVATAR_URL} width={40} height={40} alt="Leo" loading="eager" />
          {showTypingCue && (
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
          <h1 className="chat__title">Leo</h1>
          <p className="chat__sub">{showTypingCue ? "typing…" : name}</p>
        </div>
      </header>
      <div className="chat__log-wrap">
        <div
          className="chat__log"
          role="log"
          aria-label="Messages with bot"
          ref={logRef}
        >
        {loaded && items.length === 0 && (
          <div className="chat__row chat__row--sys">
            <img className="chat__bubble-avatar" src={LEO_AVATAR_URL} width={32} height={32} alt="" aria-hidden="true" />
            <div className="chat__bubble-wrap chat__bubble-wrap--sys">
              <div className="chat__bubble chat__bubble--sys">
                Hi! This is your private chat with Leo — the AI replies to any text. Log workouts with the + button
                below. History syncs across all your devices.
              </div>
            </div>
          </div>
        )}
        {items.map((m) =>
          m.role === "user" ? (
            <div key={m.uiKey} className="chat__row chat__row--user">
              <div className="chat__bubble-wrap chat__bubble-wrap--user">
                <div className="chat__bubble chat__bubble--user">
                  {m.photoUrl && (
                    <button
                      type="button"
                      className="chat__photo-wrap"
                      aria-label="Open photo"
                      onClick={() => setLightboxUrl(m.photoUrl ?? null)}
                    >
                      <img className="chat__photo" src={m.photoUrl} alt="" loading="lazy" referrerPolicy="no-referrer" />
                    </button>
                  )}
                  {m.text.trim() !== "" && <span className="chat__bubble-text">{m.text}</span>}
                </div>
                <time className="chat__time chat__time--user" dateTime={m.createdAt}>
                  {formatChatTime(m.createdAt)}
                </time>
              </div>
            </div>
          ) : (
            <div key={m.uiKey} className="chat__row chat__row--sys">
              <img className="chat__bubble-avatar" src={LEO_AVATAR_URL} width={32} height={32} alt="" aria-hidden="true" />
              <div className="chat__bubble-wrap chat__bubble-wrap--sys">
                <div className="chat__bubble chat__bubble--sys">
                  <LeoTranslatableText text={m.text} initData={initData} as="div" className="chat__bubble-text" />
                </div>
                <div className="chat__meta">
                  <time className="chat__time chat__time--sys" dateTime={m.createdAt}>
                    {formatChatTime(m.createdAt)}
                  </time>
                  {m.serverID != null && (
                    <button
                      type="button"
                      className={`chat__like${m.likeMe ? " chat__like--mine" : ""}`}
                      aria-pressed={Boolean(m.likeMe)}
                      aria-label={m.likeMe ? "Remove like" : "Like"}
                      onClick={() => void toggleLike(m.serverID)}
                    >
                      <span aria-hidden="true">❤️</span>
                      <span>{m.likeCount ?? 0}</span>
                    </button>
                  )}
                </div>
              </div>
            </div>
          )
        )}
        {showTypingCue && (
          <div className="chat__row chat__row--sys" role="status" aria-live="polite" aria-label="Leo is typing">
            <img className="chat__bubble-avatar" src={LEO_AVATAR_URL} width={32} height={32} alt="" aria-hidden="true" />
            <div className="chat__bubble-wrap chat__bubble-wrap--sys">
              <div className="chat__bubble chat__bubble--sys chat__bubble--typing" aria-hidden="true">
                <span className="chat__typing-dots">
                  <span className="chat__dot" />
                  <span className="chat__dot" />
                  <span className="chat__dot" />
                </span>
              </div>
            </div>
          </div>
        )}
          <div ref={endRef} className="chat__log-end" aria-hidden="true" />
        </div>
        {showJumpLatest && (
          <button
            type="button"
            className="chat__jump-latest"
            aria-label="Jump to latest messages"
            onClick={() => {
              userScrolledUpRef.current = false;
              setShowJumpLatest(false);
              scrollLogToEnd();
            }}
          >
            ↓
          </button>
        )}
      </div>
      {photoPreview != null && (
        <div className="chat__photo-pending">
          <img className="chat__photo-pending-img" src={photoPreview} alt="" />
          <span className="chat__photo-pending-name">{photo?.name ?? "Photo"}</span>
          <button
            type="button"
            className="chat__photo-pending-remove"
            aria-label="Remove photo"
            onClick={clearPhoto}
          >
            ✕
          </button>
        </div>
      )}
      <form
        ref={formRef}
        className="chat__form"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        <input
          ref={photoInputRef}
          type="file"
          accept="image/*"
          className="chat__photo-input"
          onChange={onPickPhoto}
          tabIndex={-1}
          aria-hidden
        />
        <button
          type="button"
          className="chat__attach"
          aria-label="Attach photo from gallery"
          disabled={sending}
          onClick={() => photoInputRef.current?.click()}
        >
          📎
        </button>
        <CameraButton className="chat__attach" onChange={onPickPhoto} disabled={sending} />
        <input
          className="chat__input"
          value={text}
          onChange={(e) => setText(e.target.value)}
          onFocus={() => {
            if (!userScrolledUpRef.current) scrollLogToEnd();
          }}
          onBlur={() => {
            window.setTimeout(scrollLogToEndIfAllowed, 120);
          }}
          placeholder={photo ? "Photo caption (optional)…" : "Leo, any tips…"}
          maxLength={4000}
          autoComplete="off"
          autoCorrect="on"
          autoCapitalize="sentences"
          spellCheck
          enterKeyHint="send"
          inputMode="text"
        />
        <button
          type="submit"
          className="chat__send"
          disabled={sending || (!text.trim() && !photo)}
          aria-label={sending ? "Sending…" : "Send"}
        >
          {sending ? "…" : "➤"}
        </button>
      </form>
      {lightboxUrl != null && (
        <PhotoLightbox src={lightboxUrl} onClose={() => setLightboxUrl(null)} />
      )}
    </div>
  );
}
