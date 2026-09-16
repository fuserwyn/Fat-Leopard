import { useEffect, useRef, useState } from "react";
import "./PhotoCropper.css";

type CropRect = { x: number; y: number; w: number; h: number };

type DragMode =
  | { kind: "move"; sx: number; sy: number; rect: CropRect }
  | { kind: "resize"; corner: "nw" | "ne" | "sw" | "se"; sx: number; sy: number; rect: CropRect }
  | null;

type Props = {
  file: File;
  onCancel: () => void;
  onConfirm: (cropped: File) => void;
};

const MIN_CROP_PX = 32;

export function PhotoCropper({ file, onCancel, onConfirm }: Props) {
  const stageRef = useRef<HTMLDivElement>(null);
  const imgRef = useRef<HTMLImageElement>(null);
  const [imgUrl, setImgUrl] = useState<string>("");
  /** Размер отображения картинки внутри сцены (px). */
  const [displayed, setDisplayed] = useState<{ w: number; h: number } | null>(null);
  /** Натуральный размер картинки. */
  const [natural, setNatural] = useState<{ w: number; h: number } | null>(null);
  /** Текущий прямоугольник в координатах отображения. */
  const [crop, setCrop] = useState<CropRect | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    const url = URL.createObjectURL(file);
    setImgUrl(url);
    return () => URL.revokeObjectURL(url);
  }, [file]);

  // Расчёт начальной геометрии после загрузки картинки и при ресайзе сцены.
  const recompute = () => {
    const stage = stageRef.current;
    const img = imgRef.current;
    if (!stage || !img) return;
    const stageRect = stage.getBoundingClientRect();
    const nW = img.naturalWidth;
    const nH = img.naturalHeight;
    if (!nW || !nH) return;
    const scale = Math.min(stageRect.width / nW, stageRect.height / nH);
    const dW = nW * scale;
    const dH = nH * scale;
    setNatural({ w: nW, h: nH });
    setDisplayed({ w: dW, h: dH });
    setCrop((prev) => {
      if (prev && displayed && Math.abs(displayed.w - dW) < 0.5 && Math.abs(displayed.h - dH) < 0.5) {
        return prev;
      }
      const inset = Math.min(dW, dH) * 0.08;
      return { x: inset, y: inset, w: dW - inset * 2, h: dH - inset * 2 };
    });
  };

  useEffect(() => {
    if (!imgUrl) return;
    const img = imgRef.current;
    if (!img) return;
    if (img.complete && img.naturalWidth > 0) {
      recompute();
    }
    const onLoad = () => recompute();
    img.addEventListener("load", onLoad);
    return () => img.removeEventListener("load", onLoad);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [imgUrl]);

  useEffect(() => {
    const onResize = () => recompute();
    window.addEventListener("resize", onResize);
    window.addEventListener("orientationchange", onResize);
    return () => {
      window.removeEventListener("resize", onResize);
      window.removeEventListener("orientationchange", onResize);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  // Блок скролла body, ESC.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onCancel();
    };
    document.addEventListener("keydown", onKey);
    const prev = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    return () => {
      document.removeEventListener("keydown", onKey);
      document.body.style.overflow = prev;
    };
  }, [onCancel]);

  const dragRef = useRef<DragMode>(null);

  const startDrag = (e: React.PointerEvent, mode: DragMode) => {
    e.preventDefault();
    e.stopPropagation();
    (e.target as Element).setPointerCapture?.(e.pointerId);
    dragRef.current = mode;
  };

  const onPointerMove = (e: React.PointerEvent) => {
    const mode = dragRef.current;
    if (!mode || !displayed) return;
    const dx = e.clientX - mode.sx;
    const dy = e.clientY - mode.sy;
    const r = { ...mode.rect };
    const maxW = displayed.w;
    const maxH = displayed.h;

    if (mode.kind === "move") {
      r.x = Math.max(0, Math.min(maxW - r.w, mode.rect.x + dx));
      r.y = Math.max(0, Math.min(maxH - r.h, mode.rect.y + dy));
    } else {
      const right = mode.rect.x + mode.rect.w;
      const bottom = mode.rect.y + mode.rect.h;
      let nx = mode.rect.x;
      let ny = mode.rect.y;
      let nw = mode.rect.w;
      let nh = mode.rect.h;
      if (mode.corner === "nw" || mode.corner === "sw") {
        nx = Math.min(right - MIN_CROP_PX, Math.max(0, mode.rect.x + dx));
        nw = right - nx;
      }
      if (mode.corner === "ne" || mode.corner === "se") {
        nw = Math.min(maxW - mode.rect.x, Math.max(MIN_CROP_PX, mode.rect.w + dx));
      }
      if (mode.corner === "nw" || mode.corner === "ne") {
        ny = Math.min(bottom - MIN_CROP_PX, Math.max(0, mode.rect.y + dy));
        nh = bottom - ny;
      }
      if (mode.corner === "sw" || mode.corner === "se") {
        nh = Math.min(maxH - mode.rect.y, Math.max(MIN_CROP_PX, mode.rect.h + dy));
      }
      r.x = nx;
      r.y = ny;
      r.w = nw;
      r.h = nh;
    }
    setCrop(r);
  };

  const endDrag = () => {
    dragRef.current = null;
  };

  const apply = async () => {
    if (busy) return;
    if (!crop || !displayed || !natural) return;
    setBusy(true);
    try {
      const scale = natural.w / displayed.w;
      const sx = Math.round(crop.x * scale);
      const sy = Math.round(crop.y * scale);
      const sw = Math.max(1, Math.round(crop.w * scale));
      const sh = Math.max(1, Math.round(crop.h * scale));
      const img = imgRef.current;
      if (!img) {
        onCancel();
        return;
      }
      // Ужимаем по длинной стороне, чтобы фото в ленте не весило мегабайты
      // (экономит R2-хранилище и ускоряет загрузку в Telegram WebView).
      const MAX_SIDE = 1280;
      const downscale = Math.min(1, MAX_SIDE / Math.max(sw, sh));
      const outW = Math.max(1, Math.round(sw * downscale));
      const outH = Math.max(1, Math.round(sh * downscale));
      const canvas = document.createElement("canvas");
      canvas.width = outW;
      canvas.height = outH;
      const ctx = canvas.getContext("2d");
      if (!ctx) {
        onCancel();
        return;
      }
      // Белая подложка: на случай прозрачного PNG, который мы кодируем в JPEG.
      ctx.fillStyle = "#ffffff";
      ctx.fillRect(0, 0, outW, outH);
      ctx.imageSmoothingQuality = "high";
      ctx.drawImage(img, sx, sy, sw, sh, 0, 0, outW, outH);
      // Фото тренировки кодируем в JPEG — заметно легче PNG при том же качестве.
      const blob: Blob | null = await new Promise((resolve) =>
        canvas.toBlob((b) => resolve(b), "image/jpeg", 0.82),
      );
      if (!blob) {
        onCancel();
        return;
      }
      const baseName = file.name.replace(/\.[^.]+$/, "") || "photo";
      const cropped = new File([blob], `${baseName}_cropped.jpg`, { type: "image/jpeg" });
      onConfirm(cropped);
    } finally {
      setBusy(false);
    }
  };

  return (
    <div
      className="ph-crop"
      role="dialog"
      aria-modal="true"
      onTouchStart={(e) => e.stopPropagation()}
      onTouchMove={(e) => e.stopPropagation()}
    >
      <header className="ph-crop__head">
        <button type="button" className="ph-crop__btn ph-crop__btn--ghost" onClick={onCancel} disabled={busy}>
          Отмена
        </button>
        <span className="ph-crop__title">Обрезать фото</span>
        <span className="ph-crop__head-spacer" aria-hidden="true" />
      </header>
      <div className="ph-crop__stage" ref={stageRef}>
        {imgUrl ? (
          <div
            className="ph-crop__canvas"
            style={
              displayed ? { width: `${displayed.w}px`, height: `${displayed.h}px` } : undefined
            }
          >
            <img
              ref={imgRef}
              className="ph-crop__img"
              src={imgUrl}
              alt=""
              draggable={false}
            />
            {crop && displayed ? (
              <div
                className="ph-crop__overlay"
                onPointerMove={onPointerMove}
                onPointerUp={endDrag}
                onPointerCancel={endDrag}
              >
                <div className="ph-crop__shade ph-crop__shade--top" style={{ height: `${crop.y}px` }} />
                <div
                  className="ph-crop__shade ph-crop__shade--bottom"
                  style={{ top: `${crop.y + crop.h}px` }}
                />
                <div
                  className="ph-crop__shade ph-crop__shade--left"
                  style={{ top: `${crop.y}px`, height: `${crop.h}px`, width: `${crop.x}px` }}
                />
                <div
                  className="ph-crop__shade ph-crop__shade--right"
                  style={{
                    top: `${crop.y}px`,
                    height: `${crop.h}px`,
                    left: `${crop.x + crop.w}px`,
                  }}
                />
                <div
                  className="ph-crop__rect"
                  style={{
                    left: `${crop.x}px`,
                    top: `${crop.y}px`,
                    width: `${crop.w}px`,
                    height: `${crop.h}px`,
                  }}
                  onPointerDown={(e) =>
                    startDrag(e, { kind: "move", sx: e.clientX, sy: e.clientY, rect: crop })
                  }
                >
                  {(["nw", "ne", "sw", "se"] as const).map((c) => (
                    <div
                      key={c}
                      className={`ph-crop__handle ph-crop__handle--${c}`}
                      onPointerDown={(e) =>
                        startDrag(e, {
                          kind: "resize",
                          corner: c,
                          sx: e.clientX,
                          sy: e.clientY,
                          rect: crop,
                        })
                      }
                    />
                  ))}
                </div>
              </div>
            ) : null}
          </div>
        ) : null}
      </div>
      <div className="ph-crop__actions">
        <button
          type="button"
          className="ph-crop__btn ph-crop__btn--primary ph-crop__btn--centered"
          onClick={apply}
          disabled={busy || !crop}
        >
          {busy ? "…" : "Готово"}
        </button>
      </div>
      <p className="ph-crop__hint">Передвигай рамку, тяни за углы.</p>
    </div>
  );
}
