import {
  useCallback,
  useEffect,
  useLayoutEffect,
  useMemo,
  useRef,
  useState,
  type ChangeEvent,
  type ReactNode,
  type RefObject,
  type TouchEvent as ReactTouchEvent,
} from "react";
import { createPortal } from "react-dom";
import { formatChatTime } from "../lib/timeAgo";
import {
  createChatScrollScheduler,
  isChatLogNearBottom,
  scrollChatLogToEnd,
} from "../lib/chatLogScroll";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { moderationUserMessage, isModerationError } from "../lib/moderationMessages";
import { clearPackGroupUnread, fetchPackGroupUnreadSummary } from "../lib/packGroupUnread";
import {
  mergePackFeedReactions,
  optimisticTogglePackFeedReaction,
  TRAINING_FEED_APPROVING_EMOJIS,
  votersToLikers,
  resolveFeedAvatarUrl,
  resolveTrainingPhotoUrl,
  type PackFeedReactionDTO,
  type VoterDTO,
} from "../lib/packFeed";
import { LikersPopover, pointerCanHover, useChipPress, useLikersPopover, type LikerGroup } from "./Likers";
import { PhotoLightbox } from "./PhotoLightbox";
import { CameraButton } from "./CameraButton";
import "./ActivityCard.css";
import "./PackGroupChatPanel.css";

type ChatReaction = { emoji: string; count: number; me: boolean; voters?: VoterDTO[] | string[] };

/** Чип реакции в чате: тап = поставить/снять, зажатие = показать список отреагировавших. */
function PackReactionChip({
  r,
  onReact,
  onLongPress,
}: {
  r: ChatReaction;
  onReact: (emoji: string) => void;
  onLongPress?: () => void;
}) {
  const press = useChipPress(() => onReact(r.emoji), onLongPress);
  return (
    <button type="button" className={`act-card__react-btn${r.me ? " act-card__react-btn--mine" : ""}`} {...press}>
      {r.emoji}
      {r.count > 0 && <span className="act-card__react-cnt">{r.count}</span>}
    </button>
  );
}

/** Активные реакции на сообщение чата + поповер «кто отреагировал» (зажатие). */
function PackMessageReactions({
  reactions,
  onReact,
}: {
  reactions: ChatReaction[];
  onReact: (emoji: string) => void;
}) {
  const anchorRef = useRef<HTMLDivElement>(null);
  const { open, setOpen, style, popRef } = useLikersPopover(anchorRef);
  const groups: LikerGroup[] = reactions
    .filter((r) => r.count > 0 && Array.isArray(r.voters) && r.voters.length > 0)
    .map((r) => ({ emoji: r.emoji, voters: votersToLikers(r.voters) }));
  const hasLikers = groups.length > 0;
  const openLikers = () => {
    if (hasLikers) setOpen(true);
  };
  // На десктопе показываем «кто отреагировал» как полноценный hover-тултип:
  // наведение на чипы открывает поповер, уход курсора — закрывает. На тач-устройствах
  // (где hover синтетический) обработчики не подключаем — там просмотр через зажатие.
  const hoverProps = pointerCanHover()
    ? { onMouseEnter: openLikers, onMouseLeave: () => setOpen(false) }
    : undefined;
  return (
    <div
      className="packroom__react-chips"
      role="group"
      aria-label="Реакции на сообщение"
      ref={anchorRef}
      {...hoverProps}
    >
      {reactions.map((r) => (
        <PackReactionChip key={r.emoji} r={r} onReact={onReact} onLongPress={hasLikers ? openLikers : undefined} />
      ))}
      {open && hasLikers && <LikersPopover groups={groups} popRef={popRef} style={style} />}
    </div>
  );
}

