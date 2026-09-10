import { useCallback, useEffect, useMemo, useRef, useState } from "react";
import { WORKOUT_TYPES as TYPES, type WorkoutCategoryId } from "../lib/workoutCategories";
import { orderWorkoutTypes } from "../lib/workoutSuggest";
import { PhotoCropper } from "./PhotoCropper";
import { CameraButton } from "./CameraButton";
import { hapticImpact, hapticNotification } from "../lib/haptics";
import "./NewWorkoutScreen.css";

type ViewportMetrics = {
  /** Высота видимой области (над клавиатурой). */
  visualH: number;
  /** Высота layout viewport — не сжимается при клавиатуре на iOS. */
  layoutH: number;
  /** Отступ снизу до клавиатуры (для fixed-кнопки «Отправить»). */
  keyboardBottom: number;
};

/** Видимая область + inset клавиатуры. Debounce + порог: без ложных перерисовок. */
function useViewportMetrics(): ViewportMetrics {
  const [m, setM] = useState<ViewportMetrics>(() => {
    if (typeof window === "undefined") {
      return { visualH: 640, layoutH: 640, keyboardBottom: 0 };
    }
    const layoutH = window.innerHeight;
    const visualH = window.visualViewport?.height ?? layoutH;
    return { visualH, layoutH, keyboardBottom: 0 };
  });

  useEffect(() => {
    let raf = 0;

    const read = () => {
      const vv = window.visualViewport;
      const layoutH = Math.max(
        320,
        window.innerHeight ||
          (typeof window.Telegram?.WebApp?.viewportHeight === "number"
            ? window.Telegram.WebApp.viewportHeight
            : 0) ||
          320,
      );
      const visualH = Math.max(
        200,
        vv && vv.height > 0
          ? vv.height
          : typeof window.Telegram?.WebApp?.viewportHeight === "number" &&
              window.Telegram.WebApp.viewportHeight > 0
            ? window.Telegram.WebApp.viewportHeight
            : layoutH,
      );
      const visualOffsetTop = Math.floor(vv?.offsetTop ?? 0);
      const keyboardBottom = Math.max(0, Math.floor(layoutH - visualH - visualOffsetTop));

      setM((prev) => {
        if (
          Math.abs(prev.visualH - visualH) < 4 &&
          Math.abs(prev.layoutH - layoutH) < 4 &&
          Math.abs(prev.keyboardBottom - keyboardBottom) < 4
        ) {
          return prev;
        }
        return { visualH, layoutH, keyboardBottom };
      });
    };

    // Throttle через rAF, а не debounce 100мс: iOS анимирует клавиатуру ~250мс
    // и шлёт серию resize — при дебаунсе замер происходил только после её конца,
    // поэтому кнопка-панель «не сразу» вставала над клавиатурой. Замер на каждом
    // кадре даёт панели отслеживать вьюпорт почти в реальном времени (быстро и плавно).
    const schedule = () => {
      if (raf) cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        raf = 0;
        read();
      });
    };

    read();
    const vv = window.visualViewport;
    vv?.addEventListener("resize", schedule);
    vv?.addEventListener("scroll", schedule);
    window.addEventListener("orientationchange", read);
    const tg = window.Telegram?.WebApp as { onEvent?: (e: string, fn: () => void) => void } | undefined;
    tg?.onEvent?.("viewportChanged", schedule);
    return () => {
      if (raf) cancelAnimationFrame(raf);
      vv?.removeEventListener("resize", schedule);
      vv?.removeEventListener("scroll", schedule);
      window.removeEventListener("orientationchange", read);
      const tgOff = window.Telegram?.WebApp as { offEvent?: (e: string, fn: () => void) => void } | undefined;
      tgOff?.offEvent?.("viewportChanged", schedule);
    };
  }, []);

  return m;
}

const INTENSITIES: { v: 1 | 2 | 3 | 4 | 5; label: string }[] = [
  { v: 1, label: "1 · Разминка" },
  { v: 2, label: "2 · Комфортно" },
  { v: 3, label: "3 · Рабочий темп" },
  { v: 4, label: "4 · Тяжело" },
  { v: 5, label: "5 · Макс" },
];

