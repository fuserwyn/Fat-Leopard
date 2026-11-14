// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@testing-library/react";
import { AchievementToast, leoCheer } from "./AchievementToast";
import { LevelUpToast } from "./LevelUpToast";
import { PackWeekSummaryModal } from "./PackWeekSummaryModal";
import { DonateThanksToast } from "./DonateThanksToast";
import { LevelCarousel } from "./LevelCarousel";
import { ErrorBoundary } from "./ErrorBoundary";
import { achievementLabel } from "../lib/achievements";
import { buildCupsHistoryRows, cupsHistoryCaption, cupsHistorySortOptions } from "../lib/cupsHistory";
import { achievementShareCard, levelShareCard, shareCardCaption, workoutShareCard } from "../lib/shareCard";
import { packWeekRangeLabel, packWeekSummaryHeadline, packWeekSummaryNextLine, workoutsWord } from "../lib/packWeekSummary";
import { pick } from "../lib/i18n";
import { dtoToCard } from "../lib/packFeed";

// Участник с выбранным русским языком должен видеть русские подписи, а не английские.
beforeEach(() => {
  localStorage.setItem("leo-ui-lang-v2", "ru");
});

afterEach(() => {
  cleanup();
  localStorage.setItem("leo-ui-lang-v2", "en");
});

const summary = {
  weekStart: "2026-10-05",
  weekEnd: "2026-10-11",
  workouts: 70,
  goal: 75,
  goalReached: false,
  nextGoal: 75,
  participants: 12,
  myWorkouts: 4,
};

describe("RU UI for members who chose Russian", () => {
  it("pick returns the variant for the language", () => {
    expect(pick("ru", "да", "yes")).toBe("да");
    expect(pick("en", "да", "yes")).toBe("yes");
  });

  it("achievement toast is Russian", () => {
    const { getByText } = render(<AchievementToast achievementKey="workout-1" onDone={() => {}} onShare={() => {}} />);
    expect(getByText("Ачивка получена!")).toBeTruthy();
    expect(getByText("1 тренировка")).toBeTruthy();
    expect(getByText(/Первая тренировка/)).toBeTruthy();
    expect(getByText("Поделиться")).toBeTruthy();
    expect(leoCheer("streak-7", "ru")).toMatch(/Так держать/);
    expect(leoCheer("workout-10", "ru")).toMatch(/зверь/);
    expect(achievementLabel("streak-7", "ru")).toBe("Стрик 7 дней");
    expect(achievementLabel("bogus" as never, "ru")).toBe("Новая ачивка");
  });

  it("level-up toast, donate toast and level carousel are Russian", () => {
    const lvl = render(<LevelUpToast level={2} onDone={() => {}} />);
    expect(lvl.getByText("Новый уровень!")).toBeTruthy();
    expect(lvl.getByText(/Лео тобой гордится/)).toBeTruthy();
    cleanup();
    const donate = render(<DonateThanksToast onDone={() => {}} />);
    expect(donate.getByText("Донат получен!")).toBeTruthy();
    cleanup();
    const carousel = render(<LevelCarousel currentLevel={2} />);
    expect(carousel.getByText(/Уровень 2/)).toBeTruthy();
    expect(carousel.getByLabelText("Следующий уровень")).toBeTruthy();
  });

  it("pack week summary is Russian", () => {
    const { getByText } = render(<PackWeekSummaryModal summary={summary} onClose={() => {}} />);
    expect(getByText("Цель недели не закрыта")).toBeTruthy();
    expect(getByText(/Итоги недели стаи · 5–11 октября/)).toBeTruthy();
    expect(getByText("твой вклад")).toBeTruthy();
    expect(getByText("Вперёд, стая!")).toBeTruthy();
    expect(getByText(/Не хватило 5 тренировок/)).toBeTruthy();
    expect(packWeekSummaryHeadline({ ...summary, goalReached: true }, "ru")).toBe("Цель недели закрыта!");
    expect(packWeekSummaryNextLine({ ...summary, goalReached: true, nextGoal: 80 }, "ru")).toMatch(/Планка растёт/);
    expect(packWeekSummaryNextLine({ ...summary, goalReached: true }, "ru")).toMatch(/^Цель новой недели — 75 тренировок/);
    expect(packWeekRangeLabel("2026-10-05", "", "ru")).toBe("с 5 октября");
    expect([1, 3, 5].map((n) => workoutsWord(n, "ru"))).toEqual(["тренировка", "тренировки", "тренировок"]);
  });

  it("error boundary falls back to the stored language", () => {
    const Boom = () => {
      throw new Error("boom");
    };
    const orig = console.error;
    console.error = () => {};
    try {
      const { getByText } = render(
        <ErrorBoundary label="Лента">
          <Boom />
        </ErrorBoundary>,
      );
      expect(getByText(/не загрузилась/)).toBeTruthy();
    } finally {
      console.error = orig;
    }
  });

  it("cups history, share cards and feed cards are Russian", () => {
    expect(cupsHistoryCaption("ru")).toBe("42 последние тренировки");
    expect(cupsHistorySortOptions("ru").map((o) => o.label)).toEqual(["Дата", "Вид спорта", "Кубки"]);
    const rows = buildCupsHistoryRows(
      [{ date: "2026-10-05", messageText: "бег, 30 мин, интенсивность 3/5", cups: 2, createdAt: "2026-10-05T10:00:00Z" }],
      [{ date: "2026-10-06", cups: 50, createdAt: "2026-10-06T10:00:00Z" }],
      42,
      "ru",
    );
    expect(rows.map((r) => r.workoutType)).toEqual(["Недельное достижение стаи", "Бег"]);
    expect(rows[1].duration).toBe("30 мин");

    const streak = achievementShareCard("streak-7", "", "ru");
    expect(streak.headline).toBe("Стрик");
    expect(streak.name).toBe("Участник стаи");
    expect(shareCardCaption(streak, "")).toMatch(/^Стрик 7 дней подряд в Fat Leopard/);
    expect(achievementShareCard("workout-10", "Аня", "ru").headline).toBe("Ачивка получена");
    expect(levelShareCard(2, "Аня", "ru").headline).toBe("Новый уровень");
    const w = workoutShareCard({ reportLine: "бег, 30 мин", kindLabel: "бег", min: 30, intensity: 3, streak: 5, name: "Аня" }, "ru");
    expect(w.badge).toBe("🔥 Стрик 5 дней");
    expect(shareCardCaption(w, "https://t.me/x")).toMatch(/Тренировка засчитана.*\nТренируйся со мной: https:\/\/t\.me\/x/s);
    expect(shareCardCaption(levelShareCard(2, "Аня", "ru"), "")).toMatch(/^Новый уровень в Fat Leopard/);

    const poll = dtoToCard(
      { id: 1, user_id: 0, username: "", type: "admin_poll", source: "feed", text: "?", created_at: "", poll: { total_votes: 3 } } as never,
      "ru",
    );
    expect(poll.name).toBe("Админ");
  });
});