function PackGroupMessageMenu({
  reactions,
  onReaction,
  canReport,
  onReport,
  reporting = false,
  scrollRootRef,
}: {
  reactions: { emoji: string; count: number; me?: boolean }[];
  onReaction: (emoji: string) => void;
  canReport?: boolean;
  onReport?: () => void;
  reporting?: boolean;
  scrollRootRef?: RefObject<HTMLElement | null>;
}) {
  const wrapRef = useRef<HTMLDivElement>(null);
  const popoverRef = useRef<HTMLDivElement>(null);
  const [open, setOpen] = useState(false);
  const [popoverPos, setPopoverPos] = useState<{ top: number; left: number } | null>(null);

  const layoutPopover = useCallback(() => {
    const wrap = wrapRef.current;
    const popover = popoverRef.current;
    if (!wrap || !popover) return;
    const toggle = wrap.querySelector(".packroom__more-toggle");
    if (!(toggle instanceof HTMLElement)) return;

    const tr = toggle.getBoundingClientRect();
    const popW = popover.offsetWidth;
    const gap = 8;
    const margin = 10;

    // Нижняя граница — верх поля ввода (композер + таббар), а НЕ низ экрана:
    // иначе нижние эмодзи уходят под форму ввода и до них нельзя доскроллить.
    const composer = document.querySelector(".packroom__form");
    const bottomLimit =
      composer instanceof HTMLElement
        ? Math.min(window.innerHeight - margin, composer.getBoundingClientRect().top - gap)
        : window.innerHeight - margin;
    const topLimit = margin;

    // Натуральная высота (без ограничения), чтобы решить, куда раскрывать.
    popover.style.maxHeight = "";
    const naturalH = popover.offsetHeight;

    const spaceBelow = bottomLimit - (tr.bottom + gap);
    const spaceAbove = tr.top - gap - topLimit;
    const openUp = spaceBelow < Math.min(naturalH, 220) && spaceAbove > spaceBelow;

    const avail = Math.max(140, Math.floor(openUp ? spaceAbove : spaceBelow));
    popover.style.maxHeight = `${Math.min(320, avail)}px`;
    const popH = Math.min(popover.offsetHeight, Math.min(320, avail));

    let top = openUp ? tr.top - gap - popH : tr.bottom + gap;
    if (top < topLimit) top = topLimit;
    if (top + popH > bottomLimit) top = Math.max(topLimit, bottomLimit - popH);

    let left = tr.right - popW;
    if (left < margin) left = margin;
    if (left + popW > window.innerWidth - margin) {
      left = Math.max(margin, window.innerWidth - margin - popW);
    }

    setPopoverPos({ top, left });
  }, []);

  useLayoutEffect(() => {
    if (!open) {
      setPopoverPos(null);
      return;
    }
    layoutPopover();
    const ro = new ResizeObserver(layoutPopover);
    if (popoverRef.current) ro.observe(popoverRef.current);
    window.addEventListener("resize", layoutPopover);
    return () => {
      ro.disconnect();
      window.removeEventListener("resize", layoutPopover);
    };
  }, [open, layoutPopover, reactions.length, canReport]);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent | TouchEvent) => {
      const t = e.target as Node;
      if (wrapRef.current?.contains(t) || popoverRef.current?.contains(t)) return;
      setOpen(false);
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    const onScroll = () => setOpen(false);
    document.addEventListener("mousedown", close);
    document.addEventListener("touchstart", close);
    document.addEventListener("keydown", esc);
    scrollRootRef?.current?.addEventListener("scroll", onScroll, { passive: true });
    window.addEventListener("scroll", onScroll, { passive: true, capture: true });
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("touchstart", close);
      document.removeEventListener("keydown", esc);
      scrollRootRef?.current?.removeEventListener("scroll", onScroll);
      window.removeEventListener("scroll", onScroll, { capture: true });
    };
  }, [open, scrollRootRef]);

  const confirmReport = () => {
    if (
      !window.confirm(
        "Отправить жалобу на это сообщение?\n\nАдмины проверят его в разделе поддержки.",
      )
    ) {
      return;
    }
    setOpen(false);
    onReport?.();
  };

  return (
    <div className={`packroom__more${open ? " packroom__more--open" : ""}`} ref={wrapRef}>
      <button
        type="button"
        className={`packroom__more-toggle${open ? " packroom__more-toggle--open" : ""}`}
        aria-expanded={open}
        aria-haspopup="menu"
        aria-label="Реакции и действия"
        disabled={reporting}
        onClick={() => setOpen((v) => !v)}
      >
        {reporting ? "…" : "⋯"}
      </button>
      {open && !reporting
        ? createPortal(
            <div
              ref={popoverRef}
              className="packroom__more-popover"
              role="menu"
              style={
                popoverPos
                  ? { position: "fixed", top: popoverPos.top, left: popoverPos.left, zIndex: 250 }
                  : { position: "fixed", visibility: "hidden", top: 0, left: 0, zIndex: 250 }
              }
              onTouchMove={(e) => e.stopPropagation()}
              onTouchStart={(e) => e.stopPropagation()}
            >
              <div className="packroom__more-emojis" role="group" aria-label="Реакции">
                {reactions.map((r) => (
                  <button
                    key={r.emoji}
                    type="button"
                    className={`act-card__react-btn${r.me ? " act-card__react-btn--mine" : ""}`}
                    onClick={() => {
                      onReaction(r.emoji);
                      setOpen(false);
                    }}
                  >
                    {r.emoji}
                    {r.count > 0 && <span className="act-card__react-cnt">{r.count}</span>}
                  </button>
                ))}
              </div>
              {canReport && onReport != null && (
                <div className="packroom__more-footer">
                  <button type="button" className="packroom__more-item" role="menuitem" onClick={confirmReport}>
                    Пожаловаться на сообщение
                  </button>
                </div>
              )}
            </div>,
            document.body,
          )
        : null}
    </div>
  );
}

// Свайп сообщения влево = "Ответить". Жест распознаётся как горизонтальный,
// чтобы не мешать вертикальному скроллу ленты; при достижении порога даём haptic.
function SwipeToReply({ onReply, children }: { onReply: () => void; children: ReactNode }) {
  const startX = useRef(0);
  const startY = useRef(0);
  const dir = useRef<null | "h" | "v">(null);
  const passed = useRef(false);
  const lastDx = useRef(0);
  const [dx, setDx] = useState(0);
  const [snap, setSnap] = useState(false);

  const THRESHOLD = 56;
  const MAX = 84;

  const hapticLight = () => {
    window.Telegram?.WebApp?.HapticFeedback?.impactOccurred?.("light");
  };

  const onTouchStart = (e: ReactTouchEvent<HTMLDivElement>) => {
    if (e.touches.length !== 1) return;
    startX.current = e.touches[0].clientX;
    startY.current = e.touches[0].clientY;
    dir.current = null;
    passed.current = false;
    lastDx.current = 0;
    setSnap(false);
  };

  const onTouchMove = (e: ReactTouchEvent<HTMLDivElement>) => {
    if (e.touches.length !== 1) return;
    const deltaX = e.touches[0].clientX - startX.current;
    const deltaY = e.touches[0].clientY - startY.current;
    if (dir.current === null) {
      if (Math.abs(deltaX) < 14 && Math.abs(deltaY) < 14) return;
      dir.current = Math.abs(deltaX) > Math.abs(deltaY) * 1.85 ? "h" : "v";
    }
    if (dir.current !== "h") return;
    let d = deltaX;
    if (d > 0) d = 0;
    if (d < -MAX) d = -MAX;
    lastDx.current = d;
    setDx(d);
    const reached = -d >= THRESHOLD;
    if (reached && !passed.current) {
      passed.current = true;
      hapticLight();
    } else if (!reached && passed.current) {
      passed.current = false;
    }
  };

  const finish = () => {
    const reached = -lastDx.current >= THRESHOLD;
    setSnap(true);
    setDx(0);
    lastDx.current = 0;
    dir.current = null;
    passed.current = false;
    if (reached) onReply();
  };

  const reached = -dx >= THRESHOLD;

  return (
    <div className="packroom__swipe">
      <span
        className={`packroom__swipe-hint${reached ? " packroom__swipe-hint--active" : ""}`}
        style={{ opacity: Math.min(1, -dx / THRESHOLD) }}
        aria-hidden
      >
        ↩
      </span>
      <div
        className="packroom__row-inner"
        style={{ transform: `translateX(${dx}px)`, transition: snap ? "transform 0.18s ease" : "none" }}
        onTouchStart={onTouchStart}
        onTouchMove={onTouchMove}
        onTouchEnd={finish}
        onTouchCancel={finish}
      >
        {children}
      </div>
    </div>
  );
}

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export type PackGroupMessage = {
  id: number;
  user_id: number;
  username: string;
  text: string;
  created_at: string;
  is_leo: boolean;
  author_photo_url?: string;
  reply_to_id?: number;
  reply_to_username?: string;
  reply_to_text?: string;
  reply_to_is_leo?: boolean;
  edited_at?: string;
  reactions?: PackFeedReactionDTO[];
  /** Фото-вложение (полный публичный URL; для оптимистичной вставки — локальный blob:). */
  photo_url?: string;
};