/** Пресеты минут (до 120); произвольное значение — поле «Своё», лимит как в формуле кубков на бэке. */
const PRESET_MIN = [5, 15, 20, 30, 45, 60, 75, 90, 120] as const;
const WORKOUT_MIN_MIN = 1;
const WORKOUT_MIN_MAX = 480;

function clampWorkoutMinutes(n: number): number {
  if (!Number.isFinite(n)) return WORKOUT_MIN_MIN;
  return Math.min(WORKOUT_MIN_MAX, Math.max(WORKOUT_MIN_MIN, Math.floor(n)));
}

type Props = {
  onClose: () => void;
  showAlert?: (message: string) => void;
  /**
   * Клик по «Хочу вносить не только спорт» — best-effort счётчик интереса к фиче.
   * Верни false, если заявку не удалось отправить (нет initData) — тогда экран не
   * блокирует повторную попытку и не показывает ложное «Спасибо».
   */
  onNonSportInterest?: () => boolean | void;
  /** Персональный порядок типов с сервера (история + контекст). */
  suggestedWorkoutTypes?: readonly WorkoutCategoryId[];
  /** Сохранение отчёта: верни false, чтобы не закрывать шторку (например, при ошибке сети). */
  onSave: (payload: {
    /** Один или несколько видов спорта (мультивыбор). Кубки — за самый эффективный. */
    types: WorkoutCategoryId[];
    min: number;
    intensity: 1 | 2 | 3 | 4 | 5;
    /** Свой вид активности, если выбрано «Другое». */
    otherLabel?: string;
    /** Произвольный текст: упражнения, ощущения — попадает в ленту и в контекст Лео. */
    note: string;
    /** Снимок с тренировки — уходит только вместе с отчётом через multipart API. */
    photo: File | null;
  }) => void | boolean | Promise<void | boolean>;
};

const NOTE_MAX = 1500;
const OTHER_LABEL_MAX = 80;

/** Прикрепление фото к отчёту. Фото грузятся в Cloudflare R2 (см. R2_* в env). */
const PHOTO_ENABLED = true;

