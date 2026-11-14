import { useEffect, useState } from "react";
import { achievementLabel, parseAchievementKey, type AchievementKey } from "../lib/achievements";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { useI18n } from "../lib/I18nContext";
import { pick, type UILang } from "../lib/i18n";
import "./AchievementToast.css";
import "./ShareCardSheet.css";

type Props = {
  /** Ключ открытой ачивки, напр. "streak-7" / "workout-10". */
  achievementKey: AchievementKey;
  /** Вызывается, когда тост скрылся (по таймеру или тапу) — родитель снимает его с очереди. */
  onDone: () => void;
  /** «Поделиться» — открыть карточку для сторис и чатов (тост при этом закрывается). */
  onShare?: () => void;
};

const VISIBLE_MS = 5000;
/** С кнопкой «Поделиться» тост висит дольше — чтобы успеть до неё дотянуться. */
const VISIBLE_WITH_SHARE_MS = 8000;
const EXIT_MS = 320;

/** Тёплая фраза от Лео — добавляет эмоций поздравлению. */
export function leoCheer(achievementKey: AchievementKey, lang: UILang = "en"): string {
  const parsed = parseAchievementKey(achievementKey);
  // Первая в жизни тренировка — самый важный момент новичка, он заслуживает отдельных слов.
  if (parsed?.kind === "workout" && parsed.threshold === 1) {
    return pick(lang, "Первая тренировка — начало пути! Лео с тобой 🐾", "First workout — you’re on your way! Leo’s with you 🐾");
  }
  return parsed?.kind === "streak" ? pick(lang, "Так держать — Лео тобой гордится!", "Keep it up — Leo’s proud of you!") : pick(lang, "Ты просто зверь! Лео даёт лапу 🐾", "You’re a beast! Leo gives you a paw 🐾");
}

/**
 * Празднующий поп-ап «Ачивка получена!» по центру экрана: аватарка Лео с лапкой,
 * искорки и тёплая фраза. Показывается поверх всего, авто-скрывается, тап закрывает
 * раньше. Очередь и дедуп — на стороне App (по ключам ачивок).
 */
export function AchievementToast({ achievementKey, onDone, onShare }: Props) {
  const { lang } = useI18n();
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
      <div className={`achievement-toast${leaving ? " is-leaving" : ""}`}>
        {/* Искорки-конфетти вокруг лапки */}
        <div className="achievement-toast__sparks" aria-hidden>
          <span className="achievement-toast__spark achievement-toast__spark--1">✨</span>
          <span className="achievement-toast__spark achievement-toast__spark--2">🎉</span>
          <span className="achievement-toast__spark achievement-toast__spark--3">⭐️</span>
          <span className="achievement-toast__spark achievement-toast__spark--4">✨</span>
        </div>

        <div className="achievement-toast__badge">
          <img className="achievement-toast__leo" src={LEO_AVATAR_URL} alt="" />
          <span className="achievement-toast__paw" aria-hidden>
            🐾
          </span>
        </div>

        <div className="achievement-toast__title">{pick(lang, "Ачивка получена!", "Achievement unlocked!")}</div>
        <div className="achievement-toast__label">{achievementLabel(achievementKey, lang)}</div>
        <div className="achievement-toast__cheer">{leoCheer(achievementKey, lang)}</div>
        {onShare ? (
          <button
            type="button"
            className="share-brag-btn"
            onClick={(e) => {
              e.stopPropagation();
              onShare();
            }}
          >
            {pick(lang, "Поделиться", "Share")}
          </button>
        ) : null}
      </div>
    </div>
  );
}