type ReplyIntent = {
  replyToMessageId: number;
  authorLabel: string;
  excerpt: string;
};

type EditIntent = {
  messageId: number;
  originalText: string;
};

function excerptText(text: string, max = 100): string {
  const t = text.trim();
  if (t.length <= max) return t;
  return `${t.slice(0, max - 1).trim()}…`;
}

// Подсветка совпадений поиска внутри текста сообщения (регистронезависимо).
function highlightSearch(text: string, query: string): ReactNode {
  const q = query.trim();
  if (q === "") return text;
  const lower = text.toLowerCase();
  const ql = q.toLowerCase();
  const parts: ReactNode[] = [];
  let i = 0;
  let key = 0;
  while (i < text.length) {
    const idx = lower.indexOf(ql, i);
    if (idx === -1) {
      parts.push(text.slice(i));
      break;
    }
    if (idx > i) parts.push(text.slice(i, idx));
    parts.push(
      <mark key={key++} className="packroom__hl">
        {text.slice(idx, idx + q.length)}
      </mark>,
    );
    i = idx + q.length;
  }
  return parts;
}

function resolveReplyQuote(
  m: PackGroupMessage,
  byId: Map<number, PackGroupMessage>,
): { author: string; text: string; parentId: number } | null {
  const parentId = m.reply_to_id ?? 0;
  if (parentId <= 0) return null;

  const serverAuthor = m.reply_to_is_leo ? "Лео" : (m.reply_to_username || "").trim();
  const serverText = (m.reply_to_text || "").trim();
  if (serverAuthor !== "" || serverText !== "") {
    return { author: serverAuthor || "Сообщение", text: serverText, parentId };
  }

  const parent = byId.get(parentId);
  if (parent) {
    return {
      author: parent.is_leo ? "Лео" : parent.username,
      text: excerptText(parent.text),
      parentId,
    };
  }
  return null;
}

type Props = {
  initData: string;
  inTelegram: boolean;
  meId: number;
  showAlert: (m: string) => void;
  onHaptic?: () => void;
  /** Подвкладка «Чат» в ленте видима (keep-alive). */
  active?: boolean;
  onRefreshTabBadges?: () => void;
  /** Сразу убрать бейдж общего чата из UI (до ответа сервера). */
  onPackGroupChatOpened?: () => void;
  /** Зритель — админ: может удалять чужие сообщения в чате стаи (модерация). */
  isAdmin?: boolean;
};

