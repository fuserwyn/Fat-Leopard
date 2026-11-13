import { useEffect, useState } from "react";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import "./AchievementToast.css";

type Props = {
  /** Вызывается, когда поп-ап скрылся (по таймеру или тапу). */
  onDone: () => void;
};

const VISIBLE_MS = 5000;
const EXIT_MS = 320;

/**
 * Празднующий поп-ап после успешного доната — та же подача, что у ачивок:
 * Лео по центру, искорки, «Рык! Спасибо за поддержку!». Тап или таймер закрывает.
 */
export function DonateThanksToast({ onDone }: Props) {
  const [leaving, setLeaving] = useState(false);

  useEffect(() => {
    const hide = window.setTimeout(() => setLeaving(true), VISIBLE_MS);
    return () => window.clearTimeout(hide);
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
        <div className="achievement-toast__sparks" aria-hidden>
          <span className="achievement-toast__spark achievement-toast__spark--1">✨</span>
          <span className="achievement-toast__spark achievement-toast__spark--2">🎉</span>
          <span className="achievement-toast__spark achievement-toast__spark--3">💛</span>
          <span className="achievement-toast__spark achievement-toast__spark--4">✨</span>
        </div>

        <div className="achievement-toast__badge">
          <img className="achievement-toast__leo" src={LEO_AVATAR_URL} alt="" />
          <span className="achievement-toast__paw" aria-hidden>
            🐆
          </span>
        </div>

        <div className="achievement-toast__title">Donation received!</div>
        <div className="achievement-toast__label">Roar! Thanks for your support!</div>
        <div className="achievement-toast__cheer">Leo appreciates the pack — you make the project stronger 🐾</div>
      </div>
    </div>
  );
}
