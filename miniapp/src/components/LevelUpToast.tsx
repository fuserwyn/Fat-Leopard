import { useEffect, useState } from "react";
import { miniappLevelName, MAX_CUP_LEVEL } from "../lib/miniappLevel";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { useI18n } from "../lib/I18nContext";
import "./AchievementToast.css";
import "./ShareCardSheet.css";

type Props = {
  /** Level reached (1…6). */
  level: number;
  /** Called when the popup hides (timer or tap) — parent clears the queue. */
  onDone: () => void;
  /** Share — open card for stories and chats (toast closes). */
  onShare?: () => void;
};

const VISIBLE_MS = 5000;
/** With Share, the toast stays longer so you can tap it. */
const VISIBLE_WITH_SHARE_MS = 8000;
const EXIT_MS = 320;

/** Level animal emoji (index = 1-based level; meerkat has no emoji — paw). */
const LEVEL_EMOJI = ["", "🐾", "🦌", "🦓", "🐆", "🦁", "🐘"];

function levelEmoji(level: number): string {
  return LEVEL_EMOJI[level] ?? "🏅";
}

function levelCheer(level: number): string {
  if (level >= MAX_CUP_LEVEL) return "Top of the pack! Leo tips his hat 🎩";
  return "You’re growing — Leo is proud of you! 🐾";
}

/**
 * Centered “New level!” celebration — same style as achievements: Leo avatar, level
 * chip, sparkles. Auto-hides; tap closes sooner. Queue/dedup in App (by level number).
 */
export function LevelUpToast({ level, onDone, onShare }: Props) {
  const { lang } = useI18n();
  const [leaving, setLeaving] = useState(false);

  useEffect(() => {
    const hide = window.setTimeout(() => setLeaving(true), onShare ? VISIBLE_WITH_SHARE_MS : VISIBLE_MS);
    return () => window.clearTimeout(hide);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- timer once per show
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

        <div className="achievement-toast__title">New level!</div>
        <div className="achievement-toast__label">
          {levelEmoji(level)} {miniappLevelName(level, lang)}
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
            Share
          </button>
        ) : null}
      </div>
    </div>
  );
}
