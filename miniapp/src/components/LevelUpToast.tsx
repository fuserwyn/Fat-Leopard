import { useEffect, useState } from "react";
import { miniappLevelName, MAX_CUP_LEVEL } from "../lib/miniappLevel";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import "./AchievementToast.css";
import "./ShareCardSheet.css";

type Props = {
  /** Достигнутый уровень (1…6). */
  level: number;
  /** Вызывается, когда поп-ап скрылся (по таймеру или тапу) — родитель снимает его с очереди. */
  onDone: () => void;
  /** «Поделиться» — открыть карточку для сторис и чатов (тост при этом закрывается). */
  onShare?: () => void;
};

const VISIBLE_MS = 5000;
/** С кнопкой «Поделиться» тост висит дольше — чтобы успеть до неё дотянуться. */
const VISIBLE_WITH_SHARE_MS = 8000;
const EXIT_MS = 320;

/** Эмодзи-животное уровня (индекс = номер уровня 1-based; сурикат без своего эмодзи — лапка). */
const LEVEL_EMOJI = ["", "🐾", "🦌", "🦓", "🐆", "🦁", "🐘"];

function levelEmoji(level: number): string {
  return LEVEL_EMOJI[level] ?? "🏅";
}

function levelCheer(level: number): string {
  if (level >= MAX_CUP_LEVEL) return "Вершина стаи! Лео снимает шляпу 🎩";
  return "Ты растёшь — Лео тобой гордится! 🐾";
}

/**
 * Празднующий поп-ап «Новый уровень!» по центру экрана — та же подача, что и у ачивок:
 * аватарка Лео, шеврон с номером уровня, искорки и тёплая фраза. Показывается поверх всего,
 * авто-скрывается, тап закрывает раньше. Очередь и дедуп — на стороне App (по номеру уровня).
 */
export function LevelUpToast({ level, onDone, onShare }: Props) {
  const [leaving, setLeaving] = useState(false);

  useEffect(() => {
    const hide = window.setTimeout(() => setLeaving(true), onShare ? VISIBLE_WITH_SHARE_MS : VISIBLE_MS);
    return () => window.clearTimeout(hide);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- таймер ставится один раз на показ
  }, []);

  useEffect(() => {
    if (!leaving) return;
    const done = window.setTimeout(onDone, EXIT_MS);
    return () => window.clearTimeout(done);
  }, [leaving, onDone]);

  return (
    <div
      className={`achievement-overlay${leaving ? " is-leaving" : ""}`}
      role="status"
      aria-live="polite"
      onClick={() => setLeaving(true)}
    >
      <div className={`achievement-toast achievement-toast--level${leaving ? " is-leaving" : ""}`}>
        <div className="achievement-toast__sparks" aria-hidden>
          <span className="achievement-toast__spark achievement-toast__spark--1">✨</span>
          <span className="achievement-toast__spark achievement-toast__spark--2">🎉</span>
          <span className="achievement-toast__spark achievement-toast__spark--3">⭐️</span>
          <span className="achievement-toast__spark achievement-toast__spark--4">✨</span>
        </div>

        <div className="achievement-toast__badge">
          <img className="achievement-toast__leo" src={LEO_AVATAR_URL} alt="" />
          <span className="achievement-toast__level-chip" aria-hidden>
            {level}
          </span>
        </div>

        <div className="achievement-toast__title">Новый уровень!</div>
        <div className="achievement-toast__label">
          {levelEmoji(level)} {miniappLevelName(level)}
        </div>
        <div className="achievement-toast__cheer">{levelCheer(level)}</div>
        {onShare ? (
          <button
            type="button"
            className="share-brag-btn"
            onClick={(e) => {
              e.stopPropagation();
              onShare();
            }}
          >
            Поделиться
          </button>
        ) : null}
      </div>
    </div>
  );
}
