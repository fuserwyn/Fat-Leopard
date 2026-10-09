// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { contractProblems, loadBackendRoutes } from "./backendContract";
import { importWithApi, mockFetch } from "./testApi";
import { openGuestJoin, parseGuestFeed } from "./guestFeed";
import { miniappAccessGate, type OnboardingEnsureResult } from "./miniappOnboarding";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  delete (window as { Telegram?: unknown }).Telegram;
});

const raw = {
  ok: true,
  in_pack: false,
  join_url: "https://t.me/leo_bot?start=src-guest_feed",
  items: [
    { id: 7, username: "Анна", type: "training_done", text: "#training_done\nбег, 30 мин", created_at: "2026-10-09T05:00:00Z", streak_days: 4, training_photo_url: "https://cdn/p.jpg" },
    { id: 8, username: "", type: "training_done", text: "йога, 20 мин", created_at: "2026-10-09T04:00:00Z" },
    { id: 9, text: "  " },
    null,
  ],
};

describe("guest feed parsing", () => {
  it("keeps posts with text and hides author ids", () => {
    const f = parseGuestFeed(raw)!;
    expect(f.inPack).toBe(false);
    expect(f.joinUrl).toBe(raw.join_url);
    expect(f.items).toHaveLength(2);
    expect(f.items[0]).toMatchObject({ id: 7, user_id: 0, username: "Анна", streak_days: 4, is_you: false, source: "feed", training_photo_url: "https://cdn/p.jpg" });
    expect(f.items[1]).toMatchObject({ username: "Участник стаи", streak_days: 0, training_photo_url: undefined });
  });

  it("drops a join link that is not t.me and garbage answers", () => {
    expect(parseGuestFeed({ in_pack: true, join_url: "javascript:alert(1)" })).toEqual({ inPack: true, items: [], joinUrl: "" });
    expect(parseGuestFeed(null)).toBeNull();
    expect(parseGuestFeed("x")).toBeNull();
  });
});

describe("guest join", () => {
  it("opens the bot inside Telegram", () => {
    const openTelegramLink = vi.fn();
    const close = vi.fn();
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: { openTelegramLink, close } };
    openGuestJoin(raw.join_url);
    expect(openTelegramLink).toHaveBeenCalledWith(raw.join_url);
    expect(close).not.toHaveBeenCalled();
  });

  it("falls back to openLink and window.open", () => {
    const openLink = vi.fn();
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: { openLink, close: vi.fn() } };
    openGuestJoin(raw.join_url);
    expect(openLink).toHaveBeenCalledWith(raw.join_url);
    delete (window as { Telegram?: unknown }).Telegram;
    const open = vi.fn();
    vi.stubGlobal("open", open);
    openGuestJoin(raw.join_url);
    expect(open).toHaveBeenCalledWith(raw.join_url, "_blank");
  });

  it("just closes the mini app without a link", () => {
    const close = vi.fn();
    (window as unknown as { Telegram: unknown }).Telegram = { WebApp: { close } };
    openGuestJoin("");
    expect(close).toHaveBeenCalled();
  });
});

describe("access gate", () => {
  const base: OnboardingEnsureResult = { ok: true, inPack: true, deleted: false, accessState: "active", justOnboarded: false, isRejoin: false };
  it("sends strangers to the guest feed and keeps members in the app", () => {
    expect(miniappAccessGate(base)).toBe("ok");
    expect(miniappAccessGate({ ...base, inPack: false, accessState: "out" })).toBe("guest");
    expect(miniappAccessGate({ ...base, inPack: false, deleted: true, accessState: "deleted" })).toBe("deleted");
    expect(miniappAccessGate({ ...base, accessState: "deleted" })).toBe("deleted");
    // Сервер не ответил — не запираем приложение.
    expect(miniappAccessGate({ ...base, ok: false, inPack: false, accessState: "unknown" })).toBe("ok");
  });
});

describe("guest feed API", () => {
  const backend = loadBackendRoutes();

  it("loads the guest feed", async () => {
    const api = await importWithApi(() => import("./guestFeed"));
    const calls = mockFetch(() => ({ json: raw }));
    const f = await api.fetchGuestFeed("i");
    expect(f?.items).toHaveLength(2);
    expect(calls[0].path).toBe("/api/miniapp/feed/guest");
    expect(calls[0].body).toEqual({ init_data: "i" });
    if (backend) expect(contractProblems(backend, calls[0])).toEqual([]);

    mockFetch(() => ({ status: 401, json: { error: "invalid_init_data" } }));
    expect(await api.fetchGuestFeed("i")).toBeNull();
    mockFetch(() => ({ json: { ok: false } }));
    expect(await api.fetchGuestFeed("i")).toBeNull();
    mockFetch(() => ({ fail: true }));
    expect(await api.fetchGuestFeed("i")).toBeNull();
    expect(await api.fetchGuestFeed(" ")).toBeNull();
  });

  it("does nothing without an API address", async () => {
    const api = await importWithApi(() => import("./guestFeed"), "");
    const calls = mockFetch(() => ({ json: raw }));
    expect(await api.fetchGuestFeed("i")).toBeNull();
    expect(calls).toHaveLength(0);
  });
});
