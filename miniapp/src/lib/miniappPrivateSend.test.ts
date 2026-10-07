import { afterEach, describe, expect, it, vi } from "vitest";
import { importWithApi, mockFetch } from "./testApi";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.useRealTimers();
});

const load = () => importWithApi(() => import("./miniappPrivateSend"));
const REPORT = "бег, 30 мин, интенсивность 3/5";

describe("sendMiniappPrivateText", () => {
  it("sends the workout report and returns the bot reply", async () => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true, reply_text: " ✅ Отчёт принят! " } }));
    expect(await api.sendMiniappPrivateText("init", REPORT)).toEqual({ ok: true, replyParts: ["✅ Отчёт принят!"] });
    expect(calls[0]).toMatchObject({ path: "/api/miniapp/messages", method: "POST", body: { init_data: "init", text: REPORT } });
  });

  it("refuses to send without init data or API address", async () => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true } }));
    expect(await api.sendMiniappPrivateText("  ", REPORT)).toMatchObject({ ok: false, error: expect.stringContaining("initData") });
    expect(calls).toHaveLength(0);
    const noApi = await importWithApi(() => import("./miniappPrivateSend"), "");
    expect(await noApi.sendMiniappPrivateText("init", REPORT)).toMatchObject({ ok: false, error: expect.stringContaining("VITE_MINIAPP_API_URL") });
  });

  it("reports network, server and moderation failures in words", async () => {
    const api = await load();
    mockFetch(() => ({ fail: true }));
    expect(await api.sendMiniappPrivateText("init", REPORT)).toMatchObject({ ok: false, error: expect.stringContaining("Сеть недоступна") });
    mockFetch(() => ({ status: 500, json: {} }));
    expect(await api.sendMiniappPrivateText("init", REPORT)).toEqual({ ok: false, error: "Ошибка 500" });
    mockFetch(() => ({ status: 409, json: { error: "chat_mismatch" } }));
    expect(await api.sendMiniappPrivateText("init", REPORT)).toEqual({ ok: false, error: "chat_mismatch" });
    mockFetch(() => ({ status: 422, json: { error: "moderation_blocked", message: "Текст нарушает правила" } }));
    const blocked = await api.sendMiniappPrivateText("init", "плохой текст");
    expect(blocked.ok).toBe(false);
    expect(blocked.ok === false && blocked.error).not.toBe("moderation_blocked");
  });

  it("with a pending reply either returns at once or polls for Leo's answer", async () => {
    const api = await load();
    mockFetch(() => ({ json: { ok: true, pending: true } }));
    expect(await api.sendMiniappPrivateText("init", REPORT, { awaitReply: false })).toEqual({ ok: true, replyParts: [] });

    vi.useFakeTimers();
    const replies = ["", "Отличная пробежка!", "Так держать", "", ""];
    const calls = mockFetch((s) =>
      s.path.endsWith("/messages") ? { json: { ok: true, pending: true } } : { json: { ok: true, reply_text: replies.shift() ?? "" } },
    );
    const waiting = api.sendMiniappPrivateText("init", REPORT);
    await vi.advanceTimersByTimeAsync(1500 * 6);
    expect(await waiting).toEqual({ ok: true, replyParts: ["Отличная пробежка!", "Так держать"] });
    expect(calls.filter((c) => c.path.endsWith("/personal-reply/poll"))).toHaveLength(5);

    // Опрос упал — пользователю ошибка, а не вечное ожидание.
    mockFetch((s) => (s.path.endsWith("/messages") ? { json: { ok: true, pending: true } } : { status: 401, json: { error: "invalid_init_data" } }));
    const failing = api.sendMiniappPrivateText("init", REPORT);
    await vi.advanceTimersByTimeAsync(2000);
    expect(await failing).toEqual({ ok: false, error: "invalid_init_data" });
  });

  it("says where to look when the server accepted the message without a reply", async () => {
    const api = await load();
    mockFetch(() => ({ json: { ok: true } }));
    const res = await api.sendMiniappPrivateText("init", "привет");
    expect(res.ok && res.replyParts[0]).toContain("Сообщение ушло боту");
  });
});

describe("sendMiniappTrainingWithPhoto", () => {
  const photo = new File([new Uint8Array([1, 2, 3])], "run.jpg", { type: "image/jpeg" });

  it("uploads the photo with the report and returns its public address", async () => {
    const api = await load();
    const calls = mockFetch(() => ({ json: { ok: true, reply_text: "✅ Отчёт принят!", photo_url: " https://cdn.test/run.jpg " } }));
    expect(await api.sendMiniappTrainingWithPhoto("init", REPORT, photo)).toEqual({
      ok: true,
      replyParts: ["✅ Отчёт принят!"],
      trainingPhotoUrl: "https://cdn.test/run.jpg",
    });
    expect(calls[0].path).toBe("/api/miniapp/workout");
    expect(calls[0].form?.get("init_data")).toBe("init");
    expect(calls[0].form?.get("text")).toBe(REPORT);
    expect((calls[0].form?.get("photo") as File).name).toBe("run.jpg");
  });

  it.each([
    ["media_not_configured", "на сервере не настроено"],
    ["unsupported_image", "Не удалось прочитать фото"],
    ["photo_too_large", "Максимум 6 МБ"],
    ["invalid_multipart", "выбрать снимок заново"],
  ])("explains %s to the user", async (code, text) => {
    const api = await load();
    mockFetch(() => ({ status: 400, json: { error: code } }));
    expect(await api.sendMiniappTrainingWithPhoto("init", REPORT, photo)).toMatchObject({ ok: false, error: expect.stringContaining(text) });
  });

  it("handles pending replies, network errors and missing init data", async () => {
    const api = await load();
    mockFetch(() => ({ json: { ok: true, pending: true, photo_url: "https://cdn.test/a.jpg" } }));
    expect(await api.sendMiniappTrainingWithPhoto("init", REPORT, photo, { awaitReply: false })).toEqual({
      ok: true,
      replyParts: [],
      trainingPhotoUrl: "https://cdn.test/a.jpg",
    });
    mockFetch(() => ({ fail: true }));
    expect(await api.sendMiniappTrainingWithPhoto("init", REPORT, photo)).toMatchObject({ ok: false, error: expect.stringContaining("Сеть недоступна") });
    expect(await api.sendMiniappTrainingWithPhoto("", REPORT, photo)).toMatchObject({ ok: false });
    mockFetch(() => ({ status: 503, json: {} }));
    expect(await api.sendMiniappTrainingWithPhoto("init", REPORT, photo)).toEqual({ ok: false, error: "Ошибка 503" });
  });
});
