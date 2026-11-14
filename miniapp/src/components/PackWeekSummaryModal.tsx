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
import { useI18n } from "../lib/I18nContext";
import { pick } from "../lib/i18n";
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
  const { lang } = useI18n();
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") onClose();
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
  }, [onClose]);

  const pct = packWeeklyProgressBarPct(summary.workouts, summary.goal);
  const range = packWeekRangeLabel(summary.weekStart, summary.weekEnd, lang);

  return (
    <div className="pack-week-summary-overlay" onClick={onClose}>
      <div
        className={`pack-week-summary${summary.goalReached ? " is-reached" : ""}`}
        role="dialog"
        aria-modal="true"
        aria-label={pick(lang, "Итоги недели стаи", "Pack week summary")}
        onClick={(e) => e.stopPropagation()}
      >
        <div className="pack-week-summary__eyebrow">{pick(lang, "Итоги недели стаи", "Pack week recap")}{range ? ` · ${range}` : ""}</div>
        <img className="pack-week-summary__leo" src={LEO_AVATAR_URL} alt="" />
        <div className="pack-week-summary__title">{packWeekSummaryHeadline(summary, lang)}</div>

        <div className="pack-week-summary__score" aria-label={pick(lang, `${summary.workouts} из ${summary.goal}`, `${summary.workouts} of ${summary.goal}`)}>
          <span className="pack-week-summary__count">{summary.workouts}</span>
          <span className="pack-week-summary__goal">/ {summary.goal}</span>
        </div>
        <div className="pack-week-summary__caption">{pick(lang, `${workoutsWord(summary.workouts, "ru")} стаи за неделю`, "pack workouts this week")}</div>
        <div className="pack-week-summary__bar" aria-hidden>
          <div className="pack-week-summary__bar-fill" style={{ width: `${pct}%` }} />
        </div>

        <div className="pack-week-summary__stats">
          <div className="pack-week-summary__stat">
            <span className="pack-week-summary__stat-value">{summary.myWorkouts}</span>
            <span className="pack-week-summary__stat-label">{pick(lang, "твой вклад", "your contribution")}</span>
          </div>
          <div className="pack-week-summary__stat">
            <span className="pack-week-summary__stat-value">{summary.participants}</span>
            <span className="pack-week-summary__stat-label">{pick(lang, "леопардов в зачёте", "leopards counted")}</span>
          </div>
          <div className="pack-week-summary__stat">
            <span className="pack-week-summary__stat-value">{summary.nextGoal}</span>
            <span className="pack-week-summary__stat-label">{pick(lang, "цель новой недели", "next week's goal")}</span>
          </div>
        </div>

        <p className="pack-week-summary__next">{packWeekSummaryNextLine(summary, lang)}</p>
        <button type="button" className="pack-week-summary__btn" onClick={onClose}>
          {pick(lang, "Вперёд, стая!", "Onward, pack!")}
        </button>
      </div>
    </div>
  );
}
