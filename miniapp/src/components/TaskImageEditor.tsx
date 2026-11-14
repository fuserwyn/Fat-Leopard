import { useCallback, useEffect, useRef, useState } from "react";
import "./TaskImageEditor.css";

export type TaskImage = {
  /** base64 без префикса data: — так его ждёт бэкенд. */
  data: string;
  filename: string;
  mime: string;
  /** data-URL для превью в форме. */
  preview: string;
};

type Props = {
  onDone: (image: TaskImage) => void;
  onCancel: () => void;
  /** Картинка, с которой сразу открыться: вставка из буфера на компьютере. */
  initialFile?: File | Blob | null;
  /** Разрешить выбрать несколько файлов сразу (без редактора — все приложатся как есть). */
  allowMultiple?: boolean;
  onDoneMany?: (images: TaskImage[]) => void;
};

function fileToTaskImage(file: File | Blob): Promise<TaskImage> {
  return new Promise((resolve, reject) => {
    const reader = new FileReader();
    reader.onload = () => {
      const preview = String(reader.result);
      const mime = file.type || "image/jpeg";
      const name = file instanceof File && file.name ? file.name : `task-${Date.now()}.jpg`;
      resolve({
        data: preview.split(",", 2)[1] ?? "",
        filename: name,
        mime,
        preview,
      });
    };
    reader.onerror = () => reject(reader.error);
    reader.readAsDataURL(file);
  });
}

const COLORS = ["#ff3b30", "#ffcc00", "#34c759", "#0a84ff", "#ffffff", "#000000"];
const SIZE = 720; // сторона холста-результата

type Stroke = { color: string; width: number; points: { x: number; y: number }[] };

/**
 * Приложить картинку к задаче: выбрать файл, подвинуть и приблизить в рамке
 * (то, что видно в рамке, и станет кадром), порисовать поверх — и отдать
 * готовое изображение. Кроп и рисование делаются в браузере: на сервер уходит
 * уже готовый кадр, чтобы трекер не хранил лишнего.
 */
