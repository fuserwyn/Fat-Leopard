import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";
import {
  createChatScrollScheduler,
  isChatLogNearBottom,
  scrollChatLogToEnd,
} from "../lib/chatLogScroll";
import { formatChatTime } from "../lib/timeAgo";
import { resolveTrainingPhotoUrl } from "../lib/packFeed";
import { CameraButton } from "./CameraButton";
import { PhotoLightbox } from "./PhotoLightbox";
import "./ChatScreen.css";
import "./SupportScreen.css";

const envApi = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

type Props = {
  initData: string;
  inTelegram: boolean;
  showAlert: (m: string) => void;
  onClose: () => void;
};

type SupportMsg = {
  uiKey: string;
  serverID?: number;
  role: "user" | "support";
  text: string;
  photoUrl?: string;
  createdAt: string;
};

type ServerMsg = {
  id: number;
  role: "user" | "support";
  text: string;
  photo_url?: string;
  created_at: string;
};

function nowId() {
  return `support-local-${Date.now()}-${Math.random().toString(36).slice(2, 9)}`;
}

function mergeFromServer(prev: SupportMsg[], incoming: ServerMsg[]): SupportMsg[] {
  if (incoming.length === 0) return prev;
  const haveServerIDs = new Set<number>();
  for (const m of prev) {
    if (m.serverID) haveServerIDs.add(m.serverID);
  }
  const fresh = incoming.filter((m) => !haveServerIDs.has(m.id));
  const out = prev.slice();
  for (const sm of fresh) {
    if (sm.role === "user") {
      const idx = out.findIndex(
        (m) =>
          !m.serverID &&
          m.role === "user" &&
          m.text.trim() === sm.text.trim() &&
          Boolean(m.photoUrl) === Boolean(sm.photo_url),
      );
      if (idx >= 0) {
        out[idx] = {
          uiKey: `support-${sm.id}`,
          serverID: sm.id,
          role: "user",
          text: sm.text,
          photoUrl: resolveTrainingPhotoUrl(sm.photo_url),
          createdAt: sm.created_at,
        };
        continue;
      }
    }
    out.push({
      uiKey: `support-${sm.id}`,
      serverID: sm.id,
      role: sm.role,
      text: sm.text,
      photoUrl: resolveTrainingPhotoUrl(sm.photo_url),
      createdAt: sm.created_at,
    });
  }
  out.sort((a, b) => {
    if (a.serverID && b.serverID) return a.serverID - b.serverID;
    if (a.serverID && !b.serverID) return -1;
    if (!a.serverID && b.serverID) return 1;
    return new Date(a.createdAt).getTime() - new Date(b.createdAt).getTime();
  });
  return out;
}

function maxServerID(items: SupportMsg[]): number {
  let max = 0;
  for (const m of items) {
    if (m.serverID && m.serverID > max) max = m.serverID;
  }
  return max;
}

