// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render, screen, fireEvent } from "@testing-library/react";
import { ActivityCard, avatarFallbackGlyph, trainingReactionVisibleCount } from "./ActivityCard";

class ROStub {
  observe() {}
  unobserve() {}
  disconnect() {}
}
(globalThis as unknown as { ResizeObserver: typeof ROStub }).ResizeObserver = ROStub;
// Эмулируем мышь, чтобы hover открывал поповер «кто отреагировал».
(globalThis as unknown as { matchMedia: (q: string) => MediaQueryList }).matchMedia = (q: string) =>
  ({ matches: q.includes("hover: hover"), media: q, addEventListener() {}, removeEventListener() {} }) as unknown as MediaQueryList;

afterEach(cleanup);

const baseProps = {
  avatar: "🦁",
  name: "Иван",
  timeAgo: "5 мин назад",
  emoji: "🏃",
  activity: "Бег",
  details: "15 мин, интенсивность 3/5",
};

describe("ActivityCard streak pill", () => {
  it("renders streak number with correctly declined aria-label", () => {
    render(<ActivityCard {...baseProps} streak={21} />);
    const pill = screen.getByLabelText("Стрик: 21 день подряд");
    expect(pill).toBeTruthy();
    expect(pill.textContent).toContain("21");
    expect(pill.textContent).toContain("Стрик");
  });

  it("uses 'дня' form for 3", () => {
    render(<ActivityCard {...baseProps} streak={3} />);
    expect(screen.getByLabelText("Стрик: 3 дня подряд")).toBeTruthy();
  });

  it("uses 'дней' form for 7", () => {
    render(<ActivityCard {...baseProps} streak={7} />);
    expect(screen.getByLabelText("Стрик: 7 дней подряд")).toBeTruthy();
  });

  it("hides the streak pill when hideStreak is set", () => {
    render(<ActivityCard {...baseProps} streak={10} hideStreak />);
    expect(screen.queryByLabelText(/^Стрик:/)).toBeNull();
  });
});

describe("trainingReactionVisibleCount", () => {
  it("always shows up to three reaction types without collapsing to more", () => {
    expect(trainingReactionVisibleCount(120, 1)).toBe(1);
    expect(trainingReactionVisibleCount(120, 2)).toBe(2);
    expect(trainingReactionVisibleCount(120, 3)).toBe(3);
  });

  it("may hide extra reaction types behind more when there are more than three", () => {
    expect(trainingReactionVisibleCount(120, 4)).toBeLessThan(4);
  });
});

describe("ActivityCard reactions popover", () => {
  // Регресс: голос без фото и без имени не должен ронять рендер (был «чёрный экран»
  // из-за v.name.trim() при отсутствии ErrorBoundary).
  it("shows Leo avatar in likers popover when Leo reacted", () => {
    const { container } = render(
      <ActivityCard
        {...baseProps}
        streak={4}
        onReactionClick={() => {}}
        reactions={[{ emoji: "🔥", count: 1, me: false, voters: [{ name: "Лео", photo_url: "/leo-avatar.png" }] }]}
      />,
    );
    const chip = container.querySelector(".act-card__react-btn");
    fireEvent.mouseEnter(chip!);
    const img = document.querySelector(".act-card__likers-ava-img") as HTMLImageElement | null;
    expect(img).toBeTruthy();
    expect(img!.src).toMatch(/leo-avatar\.png$/);
  });

  it("does not crash for a voter with no photo and missing name", () => {
    const { container } = render(
      <ActivityCard
        {...baseProps}
        streak={4}
        onReactionClick={() => {}}
        reactions={[
          // @ts-expect-error malformed voter: no name, no photo
          { emoji: "🔥", count: 1, me: false, voters: [{}] },
        ]}
      />,
    );
    const chip = container.querySelector(".act-card__react-btn");
    fireEvent.mouseEnter(chip!);
    const pop = document.querySelector(".act-card__likers");
    expect(pop).toBeTruthy();
    expect(pop!.textContent).toContain("Участник");
  });
});

describe("ActivityCard training photo fallback", () => {
  it("renders the photo when trainingPhotoUrl is present", () => {
    const { container } = render(
      <ActivityCard {...baseProps} streak={5} trainingPhotoUrl="https://example.test/p.jpg" />,
    );
    const img = container.querySelector(".act-card__photo") as HTMLImageElement | null;
    expect(img).toBeTruthy();
    expect(img!.src).toContain("https://example.test/p.jpg");
    expect(container.querySelector(".act-card__photo-fallback")).toBeNull();
  });

  it("shows a retry fallback instead of silently hiding a broken photo", () => {
    const { container } = render(
      <ActivityCard {...baseProps} streak={5} trainingPhotoUrl="https://example.test/broken.jpg" />,
    );
    const img = container.querySelector(".act-card__photo") as HTMLImageElement;
    fireEvent.error(img);
    // Фото больше не рендерится, но и не исчезает молча — есть кликабельная заглушка.
    expect(container.querySelector(".act-card__photo")).toBeNull();
    const fallback = container.querySelector(".act-card__photo-fallback");
    expect(fallback).toBeTruthy();
    expect(fallback!.textContent).toContain("Фото не загрузилось");
  });

  it("re-requests the photo with a cache-bust param on retry", () => {
    const { container } = render(
      <ActivityCard {...baseProps} streak={5} trainingPhotoUrl="https://example.test/broken.jpg" />,
    );
    fireEvent.error(container.querySelector(".act-card__photo")!);
    fireEvent.click(container.querySelector(".act-card__photo-fallback")!);
    const img = container.querySelector(".act-card__photo") as HTMLImageElement;
    expect(img).toBeTruthy();
    expect(img.src).toContain("r=1");
  });
});

