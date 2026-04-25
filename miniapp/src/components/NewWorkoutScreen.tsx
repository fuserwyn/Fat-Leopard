import { useState } from "react";
import "./NewWorkoutScreen.css";

const TYPES: { id: string; label: string; emoji: string }[] = [
  { id: "run", label: "Бег", emoji: "🏃" },
  { id: "walk", label: "Ходьба", emoji: "🚶" },
  { id: "bike", label: "Велосипед", emoji: "🚴" },
  { id: "swim", label: "Плавание", emoji: "🏊" },
  { id: "strength", label: "Силовая", emoji: "🏋️" },
  { id: "other", label: "Другое", emoji: "✨" },
];

const INTENSITIES: { v: 1 | 2 | 3 | 4 | 5; label: string }[] = [
  { v: 1, label: "1 · Разминка" },
  { v: 2, label: "2 · Комфортно" },
  { v: 3, label: "3 · Рабочий темп" },
  { v: 4, label: "4 · Тяжело" },
  { v: 5, label: "5 · Макс" },
];

const PRESET_MIN = [5, 15, 30, 45, 60] as const;

type Props = { onClose: () => void; onSave: (payload: { type: string; min: number; intensity: 1 | 2 | 3 | 4 | 5 }) => void };

export function NewWorkoutScreen({ onClose, onSave }: Props) {
  const [type, setType] = useState("strength");
  const [min, setMin] = useState(15);
  const [intensity, setIntensity] = useState<1 | 2 | 3 | 4 | 5>(3);

  const dec = (d: number) => setMin((m) => Math.max(1, m + d));
  return (
    <div className="nwo">
      <header className="nwo__head">
        <button type="button" className="nwo__close" onClick={onClose} aria-label="Закрыть">
          ✕
        </button>
        <h1 className="nwo__title">Новая тренировка</h1>
        <span className="nwo__spacer" aria-hidden />
      </header>

      <div className="nwo__body">
        <h2 className="section-title">Тип</h2>
        <div className="chip-group">
          {TYPES.map((t) => (
            <button
              key={t.id}
              type="button"
              className="chip"
              aria-pressed={type === t.id}
              onClick={() => setType(t.id)}
            >
              {t.emoji} {t.label}
            </button>
          ))}
        </div>

        <h2 className="section-title" style={{ marginTop: 22 }}>
          Длительность
        </h2>
        <p className="nwo__big-min">{min} мин</p>
        <div className="nwo__presets">
          {PRESET_MIN.map((p) => (
            <button
              key={p}
              type="button"
              className={`nwo__circle ${min === p ? "is-on" : ""}`}
              aria-pressed={min === p}
              onClick={() => setMin(p)}
            >
              {p}
            </button>
          ))}
        </div>
        <div className="nwo__stepper">
          <button type="button" className="nwo__step" onClick={() => dec(-5)}>
            -5
          </button>
          <button type="button" className="nwo__step" onClick={() => dec(-1)}>
            -1
          </button>
          <button type="button" className="nwo__step" onClick={() => dec(1)}>
            +1
          </button>
          <button type="button" className="nwo__step" onClick={() => dec(5)}>
            +5
          </button>
        </div>

        <h2 className="section-title" style={{ marginTop: 22 }}>
          Интенсивность
        </h2>
        <div className="nwo__intensity">
          {INTENSITIES.map((i) => (
            <button
              key={i.v}
              type="button"
              className="chip nwo__intensity-chip"
              aria-pressed={intensity === i.v}
              onClick={() => setIntensity(i.v)}
            >
              {i.label}
            </button>
          ))}
        </div>
      </div>

      <footer className="nwo__foot">
        <button
          type="button"
          className="nwo__save"
          onClick={() => {
            onSave({ type, min, intensity });
            onClose();
          }}
        >
          Сохранить
        </button>
      </footer>
    </div>
  );
}