export function NewWorkoutScreen({
  onClose,
  onSave,
  showAlert,
  onNonSportInterest,
  suggestedWorkoutTypes,
}: Props) {
  const { visualH } = useViewportMetrics();
  const workoutTypes = useMemo(
    () => orderWorkoutTypes(TYPES, suggestedWorkoutTypes),
    [suggestedWorkoutTypes],
  );

  useEffect(() => {
    window.scrollTo(0, 0);
    document.documentElement.scrollTop = 0;
    document.body.scrollTop = 0;
  }, []);
  const sheetRef = useRef<HTMLDivElement>(null);
  // Отступ шторки от верха (CSS top: safe-top + 44px) — нужен, чтобы при клавиатуре
  // посчитать высоту так, чтобы низ шторки совпал с верхом клавиатуры (visualH − top),
  // сохранив верхний зазор. Берём из computed style: transform входной анимации
  // на него не влияет (в отличие от getBoundingClientRect).
  const [sheetTop, setSheetTop] = useState(0);
  useEffect(() => {
    const el = sheetRef.current;
    if (!el) return;
    const topPx = parseFloat(window.getComputedStyle(el).top);
    if (Number.isFinite(topPx)) setSheetTop(Math.max(0, Math.round(topPx)));
  }, []);
  const bodyRef = useRef<HTMLDivElement>(null);
  const noteTaRef = useRef<HTMLTextAreaElement>(null);
  const otherInputRef = useRef<HTMLInputElement>(null);
  const noteFocusedRef = useRef(false);
  // Какое поле сейчас в фокусе — чтобы подматывать над клавиатурой именно его
  // (и «Описание», и «Свой тип»), а не только текстовую заметку.
  const activeFieldRef = useRef<HTMLElement | null>(null);
  const [inputFocused, setInputFocused] = useState(false);
  const showKeyboardBar = inputFocused;

  const [types, setTypes] = useState<WorkoutCategoryId[]>([]);
  const otherSelected = types.includes("other");
  const toggleType = useCallback((id: WorkoutCategoryId) => {
    setTypes((prev) => (prev.includes(id) ? prev.filter((t) => t !== id) : [...prev, id]));
  }, []);
  const [min, setMin] = useState(15);
  const [minDraft, setMinDraft] = useState("15");
  const [intensity, setIntensity] = useState<1 | 2 | 3 | 4 | 5>(3);
  const [note, setNote] = useState("");
  const [otherLabel, setOtherLabel] = useState("");
  const [busy, setBusy] = useState(false);
  const [photo, setPhoto] = useState<File | null>(null);
  const [pendingCrop, setPendingCrop] = useState<File | null>(null);

  /** Только если низ поля ушёл под низ скролла — чуть увеличить scrollTop. Без scrollIntoView(start): иначе прыжок к началу формы. */
  const nudgeIntoView = useCallback((el: HTMLElement | null) => {
    const body = bodyRef.current;
    if (!el || !body) return;
    const bodyRect = body.getBoundingClientRect();
    const elRect = el.getBoundingClientRect();
    const pad = 10;
    if (elRect.bottom > bodyRect.bottom - pad) {
      body.scrollTop += elRect.bottom - (bodyRect.bottom - pad);
    }
  }, []);
  const nudgeActiveIntoView = useCallback(() => nudgeIntoView(activeFieldRef.current), [nudgeIntoView]);

  useEffect(() => {
    if (!activeFieldRef.current) return;
    const t = window.setTimeout(nudgeActiveIntoView, 120);
    return () => window.clearTimeout(t);
  }, [visualH, nudgeActiveIntoView]);

  // При выборе «Другое» сразу ставим фокус в поле своего типа и подматываем его
  // над клавиатурой — иначе оно появляется под клавиатурой и не видно, куда вводить.
  useEffect(() => {
    if (!otherSelected) return;
    const el = otherInputRef.current;
    if (!el) return;
    el.focus({ preventScroll: true });
    window.setTimeout(nudgeActiveIntoView, 180);
  }, [otherSelected, nudgeActiveIntoView]);

  // Перед закрытием снимаем фокус с активного поля: если textarea уничтожить
  // сфокусированной, focusout не стреляет и глобальный класс app-keyboard-open
  // зависает (прятал таббар и поле ввода ленты после свайпа вниз).
  const onCloseRef = useRef(onClose);
  onCloseRef.current = onClose;
  const closeSheet = useCallback(() => {
    const el = document.activeElement;
    if (el instanceof HTMLElement) el.blur();
    onCloseRef.current();
  }, []);

  const handleSubmit = useCallback(async () => {
    if (busy) return;
    if (types.length === 0) {
      (showAlert ?? window.alert)("Выбери тип тренировки.");
      return;
    }
    if (otherSelected && !otherLabel.trim()) {
      (showAlert ?? window.alert)("Укажи свой тип активности или выбери категорию из списка.");
      return;
    }
    setBusy(true);
    hapticImpact("medium");
    try {
      const r = await onSave({
        types,
        min,
        intensity,
        otherLabel: otherSelected ? otherLabel.trim() : undefined,
        note: note.trim(),
        photo,
      });
      if (r !== false) {
        hapticNotification("success");
        closeSheet();
      } else {
        hapticNotification("error");
      }
    } finally {
      setBusy(false);
    }
  }, [busy, types, otherSelected, otherLabel, min, intensity, note, photo, onSave, closeSheet, showAlert]);

  useEffect(() => {
    let raf = 0;
    const onVV = () => {
      if (!activeFieldRef.current) return;
      if (raf) cancelAnimationFrame(raf);
      raf = requestAnimationFrame(() => {
        raf = 0;
        nudgeActiveIntoView();
      });
    };
    const vv = window.visualViewport;
    vv?.addEventListener("resize", onVV);
    vv?.addEventListener("scroll", onVV);
    const tg = window.Telegram?.WebApp as { onEvent?: (e: string, fn: () => void) => void } | undefined;
    tg?.onEvent?.("viewportChanged", onVV);
    return () => {
      if (raf) cancelAnimationFrame(raf);
      vv?.removeEventListener("resize", onVV);
      vv?.removeEventListener("scroll", onVV);
      const tgOff = window.Telegram?.WebApp as { offEvent?: (e: string, fn: () => void) => void } | undefined;
      tgOff?.offEvent?.("viewportChanged", onVV);
    };
  }, [nudgeActiveIntoView]);

  // Свайп шторки вниз = закрыть (как системные bottom-sheet). Тянуть можно за
  // грабер/шапку или за тело формы, когда оно прокручено к самому верху; сам
  // жест начинается только после заметного смещения вниз, поэтому тапы по чипам
  // и вертикальный скролл формы не страдают. Нативные слушатели (не React):
  // touchmove должен быть non-passive, чтобы preventDefault остановил скролл.
  const busyRef = useRef(busy);
  busyRef.current = busy;
  useEffect(() => {
    const sheet = sheetRef.current;
    const body = bodyRef.current;
    if (!sheet) return;
    let startY = 0;
    let dy = 0;
    let tracking = false;
    let dragging = false;
    const onStart = (e: TouchEvent) => {
      if (busyRef.current) return;
      const t = e.touches[0];
      if (!t) return;
      const target = e.target as HTMLElement | null;
      // Из полей ввода жест не начинаем (там выделение текста/каретка).
      if (target?.closest("textarea, input, select")) return;
      // Из тела формы — только когда оно у самого верха (иначе это обычный скролл).
      if (body?.contains(target) && (body?.scrollTop ?? 0) > 1) return;
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
          if (dy < -8) tracking = false; // жест вверх — это скролл, отпускаем
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
        // Blur сразу: клавиатура прячется вместе с уезжающей шторкой, а
        // focusout успевает снять глобальный app-keyboard-open до анмаунта.
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

  const [nonSportSent, setNonSportSent] = useState(false);
  const handleNonSportInterest = useCallback(() => {
    if (nonSportSent) {
      (showAlert ?? window.alert)("Спасибо за ваш интерес! Мы работаем над новым функционалом.");
      return;
    }
    // Заявку помечаем отправленной и благодарим только если она реально ушла.
    // Иначе (нет initData) — не блокируем повтор и честно просим попробовать снова.
    const dispatched = onNonSportInterest?.();
    if (dispatched === false) {
      (showAlert ?? window.alert)("Не получилось отправить заявку. Открой мини-апп из Telegram и попробуй ещё раз.");
      return;
    }
    setNonSportSent(true);
    (showAlert ?? window.alert)("Спасибо за ваш интерес! Мы работаем над новым функционалом.");
  }, [nonSportSent, onNonSportInterest, showAlert]);

  const applyMinutes = useCallback((n: number) => {
    const m = clampWorkoutMinutes(n);
    setMin(m);
    setMinDraft(String(m));
  }, []);

  return (
    <>
      <div
        className="nwo-backdrop"
        aria-hidden="true"
        onClick={() => {
          if (!busy) closeSheet();
        }}
      />
      <div
        ref={sheetRef}
        className={`nwo${showKeyboardBar ? " nwo--keyboard" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label="Новая тренировка"
        style={
        // Пока показана клавиатурная панель — высота от верхнего зазора до верха
        // клавиатуры: visualH − top шторки (та же проверенная формула, что была с
        // top:0 и height=visualH, минус зазор). bottom: auto, иначе CSS bottom:0
        // растянул бы шторку под клавиатуру. Скругления и зазор сверху сохраняются:
        // шторка остаётся модалкой. Флаг тот же, что у футера (showKeyboardBar).
        showKeyboardBar
          ? {
              height: `${Math.max(180, visualH - sheetTop)}px`,
              maxHeight: `${Math.max(180, visualH - sheetTop)}px`,
              bottom: "auto",
            }
          : undefined
      }
    >
      <div className="nwo__grabber" aria-hidden="true" />
      <header className="nwo__head">
        <h1 className="nwo__title">Тренировка</h1>
        <button type="button" className="nwo__close" onClick={closeSheet} aria-label="Закрыть">
          ✕
        </button>
      </header>

      <div className="nwo__body" ref={bodyRef}>
        <div className="nwo__note-block">
          <h2 className="nwo__sec">Описание</h2>
          <div className="nwo__note-wrap">
          <textarea
            ref={noteTaRef}
            className="nwo__note"
            value={note}
            rows={3}
            onChange={(e) => setNote(e.target.value.slice(0, NOTE_MAX))}
            maxLength={NOTE_MAX}
            placeholder="Как прошла тренировка, что интересного было…"
            enterKeyHint="done"
            autoCorrect="on"
            spellCheck
            onFocus={() => {
              noteFocusedRef.current = true;
              activeFieldRef.current = noteTaRef.current;
              setInputFocused(true);
              window.setTimeout(nudgeActiveIntoView, 180);
            }}
            onBlur={() => {
              noteFocusedRef.current = false;
              if (activeFieldRef.current === noteTaRef.current) activeFieldRef.current = null;
              window.setTimeout(() => {
                if (!document.activeElement?.closest(".nwo")) setInputFocused(false);
              }, 80);
            }}
          />
          <span className="nwo__note-cnt" aria-live="polite">
            {note.length}/{NOTE_MAX}
          </span>
          </div>
          {PHOTO_ENABLED && (
            <div className="nwo__photo-row">
              <div className="nwo__photo-picks">
                <label className="nwo__photo-add">
                  <input
                    className="nwo__file-hidden"
                    type="file"
                    accept="image/*"
                    title="Необязательно — стая увидит снимок в ленте"
                    onChange={(e) => {
                      const f = e.target.files?.[0] ?? null;
                      if (f) setPendingCrop(f);
                      e.target.value = "";
                    }}
                  />
                  <span className="nwo__photo-add-ico" aria-hidden>
                    🖼
                  </span>
                  {photo ? "Заменить" : "Из галереи"}
                </label>
                <CameraButton
                  className="nwo__photo-add"
                  ariaLabel="Сделать фото"
                  title="Снять фото камерой прямо сейчас"
                  onChange={(e) => {
                    const f = e.target.files?.[0] ?? null;
                    if (f) setPendingCrop(f);
                  }}
                >
                  <span className="nwo__photo-add-ico" aria-hidden>
                    📷
                  </span>
                  Камера
                </CameraButton>
              </div>
              {photo ? (
                <>
                  <span className="nwo__photo-name" aria-live="polite">
                    {photo.name.length > 18 ? `${photo.name.slice(0, 16)}…` : photo.name}
                  </span>
                  <button
                    type="button"
                    className="nwo__photo-edit"
                    onClick={() => setPendingCrop(photo)}
                  >
                    Обрезать
                  </button>
                  <button
                    type="button"
                    className="nwo__photo-edit nwo__photo-edit--ghost"
                    onClick={() => setPhoto(null)}
                    aria-label="Убрать фото"
                  >
                    ✕
                  </button>
                </>
              ) : null}
            </div>
          )}
        </div>

        <div className="nwo__upper">
          <h2 className="nwo__sec">Тип</h2>
          <div className="nwo__types-scroll" role="group" aria-label="Тип тренировки (можно выбрать несколько)">
            {workoutTypes.map((t) => (
              <button
                key={t.id}
                type="button"
                className="chip nwo__type-chip"
                title={t.label}
                aria-pressed={types.includes(t.id)}
                onClick={() => toggleType(t.id)}
              >
                <span className="nwo__type-emoji" aria-hidden>
                  {t.emoji}
                </span>
                <span className="nwo__type-lbl">{t.label}</span>
              </button>
            ))}
          </div>
          {otherSelected && (
            <div className="nwo__other-field">
              <label className="nwo__other-label" htmlFor="nwo-other-type">
                Свой тип
              </label>
              <input
                id="nwo-other-type"
                ref={otherInputRef}
                className="nwo__other-input"
                type="text"
                value={otherLabel}
                onChange={(e) => setOtherLabel(e.target.value.slice(0, OTHER_LABEL_MAX))}
                maxLength={OTHER_LABEL_MAX}
                placeholder="Пилатес, скалолазанье…"
                autoComplete="off"
                enterKeyHint="done"
                onFocus={() => {
                  activeFieldRef.current = otherInputRef.current;
                  setInputFocused(true);
                  window.setTimeout(nudgeActiveIntoView, 180);
                }}
                onBlur={() => {
                  if (activeFieldRef.current === otherInputRef.current) activeFieldRef.current = null;
                  window.setTimeout(() => {
                    if (!document.activeElement?.closest(".nwo")) setInputFocused(false);
                  }, 80);
                }}
              />
            </div>
          )}
          <button type="button" className="nwo__non-sport" onClick={handleNonSportInterest}>
            Хочу вносить не только спорт
          </button>

          <div className="nwo__mid">
            <div className="nwo__dur">
              <h2 className="nwo__sec">Минуты</h2>
              <div className="nwo__dur-top">
                <span className="nwo__big-min">{min}</span>
                <span className="nwo__big-suf">мин</span>
              </div>
              <div className="nwo__presets-row">
                <div className="nwo__presets" role="group" aria-label="Быстрый выбор минут">
                  {PRESET_MIN.map((p) => (
                    <button
                      key={p}
                      type="button"
                      className={`nwo__circle ${min === p ? "is-on" : ""}`}
                      aria-pressed={min === p}
                      onClick={() => applyMinutes(p)}
                    >
                      {p}
                    </button>
                  ))}
                </div>
                <div className="nwo__min-custom">
                  <label className="nwo__min-custom-lbl" htmlFor="nwo-min-custom">
                    Своё
                  </label>
                  <input
                    id="nwo-min-custom"
                    className="nwo__min-custom-input"
                    type="text"
                    inputMode="numeric"
                    autoComplete="off"
                    enterKeyHint="done"
                    maxLength={3}
                    aria-label={`Минуты, от ${WORKOUT_MIN_MIN} до ${WORKOUT_MIN_MAX}`}
                    placeholder={`${WORKOUT_MIN_MIN}–${WORKOUT_MIN_MAX}`}
                    value={minDraft}
                    onChange={(e) => {
                      const s = e.target.value.replace(/\D/g, "").slice(0, 3);
                      setMinDraft(s);
                      if (s !== "") applyMinutes(parseInt(s, 10));
                    }}
                    onFocus={() => setInputFocused(true)}
                    onBlur={() => {
                      if (minDraft === "") applyMinutes(min);
                      else applyMinutes(parseInt(minDraft, 10) || min);
                      window.setTimeout(() => {
                        if (!document.activeElement?.closest(".nwo")) setInputFocused(false);
                      }, 80);
                    }}
                  />
                </div>
              </div>
            </div>

            <div className="nwo__int-wrap">
              <h2 className="nwo__sec">Интенсивность</h2>
              <div className="nwo__intensity-row" role="group" aria-label="Интенсивность">
                {INTENSITIES.map((i) => (
                  <button
                    key={i.v}
                    type="button"
                    className={`nwo__int-btn ${intensity === i.v ? "is-on" : ""}`}
                    aria-pressed={intensity === i.v}
                    aria-label={i.label}
                    title={i.label}
                    onClick={() => setIntensity(i.v)}
                  >
                    {i.v}
                  </button>
                ))}
              </div>
            </div>
          </div>
        </div>

      </div>

      <footer className="nwo__foot" hidden={showKeyboardBar}>
        <button type="button" className="nwo__save" disabled={busy} onClick={() => void handleSubmit()}>
          {busy ? "Отправка…" : "Отправить отчёт"}
        </button>
      </footer>

      {showKeyboardBar ? (
        <div className="nwo__keyboard-bar" role="toolbar" aria-label="Отправка отчёта">
          <button
            type="button"
            className="nwo__keyboard-send"
            disabled={busy}
            onClick={() => void handleSubmit()}
          >
            {busy ? "Отправка…" : "Отправить"}
          </button>
        </div>
      ) : null}
      {PHOTO_ENABLED && pendingCrop ? (
        <PhotoCropper
          file={pendingCrop}
          onCancel={() => setPendingCrop(null)}
          onConfirm={(cropped) => {
            setPhoto(cropped);
            setPendingCrop(null);
          }}
        />
      ) : null}
      </div>
    </>
  );
}
