import {
  useEffect,
  useLayoutEffect,
  useRef,
  useState,
  type CSSProperties,
  type MouseEvent as ReactMouseEvent,
  type RefObject,
  type TouchEvent as ReactTouchEvent,
} from "react";
import { createPortal } from "react-dom";

/** Один отреагировавший: имя + (опц.) уже отрезолвленный URL аватара. */
export type Liker = { name: string; photoUrl?: string };

/** Группа отреагировавших по одной эмодзи. */
export type LikerGroup = { emoji: string; voters: Liker[] };

const OFFSCREEN: CSSProperties = { position: "fixed", top: "-9999px", left: "0px" };

/**
 * Открывать поповер «кто отреагировал» по наведению только на устройствах с настоящим
 * hover-указателем (мышь). На тач-вебвью Telegram (мобильный) синтетический mouseenter
 * срабатывает при скролле/тапе и спорадически открывает поповер — там используем long-press.
 */
export function pointerCanHover(): boolean {
  if (typeof window === "undefined" || typeof window.matchMedia !== "function") return false;
  return window.matchMedia("(hover: hover) and (pointer: fine)").matches;
}

/**
 * Управляет поповером «кто отреагировал». Поповер рендерится порталом в body
 * (чтобы не обрезался overflow-ом карточки) и позиционируется position: fixed
 * относительно якоря: над ним, либо под ним — туда, где больше места; высота
 * ограничена, внутри скролл. Закрытие — тап вне (кроме якоря и самого окна) / Esc.
 */
export function useLikersPopover(anchorRef: RefObject<HTMLElement | null>) {
  const [open, setOpen] = useState(false);
  const [style, setStyle] = useState<CSSProperties>(OFFSCREEN);
  const popRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!open) return;
    const close = (e: MouseEvent | TouchEvent) => {
      const t = e.target as Node;
      if (anchorRef.current?.contains(t) || popRef.current?.contains(t)) return;
      setOpen(false);
    };
    const esc = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", close);
    document.addEventListener("touchstart", close);
    document.addEventListener("keydown", esc);
    return () => {
      document.removeEventListener("mousedown", close);
      document.removeEventListener("touchstart", close);
      document.removeEventListener("keydown", esc);
    };
  }, [open, anchorRef]);

  useLayoutEffect(() => {
    if (!open) {
      setStyle(OFFSCREEN);
      return;
    }
    const position = () => {
      const anchor = anchorRef.current;
      const pop = popRef.current;
      if (!anchor || !pop) return;
      const a = anchor.getBoundingClientRect();
      const margin = 8;
      const gap = 6;
      const vw = window.innerWidth;
      const vh = window.innerHeight;

      // По вертикали выбираем сторону с бóльшим запасом; высоту ограничиваем.
      const spaceAbove = a.top - margin - gap;
      const spaceBelow = vh - a.bottom - margin - gap;
      const placeAbove = spaceAbove >= spaceBelow;
      const avail = Math.max(0, placeAbove ? spaceAbove : spaceBelow);
      const maxHeight = Math.max(96, Math.min(260, avail));
      const contentH = pop.scrollHeight;
      const h = Math.min(contentH, maxHeight);

      // По горизонтали — центр по якорю, прижатый к экрану.
      const pw = pop.offsetWidth;
      let left = a.left + a.width / 2 - pw / 2;
      left = Math.max(margin, Math.min(left, vw - margin - pw));

      let top = placeAbove ? a.top - gap - h : a.bottom + gap;
      // Финальный зажим по вертикали: окно всегда целиком на экране.
      top = Math.max(margin, Math.min(top, vh - margin - h));

      setStyle({ position: "fixed", left: `${left}px`, top: `${top}px`, maxHeight: `${maxHeight}px` });
    };
    position();
    window.addEventListener("scroll", position, true);
    window.addEventListener("resize", position);
    return () => {
      window.removeEventListener("scroll", position, true);
      window.removeEventListener("resize", position);
    };
  }, [open, anchorRef]);

  return { open, setOpen, style, popRef };
}

/** Аватар голосующего: при ошибке загрузки фото показывает инициал, а не «битую картинку». */
function LikerAvatar({ photoUrl, name }: { photoUrl?: string; name: string }) {
  const [failed, setFailed] = useState(false);
  useEffect(() => setFailed(false), [photoUrl]);
  const initial = (name[0] || "?").toUpperCase();
  if (!photoUrl || failed) {
    return (
      <span className="act-card__likers-ava" aria-hidden>
        {initial}
      </span>
    );
  }
  return (
    <span className="act-card__likers-ava" aria-hidden>
      <img
        className="act-card__likers-ava-img"
        src={photoUrl}
        alt=""
        loading="lazy"
        referrerPolicy="no-referrer"
        onError={() => setFailed(true)}
      />
    </span>
  );
}

/**
 * Всплывающий выбор реакции (как в Telegram): горизонтальный ряд эмодзи в «таблетке».
 * Портал в body, position: fixed по координатам из useLikersPopover. Тап по эмодзи —
 * поставить/снять реакцию и закрыть окно. Своя реакция подсвечена.
 */
