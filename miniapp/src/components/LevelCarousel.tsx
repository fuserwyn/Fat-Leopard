import { useEffect, useState } from "react";
import { MAX_CUP_LEVEL, miniappLevelEmoji, miniappLevelMinCups, miniappLevelName } from "../lib/miniappLevel";
import "./LevelCarousel.css";

type Props = {
  /** Текущий уровень пользователя (1…6). */
  currentLevel: number;
};

function formatCups(n: number): string {
  return n.toLocaleString("ru-RU").replace(/ /g, " ");
}

function cupsHint(level: number): string {
  const min = miniappLevelMinCups(level);
  return min <= 0 ? "старт · 0 🏆" : `от ${formatCups(min)} 🏆`;
}

/**
 * Карусель уровней в профиле: одна карточка уровня (эмодзи, номер, имя, минимум кубков),
 * листается кнопками влево/вправо. Пройденные и текущий уровни — яркие, закрытые — приглушены,
 * но листать к ним можно.
 */
export function LevelCarousel({ currentLevel }: Props) {
  const current = Math.min(Math.max(1, Math.floor(currentLevel) || 1), MAX_CUP_LEVEL);
  const [shown, setShown] = useState(current);

  useEffect(() => {
    setShown(current);
  }, [current]);

  const unlocked = shown <= current;
  const isCurrent = shown === current;
  const status = isCurrent ? "текущий" : unlocked ? "пройден" : "закрыт";

  return (
    <div className="level-carousel" role="group" aria-label="Уровни">
      <button
        type="button"
        className="level-carousel__arrow"
        aria-label="Предыдущий уровень"
        disabled={shown <= 1}
        onClick={() => setShown((s) => Math.max(1, s - 1))}
      >
        ‹
      </button>
      <div
        className={`level-carousel__card${unlocked ? " is-unlocked" : " is-locked"}${isCurrent ? " is-current" : ""}`}
        aria-live="polite"
        aria-disabled={!unlocked || undefined}
        data-level={shown}
      >
        <span className="level-carousel__emoji" aria-hidden>
          {miniappLevelEmoji(shown)}
        </span>
        <span className="level-carousel__text">
          <span className="level-carousel__title">
            Уровень {shown} · {miniappLevelName(shown)}
            {!unlocked ? <span className="level-carousel__lock" aria-hidden> 🔒</span> : null}
          </span>
          <span className="level-carousel__hint">
            {cupsHint(shown)}
            <span className="level-carousel__status"> · {status}</span>
          </span>
        </span>
      </div>
      <button
        type="button"
        className="level-carousel__arrow"
        aria-label="Следующий уровень"
        disabled={shown >= MAX_CUP_LEVEL}
        onClick={() => setShown((s) => Math.min(MAX_CUP_LEVEL, s + 1))}
      >
        ›
      </button>
    </div>
  );
}
