// @vitest-environment jsdom
import { afterEach, describe, expect, it, vi } from "vitest";
import { importWithApi, mockFetch } from "./testApi";
import {
  achievementShareCard,
  drawShareCard,
  fitFont,
  levelShareCard,
  openShareLink,
  renderShareCard,
  saveShareImage,
  shareCardCaption,
  shareImageViaSystem,
  shareLinkDisplay,
  shareToTelegramStory,
  telegramChatShareUrl,
  uploadShareCard,
  vkShareUrl,
  workoutShareCard,
} from "./shareCard";

afterEach(() => {
  vi.unstubAllGlobals();
  vi.unstubAllEnvs();
  vi.restoreAllMocks();
  delete (window as unknown as { Telegram?: unknown }).Telegram;
});

const setWebApp = (wa: Record<string, unknown>) => {
  (window as unknown as { Telegram: unknown }).Telegram = { WebApp: wa };
};

/** Поддельный 2D-контекст: запоминает текст; ширина текста — 30px на символ. */
function fakeCtx() {
  const texts: string[] = [];
  const grad = { addColorStop: vi.fn() };
  const ctx = {
    fillStyle: "" as unknown,
    font: "",
    textAlign: "left",
    textBaseline: "alphabetic",
    globalAlpha: 1,
    fillRect: vi.fn(),
    fillText: vi.fn((t: string) => texts.push(t)),
    measureText: vi.fn(function (this: { font: string }, t: string) {
      const size = Number(/(\d+)px/.exec(this.font)?.[1] ?? 10);
      return { width: t.length * size * 0.5 } as TextMetrics;
    }),
    drawImage: vi.fn(),
    createLinearGradient: vi.fn(() => grad),
    beginPath: vi.fn(),
    roundRect: vi.fn(),
    fill: vi.fn(),
    save: vi.fn(),
    restore: vi.fn(),
  };
  return { ctx, texts };
}

describe("карточки", () => {
  it("стрик 30 дней", () => {
    const c = achievementShareCard("streak-30", "Аня");
    expect(c).toMatchObject({ kind: "achievement", emoji: "🔥", headline: "Стрик", big: "30", bigCaption: "дней подряд", name: "Аня" });
  });

  it("ачивка за тренировки и неизвестный ключ", () => {
    expect(achievementShareCard("workout-100", " ")).toMatchObject({ big: "100", bigCaption: "тренировок", name: "Участник стаи" });
    expect(achievementShareCard("oops", "Аня")).toMatchObject({ big: "🏆", bigCaption: "Новая ачивка" });
  });

  it("новый уровень", () => {
    expect(levelShareCard(4, "Боря")).toMatchObject({ kind: "level", emoji: "🐆", big: "4", bigCaption: "Гепард" });
  });

  it("тренировка: вид, интенсивность, стрик и фото", () => {
    const photo = new Blob(["x"]);
    const c = workoutShareCard({
      reportLine: "бег, 30 мин, интенсивность 3/5",
      kindLabel: "бег",
      min: 30,
      intensity: 3,
      streak: 5,
      name: "Аня",
      photo,
    });
    expect(c).toMatchObject({ kind: "workout", big: "30", bigCaption: "минут", details: "бег · интенсивность 3/5 · стрик 5 дней", photo });
    expect(c.emoji).not.toBe("");
    const noStreak = workoutShareCard({ reportLine: "что-то", kindLabel: "", min: -1, intensity: 2, streak: 0, name: "Аня" });
    expect(noStreak).toMatchObject({ big: "0", details: "интенсивность 2/5", photo: null });
  });
});

