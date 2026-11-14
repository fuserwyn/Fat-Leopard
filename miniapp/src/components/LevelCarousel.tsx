import { useEffect, useState } from "react";
import { MAX_CUP_LEVEL, miniappLevelEmoji, miniappLevelMinCups, miniappLevelName } from "../lib/miniappLevel";
import { useI18n } from "../lib/I18nContext";
import { pick, type UILang } from "../lib/i18n";
import "./LevelCarousel.css";

type Props = {
  /** User’s current level (1…6). */
  currentLevel: number;
};

function formatCups(n: number): string {
  return n.toLocaleString("en-US").replace(/ /g, " ");
}

function cupsHint(level: number, lang: UILang): string {
  const min = miniappLevelMinCups(level);
  return min <= 0 ? pick(lang, "старт · 0 🏆", "start · 0 🏆") : pick(lang, `от ${formatCups(min)} 🏆`, `from ${formatCups(min)} 🏆`);
}

/**
 * Level carousel in profile: one level card (emoji, number, name, min cups),
 * paged with arrows. Current and past levels are bright; locked ones are muted but visible.
 */
export function LevelCarousel({ currentLevel }: Props) {
  const { lang } = useI18n();
  const current = Math.min(Math.max(1, Math.floor(currentLevel) || 1), MAX_CUP_LEVEL);
  const [shown, setShown] = useState(current);

  useEffect(() => {
    setShown(current);
  }, [current]);

  const unlocked = shown <= current;
  const isCurrent = shown === current;
  const status = isCurrent ? pick(lang, "текущий", "current") : unlocked ? pick(lang, "пройден", "completed") : pick(lang, "закрыт", "locked");

  return (
    <div className="level-carousel" role="group" aria-label={pick(lang, "Уровни", "Levels")}>
      <button
        type="button"
        className="level-carousel__arrow"
        aria-label={pick(lang, "Предыдущий уровень", "Previous level")}
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
            {pick(lang, "Уровень", "Level")} {shown} · {miniappLevelName(shown, lang)}
            {!unlocked ? <span className="level-carousel__lock" aria-hidden> 🔒</span> : null}
          </span>
          <span className="level-carousel__hint">
            {cupsHint(shown, lang)}
            <span className="level-carousel__status"> · {status}</span>
          </span>
        </span>
      </div>
      <button
        type="button"
        className="level-carousel__arrow"
        aria-label={pick(lang, "Следующий уровень", "Next level")}
        disabled={shown >= MAX_CUP_LEVEL}
        onClick={() => setShown((s) => Math.min(MAX_CUP_LEVEL, s + 1))}
      >
        ›
      </button>
    </div>
  );
}