export function ReactionPickerPopover({
  reactions,
  onPick,
  popRef,
  style,
}: {
  reactions: { emoji: string; me?: boolean }[];
  onPick: (emoji: string) => void;
  popRef: RefObject<HTMLDivElement>;
  style: CSSProperties;
}) {
  return createPortal(
    <div className="act-card__react-picker" role="menu" aria-label="Pick a reaction" ref={popRef} style={style}>
      {reactions.map((r) => (
        <button
          key={r.emoji}
          type="button"
          role="menuitem"
          className={`act-card__react-pick${r.me ? " act-card__react-pick--mine" : ""}`}
          onClick={() => onPick(r.emoji)}
        >
          {r.emoji}
        </button>
      ))}
    </div>,
    document.body,
  );
}

/** Поповер со списком отреагировавших (портал в body, сгруппирован по эмодзи, со скроллом). */
export function LikersPopover({
  groups,
  popRef,
  style,
  label = "Who reacted",
}: {
  groups: LikerGroup[];
  popRef: RefObject<HTMLDivElement>;
  style: CSSProperties;
  label?: string;
}) {
  return createPortal(
    <div className="act-card__likers" role="dialog" aria-label={label} ref={popRef} style={style}>
      <div className="act-card__likers-head">{label}</div>
      <div className="act-card__likers-scroll">
        {groups.map((g) => (
          <div key={g.emoji} className="act-card__likers-grp">
            <div className="act-card__likers-grp-head">
              <span className="act-card__likers-grp-emoji">{g.emoji}</span>
              <span className="act-card__likers-count">{g.voters.length}</span>
            </div>
            {g.voters.map((v, i) => {
              const name = String(v.name ?? "").trim();
              return (
                <div key={`${name}-${i}`} className="act-card__likers-item">
                  <LikerAvatar photoUrl={v.photoUrl} name={name} />
                  <span className="act-card__likers-name">{name || "Member"}</span>
                </div>
              );
            })}
          </div>
        ))}
      </div>
    </div>,
    document.body,
  );
}

/**
 * Пропсы для чипа/кнопки реакции: короткий тап = `onTap` (поставить/снять),
 * зажатие (~350мс) или hover на десктопе = `onLongPress` (просмотр списка).
 * preventDefault в touchend гасит синтетический click, поэтому реакция не двоится
 * и не переключается при долгом нажатии.
 */
export function useChipPress(onTap: () => void, onLongPress?: () => void, disabled?: boolean) {
  const lpTimer = useRef<number | null>(null);
  const longPressed = useRef(false);
  const moved = useRef(false);
  const startXY = useRef<{ x: number; y: number } | null>(null);
  const recentTouch = useRef(0);
  const canLong = typeof onLongPress === "function";

  const clearTimer = () => {
    if (lpTimer.current != null) {
      window.clearTimeout(lpTimer.current);
      lpTimer.current = null;
    }
  };

  return {
    onClick: () => {
      if (Date.now() - recentTouch.current < 600) return; // синтетический click после тача
      if (!disabled) onTap();
    },
    onTouchStart: (e: ReactTouchEvent) => {
      e.stopPropagation(); // чтобы тач по чипу не запускал long-press всего поста
      longPressed.current = false;
      moved.current = false;
      const t = e.touches[0];
      startXY.current = t ? { x: t.clientX, y: t.clientY } : null;
      clearTimer();
      if (canLong) {
        lpTimer.current = window.setTimeout(() => {
          longPressed.current = true;
          onLongPress?.();
        }, 350);
      }
    },
    onTouchMove: (e: ReactTouchEvent) => {
      const t = e.touches[0];
      const s = startXY.current;
      if (t && s && (Math.abs(t.clientX - s.x) > 14 || Math.abs(t.clientY - s.y) > 14)) {
        moved.current = true;
        clearTimer();
      }
    },
    onTouchEnd: (e: ReactTouchEvent) => {
      clearTimer();
      recentTouch.current = Date.now();
      if (longPressed.current) {
        e.preventDefault();
        return;
      }
      if (moved.current) return;
      e.preventDefault();
      if (!disabled) onTap();
    },
    onTouchCancel: clearTimer,
    onMouseEnter: () => {
      // Только мышь (desktop). На тач-устройствах синтетический mouseenter игнорируем —
      // просмотр списка там через зажатие (long-press).
      if (!pointerCanHover()) return;
      if (Date.now() - recentTouch.current < 600) return;
      onLongPress?.();
    },
  };
}

/**
 * Долгое нажатие на крупный элемент (например, на весь пост), не мешающее обычным
 * тапам и скроллу: тап проходит как обычно, скролл отменяет удержание, а после
 * сработавшего long-press следующий click подавляется (onClickCapture).
 */
export function useLongPress(onLongPress: () => void) {
  const timer = useRef<number | null>(null);
  const startXY = useRef<{ x: number; y: number } | null>(null);
  const fired = useRef(false);

  const clear = () => {
    if (timer.current != null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
  };

  return {
    onTouchStart: (e: ReactTouchEvent) => {
      fired.current = false;
      const t = e.touches[0];
      startXY.current = t ? { x: t.clientX, y: t.clientY } : null;
      clear();
      timer.current = window.setTimeout(() => {
        fired.current = true;
        onLongPress();
      }, 450);
    },
    onTouchMove: (e: ReactTouchEvent) => {
      const t = e.touches[0];
      const s = startXY.current;
      if (t && s && (Math.abs(t.clientX - s.x) > 14 || Math.abs(t.clientY - s.y) > 14)) clear();
    },
    onTouchEnd: clear,
    onTouchCancel: clear,
    onClickCapture: (e: ReactMouseEvent) => {
      if (fired.current) {
        e.preventDefault();
        e.stopPropagation();
        fired.current = false;
      }
    },
  };
}
