import { useEffect } from "react";
import { LEO_AVATAR_URL } from "../lib/leoAvatar";
import { packWeeklyProgressBarPct } from "../lib/packWeeklyGoal";
import {
  packWeekRangeLabel,
  packWeekSummaryHeadline,
  packWeekSummaryNextLine,
  workoutsWord,
  type PackWeekSummary,
} from "../lib/packWeekSummary";
import "./PackWeekSummaryModal.css";

type Props = {
  summary: PackWeekSummary;
  /** Закрыли модалку — родитель отмечает итоги просмотренными. */
  onClose: () => void;
};

/**
 * «Итоги недели стаи» — карточка, которую Лео собирает в понедельник: сколько тренировок
 * сделала стая, закрыта ли цель, вклад участника и цель новой недели.
 */
export function PackWeekSummaryModal({ summary, onClose }: Props) {
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const pct = packWeeklyProgressBarPct(summary.workouts, summary.goal);
  const range = packWeekRangeLabel(summary.weekStart, summary.weekEnd);

  return (
    <div className="pack-week-summary-overlay" onClick={onClose}>
      <div
        className={`pack-week-summary${summary.goalReached ? " is-reached" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label="Итоги недели стаи"
        onClick={(e) => e.stopPropagation()}
      >
        <div className="pack-week-summary__eyebrow">Итоги недели стаи{range ? ` · ${range}` : ""}</div>
        <img className="pack-week-summary__leo" src={LEO_AVATAR_URL} alt="" />
        <div className="pack-week-summary__title">{packWeekSummaryHeadline(summary)}</div>

        <div className="pack-week-summary__score" aria-label={`${summary.workouts} из ${summary.goal}`}>
          <span className="pack-week-summary__count">{summary.workouts}</span>
          <span className="pack-week-summary__goal">/ {summary.goal}</span>
        </div>
        <div className="pack-week-summary__caption">{workoutsWord(summary.workouts)} стаи за неделю</div>
        <div className="pack-week-summary__bar" aria-hidden>
          <div className="pack-week-summary__bar-fill" style={{ width: `${pct}%` }} />
        </div>

        <div className="pack-week-summary__stats">
          <div className="pack-week-summary__stat">
            <span className="pack-week-summary__stat-value">{summary.myWorkouts}</span>
            <span className="pack-week-summary__stat-label">твой вклад</span>
          </div>
          <div className="pack-week-summary__stat">
            <span className="pack-week-summary__stat-value">{summary.participants}</span>
            <span className="pack-week-summary__stat-label">леопардов в зачёте</span>
          </div>
          <div className="pack-week-summary__stat">
            <span className="pack-week-summary__stat-value">{summary.nextGoal}</span>
            <span className="pack-week-summary__stat-label">цель новой недели</span>
          </div>
        </div>

        <p className="pack-week-summary__next">{packWeekSummaryNextLine(summary)}</p>
        <button type="button" className="pack-week-summary__btn" onClick={onClose}>
          Вперёд, стая!
        </button>
      </div>
    </div>
  );
}