export function SupportScreen({ initData, inTelegram, showAlert, onClose }: Props) {
  const sheetRef = useRef<HTMLDivElement>(null);
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;

  const closeSheet = useCallback(() => {
    const el = document.activeElement;
    if (el instanceof HTMLElement) el.blur();
    onCloseRef.current();
  }, []);

  const [text, setText] = useState("");
  const [photo, setPhoto] = useState<File | null>(null);
  const [photoPreview, setPhotoPreview] = useState<string | null>(null);
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null);
  const photoInputRef = useRef<HTMLInputElement | null>(null);
  const optimisticBlobsRef = useRef<string[]>([]);
  const [sending, setSending] = useState(false);
  const sendingRef = useRef(sending);
  sendingRef.current = sending;
  const [items, setItems] = useState<SupportMsg[]>([]);
  const [loaded, setLoaded] = useState(false);
  const logRef = useRef<HTMLDivElement | null>(null);
  const didInitialScrollRef = useRef(false);
  const forceScrollRef = useRef(false);
  const userScrolledUpRef = useRef(false);
  const userScrollingRef = useRef(false);
  const scrollGestureTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);

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

  const clearPhoto = useCallback(() => {
    setPhoto(null);
    setPhotoPreview((prev) => {
      if (prev) URL.revokeObjectURL(prev);
      return null;
    });
    if (photoInputRef.current) photoInputRef.current.value = "";
  }, []);

  const onPickPhoto = useCallback(
    (e: ChangeEvent<HTMLInputElement>) => {
      const f = e.target.files?.[0];
      if (!f) return;
      setPhoto(f);
      setPhotoPreview((prev) => {
        if (prev) URL.revokeObjectURL(prev);
        return URL.createObjectURL(f);
      });
    },
    [],
  );

  useEffect(() => {
    return () => {
      if (photoPreview) URL.revokeObjectURL(photoPreview);
      for (const u of optimisticBlobsRef.current) URL.revokeObjectURL(u);
      optimisticBlobsRef.current = [];
    };
  }, [photoPreview]);

  useEffect(() => {
    const el = logRef.current;
    if (!el) return;
    const onScroll = () => {
      userScrolledUpRef.current = !isNearLogBottom();
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
  }, [scrollLogToEnd]);

  /** После закрытия клавиатуры — не оставлять низ под полем ввода. */
  useEffect(() => {
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
  }, [scrollLogToEndIfAllowed]);

  useEffect(() => {
    const el = logRef.current;
    if (!el || !loaded) return;
    if (!didInitialScrollRef.current) {
      if (items.length === 0) return;
      didInitialScrollRef.current = true;
      userScrolledUpRef.current = false;
      requestAnimationFrame(scrollLogToEnd);
      return;
    }
    if (forceScrollRef.current) {
      forceScrollRef.current = false;
      userScrolledUpRef.current = false;
      scrollLogToEnd();
      return;
    }
    if (userScrollingRef.current) return;
    if (isNearLogBottom(120)) scrollLogToEnd();
  }, [items, sending, loaded, isNearLogBottom, scrollLogToEnd]);

  useEffect(() => {
    if (!envApi || !inTelegram || !initData?.trim()) return;
    let cancelled = false;
    let timer: ReturnType<typeof setTimeout> | null = null;

    const fetchSlice = async (sinceID: number) => {
      try {
        const res = await fetch(`${envApi}/api/miniapp/support/feed`, {
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
        return;
      }
    };

    const loop = async () => {
      let sinceID = 0;
      while (!cancelled) {
        await fetchSlice(sinceID);
        if (cancelled) return;
        setLoaded(true);
        await new Promise<void>((r) => {
          setItems((prev) => {
            sinceID = Math.max(sinceID, maxServerID(prev));
            return prev;
          });
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
  }, [inTelegram, initData]);

  const send = useCallback(async () => {
    const t = text.trim();
    const sentPhoto = photo;
    if ((!t && !sentPhoto) || sending) return;
    if (!envApi) {
      showAlert("Support API not configured.");
      return;
    }
    if (!inTelegram || !initData) {
      showAlert("Open the miniapp from Telegram (initData required).");
      return;
    }

    const localPhotoUrl = sentPhoto ? URL.createObjectURL(sentPhoto) : undefined;
    if (localPhotoUrl) optimisticBlobsRef.current.push(localPhotoUrl);

    forceScrollRef.current = true;
    setItems((prev) => [
      ...prev,
      { uiKey: nowId(), role: "user", text: t, photoUrl: localPhotoUrl, createdAt: new Date().toISOString() },
    ]);
    setSending(true);
    setText("");
    clearPhoto();
    try {
      let res: Response;
      if (sentPhoto) {
        const fd = new FormData();
        fd.append("init_data", initData);
        fd.append("text", t);
        fd.append("photo", sentPhoto, sentPhoto.name || "photo.jpg");
        res = await fetch(`${envApi}/api/miniapp/support/send/photo`, { method: "POST", body: fd });
      } else {
        res = await fetch(`${envApi}/api/miniapp/support/send`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, text: t }),
        });
      }
      const j = (await res.json().catch(() => ({}))) as { error?: string; message?: string; ok?: boolean };
      if (!res.ok || !j.ok) {
        const errMap: Record<string, string> = {
          media_not_configured: "Photo upload isn’t configured on the server.",
          unsupported_image: "Couldn’t read the photo. Try JPG, PNG, WEBP, or GIF.",
          photo_too_large: "Photo too large. Max 6 MB.",
          invalid_multipart: "Couldn’t send the photo. Pick the image again.",
          missing_photo: "No photo attached.",
          chat_mismatch: "Open the miniapp from the pack chat",
        };
        showAlert(j.message ?? errMap[j.error ?? ""] ?? j.error ?? `Error ${res.status}`);
      }
    } catch (e) {
      showAlert(e instanceof Error ? e.message : "Network error");
    } finally {
      setSending(false);
    }
  }, [text, photo, sending, inTelegram, initData, showAlert, clearPhoto]);

  useEffect(() => {
    const sheet = sheetRef.current;
    const log = logRef.current;
    if (!sheet) return;
    let startY = 0;
    let dy = 0;
    let tracking = false;
    let dragging = false;

    const onStart = (e: TouchEvent) => {
      if (sendingRef.current) return;
      const t = e.touches[0];
      if (!t) return;
      const target = e.target as HTMLElement | null;
      if (target?.closest("textarea, input, select, [contenteditable='true']")) return;
      if (log?.contains(target) && (log?.scrollTop ?? 0) > 1) return;
      startY = t.clientY;
      dy = 0;
      dragging = false;
      tracking = true;
    };

    const onMove = (e: TouchEvent) => {
      if (!tracking) return;
      const t = e.touches[0];
      if (!t) return;
      dy = t.clientY - startY;
      if (!dragging) {
        if (dy > 14) {
          dragging = true;
          sheet.style.transition = "none";
        } else {
          if (dy < -8) tracking = false;
          return;
        }
      }
      e.preventDefault();
      sheet.style.transform = `translateY(${Math.max(0, dy)}px)`;
    };

    const onEnd = () => {
      if (!tracking) return;
      tracking = false;
      if (!dragging) return;
      dragging = false;
      if (dy > 96) {
        const active = document.activeElement;
        if (active instanceof HTMLElement) active.blur();
        sheet.style.transition = "transform 0.18s ease-in";
        sheet.style.transform = "translateY(105%)";
        window.setTimeout(() => onCloseRef.current(), 170);
      } else {
        sheet.style.transition = "transform 0.18s ease-out";
        sheet.style.transform = "";
      }
    };

    sheet.addEventListener("touchstart", onStart, { passive: true });
    sheet.addEventListener("touchmove", onMove, { passive: false });
    sheet.addEventListener("touchend", onEnd);
    sheet.addEventListener("touchcancel", onEnd);
    return () => {
      sheet.removeEventListener("touchstart", onStart);
      sheet.removeEventListener("touchmove", onMove);
      sheet.removeEventListener("touchend", onEnd);
      sheet.removeEventListener("touchcancel", onEnd);
    };
  }, []);

  const renderBubbleContent = (m: SupportMsg) => (
    <>
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
      {m.text ? <span>{m.text}</span> : null}
    </>
  );

  return (
    <>
      <div
        className="sup-backdrop"
        aria-hidden="true"
        onClick={() => {
          if (!sending) closeSheet();
        }}
      />
      <div ref={sheetRef} className="sup" role="dialog" aria-modal="true" aria-label="Support">
        <div className="sup__grabber" aria-hidden="true" />
        <div className="chat support-chat">
          <header className="chat__head">
            <div className="support-chat__head-avatar" aria-hidden>
              ?
            </div>
            <div className="chat__head-text">
              <h1 className="chat__title">Support</h1>
              <p className="chat__sub">A human replies here, not Leo</p>
            </div>
            <button type="button" className="sup__close" onClick={closeSheet} aria-label="Close">
              ✕
            </button>
          </header>
          <div className="chat__log" role="log" aria-label="Support messages" ref={logRef}>
            {loaded && items.length === 0 && (
              <div className="chat__row chat__row--sys">
                <div className="support-chat__bubble-avatar" aria-hidden>
                  ?
                </div>
                <div className="chat__bubble chat__bubble--sys">
                  Message support if something broke, you need billing help, or the question isn’t for Leo.
                </div>
              </div>
            )}
            {items.map((m) =>
              m.role === "user" ? (
                <div key={m.uiKey} className="chat__row chat__row--user">
                  <div className="chat__bubble-wrap chat__bubble-wrap--user">
                    <div className="chat__bubble chat__bubble--user">{renderBubbleContent(m)}</div>
                    <div className="chat__time chat__time--user">{formatChatTime(m.createdAt)}</div>
                  </div>
                </div>
              ) : (
                <div key={m.uiKey} className="chat__row chat__row--sys">
                  <div className="support-chat__bubble-avatar" aria-hidden>
                    ?
                  </div>
                  <div className="chat__bubble-wrap chat__bubble-wrap--sys">
                    <div className="chat__bubble chat__bubble--sys">{renderBubbleContent(m)}</div>
                    <div className="chat__time chat__time--sys">{formatChatTime(m.createdAt)}</div>
                  </div>
                </div>
              ),
            )}
          </div>
          {photoPreview != null && (
            <div className="chat__photo-pending">
              <img className="chat__photo-pending-img" src={photoPreview} alt="" />
              <span className="chat__photo-pending-name">{photo?.name ?? "Photo"}</span>
              <button type="button" className="chat__photo-pending-remove" aria-label="Remove photo" onClick={clearPhoto}>
                ✕
              </button>
            </div>
          )}
          <form
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
              placeholder={photo ? "Photo caption (optional)…" : "Message to support…"}
              maxLength={4000}
              autoComplete="off"
              enterKeyHint="send"
            />
            <button type="submit" className="chat__send" disabled={sending || (!text.trim() && !photo)}>
              {sending ? "…" : "➤"}
            </button>
          </form>
        </div>
      </div>
      {lightboxUrl && <PhotoLightbox src={lightboxUrl} onClose={() => setLightboxUrl(null)} />}
    </>
  );
}