describe("avatarFallbackGlyph", () => {
  it("uses the first meaningful letter for @usernames (not a generic paw)", () => {
    expect(avatarFallbackGlyph("@arturio222")).toBe("A");
    expect(avatarFallbackGlyph("@f_emmm")).toBe("F");
    expect(avatarFallbackGlyph("#tag")).toBe("T");
  });
  it("keeps emoji names and only falls back to paw when truly empty", () => {
    expect(avatarFallbackGlyph("🐆 Лео")).toBe("🐆");
    expect(avatarFallbackGlyph("Аня")).toBe("А");
    expect(avatarFallbackGlyph("")).toBe("🐾");
    expect(avatarFallbackGlyph("@")).toBe("🐾");
  });
});

describe("ActivityCard reaction picker gesture", () => {
  it("does not open the picker on a short touch tap (keeps scroll free)", () => {
    const { container } = render(
      <ActivityCard
        {...baseProps}
        streak={2}
        onReactionClick={() => {}}
        reactions={[{ emoji: "🔥", count: 0, me: false }]}
      />,
    );
    const card = container.querySelector(".act-card")!;
    fireEvent.touchStart(card, { touches: [{ clientX: 12, clientY: 12 }] });
    fireEvent.touchEnd(card);
    expect(document.querySelector(".act-card__react-picker")).toBeNull();
  });

  it("opens the picker on a mouse click (desktop)", () => {
    const { container } = render(
      <ActivityCard
        {...baseProps}
        streak={2}
        onReactionClick={() => {}}
        reactions={[{ emoji: "🔥", count: 0, me: false }]}
      />,
    );
    fireEvent.click(container.querySelector(".act-card")!);
    expect(document.querySelector(".act-card__react-picker")).toBeTruthy();
  });

  it("clamps a long regular comment and expands on tap", () => {
    const long = "Очень длинный отчёт. ".repeat(20);
    render(<ActivityCard {...baseProps} streak={2} comment={long} />);
    const toggle = screen.getByRole("button", { name: "Показать полностью" });
    expect(toggle).toBeTruthy();
    fireEvent.click(toggle);
    expect(screen.getByRole("button", { name: "Свернуть" })).toBeTruthy();
  });
});

describe("ActivityCard thread reply", () => {
  const thread = [
    {
      id: 11,
      author: "Лео",
      text: "Красиво пробежал",
      timeAgo: "сейчас",
      isYou: false,
      isLeo: true,
    },
    {
      id: 12,
      author: "Аня",
      text: "огонь",
      timeAgo: "сейчас",
      isYou: false,
    },
  ];
  const composer = {
    draft: "",
    onDraftChange: () => {},
    onSubmit: () => {},
    posting: false,
  };

  it("lets you reply to Leo or another participant in the thread", () => {
    const calls: { replyToThreadId: number; authorLabel: string }[] = [];
    render(
      <ActivityCard
        {...baseProps}
        streak={2}
        threadReplies={thread}
        threadComposer={composer}
        onThreadReplyIntent={(payload) => calls.push(payload)}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Комментарии/ }));
    const buttons = screen.getAllByRole("button", { name: "Ответить" });
    expect(buttons).toHaveLength(2);
    fireEvent.click(buttons[0]);
    fireEvent.click(buttons[1]);
    expect(calls[0]).toMatchObject({ replyToThreadId: 11, authorLabel: "Лео" });
    expect(calls[1]).toMatchObject({ replyToThreadId: 12, authorLabel: "Аня" });
  });

  it("shows the reply intent banner like on a report card", () => {
    render(
      <ActivityCard
        {...baseProps}
        streak={2}
        threadReplies={thread}
        threadComposer={composer}
        threadReplyIntent={{ replyToThreadId: 11, authorLabel: "Лео", excerpt: "Красиво пробежал" }}
        onCancelThreadReplyIntent={() => {}}
      />,
    );
    fireEvent.click(screen.getByRole("button", { name: /Комментарии/ }));
    const banner = document.querySelector(".act-card__reply-intent");
    expect(banner?.textContent).toContain("Ответ");
    expect(banner?.textContent).toContain("Лео");
    expect(banner?.textContent).toContain("Красиво пробежал");
    expect(screen.getByPlaceholderText("Сообщение для Лео…")).toBeTruthy();
  });
});

describe("ActivityCard avatar fallback on load error", () => {
  it("shows the author initial when the avatar image fails, not a paw", () => {
    const { container } = render(
      <ActivityCard {...baseProps} name="@arturio222" streak={1} avatar="https://example.test/a.jpg" />,
    );
    const img = container.querySelector(".act-card__avatar-img") as HTMLImageElement;
    expect(img).toBeTruthy();
    fireEvent.error(img);
    const avatar = container.querySelector(".act-card__avatar");
    expect(avatar?.textContent).toBe("A");
  });
});
