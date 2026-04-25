import { useState } from "react";
import "./ProfileScreen.css";

type Props = { name: string; streak: number; workouts: number };

const LEVELS = [200];

export function ProfileScreen({ name, streak, workouts }: Props) {
  const xp = 25;
  const [burn, setBurn] = useState<3 | 5 | 7>(5);

  return (
    <div className="profile">
      <header className="profile__hero">
        <div className="profile__avatar" aria-hidden>
          🐆
        </div>
        <h1 className="profile__name">{name}</h1>
        <p className="profile__level muted">Уровень 1 · Новичок</p>
        <div className="profile__xp">
          <div className="profile__xp-bar">
            <div
              className="profile__xp-fill"
              style={{ width: `${Math.min(100, (xp / LEVELS[0]) * 100)}%` }}
            />
          </div>
          <span className="profile__xp-txt">
            {xp} XP / {LEVELS[0]}
          </span>
        </div>
      </header>

      <div className="profile__grid3">
        <div className="stat-card">
          <div className="stat-card__label">СТРИК</div>
          <div className="stat-card__val">
            <span className="stat-card__streak-ico">🔥</span> {streak}
          </div>
        </div>
        <div className="stat-card">
          <div className="stat-card__label">РЕКОРД</div>
          <div className="stat-card__val">{streak} д</div>
        </div>
        <div className="stat-card">
          <div className="stat-card__label">ТРЕНИРОВОК</div>
          <div className="stat-card__val">{workouts}</div>
        </div>
      </div>

      <h2 className="section-title">За неделю</h2>
      <div className="profile__grid2">
        <div className="wide-card">
          <div className="wide-card__label">Тренировок</div>
          <div className="wide-card__val">{workouts}</div>
        </div>
        <div className="wide-card">
          <div className="wide-card__label">Средняя интенсивность</div>
          <div className="wide-card__val">{workouts > 0 ? "3.0" : "—"}</div>
        </div>
      </div>

      <h2 className="section-title">Достижения</h2>
      <div className="profile__empty">Пока нет — тренируйся, и они появятся</div>

      <h2 className="section-title">Порог сгорания</h2>
      <p className="profile__hint muted">Текущий: {burn} дн. без тренировки — и стрик сгорает</p>
      <div className="burn-row" role="group" aria-label="Дней до сгорания стрика">
        {([3, 5, 7] as const).map((d) => (
          <button
            key={d}
            type="button"
            className={`burn-btn ${burn === d ? "is-on" : ""}`}
            aria-pressed={burn === d}
            onClick={() => setBurn(d)}
          >
            {d}
          </button>
        ))}
      </div>

      <h2 className="section-title">Заморозка</h2>
      <p className="profile__hint muted">Осталось: 1 из 1 в месяц (Free)</p>
    </div>
  );
}
