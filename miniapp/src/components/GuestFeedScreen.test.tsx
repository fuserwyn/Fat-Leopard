// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { GuestFeedScreen } from "./GuestFeedScreen";
import { parseGuestFeed } from "../lib/guestFeed";

afterEach(() => {
  cleanup();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  delete (window as { Telegram?: unknown }).Telegram;
});

const feed = parseGuestFeed({
  in_pack: false,
  join_url: "https://t.me/leo_bot?start=src-guest_feed",
  items: [
    { id: 1, username: "Анна", type: "training_done", text: "#training_done\nбег, 30 мин, интенсивность 3/5\nутренняя пробежка", created_at: "2026-10-09T05:00:00Z", streak_days: 4 },
    { id: 2, username: "Борис", type: "training_done", text: "#training_done\nйога, 20 мин\nрастяжка", created_at: "2026-10-09T04:00:00Z", streak_days: 1 },
  ],
})!;

describe("GuestFeedScreen", () => {
  it("shows a few pack posts and the join button", () => {
    const openTelegramLink = vi.fn();
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: { openTelegramLink, close: vi.fn() } };
    const onInPack = vi.fn();
    const { getByText, getByRole, queryByRole } = render(<GuestFeedScreen initData="x" onInPack={onInPack} initialFeed={feed} />);
    expect(getByText("Анна")).toBeTruthy();
    expect(getByText("Борис")).toBeTruthy();
    expect(getByText(/утренняя пробежка/)).toBeTruthy();
    // Только чтение: ни поля комментария, ни меню поста.
    expect(queryByRole("textbox")).toBeNull();
    fireEvent.click(getByRole("button", { name: "Вступить" }));
    expect(openTelegramLink).toHaveBeenCalledWith("https://t.me/leo_bot?start=src-guest_feed");
    expect(onInPack).not.toHaveBeenCalled();
  });

  it("explains an empty feed and closes the app on join without a link", () => {
    const close = vi.fn();
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: { close } };
    const { getByText, getByRole } = render(<GuestFeedScreen initData="x" onInPack={vi.fn()} initialFeed={null} />);
    expect(getByText(/Свежих тренировок пока не видно/)).toBeTruthy();
    fireEvent.click(getByRole("button", { name: "Вступить" }));
    expect(close).toHaveBeenCalled();
  });

  it("hands over to the app when the viewer is already in the pack", () => {
    const onInPack = vi.fn();
    render(<GuestFeedScreen initData="x" onInPack={onInPack} initialFeed={{ inPack: true, items: [], joinUrl: "" }} />);
    expect(onInPack).toHaveBeenCalled();
  });

  it("loads the feed from the server", async () => {
    vi.stubEnv("VITE_MINIAPP_API_URL", "https://api.test");
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => ({
        ok: true,
        json: async () => ({ ok: true, in_pack: false, items: [{ id: 3, username: "Вера", type: "training_done", text: "#training_done\nплавание, 40 мин", created_at: "2026-10-09T03:00:00Z" }] }),
      })),
    );
    vi.resetModules();
    const { GuestFeedScreen: Fresh } = await import("./GuestFeedScreen");
    const { getByText, findByText } = render(<Fresh initData="x" onInPack={vi.fn()} />);
    expect(getByText("Загружаем ленту…")).toBeTruthy();
    expect(await findByText("Вера")).toBeTruthy();
    await waitFor(() => expect(() => getByText("Загружаем ленту…")).toThrow());
  });
});