export function PackGroupChatPanel({
  initData,
  inTelegram,
  meId,
  showAlert,
  onHaptic,
  active = true,
  onRefreshTabBadges,
  onPackGroupChatOpened,
  isAdmin = false,
}: Props) {
  const [items, setItems] = useState<PackGroupMessage[]>([]);
  const [text, setText] = useState("");
  const [replyIntent, setReplyIntent] = useState<ReplyIntent | null>(null);
  const [editIntent, setEditIntent] = useState<EditIntent | null>(null);
  const [sending, setSending] = useState(false);
  const [photo, setPhoto] = useState<File | null>(null);
  const [photoPreview, setPhotoPreview] = useState<string | null>(null);
  const [lightboxUrl, setLightboxUrl] = useState<string | null>(null);
  const photoInputRef = useRef<HTMLInputElement | null>(null);
  const [reportPosting, setReportPosting] = useState<Record<number, boolean>>({});
  const [err, setErr] = useState<string | null>(null);
  const endRef = useRef<HTMLDivElement | null>(null);
  const logRef = useRef<HTMLDivElement | null>(null);
  const roomRef = useRef<HTMLDivElement | null>(null);
  const formRef = useRef<HTMLFormElement | null>(null);
  const inputRef = useRef<HTMLInputElement | null>(null);
  /** true когда пользователь прокрутил вверх — не перебиваем позицию при поллинге */
  const userScrolledUpRef = useRef(false);
  /** Пока идёт жест скролла — не дёргаем scrollTop из ResizeObserver/поллинга. */
  const userScrollingRef = useRef(false);
  const scrollGestureTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const tailMessageIdRef = useRef<number | null>(null);
  const didInitialScrollRef = useRef(false);
  /** После отправки — всегда вниз, даже если читал историю выше */
  const forceScrollRef = useRef(false);
  const wasActiveRef = useRef(false);
  const [highlightMessageId, setHighlightMessageId] = useState<number | null>(null);
  const [unreadMessageIds, setUnreadMessageIds] = useState<Set<number>>(() => new Set());
  const highlightTimerRef = useRef<ReturnType<typeof setTimeout> | null>(null);
  const pendingUnreadScrollRef = useRef<number | null>(null);
  const [searchQuery, setSearchQuery] = useState("");
  const [serverResults, setServerResults] = useState<PackGroupMessage[]>([]);
  const [searching, setSearching] = useState(false);

  const isNearBottom = useCallback((el: HTMLDivElement, threshold = 80) => {
    return isChatLogNearBottom(el, threshold);
  }, []);

  const scrollToBottom = useCallback(() => {
    const el = logRef.current;
    if (!el) return;
    scrollChatLogToEnd(el);
  }, []);

  const scrollToBottomIfAllowed = useCallback(() => {
    const el = logRef.current;
    if (!el || userScrollingRef.current) return;
    if (userScrolledUpRef.current && !isNearBottom(el, 120)) return;
    scrollToBottom();
  }, [isNearBottom, scrollToBottom]);

  /** Пока клавиатура анимируется — держим низ ленты у поля ввода. */
  useEffect(() => {
    if (!active) return;
    const vv = window.visualViewport;
    const onViewportChange = () => {
      const input = document.activeElement;
      if (!(input instanceof HTMLInputElement) || !input.classList.contains("packroom__input")) return;
      if (userScrolledUpRef.current) return;
      scrollToBottom();
    };
    vv?.addEventListener("resize", onViewportChange);
    vv?.addEventListener("scroll", onViewportChange);
    return () => {
      vv?.removeEventListener("resize", onViewportChange);
      vv?.removeEventListener("scroll", onViewportChange);
    };
  }, [active, scrollToBottom]);

  /** После закрытия клавиатуры (тап по ленте) — не оставлять низ под полем ввода. */
  useEffect(() => {
    if (!active) return;
    const root = document.documentElement;
    let wasOpen = root.classList.contains("app-keyboard-open");
    const mo = new MutationObserver(() => {
      const open = root.classList.contains("app-keyboard-open");
      if (wasOpen && !open) {
        requestAnimationFrame(scrollToBottomIfAllowed);
        window.setTimeout(scrollToBottomIfAllowed, 120);
      }
      wasOpen = open;
    });
    mo.observe(root, { attributes: true, attributeFilter: ["class"] });
    return () => mo.disconnect();
  }, [active, scrollToBottomIfAllowed]);

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

  const bootstrapUnreadAndLoad = useCallback(async () => {
    if (!inTelegram || !initData.trim()) return;
    onPackGroupChatOpened?.();
    const summary = await fetchPackGroupUnreadSummary(initData);
    setUnreadMessageIds(new Set(summary.messageIds));
    pendingUnreadScrollRef.current = summary.messageIds[0] ?? null;
    void clearPackGroupUnread(initData).then(() => onRefreshTabBadges?.());
    void load();
  }, [inTelegram, initData, load, onRefreshTabBadges, onPackGroupChatOpened]);

  useEffect(() => {
    if (!active) return;
    void bootstrapUnreadAndLoad();
  }, [active, bootstrapUnreadAndLoad]);

  // Пока чат открыт, входящие сообщения (Лео или другой участник) сразу помечаем
  // прочитанными на сервере и гасим бейдж. Presence у мини-аппа нет: бэкенд создаёт
  // «непрочитанное» даже когда пользователь уже смотрит чат, и фоновый опрос бейджей
  // в App.tsx иначе вернул бы значок, хотя сообщение уже на экране.
  const lastSeenTailRef = useRef<number | null>(null);
  useEffect(() => {
    if (!active) {
      lastSeenTailRef.current = null;
      return;
    }
    const tail = items.length > 0 ? items[items.length - 1] : null;
    if (!tail || tail.id <= 0) return; // нет серверного хвоста или оптимистичная вставка
    const prev = lastSeenTailRef.current;
    lastSeenTailRef.current = tail.id;
    if (prev == null || tail.id === prev) return; // первый замер: bootstrap уже почистил
    const incoming = tail.is_leo || tail.user_id !== meId;
    if (!incoming) return; // своё сообщение непрочитанного не создаёт
    void clearPackGroupUnread(initData).then(() => onRefreshTabBadges?.());
  }, [active, items, initData, meId, onRefreshTabBadges]);

  useEffect(() => {
    if (!active) {
      didInitialScrollRef.current = false;
      userScrolledUpRef.current = false;
      wasActiveRef.current = false;
      return;
    }
    if (!wasActiveRef.current) {
      didInitialScrollRef.current = false;
      userScrolledUpRef.current = false;
      forceScrollRef.current = true;
      wasActiveRef.current = true;
    }
  }, [active]);

  // body--lock только пока открыта подвкладка «Чат» стаи.
  // Unlock в render / useLayoutEffect, чтобы restore скролла ленты не гонялся с #root padding.
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
    return () => document.body.classList.remove("body--lock");
  }, [active]);

  useEffect(() => {
    if (!active || !apiBase || !inTelegram || !initData) return;
    const t = setInterval(() => void load(), 5000);
    return () => clearInterval(t);
  }, [load, active, inTelegram, initData]);

  // Отслеживаем, прокрутил ли пользователь вверх, чтобы не перебивать позицию при поллинге.
  useEffect(() => {
    const el = logRef.current;
    if (!el) return;
    const onScroll = () => {
      userScrolledUpRef.current = !isNearBottom(el);
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
  }, [isNearBottom]);

  // Рост ленты (поллинг, аватары) — держим конец видимым, если пользователь внизу.
  useEffect(() => {
    const el = logRef.current;
    if (!el) return;
    const inner = el.firstElementChild;
    if (!(inner instanceof HTMLElement)) return;

    const scheduleScroll = createChatScrollScheduler(() => {
      if (userScrollingRef.current) return;
      if (forceScrollRef.current || !userScrolledUpRef.current) {
        scrollToBottom();
      }
    });

    const ro = new ResizeObserver(scheduleScroll);
    ro.observe(inner);
    const onImgLoad = (e: Event) => {
      if (!(e.target instanceof HTMLImageElement)) return;
      if (!e.target.classList.contains("packroom__avatar")) return;
      scheduleScroll();
    };
    inner.addEventListener("load", onImgLoad, true);
    return () => {
      ro.disconnect();
      inner.removeEventListener("load", onImgLoad, true);
    };
  }, [items.length, scrollToBottom]);

  const scrollToQuotedMessage = useCallback((messageId: number) => {
    const log = logRef.current;
    if (!log || messageId <= 0) return;
    const target = log.querySelector<HTMLElement>(`[data-pack-msg-id="${messageId}"]`);
    if (!target) return;
    target.scrollIntoView({ block: "center", behavior: "smooth" });
    setHighlightMessageId(messageId);
    if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current);
    highlightTimerRef.current = setTimeout(() => setHighlightMessageId(null), 1600);
  }, []);

  useEffect(() => {
    if (!active) return;
    const el = logRef.current;
    if (!el || items.length === 0) return;

    if (!didInitialScrollRef.current) {
      didInitialScrollRef.current = true;
      userScrolledUpRef.current = false;
      tailMessageIdRef.current = items[items.length - 1]?.id ?? null;
      const firstUnread = pendingUnreadScrollRef.current;
      if (firstUnread != null && firstUnread > 0) {
        pendingUnreadScrollRef.current = null;
        window.setTimeout(() => scrollToQuotedMessage(firstUnread), 60);
        return;
      }
      scrollToBottom();
      requestAnimationFrame(scrollToBottom);
      return;
    }

    const tailId = items[items.length - 1]?.id ?? null;
    const tailChanged = tailId !== tailMessageIdRef.current;
    tailMessageIdRef.current = tailId;

    if (forceScrollRef.current) {
      forceScrollRef.current = false;
      userScrolledUpRef.current = false;
      scrollToBottom();
      return;
    }

    if (tailChanged && !userScrollingRef.current && (!userScrolledUpRef.current || isNearBottom(el))) {
      scrollToBottom();
    }
  }, [active, items, isNearBottom, scrollToBottom, scrollToQuotedMessage]);

  const send = useCallback(async () => {
    const t = text.trim();
    if ((!t && !photo) || sending) return;
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

    // Режим редактирования: правим существующее сообщение, не создаём новое.
    if (editIntent != null) {
      const editId = editIntent.messageId;
      const original = editIntent.originalText;
      setText("");
      setEditIntent(null);
      setItems((prev) => prev.map((m) => (m.id === editId ? { ...m, text: t } : m)));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/pack-group/messages/edit`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, message_id: editId, text: t }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string; message?: string };
        if (!res.ok) {
          setItems((prev) => prev.map((m) => (m.id === editId ? { ...m, text: original } : m)));
          if (isModerationError(j.error)) {
            showAlert(moderationUserMessage(j.error, j.message));
            return;
          }
          const errMap: Record<string, string> = {
            not_found: "Сообщение не найдено (обнови чат)",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            empty_text: "Сообщение не может быть пустым",
            text_too_long: "Слишком длинное сообщение",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        await load();
      } catch (e) {
        setItems((prev) => prev.map((m) => (m.id === editId ? { ...m, text: original } : m)));
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setSending(false);
      }
      return;
    }

    const replyToId = replyIntent?.replyToMessageId ?? 0;
    const replyLabel = replyIntent?.authorLabel ?? "";
    const replyExcerpt = replyIntent?.excerpt ?? "";
    const sentPhoto = photo;
    const localPhotoUrl = photoPreview;
    setText("");
    setReplyIntent(null);
    // Снимаем фото из формы, но не ревокаем blob: он ещё нужен для оптимистичного превью.
    setPhoto(null);
    setPhotoPreview(null);
    if (photoInputRef.current) photoInputRef.current.value = "";
    forceScrollRef.current = true;
    userScrolledUpRef.current = false;

    // Оптимистичная вставка своего сообщения — мгновенная обратная связь.
    // Временный отрицательный id; после успешного load() заменяется серверной строкой,
    // при ошибке откатывается.
    const tempId = -Date.now();
    setItems((prev) => {
      const myName = prev.find((m) => !m.is_leo && m.user_id === meId)?.username ?? "";
      const optimistic: PackGroupMessage = {
        id: tempId,
        user_id: meId,
        username: myName,
        text: t,
        created_at: new Date().toISOString(),
        is_leo: false,
        photo_url: localPhotoUrl ?? undefined,
      };
      if (replyToId > 0) {
        optimistic.reply_to_id = replyToId;
        optimistic.reply_to_username = replyLabel === "Лео" ? "" : replyLabel;
        optimistic.reply_to_text = replyExcerpt;
        optimistic.reply_to_is_leo = replyLabel === "Лео";
      }
      return [...prev, optimistic];
    });

    const cleanupLocalPhoto = () => {
      if (localPhotoUrl) URL.revokeObjectURL(localPhotoUrl);
    };

    try {
      let res: Response;
      if (sentPhoto) {
        const fd = new FormData();
        fd.append("init_data", initData);
        fd.append("text", t);
        if (replyToId > 0) fd.append("reply_to_id", String(replyToId));
        fd.append("photo", sentPhoto, sentPhoto.name || "photo.jpg");
        res = await fetch(`${apiBase}/api/miniapp/pack-group/messages/photo`, {
          method: "POST",
          body: fd,
        });
      } else {
        const body: { init_data: string; text: string; reply_to_id?: number } = {
          init_data: initData,
          text: t,
        };
        if (replyToId > 0) body.reply_to_id = replyToId;
        res = await fetch(`${apiBase}/api/miniapp/pack-group/messages`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify(body),
        });
      }
      const j = (await res.json().catch(() => ({}))) as { error?: string; message?: string; ok?: boolean; reply_text?: string };
      if (!res.ok) {
        setItems((prev) => prev.filter((m) => m.id !== tempId));
        cleanupLocalPhoto();
        if (isModerationError(j.error)) {
          showAlert(moderationUserMessage(j.error, j.message));
          return;
        }
        const errMap: Record<string, string> = {
          media_not_configured: "Загрузка фото на сервере не настроена.",
          unsupported_image: "Не удалось прочитать фото. Попробуй JPG, PNG, WEBP или GIF.",
          photo_too_large: "Фото слишком большое. Максимум 6 МБ.",
          invalid_multipart: "Не удалось отправить фото. Выбери снимок заново.",
          missing_photo: "Фото не приложено.",
          chat_mismatch: "Открой мини-апп из чата стаи",
          forbidden: "Нет доступа",
        };
        showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
        return;
      }
      await load();
      cleanupLocalPhoto();
    } catch (e) {
      setItems((prev) => prev.filter((m) => m.id !== tempId));
      cleanupLocalPhoto();
      showAlert(e instanceof Error ? e.message : "Сеть");
    } finally {
      setSending(false);
    }
  }, [text, photo, photoPreview, sending, inTelegram, initData, showAlert, load, onHaptic, replyIntent, editIntent, meId]);

  const reportMessage = useCallback(
    async (messageID: number) => {
      if (!apiBase || !inTelegram || !initData || !messageID) return;
      setReportPosting((p) => ({ ...p, [messageID]: true }));
      try {
        const res = await fetch(`${apiBase}/api/miniapp/pack-group/report`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, message_id: messageID }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          const errMap: Record<string, string> = {
            not_found: "Сообщение не найдено (обнови чат)",
            forbidden: "Нет доступа",
            chat_mismatch: "Открой мини-апп из чата стаи",
            cannot_report_self: "Нельзя пожаловаться на своё сообщение",
            cannot_report_leo: "На это сообщение пожаловаться нельзя",
            already_reported: "Ты уже отправлял жалобу на это",
            report_error: "Не удалось отправить жалобу",
          };
          showAlert(errMap[j.error ?? ""] ?? j.error ?? `Ошибка ${res.status}`);
          return;
        }
        showAlert("Жалоба отправлена. Админы увидят её в поддержке.");
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      } finally {
        setReportPosting((p) => ({ ...p, [messageID]: false }));
      }
    },
    [inTelegram, initData, showAlert],
  );

  const startReply = useCallback((m: PackGroupMessage) => {
    const authorLabel = m.is_leo ? "Лео" : m.username;
    const excerpt = m.text.length > 100 ? `${m.text.slice(0, 99).trim()}…` : m.text.trim();
    setEditIntent(null);
    setReplyIntent({ replyToMessageId: m.id, authorLabel, excerpt });
    window.setTimeout(() => inputRef.current?.focus(), 80);
  }, []);

  const startEdit = useCallback((m: PackGroupMessage) => {
    setReplyIntent(null);
    setEditIntent({ messageId: m.id, originalText: m.text });
    setText(m.text);
    window.setTimeout(() => {
      const el = inputRef.current;
      if (!el) return;
      el.focus();
      const end = el.value.length;
      el.setSelectionRange(end, end);
    }, 80);
  }, []);

  const cancelEdit = useCallback(() => {
    setEditIntent(null);
    setText("");
  }, []);

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
      const f = e.target.files?.[0] ?? null;
      if (!f) return;
      if (!/^image\//.test(f.type)) {
        showAlert("Можно прикрепить только изображение (JPG, PNG, WEBP, GIF).");
        if (photoInputRef.current) photoInputRef.current.value = "";
        return;
      }
      if (f.size > 6 * 1024 * 1024) {
        showAlert("Фото слишком большое. Максимум 6 МБ.");
        if (photoInputRef.current) photoInputRef.current.value = "";
        return;
      }
      setPhoto(f);
      setPhotoPreview((prev) => {
        if (prev) URL.revokeObjectURL(prev);
        return URL.createObjectURL(f);
      });
      setEditIntent(null);
    },
    [showAlert],
  );

  // Подчищаем blob превью на размонтировании, чтобы не текла память.
  useEffect(() => {
    return () => {
      if (photoPreview) URL.revokeObjectURL(photoPreview);
    };
  }, [photoPreview]);

  // Лайтбокс: Esc закрывает, фон не скроллится.
  useEffect(() => {
    if (!lightboxUrl) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setLightboxUrl(null);
    };
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [lightboxUrl]);

  useEffect(
    () => () => {
      if (highlightTimerRef.current) clearTimeout(highlightTimerRef.current);
    },
    [],
  );

  const removeMine = useCallback(
    async (messageID: number) => {
      if (!apiBase || !inTelegram || !initData || !messageID) return;
      try {
        const res = await fetch(`${apiBase}/api/miniapp/pack-group/messages/delete`, {
          method: "POST",
          headers: { "Content-Type": "application/json" },
          body: JSON.stringify({ init_data: initData, message_id: messageID }),
        });
        const j = (await res.json().catch(() => ({}))) as { error?: string };
        if (!res.ok) {
          showAlert(j.error ?? `Ошибка ${res.status}`);
          return;
        }
        setItems((prev) => prev.filter((m) => m.id !== messageID));
      } catch (e) {
        showAlert(e instanceof Error ? e.message : "Сеть");
      }
    },
    [inTelegram, initData, showAlert],
  );

  const postReaction = useCallback(
    (messageId: number, emoji: string) => {
      if (!apiBase || !inTelegram || !initData || !messageId) return;

      onHaptic?.();

      let snapshot: PackFeedReactionDTO[] | undefined;
      setItems((prev) =>
        prev.map((m) => {
          if (m.id !== messageId) return m;
          snapshot = m.reactions;
          return { ...m, reactions: optimisticTogglePackFeedReaction(m.reactions, emoji) };
        }),
      );

      void (async () => {
        try {
          const res = await fetch(`${apiBase}/api/miniapp/pack-group/react`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData, message_id: messageId, emoji }),
          });
          const j = (await res.json().catch(() => ({}))) as { error?: string };
          if (!res.ok) {
            if (snapshot !== undefined) {
              setItems((prev) =>
                prev.map((m) => (m.id === messageId ? { ...m, reactions: snapshot } : m)),
              );
            }
            showAlert(j.error === "invalid_emoji" ? "Такую реакцию нельзя" : j.error ?? `Ошибка ${res.status}`);
          }
        } catch (e) {
          if (snapshot !== undefined) {
            setItems((prev) =>
              prev.map((m) => (m.id === messageId ? { ...m, reactions: snapshot } : m)),
            );
          }
          showAlert(e instanceof Error ? e.message : "Сеть");
        }
      })();
    },
    [inTelegram, initData, showAlert, onHaptic],
  );

  const itemsById = useMemo(() => {
    const map = new Map<number, PackGroupMessage>();
    for (const m of items) map.set(m.id, m);
    return map;
  }, [items]);

  // Подсветка совпадений в загруженном окне (для тонировки строк).
  const searchMatchSet = useMemo(() => {
    const q = searchQuery.trim().toLowerCase();
    if (q === "") return new Set<number>();
    const s = new Set<number>();
    for (const m of items) {
      if (m.text.toLowerCase().includes(q) || (m.username || "").toLowerCase().includes(q)) s.add(m.id);
    }
    return s;
  }, [items, searchQuery]);

  const clearSearch = useCallback(() => {
    setSearchQuery("");
    setServerResults([]);
    setSearching(false);
  }, []);

  // Серверный поиск по всей истории чата (debounce). Минимум 2 символа.
  useEffect(() => {
    const q = searchQuery.trim();
    if (q.length < 2 || !apiBase || !inTelegram || !initData) {
      setServerResults([]);
      setSearching(false);
      return;
    }
    setSearching(true);
    const ctrl = new AbortController();
    const timer = window.setTimeout(() => {
      void (async () => {
        try {
          const res = await fetch(`${apiBase}/api/miniapp/pack-group/search`, {
            method: "POST",
            headers: { "Content-Type": "application/json" },
            body: JSON.stringify({ init_data: initData, query: q, limit: 50 }),
            signal: ctrl.signal,
          });
          const j = (await res.json().catch(() => ({}))) as { messages?: PackGroupMessage[] };
          if (res.ok && Array.isArray(j.messages)) setServerResults(j.messages);
          else setServerResults([]);
        } catch {
          // отменён/сеть — игнорируем
        } finally {
          setSearching(false);
        }
      })();
    }, 280);
    return () => {
      ctrl.abort();
      window.clearTimeout(timer);
    };
  }, [searchQuery, inTelegram, initData]);

  const handleSearchResultTap = useCallback(
    (m: PackGroupMessage) => {
      if (itemsById.has(m.id)) {
        clearSearch();
        window.setTimeout(() => scrollToQuotedMessage(m.id), 40);
      } else {
        showAlert("Это сообщение в старой истории — оно вне загруженного окна чата, перейти к нему нельзя.");
      }
    },
    [itemsById, clearSearch, scrollToQuotedMessage, showAlert],
  );

  if (!apiBase) {
    return <p className="packroom__warn muted">Нет API URL в билде.</p>;
  }

  return (
    <div className="packroom" ref={roomRef}>
      {err && <p className="packroom__err">{err}</p>}
      <div className="packroom__search">
        <div className="packroom__search-bar">
          <span className="packroom__search-ico" aria-hidden>
            🔍
          </span>
          <input
            className="packroom__search-input"
            type="search"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="Поиск по всей истории чата"
            aria-label="Поиск по сообщениям чата"
            autoComplete="off"
            onKeyDown={(e) => {
              if (e.key === "Escape") clearSearch();
            }}
          />
          {searchQuery.trim() !== "" && (
            <button
              type="button"
              className="packroom__search-btn packroom__search-btn--clear"
              aria-label="Очистить поиск"
              onClick={clearSearch}
            >
              ✕
            </button>
          )}
        </div>
        {searchQuery.trim() !== "" && (
          <div className="packroom__search-results" role="listbox" aria-label="Результаты поиска">
            {searching && serverResults.length === 0 ? (
              <p className="packroom__search-empty muted">Поиск…</p>
            ) : searchQuery.trim().length < 2 ? (
              <p className="packroom__search-empty muted">Введите минимум 2 символа</p>
            ) : serverResults.length === 0 ? (
              <p className="packroom__search-empty muted">Ничего не найдено</p>
            ) : (
              <ul className="packroom__search-list">
                {serverResults.map((m) => {
                  const loaded = itemsById.has(m.id);
                  return (
                    <li key={m.id}>
                      <button
                        type="button"
                        className={`packroom__search-result${loaded ? "" : " packroom__search-result--old"}`}
                        onClick={() => handleSearchResultTap(m)}
                      >
                        <span className="packroom__search-result-head">
                          <span className="packroom__search-result-author">
                            {m.is_leo ? "Лео" : m.username || "Участник"}
                          </span>
                          <span className="packroom__search-result-time muted">{formatChatTime(m.created_at)}</span>
                        </span>
                        <span className="packroom__search-result-text">
                          {highlightSearch(excerptText(m.text, 140), searchQuery)}
                        </span>
                      </button>
                    </li>
                  );
                })}
              </ul>
            )}
          </div>
        )}
      </div>
      <div
        className="packroom__log"
        role="log"
        aria-label="Чат стаи"
        ref={logRef}
      >
        <div className="packroom__log-inner">
        {items.map((m) => {
          const mine = !m.is_leo && m.user_id === meId;
          const replyQuote = resolveReplyQuote(m, itemsById);
          const isReply = replyQuote != null;
          const rowTone = m.is_leo ? "packroom__row--leo" : mine ? "packroom__row--me" : "packroom__row--oth";
          const isUnread = unreadMessageIds.has(m.id);
          const canReport = !mine && !m.is_leo;
          const reactions = mergePackFeedReactions(TRAINING_FEED_APPROVING_EMOJIS, m.reactions);
          const activeReactions = reactions.filter((r) => r.count > 0);
          const avatarUrl = resolveFeedAvatarUrl(m.author_photo_url);
          const photoUrl = m.photo_url ? resolveTrainingPhotoUrl(m.photo_url) : undefined;
          const hasText = m.text.trim() !== "";
          return (
            <div
              key={m.id}
              data-pack-msg-id={m.id}
              className={`packroom__row ${rowTone}${isReply ? " packroom__row--reply" : ""}${
                highlightMessageId === m.id ? " packroom__row--highlight" : ""
              }${isUnread ? " packroom__row--unread" : ""}${
                searchMatchSet.has(m.id) ? " packroom__row--search-match" : ""
              }`}
            >
              <SwipeToReply onReply={() => startReply(m)}>
                <div className="packroom__ava" aria-hidden>
                  {m.is_leo ? (
                    <img className="packroom__avatar" src={LEO_AVATAR_URL} width={32} height={32} alt="" loading="lazy" />
                  ) : avatarUrl ? (
                    <img
                      className="packroom__avatar"
                      src={avatarUrl}
                      width={32}
                      height={32}
                      alt=""
                      loading="lazy"
                      referrerPolicy="no-referrer"
                    />
                  ) : (
                    <span className="packroom__avatar-ph">🐾</span>
                  )}
                </div>
                <div className="packroom__content">
                  <div className="packroom__meta-row">
                    <div className="packroom__meta">
                      {m.is_leo ? "Лео" : m.username} · {formatChatTime(m.created_at)}
                      {m.edited_at ? <span className="packroom__edited"> · изменено</span> : null}
                    </div>
                  </div>
                  <div className="packroom__bubble-wrap">
                    <div className="packroom__bubble">
                      {isReply && replyQuote != null && (
                        <button
                          type="button"
                          className="packroom__quote"
                          aria-label="Перейти к сообщению, на которое ответили"
                          onClick={() => scrollToQuotedMessage(replyQuote.parentId)}
                        >
                          <span className="packroom__quote-author">{replyQuote.author}</span>
                          {replyQuote.text !== "" && (
                            <span className="packroom__quote-text">{replyQuote.text}</span>
                          )}
                        </button>
                      )}
                      {photoUrl && (
                        <button
                          type="button"
                          className="packroom__photo-wrap"
                          aria-label="Открыть фото"
                          onClick={() => setLightboxUrl(photoUrl)}
                        >
                          <img
                            className="packroom__photo"
                            src={photoUrl}
                            alt=""
                            loading="lazy"
                            referrerPolicy="no-referrer"
                          />
                        </button>
                      )}
                      {hasText && (
                        <p className="packroom__bubble-text">
                          {searchQuery.trim() !== "" ? highlightSearch(m.text, searchQuery) : m.text}
                        </p>
                      )}
                    </div>
                    <div className="packroom__bubble-actions">
                      <button type="button" className="packroom__reply" onClick={() => startReply(m)}>
                        Ответить
                      </button>
                      <PackGroupMessageMenu
                        reactions={reactions}
                        onReaction={(emoji) => postReaction(m.id, emoji)}
                        canReport={canReport}
                        onReport={() => void reportMessage(m.id)}
                        reporting={Boolean(reportPosting[m.id])}
                        scrollRootRef={logRef}
                      />
                      {mine && (
                        <button type="button" className="packroom__edit" onClick={() => startEdit(m)}>
                          Изменить
                        </button>
                      )}
                      {(mine || (isAdmin && !m.is_leo)) && (
                        <button
                          type="button"
                          className="packroom__del"
                          onClick={() => {
                            const q = mine
                              ? "Удалить сообщение?"
                              : "Скрыть сообщение участника?\n\nВернуть можно в боте: Поддержка → Скрытое.";
                            if (!window.confirm(q)) return;
                            void removeMine(m.id);
                          }}
                        >
                          {mine ? "Удалить" : "Скрыть"}
                        </button>
                      )}
                      {activeReactions.length > 0 && (
                        <PackMessageReactions
                          reactions={activeReactions}
                          onReact={(emoji) => postReaction(m.id, emoji)}
                        />
                      )}
                    </div>
                  </div>
                </div>
              </SwipeToReply>
            </div>
          );
        })}
        <div ref={endRef} className="packroom__log-end" aria-hidden />
        </div>
      </div>
      <form
        ref={formRef}
        className="packroom__form"
        onSubmit={(e) => {
          e.preventDefault();
          void send();
        }}
      >
        {editIntent != null && (
          <div className="packroom__reply-intent packroom__reply-intent--edit">
            <div className="packroom__reply-intent-row">
              <span className="packroom__reply-intent-label">
                Редактирование сообщения
              </span>
              <button type="button" className="packroom__reply-intent-cancel" onClick={cancelEdit}>
                ✕
              </button>
            </div>
          </div>
        )}
        {replyIntent != null && editIntent == null && (
          <div className="packroom__reply-intent">
            <div className="packroom__reply-intent-row">
              <span className="packroom__reply-intent-label">
                Ответ <strong>{replyIntent.authorLabel}</strong>
              </span>
              <button type="button" className="packroom__reply-intent-cancel" onClick={() => setReplyIntent(null)}>
                ✕
              </button>
            </div>
            {replyIntent.excerpt.trim() !== "" && (
              <p className="packroom__reply-intent-excerpt">{replyIntent.excerpt}</p>
            )}
          </div>
        )}
        {photoPreview != null && editIntent == null && (
          <div className="packroom__photo-pending">
            <img className="packroom__photo-pending-img" src={photoPreview} alt="" />
            <span className="packroom__photo-pending-name">{photo?.name ?? "Фото"}</span>
            <button
              type="button"
              className="packroom__photo-pending-remove"
              aria-label="Убрать фото"
              onClick={clearPhoto}
            >
              ✕
            </button>
          </div>
        )}
        <div className="packroom__form-row">
        <input
          ref={photoInputRef}
          type="file"
          accept="image/*"
          className="packroom__photo-input"
          onChange={onPickPhoto}
          tabIndex={-1}
          aria-hidden
        />
        {editIntent == null && (
          <button
            type="button"
            className="packroom__attach"
            aria-label="Прикрепить фото из галереи"
            disabled={sending}
            onClick={() => photoInputRef.current?.click()}
          >
            📎
          </button>
        )}
        {editIntent == null && (
          <CameraButton className="packroom__attach" onChange={onPickPhoto} disabled={sending} />
        )}
        <input
          ref={inputRef}
          className="packroom__input"
          value={text}
          onChange={(e) => setText(e.target.value)}
          placeholder={
            editIntent != null
              ? "Измени сообщение…"
              : replyIntent != null
                ? `Ответ ${replyIntent.authorLabel}… @leo — чтобы ответил бот`
                : "Сообщение… @leo — чтобы ответил бот"
          }
          maxLength={4000}
          autoComplete="off"
          enterKeyHint="send"
          onFocus={() => {
            if (!userScrolledUpRef.current) scrollToBottom();
          }}
          onBlur={() => {
            window.setTimeout(scrollToBottomIfAllowed, 120);
          }}
        />
        <button
          type="submit"
          className="packroom__send"
          disabled={sending || (!text.trim() && !photo)}
          aria-label={editIntent != null ? "Сохранить изменения" : "Отправить"}
        >
          {sending ? "…" : editIntent != null ? "✓" : "➤"}
        </button>
        </div>
      </form>
      {lightboxUrl != null &&
        createPortal(
          <PhotoLightbox src={lightboxUrl} onClose={() => setLightboxUrl(null)} />,
          document.body,
        )}
    </div>
  );
}
