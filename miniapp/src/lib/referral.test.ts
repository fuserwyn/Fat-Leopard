import { afterEach, describe, expect, it, vi } from "vitest";
import { friendsWord, parseReferralState, referralProgressLine, referralShareUrl } from "./referral";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.resetModules();
});

describe("parseReferralState", () => {
  it("parses the server answer", () => {
    const s = parseReferralState({
      link: "https://t.me/leo_bot?start=ref-42",
      joined: 14,
      qualified: 12,
      reward_every: 10,
      rewards: 1,
      next_in: 8,
    });
    expect(s).toEqual({ link: "https://t.me/leo_bot?start=ref-42", joined: 14, qualified: 12, rewardEvery: 10, rewards: 1, nextIn: 8 });
  });

  it("rejects answers without a t.me link and fills defaults", () => {
    expect(parseReferralState(null)).toBeNull();
    expect(parseReferralState({ link: "" })).toBeNull();
    expect(parseReferralState({ link: "https://evil.example/x" })).toBeNull();
    const s = parseReferralState({ link: "https://t.me/b?start=ref-1", qualified: 3, joined: 1 });
    expect(s).toMatchObject({ joined: 3, qualified: 3, rewardEvery: 10, rewards: 0, nextIn: 7 });
  });
});

describe("texts", () => {
  it("declines friends", () => {
    expect([1, 2, 5, 11, 21, 22, 10].map(friendsWord)).toEqual(["друг", "друга", "друзей", "друзей", "друг", "друга", "друзей"]);
  });

  it("shows progress to the next streak save", () => {
    const s = { link: "https://t.me/b", joined: 20, qualified: 13, rewardEvery: 10, rewards: 1, nextIn: 7 };
    expect(referralProgressLine(s)).toBe("До попытки спасти стрик: 3 из 10 — ещё 7 друзей с первой тренировкой");
  });

  it("builds a Telegram share url", () => {
    const url = referralShareUrl("https://t.me/b?start=ref-1");
    expect(url.startsWith("https://t.me/share/url?url=https%3A%2F%2Ft.me%2Fb%3Fstart%3Dref-1&text=")).toBe(true);
  });
});

describe("fetchReferralState", () => {
  it("posts init data and parses the referral", async () => {
    vi.stubEnv("VITE_MINIAPP_API_URL", "https://api.test/");
    const fetchMock = vi.fn(async () => ({
      ok: true,
      json: async () => ({ ok: true, referral: { link: "https://t.me/b?start=ref-1", joined: 2, qualified: 1, reward_every: 10, rewards: 0, next_in: 9 } }),
    }));
    vi.stubGlobal("fetch", fetchMock);
    const { fetchReferralState } = await import("./referral");
    const s = await fetchReferralState("init");
    expect(s?.joined).toBe(2);
    expect(fetchMock).toHaveBeenCalledWith("https://api.test/api/miniapp/referral/state", expect.objectContaining({ method: "POST" }));
    expect(await fetchReferralState("  ")).toBeNull();
  });

  it("returns null on errors", async () => {
    vi.stubEnv("VITE_MINIAPP_API_URL", "https://api.test");
    vi.stubGlobal("fetch", vi.fn(async () => ({ ok: false, json: async () => ({ ok: false }) })));
    const { fetchReferralState } = await import("./referral");
    expect(await fetchReferralState("init")).toBeNull();
    vi.stubGlobal("fetch", vi.fn(async () => { throw new Error("net"); }));
    expect(await fetchReferralState("init")).toBeNull();
  });
});