describe("подписи и ссылки", () => {
  const link = "https://t.me/leo_bot?start=ref-42";

  it("короткий вид ссылки на карточке", () => {
    expect(shareLinkDisplay(link)).toBe("t.me/leo_bot");
  });

  it("подпись для каждого вида карточки, со ссылкой и без", () => {
    expect(shareCardCaption(achievementShareCard("streak-30", "Аня"), link)).toBe(
      `Стрик 30 дней подряд в Fat Leopard 🔥\nТренируйся со мной: ${link}`,
    );
    expect(shareCardCaption(achievementShareCard("workout-10", "Аня"), "")).toContain("Ачивка в Fat Leopard: 10 тренировок");
    expect(shareCardCaption(levelShareCard(5, "Аня"), link)).toContain("Новый уровень в Fat Leopard: 5 — Лев");
    const w = workoutShareCard({ reportLine: "бег", kindLabel: "бег", min: 30, intensity: 3, streak: 0, name: "А" });
    expect(shareCardCaption(w, "")).toMatch(/^Тренировка засчитана: 30 минут/);
    expect(shareCardCaption(w, `https://t.me/${"x".repeat(300)}`).length).toBe(200);
  });

  it("ссылки на окна «Поделиться»", () => {
    expect(telegramChatShareUrl("https://cdn/x.jpg", "Привет")).toBe(
      "https://t.me/share/url?url=https%3A%2F%2Fcdn%2Fx.jpg&text=%D0%9F%D1%80%D0%B8%D0%B2%D0%B5%D1%82",
    );
    const vk = new URL(vkShareUrl(link, "https://cdn/x.jpg", "Строка 1\nСтрока 2"));
    expect(vk.origin + vk.pathname).toBe("https://vk.com/share.php");
    expect(vk.searchParams.get("url")).toBe(link);
    expect(vk.searchParams.get("image")).toBe("https://cdn/x.jpg");
    expect(vk.searchParams.get("title")).toBe("Строка 1");
    expect(new URL(vkShareUrl("", "https://cdn/x.jpg", "a")).searchParams.get("url")).toBe("https://cdn/x.jpg");
  });
});

describe("рисование", () => {
  it("fitFont уменьшает кегль под ширину", () => {
    const { ctx } = fakeCtx();
    expect(fitFont(ctx, "abc", 700, 100, 1000)).toBe(100);
    expect(fitFont(ctx, "a".repeat(40), 700, 100, 1000)).toBeLessThan(100);
    expect(fitFont(ctx, "a".repeat(4000), 700, 100, 10)).toBe(24);
    expect(ctx.font).toContain("24px");
  });

  it("на карточке имя, цифра, подпись и ссылка на бота", () => {
    const { ctx, texts } = fakeCtx();
    drawShareCard(ctx as never, achievementShareCard("streak-30", "Аня"), "https://t.me/leo_bot?start=ref-1");
    expect(texts).toEqual(expect.arrayContaining(["30", "дней подряд", "Аня", "t.me/leo_bot", "Стрик"]));
    expect(ctx.drawImage).not.toHaveBeenCalled();
  });

  it("фото тренировки ложится фоном, без ссылки — название бота", () => {
    const { ctx, texts } = fakeCtx();
    const card = workoutShareCard({ reportLine: "бег", kindLabel: "бег", min: 30, intensity: 3, streak: 2, name: "Аня" });
    drawShareCard(ctx as never, card, "", { width: 2000, height: 1000 } as never);
    expect(ctx.drawImage).toHaveBeenCalledTimes(1);
    expect(texts).toEqual(expect.arrayContaining(["Fat Leopard в Telegram", "бег · интенсивность 3/5 · стрик 2 дня"]));
  });

  it("renderShareCard: JPEG с холста, null без 2D-контекста", async () => {
    const { ctx } = fakeCtx();
    const blob = new Blob(["jpg"], { type: "image/jpeg" });
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(ctx as never);
    vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(function (cb: BlobCallback) {
      cb(blob);
    });
    vi.stubGlobal("createImageBitmap", vi.fn(async () => ({ width: 10, height: 10 })));
    const card = workoutShareCard({ reportLine: "бег", kindLabel: "бег", min: 1, intensity: 1, streak: 0, name: "А", photo: new Blob(["p"]) });
    expect(await renderShareCard(card, "")).toBe(blob);
    expect(ctx.drawImage).toHaveBeenCalled();

    vi.stubGlobal("createImageBitmap", vi.fn(async () => Promise.reject(new Error("bad"))));
    expect(await renderShareCard(card, "")).toBe(blob);

    vi.spyOn(HTMLCanvasElement.prototype, "toBlob").mockImplementation(() => {
      throw new Error("tainted");
    });
    expect(await renderShareCard(card, "")).toBeNull();

    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockReturnValue(null);
    expect(await renderShareCard(card, "")).toBeNull();
    vi.spyOn(HTMLCanvasElement.prototype, "getContext").mockImplementation(() => {
      throw new Error("no canvas");
    });
    expect(await renderShareCard(card, "")).toBeNull();
  });
});

