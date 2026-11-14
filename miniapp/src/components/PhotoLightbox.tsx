import { useCallback, useEffect, useRef, useState, type TouchEvent as ReactTouchEvent, type WheelEvent as ReactWheelEvent } from "react";
import "./PhotoLightbox.css";

const MIN_SCALE = 1;
const MAX_SCALE = 5;
const DOUBLE_TAP_SCALE = 2.5;

function clamp(v: number, min: number, max: number): number {
  return Math.min(max, Math.max(min, v));
}

function touchDistance(a: React.Touch, b: React.Touch): number {
  const dx = a.clientX - b.clientX;
  const dy = a.clientY - b.clientY;
  return Math.hypot(dx, dy);
}

/**
 * Полноэкранный просмотр фото с зумом: pinch (два пальца), двойной тап,
 * колесо мыши; панорама одним пальцем, когда увеличено. Клик по фону
 * (в масштабе 1:1, без перетаскивания) закрывает.
 */
export function PhotoLightbox({
  src,
  onClose,
  closeLabel = "Close",
}: {
  src: string;
  onClose: () => void;
  /** Подпись кнопки закрытия; админка передаёт русскую. */
  closeLabel?: string;
}) {
  const containerRef = useRef<HTMLDivElement>(null);
  const [scale, setScale] = useState(1);
  const [tx, setTx] = useState(0);
  const [ty, setTy] = useState(0);

  // Состояние жеста держим в ref, чтобы не гонять ре-рендеры на каждый touchmove.
  const g = useRef({
    mode: "none" as "none" | "pan" | "pinch",
    startDist: 0,
    startScale: 1,
    startTx: 0,
    startTy: 0,
    startX: 0,
    startY: 0,
    moved: false,
    lastTap: 0,
  });

  const clampTranslate = useCallback((nx: number, ny: number, s: number): [number, number] => {
    const el = containerRef.current;
    const w = el?.clientWidth ?? window.innerWidth;
    const h = el?.clientHeight ?? window.innerHeight;
    const maxX = ((s - 1) * w) / 2;
    const maxY = ((s - 1) * h) / 2;
    return [clamp(nx, -maxX, maxX), clamp(ny, -maxY, maxY)];
  }, []);

  const applyScale = useCallback(
    (next: number) => {
      const s = clamp(next, MIN_SCALE, MAX_SCALE);
      setScale(s);
      if (s <= 1) {
        setTx(0);
        setTy(0);
      } else {
        setTx((prev) => clampTranslate(prev, 0, s)[0]);
        setTy((prev) => clampTranslate(0, prev, s)[1]);
      }
    },
    [clampTranslate],
  );

  const onTouchStart = (e: ReactTouchEvent<HTMLDivElement>) => {
    const t = e.touches;
    if (t.length === 2) {
      g.current.mode = "pinch";
      g.current.startDist = touchDistance(t[0], t[1]);
      g.current.startScale = scale;
      g.current.moved = false;
    } else if (t.length === 1) {
      g.current.mode = scale > 1 ? "pan" : "none";
      g.current.startX = t[0].clientX;
      g.current.startY = t[0].clientY;
      g.current.startTx = tx;
      g.current.startTy = ty;
      g.current.moved = false;
    }
  };

  const onTouchMove = (e: ReactTouchEvent<HTMLDivElement>) => {
    const t = e.touches;
    if (g.current.mode === "pinch" && t.length === 2) {
      const dist = touchDistance(t[0], t[1]);
      if (g.current.startDist > 0) {
        applyScale((g.current.startScale * dist) / g.current.startDist);
        g.current.moved = true;
      }
    } else if (g.current.mode === "pan" && t.length === 1) {
      const dx = t[0].clientX - g.current.startX;
      const dy = t[0].clientY - g.current.startY;
      if (Math.abs(dx) > 4 || Math.abs(dy) > 4) g.current.moved = true;
      const [nx, ny] = clampTranslate(g.current.startTx + dx, g.current.startTy + dy, scale);
      setTx(nx);
      setTy(ny);
    }
  };

  const onTouchEnd = (e: ReactTouchEvent<HTMLDivElement>) => {
    // Двойной тап одним пальцем — переключаем зум.
    if (g.current.mode !== "pinch" && !g.current.moved && e.touches.length === 0) {
      const now = e.timeStamp;
      if (now - g.current.lastTap < 300) {
        applyScale(scale > 1 ? 1 : DOUBLE_TAP_SCALE);
        g.current.lastTap = 0;
      } else {
        g.current.lastTap = now;
      }
    }
    if (e.touches.length === 0) g.current.mode = "none";
  };

  const onWheel = (e: ReactWheelEvent<HTMLDivElement>) => {
    e.preventDefault();
    applyScale(scale - e.deltaY * 0.003 * scale);
  };

  const onBackdropClick = () => {
    // Клик по фону закрывает только в исходном масштабе и без перетаскивания.
    if (scale <= 1 && !g.current.moved) onClose();
  };

  useEffect(() => {
    const onKey = (ev: KeyboardEvent) => {
      if (ev.key === "Escape") onClose();
    };
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [onClose]);

  return (
    <div
      ref={containerRef}
      className="photo-lightbox"
      role="dialog"
      aria-modal="true"
      onClick={onBackdropClick}
      onTouchStart={onTouchStart}
      onTouchMove={onTouchMove}
      onTouchEnd={onTouchEnd}
      onWheel={onWheel}
    >
      <img
        className="photo-lightbox__img"
        src={src}
        alt=""
        referrerPolicy="no-referrer"
        draggable={false}
        style={{
          transform: `translate3d(${tx}px, ${ty}px, 0) scale(${scale})`,
          transition: g.current.mode === "none" ? "transform 0.18s ease-out" : "none",
        }}
        onClick={(e) => e.stopPropagation()}
        onDoubleClick={(e) => {
          e.stopPropagation();
          applyScale(scale > 1 ? 1 : DOUBLE_TAP_SCALE);
        }}
      />
      <button
        type="button"
        className="photo-lightbox__close"
        aria-label={closeLabel}
        onClick={(e) => {
          e.stopPropagation();
          onClose();
        }}
      >
        ✕
      </button>
    </div>
  );
}
