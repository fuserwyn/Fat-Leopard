import "./BottomNav.css";

type Tab = "feed" | "profile";

type Props = {
  active: Tab;
  onFeed: () => void;
  onWorkout: () => void;
  onProfile: () => void;
};

export function BottomNav({ active, onFeed, onWorkout, onProfile }: Props) {
  return (
    <nav className="bottom-nav" role="navigation" aria-label="Основное меню">
      <button
        type="button"
        className={`bottom-nav__item ${active === "feed" ? "is-active" : ""}`}
        onClick={onFeed}
        aria-current={active === "feed" ? "page" : undefined}
      >
        <span className="bottom-nav__icon" aria-hidden>
          📰
        </span>
        <span className="bottom-nav__label">Лента</span>
      </button>
      <button type="button" className="bottom-nav__fab" onClick={onWorkout} aria-label="Новая тренировка">
        <span>🔥</span>
      </button>
      <button
        type="button"
        className={`bottom-nav__item ${active === "profile" ? "is-active" : ""}`}
        onClick={onProfile}
        aria-current={active === "profile" ? "page" : undefined}
      >
        <span className="bottom-nav__icon" aria-hidden>
          👤
        </span>
        <span className="bottom-nav__label">Профиль</span>
      </button>
    </nav>
  );
}