describe("загрузка карточки", () => {
  it("без адреса API или initData не ходит в сеть", async () => {
    expect(await uploadShareCard("init", new Blob(["x"]))).toBeNull();
    const m = await importWithApi(() => import("./shareCard"));
    expect(await m.uploadShareCard(" ", new Blob(["x"]))).toBeNull();
  });

  it("отправляет картинку и разбирает ответ", async () => {
    const m = await importWithApi(() => import("./shareCard"));
    const calls = mockFetch(() => ({ json: { ok: true, url: "https://cdn/x.jpg", link: "https://t.me/b?start=ref-1" } }));
    expect(await m.uploadShareCard("init", new Blob(["x"]))).toEqual({ url: "https://cdn/x.jpg", link: "https://t.me/b?start=ref-1" });
    expect(calls[0].path).toBe("/api/miniapp/share/card");
    expect(calls[0].form?.get("init_data")).toBe("init");
    expect(calls[0].form?.get("photo")).toBeInstanceOf(Blob);

    mockFetch(() => ({ json: { ok: true, url: "https://cdn/x.jpg" } }));
    expect(await m.uploadShareCard("init", new Blob(["x"]))).toEqual({ url: "https://cdn/x.jpg", link: "" });
    mockFetch(() => ({ json: { ok: true, url: "http://insecure/x.jpg" } }));
    expect(await m.uploadShareCard("init", new Blob(["x"]))).toBeNull();
    mockFetch(() => ({ status: 400, json: { error: "unsupported_image" } }));
    expect(await m.uploadShareCard("init", new Blob(["x"]))).toBeNull();
    mockFetch(() => ({ status: 500 }));
    expect(await m.uploadShareCard("init", new Blob(["x"]))).toBeNull();
    mockFetch(() => ({ fail: true }));
    expect(await m.uploadShareCard("init", new Blob(["x"]))).toBeNull();
  });
});

describe("публикация", () => {
  it("сторис Telegram", () => {
    expect(shareToTelegramStory("https://cdn/x.jpg", "t")).toBe(false);
    const shareToStory = vi.fn();
    setWebApp({ shareToStory });
    expect(shareToTelegramStory("https://cdn/x.jpg", "t")).toBe(true);
    expect(shareToStory).toHaveBeenCalledWith("https://cdn/x.jpg", { text: "t" });
    setWebApp({
      shareToStory: () => {
        throw new Error("WebAppMethodUnsupported");
      },
    });
    expect(shareToTelegramStory("https://cdn/x.jpg", "t")).toBe(false);
  });

  it("ссылки: t.me внутри Telegram, остальное — во внешнем браузере", () => {
    const open = vi.spyOn(window, "open").mockReturnValue(null);
    openShareLink("https://vk.com/share.php");
    expect(open).toHaveBeenCalledWith("https://vk.com/share.php", "_blank", "noopener");
    const openTelegramLink = vi.fn();
    const openLink = vi.fn();
    setWebApp({ openTelegramLink, openLink });
    openShareLink("https://t.me/share/url?url=x");
    openShareLink("https://vk.com/share.php");
    expect(openTelegramLink).toHaveBeenCalledWith("https://t.me/share/url?url=x");
    expect(openLink).toHaveBeenCalledWith("https://vk.com/share.php");
  });

  it("системное «Поделиться» с файлом", async () => {
    const img = new Blob(["x"], { type: "image/jpeg" });
    vi.stubGlobal("navigator", {});
    expect(await shareImageViaSystem(img, "t")).toBe("unsupported");

    const share = vi.fn(async () => {});
    vi.stubGlobal("navigator", { share, canShare: () => false });
    expect(await shareImageViaSystem(img, "t")).toBe("unsupported");

    vi.stubGlobal("navigator", { share, canShare: () => true });
    expect(await shareImageViaSystem(new Blob(["x"]), "t")).toBe("shared");
    const data = (share.mock.calls[0] as unknown[])[0] as ShareData;
    expect(data.files?.[0].type).toBe("image/jpeg");

    const abort = Object.assign(new Error("cancel"), { name: "AbortError" });
    vi.stubGlobal("navigator", { share: vi.fn(async () => Promise.reject(abort)) });
    expect(await shareImageViaSystem(img, "t")).toBe("cancelled");
    vi.stubGlobal("navigator", { share: vi.fn(async () => Promise.reject(new Error("NotAllowed"))) });
    expect(await shareImageViaSystem(img, "t")).toBe("unsupported");
  });

  it("сохранение картинки", () => {
    const downloadFile = vi.fn();
    setWebApp({ downloadFile });
    saveShareImage("https://cdn/x.jpg");
    expect(downloadFile).toHaveBeenCalledWith({ url: "https://cdn/x.jpg", file_name: "fat-leopard.jpg" });

    const openLink = vi.fn();
    setWebApp({
      openLink,
      downloadFile: () => {
        throw new Error("old client");
      },
    });
    saveShareImage("https://cdn/x.jpg");
    expect(openLink).toHaveBeenCalledWith("https://cdn/x.jpg");
  });
});