export function TaskImageEditor({ onDone, onCancel, initialFile, allowMultiple, onDoneMany }: Props) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const imgRef = useRef<HTMLImageElement | null>(null);
  const strokesRef = useRef<Stroke[]>([]);
  const offsetRef = useRef({ x: 0, y: 0 });
  const zoomRef = useRef(1);
  const [ready, setReady] = useState(false);
  const [mode, setMode] = useState<"move" | "draw">("move");
  const [zoom, setZoom] = useState(1);
  const [offset, setOffset] = useState({ x: 0, y: 0 });
  const [strokes, setStrokes] = useState<Stroke[]>([]);
  const [color, setColor] = useState(COLORS[0]);
  const [width, setWidth] = useState(6);
  const dragRef = useRef<{ x: number; y: number } | null>(null);
  const drawingRef = useRef<Stroke | null>(null);

  const pickFile = useCallback((file: File | Blob) => {
    const reader = new FileReader();
    reader.onload = () => {
      const img = new Image();
      img.onload = () => {
        imgRef.current = img;
        // Вписываем по меньшей стороне: кадр заполнен, пустых полей нет.
        const base = Math.max(SIZE / img.width, SIZE / img.height);
        const nextOffset = { x: (SIZE - img.width * base) / 2, y: (SIZE - img.height * base) / 2 };
        zoomRef.current = base;
        offsetRef.current = nextOffset;
        strokesRef.current = [];
        setZoom(base);
        setOffset(nextOffset);
        setStrokes([]);
        setReady(true);
      };
      img.src = String(reader.result);
    };
    reader.readAsDataURL(file);
  }, []);

  // Вставили картинку из буфера — открываемся сразу с ней, без выбора файла.
  useEffect(() => {
    if (initialFile) pickFile(initialFile);
  }, [initialFile, pickFile]);

  const redraw = useCallback(() => {
    const canvas = canvasRef.current;
    const img = imgRef.current;
    if (!canvas || !img) return;
    const ctx = canvas.getContext("2d");
    if (!ctx) return;
    const { x: ox, y: oy } = offsetRef.current;
    const z = zoomRef.current;
    ctx.fillStyle = "#000";
    ctx.fillRect(0, 0, SIZE, SIZE);
    ctx.drawImage(img, ox, oy, img.width * z, img.height * z);
    ctx.lineCap = "round";
    ctx.lineJoin = "round";
    for (const s of strokesRef.current) {
      const pts = s.points;
      if (!pts?.length) continue;
      ctx.strokeStyle = s.color;
      ctx.lineWidth = s.width;
      ctx.beginPath();
      ctx.moveTo(pts[0].x, pts[0].y);
      for (const p of pts.slice(1)) ctx.lineTo(p.x, p.y);
      if (pts.length === 1) ctx.lineTo(pts[0].x + 0.1, pts[0].y + 0.1);
      ctx.stroke();
    }
  }, []);

  useEffect(() => {
    offsetRef.current = offset;
    redraw();
  }, [offset, redraw]);

  useEffect(() => {
    zoomRef.current = zoom;
    redraw();
  }, [zoom, redraw]);

  useEffect(() => {
    strokesRef.current = strokes;
    redraw();
  }, [strokes, redraw]);

  useEffect(() => {
    if (ready) redraw();
  }, [ready, redraw]);

  const toCanvas = (e: React.PointerEvent<HTMLCanvasElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    return {
      x: ((e.clientX - rect.left) / rect.width) * SIZE,
      y: ((e.clientY - rect.top) / rect.height) * SIZE,
    };
  };

  const onPointerDown = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!ready) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    const p = toCanvas(e);
    if (mode === "draw") {
      drawingRef.current = { color, width, points: [p] };
      strokesRef.current = [...strokesRef.current, drawingRef.current];
      redraw();
    } else {
      dragRef.current = { x: p.x - offsetRef.current.x, y: p.y - offsetRef.current.y };
    }
  };

  const onPointerMove = (e: React.PointerEvent<HTMLCanvasElement>) => {
    if (!ready) return;
    const p = toCanvas(e);
    if (mode === "draw" && drawingRef.current) {
      drawingRef.current.points.push(p);
      redraw();
      return;
    }
    if (dragRef.current) {
      const next = { x: p.x - dragRef.current.x, y: p.y - dragRef.current.y };
      offsetRef.current = next;
      redraw();
      setOffset(next);
    }
  };

  const onPointerUp = () => {
    if (drawingRef.current) {
      const committed = strokesRef.current.map((s) => ({
        color: s.color,
        width: s.width,
        points: s.points.map((pt) => ({ x: pt.x, y: pt.y })),
      }));
      strokesRef.current = committed;
      setStrokes(committed);
    }
    drawingRef.current = null;
    dragRef.current = null;
  };

  const attach = () => {
    const canvas = canvasRef.current;
    if (!canvas || !ready) return;
    const url = canvas.toDataURL("image/jpeg", 0.9);
    onDone({
      data: url.split(",", 2)[1] ?? "",
      filename: `task-${Date.now()}.jpg`,
      mime: "image/jpeg",
      preview: url,
    });
  };

  return (
    <div
      className="imged"
      role="dialog"
      aria-modal="true"
      onPointerDown={(e) => e.stopPropagation()}
      onTouchStart={(e) => e.stopPropagation()}
    >
      <div className="imged__box">
        <div className="imged__head">
          <b>Картинка к задаче</b>
          <button type="button" className="imged__close" onClick={onCancel} aria-label="Закрыть">
            ✕
          </button>
        </div>

        {!ready ? (
          <label className="imged__pick">
            <input
              type="file"
              accept="image/*"
              multiple={allowMultiple}
              onChange={(e) => {
                const files = Array.from(e.target.files ?? []);
                e.target.value = "";
                if (!files.length) return;
                if (allowMultiple && files.length > 1 && onDoneMany) {
                  void Promise.all(files.map((f) => fileToTaskImage(f)))
                    .then(onDoneMany)
                    .catch(() => {});
                  return;
                }
                pickFile(files[0]);
              }}
            />
            <span>
              {allowMultiple
                ? "📷 Выбрать фото или скриншот (можно несколько)"
                : "📷 Выбрать фото или скриншот"}
            </span>
          </label>
        ) : null}

        <canvas
          ref={canvasRef}
          width={SIZE}
          height={SIZE}
          className={`imged__canvas${ready ? "" : " is-empty"} imged__canvas--${mode}`}
          onPointerDown={onPointerDown}
          onPointerMove={onPointerMove}
          onPointerUp={onPointerUp}
          onPointerCancel={onPointerUp}
        />

        {ready ? (
          <>
            <div className="imged__row">
              <button type="button" className={mode === "move" ? "on" : ""} onClick={() => setMode("move")}>
                ✋ Двигать
              </button>
              <button type="button" className={mode === "draw" ? "on" : ""} onClick={() => setMode("draw")}>
                ✏️ Рисовать
              </button>
              <button type="button" onClick={() => setStrokes((prev) => prev.slice(0, -1))} disabled={strokes.length === 0}>
                ↶ Отменить
              </button>
            </div>

            <label className="imged__zoom">
              Масштаб
              <input
                type="range"
                min={0.2}
                max={4}
                step={0.02}
                value={zoom}
                onChange={(e) => setZoom(Number(e.target.value))}
              />
            </label>

            {mode === "draw" ? (
              <div className="imged__row imged__row--pens">
                {COLORS.map((c) => (
                  <button
                    key={c}
                    type="button"
                    className={`imged__color${color === c ? " on" : ""}`}
                    style={{ background: c }}
                    aria-label={`Цвет ${c}`}
                    onClick={() => setColor(c)}
                  />
                ))}
                <input
                  type="range"
                  min={2}
                  max={24}
                  step={1}
                  value={width}
                  onChange={(e) => setWidth(Number(e.target.value))}
                  aria-label="Толщина"
                />
              </div>
            ) : null}

            <p className="imged__hint">
              В кадр попадёт то, что видно в рамке: двигай пальцем и меняй масштаб. Рисовать — поверх кадра.
            </p>
          </>
        ) : null}

        <div className="imged__actions">
          <button type="button" onClick={onCancel}>
            Отмена
          </button>
          <button type="button" className="imged__primary" disabled={!ready} onClick={attach}>
            Приложить
          </button>
        </div>
      </div>
    </div>
  );
}
