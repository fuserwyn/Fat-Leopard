// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { importWithApi, mockFetch } from "./testApi";

type Mod = typeof import("./timezoneSync");

const load = () => importWithApi<Mod>(() => import("./timezoneSync"));

/** Подменяет пояс устройства: utcOffsetHours — смещение от UTC (Москва = 3, Белград зимой = 1). */
function setDeviceUtcOffset(utcOffsetHours: number) {
  vi.spyOn(Date.prototype, "getTimezoneOffset").mockReturnValue(-utcOffsetHours * 60);
}

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.useRealTimers();
});

describe("detectOffsetFromMoscow", () => {
  it("считает смещение от МСК и зажимает в -12..+12", async () => {
    const m = await load();
    setDeviceUtcOffset(3);
    expect(m.detectOffsetFromMoscow()).toBe(0);
    setDeviceUtcOffset(1);
    expect(m.detectOffsetFromMoscow()).toBe(-2);
    setDeviceUtcOffset(5.5);
    expect(m.detectOffsetFromMoscow()).toBe(3);
    setDeviceUtcOffset(-14);
    expect(m.detectOffsetFromMoscow()).toBe(-12);
    setDeviceUtcOffset(16);
    expect(m.detectOffsetFromMoscow()).toBe(12);
  });

  it("detectTimezoneInfo отдаёт IANA-имя и смещение; без Intl — пустое имя", async () => {
    const m = await load();
    setDeviceUtcOffset(1);
    const info = m.detectTimezoneInfo();
    expect(info.offsetFromMoscow).toBe(-2);
    expect(typeof info.ianaName).toBe("string");
    vi.spyOn(Intl, "DateTimeFormat").mockImplementation(() => {
      throw new Error("no intl");
    });
    expect(m.detectTimezoneInfo().ianaName).toBe("");
  });
});

describe("syncDeviceTimezone", () => {
  it("Москва → Белград: сохраняет новый пояс на бэкенд", async () => {
    const m = await load();
    m.resetTimezoneSyncState();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    setDeviceUtcOffset(1);
    await m.syncDeviceTimezone("init", 0);
    expect(calls).toHaveLength(1);
    expect(calls[0].path).toBe("/api/miniapp/profile/save");
    expect(calls[0].body).toEqual({ init_data: "init", timezone_offset: -2 });
    expect(m.getKnownServerOffset()).toBe(-2);
  });

  it("пояс не менялся — сеть не дёргаем", async () => {
    const m = await load();
    m.resetTimezoneSyncState();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    setDeviceUtcOffset(3);
    await m.syncDeviceTimezone("init", 0);
    expect(calls).toHaveLength(0);
    expect(m.getKnownServerOffset()).toBe(0);
  });

  it("без initData или без API — ничего не делает", async () => {
    const m = await load();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    setDeviceUtcOffset(1);
    await m.syncDeviceTimezone("  ", 0);
    expect(calls).toHaveLength(0);
    const noApi = await importWithApi<Mod>(() => import("./timezoneSync"), "");
    await noApi.syncDeviceTimezone("init", 0);
    expect(calls).toHaveLength(0);
    expect(noApi.watchDeviceTimezone("init")).toBeTypeOf("function");
  });

  it("ошибка сети или не-2xx не ломает и не запоминает пояс", async () => {
    const m = await load();
    m.resetTimezoneSyncState();
    setDeviceUtcOffset(1);
    mockFetch(() => ({ fail: true }));
    await expect(m.syncDeviceTimezone("init", 0)).resolves.toBeUndefined();
    expect(m.getKnownServerOffset()).toBe(0);
    mockFetch(() => ({ status: 500, json: { ok: false } }));
    await m.syncDeviceTimezone("init", 0);
    expect(m.getKnownServerOffset()).toBe(0);
  });
});

describe("recheckDeviceTimezone / watchDeviceTimezone", () => {
  it("пока серверный пояс неизвестен — не шлёт", async () => {
    const m = await load();
    m.resetTimezoneSyncState();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    setDeviceUtcOffset(1);
    await m.recheckDeviceTimezone("init");
    expect(calls).toHaveLength(0);
  });

  it("при возврате в приложение после перелёта пересохраняет пояс один раз", async () => {
    const m = await load();
    m.resetTimezoneSyncState();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    setDeviceUtcOffset(3);
    await m.syncDeviceTimezone("init", 0);
    const stop = m.watchDeviceTimezone("init");

    setDeviceUtcOffset(1);
    document.dispatchEvent(new Event("visibilitychange"));
    window.dispatchEvent(new Event("focus")); // параллельный триггер — дубль не уходит
    await vi.waitFor(() => expect(m.getKnownServerOffset()).toBe(-2));
    expect(calls).toHaveLength(1);
    expect(calls[0].body).toEqual({ init_data: "init", timezone_offset: -2 });

    // Повторный возврат в той же зоне — тишина.
    window.dispatchEvent(new Event("focus"));
    await m.recheckDeviceTimezone("init");
    expect(calls).toHaveLength(1);

    stop();
    setDeviceUtcOffset(3);
    window.dispatchEvent(new Event("focus"));
    await Promise.resolve();
    expect(calls).toHaveLength(1);
  });

  it("скрытая вкладка не проверяет; периодическая проверка ловит смену пояса", async () => {
    vi.useFakeTimers();
    const m = await load();
    m.resetTimezoneSyncState();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    setDeviceUtcOffset(3);
    await m.syncDeviceTimezone("init", 0);
    const stop = m.watchDeviceTimezone("init");

    setDeviceUtcOffset(4);
    const vis = vi.spyOn(document, "visibilityState", "get").mockReturnValue("hidden");
    document.dispatchEvent(new Event("visibilitychange"));
    expect(calls).toHaveLength(0);
    vis.mockReturnValue("visible");

    await vi.advanceTimersByTimeAsync(m.TIMEZONE_RECHECK_INTERVAL_MS);
    expect(calls).toHaveLength(1);
    expect(calls[0].body).toEqual({ init_data: "init", timezone_offset: 1 });
    stop();
  });

  it("без initData watch возвращает no-op", async () => {
    const m = await load();
    const stop = m.watchDeviceTimezone("");
    expect(() => stop()).not.toThrow();
  });
});
