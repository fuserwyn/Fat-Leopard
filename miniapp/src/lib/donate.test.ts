import { afterEach, describe, expect, it, vi } from "vitest";
import { importWithApi, mockFetch } from "./testApi";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.useRealTimers();
});

const load = () => importWithApi(() => import("./donate"));

describe("donate client", () => {
  it("maps server options and falls back to empty on any failure", async () => {
    const donate = await load();
    const calls = mockFetch(() => ({
      json: { ok: true, stars_tiers: [50, 150], card_tiers_rub: [100], stars_available: true, card_available: false, completed_count: 2 },
    }));
    expect(await donate.fetchDonateOptions("init")).toEqual({
      starsTiers: [50, 150],
      cardTiersRub: [100],
      starsAvailable: true,
      cardAvailable: false,
      completedCount: 2,
    });
    expect(calls[0]).toMatchObject({ path: "/api/miniapp/donate/options", method: "POST", body: { init_data: "init" } });

    // Без initData на сервер не ходим.
    expect(await donate.fetchDonateOptions("  ")).toEqual(donate.emptyDonateOptions);
    expect(calls).toHaveLength(1);

    mockFetch(() => ({ status: 500, json: { error: "boom" } }));
    expect(await donate.fetchDonateOptions("init")).toEqual(donate.emptyDonateOptions);
    mockFetch(() => ({ fail: true }));
    expect(await donate.fetchDonateOptions("init")).toEqual(donate.emptyDonateOptions);
    mockFetch(() => ({ json: { ok: true, stars_tiers: "мусор" } }));
    expect((await donate.fetchDonateOptions("init")).starsTiers).toEqual([]);
  });

  it("creates stars and card payments with the chosen amount", async () => {
    const donate = await load();
    const calls = mockFetch((s) =>
      s.path.endsWith("/stars")
        ? { json: { ok: true, invoice_link: "https://t.me/$inv", donation_id: 7 } }
        : { json: { ok: true, confirmation_url: "https://kassa.test/pay", donation_id: 8 } },
    );
    expect(await donate.createStarsDonateInvoice("init", 150)).toEqual({ link: "https://t.me/$inv", donationId: 7 });
    expect(await donate.createCardDonatePayment("init", 300)).toEqual({ link: "https://kassa.test/pay", donationId: 8 });
    expect(calls[0].body).toEqual({ init_data: "init", stars: 150 });
    expect(calls[1]).toMatchObject({ path: "/api/miniapp/donate/card", body: { init_data: "init", rub: 300 } });

    // Сервер ответил без ссылки — платёж не начинаем.
    mockFetch(() => ({ json: { ok: true, donation_id: 9 } }));
    expect(await donate.createStarsDonateInvoice("init", 150)).toBeNull();
    expect(await donate.createCardDonatePayment("init", 300)).toBeNull();
  });

  it("polls card donation status until it completes or attempts run out", async () => {
    const donate = await load();
    vi.useFakeTimers();
    let n = 0;
    const calls = mockFetch(() => ({ json: { ok: true, status: ++n >= 3 ? "completed" : "pending" } }));
    const done = donate.waitForDonationCompleted("init", 8, 6, 2500);
    await vi.advanceTimersByTimeAsync(2500 * 6);
    expect(await done).toBe(true);
    expect(calls).toHaveLength(3);
    expect(calls[0].body).toEqual({ init_data: "init", donation_id: 8 });

    mockFetch(() => ({ json: { ok: true, status: "pending" } }));
    const never = donate.waitForDonationCompleted("init", 8, 2, 1000);
    await vi.advanceTimersByTimeAsync(5000);
    expect(await never).toBe(false);

    mockFetch(() => ({ status: 404, json: {} }));
    expect(await donate.fetchDonationStatus("init", 8)).toBeNull();
  });

  it("does nothing without an API address", async () => {
    const donate = await importWithApi(() => import("./donate"), "");
    const calls = mockFetch(() => ({ json: { ok: true } }));
    expect(await donate.fetchDonateOptions("init")).toEqual(donate.emptyDonateOptions);
    expect(await donate.createStarsDonateInvoice("init", 50)).toBeNull();
    expect(calls).toHaveLength(0);
  });
});
