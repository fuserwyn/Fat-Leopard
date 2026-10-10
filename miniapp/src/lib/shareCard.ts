/**
 * Карточки «поделиться»: картинка 1080×1920 (формат сторис) с именем, цифрой и ссылкой
 * на бота — за ачивку, новый уровень или засчитанную тренировку. Мини-апп рисует её сам
 * на canvas, а публикует так:
 *  - сторис Telegram — WebApp.shareToStory (нужен публичный https-адрес картинки,
 *    поэтому картинка сначала грузится на сервер: POST /api/miniapp/share/card);
 *  - чат Telegram — окно t.me/share/url со ссылкой на картинку и на бота;
 *  - ВКонтакте — vk.com/share.php;
 *  - Instagram и TikTok — системное «Поделиться» с файлом (navigator.share), а если
 *    WebView его не умеет — картинка сохраняется, и её можно выложить из галереи.
 */
import { achievementLabel, parseAchievementKey, type AchievementKey } from "./achievements";
import { miniappLevelEmoji, miniappLevelName } from "./miniappLevel";
import { daysWordRu } from "./streakLabel";
import { trainingDoneCategoryEmoji } from "./workoutCategories";

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export const SHARE_CARD_WIDTH = 1080;
export const SHARE_CARD_HEIGHT = 1920;

export type ShareCardKind = "achievement" | "level" | "workout";

export type ShareCard = {
  kind: ShareCardKind;
  /** Большой эмодзи сверху. */
  emoji: string;
  /** Заголовок над цифрой: «Стрик», «Новый уровень», «Тренировка засчитана». */
  headline: string;
  /** Главная цифра карточки. */
  big: string;
  /** Подпись под цифрой: «дней подряд», «Гепард», «минут». */
  bigCaption: string;
  /** Необязательная строка деталей: вид спорта, интенсивность. */
  details?: string;
  /** Крупная плашка под деталями — стрик на карточке тренировки: «🔥 Стрик 13 дней». */
  badge?: string;
  /** Имя участника. */
  name: string;
  /** Фото тренировки — фоном карточки. */
  photo?: Blob | null;
};

const cleanName = (name: string) => name.trim() || "Участник стаи";

/** Карточка ачивки: «Стрик 30 дней» или «100 тренировок». */
export function achievementShareCard(key: AchievementKey, name: string): ShareCard {
  const parsed = parseAchievementKey(key);
  if (!parsed) {
    return { kind: "achievement", emoji: "🏅", headline: "Ачивка", big: "🏆", bigCaption: achievementLabel(key), name: cleanName(name) };
  }
  if (parsed.kind === "streak") {
    return {
      kind: "achievement",
      emoji: "🔥",
      headline: "Стрик",
      big: String(parsed.threshold),
      bigCaption: `${daysWordRu(parsed.threshold)} подряд`,
      name: cleanName(name),
    };
  }
  const label = achievementLabel(key);
  return {
    kind: "achievement",
    emoji: "💪",
    headline: "Ачивка получена",
    big: String(parsed.threshold),
    bigCaption: label.slice(String(parsed.threshold).length).trim(),
    name: cleanName(name),
  };
}

/** Карточка нового уровня: номер и зверь уровня. */
export function levelShareCard(level: number, name: string): ShareCard {
  return {
    kind: "level",
    emoji: miniappLevelEmoji(level),
    headline: "Новый уровень",
    big: String(level),
    bigCaption: miniappLevelName(level),
    name: cleanName(name),
  };
}

export type WorkoutShareInput = {
  /** Первая строка отчёта: «бег + йога, 30 мин, интенсивность 3/5». */
  reportLine: string;
  /** Вид спорта как в отчёте: «бег + йога». */
  kindLabel: string;
  min: number;
  intensity: number;
  streak: number;
  name: string;
  photo?: Blob | null;
};

/** Карточка засчитанной тренировки: минуты, вид спорта, интенсивность и стрик. */
export function workoutShareCard(w: WorkoutShareInput): ShareCard {
  const parts = [w.kindLabel.trim(), `интенсивность ${w.intensity}/5`].filter(Boolean);
  return {
    kind: "workout",
    emoji: trainingDoneCategoryEmoji(w.reportLine) || "💪",
    headline: "Тренировка засчитана",
    big: String(Math.max(0, Math.round(w.min))),
    bigCaption: "минут",
    details: parts.join(" · "),
    // Стрик — второе, чем хвастаются после самой тренировки: выносим его
    // отдельной крупной плашкой, а не прячем в мелкую строку деталей.
    badge: w.streak > 0 ? `🔥 Стрик ${w.streak} ${daysWordRu(w.streak)}` : undefined,
    name: cleanName(w.name),
    photo: w.photo ?? null,
  };
}

/** Короткий вид ссылки для картинки: «t.me/leo_bot» без https и параметров. */
export function shareLinkDisplay(link: string): string {
  return link.replace(/^https?:\/\//, "").replace(/[?#].*$/, "");
}

/** Подпись к сторис и сообщению (Telegram ограничивает подпись сторис 200 символами). */
export function shareCardCaption(card: ShareCard, link: string): string {
  const what =
    card.kind === "level"
      ? `Новый уровень в Fat Leopard: ${card.big} — ${card.bigCaption} ${card.emoji}`
      : card.kind === "workout"
        ? `Тренировка засчитана: ${card.big} ${card.bigCaption} ${card.emoji}${card.badge ? ` · ${card.badge.replace(/^🔥\s*/, "").toLowerCase()} 🔥` : ""}`
        : card.headline === "Стрик"
          ? `Стрик ${card.big} ${card.bigCaption} в Fat Leopard 🔥`
          : `Ачивка в Fat Leopard: ${card.big} ${card.bigCaption} 💪`;
  const tail = link ? `\nТренируйся со мной: ${link}` : "\nТренируйся со мной в Fat Leopard 🐆";
  const text = what + tail;
  return text.length > 200 ? `${text.slice(0, 199)}…` : text;
}

type Ctx = Pick<
  CanvasRenderingContext2D,
  | "fillRect"
  | "fillText"
  | "measureText"
  | "drawImage"
  | "createLinearGradient"
  | "beginPath"
  | "roundRect"
  | "fill"
  | "save"
  | "restore"
> & {
  fillStyle: CanvasRenderingContext2D["fillStyle"];
  font: string;
  textAlign: CanvasTextAlign;
  textBaseline: CanvasTextBaseline;
  globalAlpha: number;
};

const FONT = `-apple-system, "SF Pro Display", "Segoe UI", Roboto, system-ui, sans-serif`;

/** Уменьшает кегль, пока текст не влезет в ширину. */
export function fitFont(ctx: Pick<Ctx, "measureText" | "font">, text: string, weight: number, size: number, maxWidth: number): number {
  let s = size;
  for (; s > 24; s -= 4) {
    ctx.font = `${weight} ${s}px ${FONT}`;
    if (ctx.measureText(text).width <= maxWidth) return s;
  }
  ctx.font = `${weight} ${s}px ${FONT}`;
  return s;
}

/** Рисует фон «под обложку»: картинка заполняет холст без искажений. */
function drawCover(ctx: Ctx, img: CanvasImageSource & { width: number; height: number }) {
  const W = SHARE_CARD_WIDTH;
  const H = SHARE_CARD_HEIGHT;
  const scale = Math.max(W / img.width, H / img.height);
  const w = img.width * scale;
  const h = img.height * scale;
  ctx.drawImage(img, (W - w) / 2, (H - h) / 2, w, h);
}

/** Рисует карточку на 2D-контексте холста 1080×1920. */
export function drawShareCard(
  ctx: Ctx,
  card: ShareCard,
  link: string,
  background?: (CanvasImageSource & { width: number; height: number }) | null,
): void {
  const W = SHARE_CARD_WIDTH;
  const H = SHARE_CARD_HEIGHT;
  const cx = W / 2;
  const maxW = W - 160;

  const bg = ctx.createLinearGradient(0, 0, 0, H);
  bg.addColorStop(0, "#2a1606");
  bg.addColorStop(0.55, "#120a04");
  bg.addColorStop(1, "#000000");
  ctx.fillStyle = bg;
  ctx.fillRect(0, 0, W, H);
  if (background && background.width > 0 && background.height > 0) {
    drawCover(ctx, background);
    ctx.fillStyle = "rgba(0, 0, 0, 0.58)";
    ctx.fillRect(0, 0, W, H);
  }

  ctx.textAlign = "center";
  ctx.textBaseline = "alphabetic";

  ctx.fillStyle = "#ffb347";
  ctx.font = `800 52px ${FONT}`;
  ctx.fillText("FAT LEOPARD 🐆", cx, 210);

  ctx.font = `200px ${FONT}`;
  ctx.fillText(card.emoji, cx, 600);

  ctx.fillStyle = "#ffffff";
  fitFont(ctx, card.headline, 700, 76, maxW);
  ctx.fillText(card.headline, cx, 790);

  const gold = ctx.createLinearGradient(0, 860, 0, 1160);
  gold.addColorStop(0, "#ffe08a");
  gold.addColorStop(1, "#ff8a00");
  ctx.fillStyle = gold;
  fitFont(ctx, card.big, 900, 340, maxW);
  ctx.fillText(card.big, cx, 1130);

  ctx.fillStyle = "#ffffff";
  fitFont(ctx, card.bigCaption, 700, 80, maxW);
  ctx.fillText(card.bigCaption, cx, 1260);

  if (card.details) {
    ctx.fillStyle = "rgba(255, 255, 255, 0.75)";
    fitFont(ctx, card.details, 500, 46, maxW);
    ctx.fillText(card.details, cx, 1350);
  }

  // Плашка стрика: золотая рамка и крупный текст между деталями и именем.
  let nameY = 1560;
  if (card.badge) {
    const size = fitFont(ctx, card.badge, 800, 84, maxW - 120);
    const badgeW = Math.min(maxW, ctx.measureText(card.badge).width + 120);
    ctx.save();
    ctx.fillStyle = "rgba(255, 179, 71, 0.16)";
    ctx.beginPath();
    ctx.roundRect(cx - badgeW / 2, 1392, badgeW, 136, 68);
    ctx.fill();
    ctx.restore();
    ctx.fillStyle = "#ffb347";
    ctx.font = `800 ${size}px ${FONT}`;
    ctx.fillText(card.badge, cx, 1490);
    nameY = 1620;
  }

  ctx.fillStyle = "#ffffff";
  fitFont(ctx, card.name, 800, 72, maxW);
  ctx.fillText(card.name, cx, nameY);

  const shown = link ? shareLinkDisplay(link) : "Fat Leopard в Telegram";
  ctx.font = `700 44px ${FONT}`;
  const pillW = Math.min(maxW, ctx.measureText(shown).width + 100);
  ctx.save();
  ctx.fillStyle = "#ff8a00";
  ctx.beginPath();
  ctx.roundRect(cx - pillW / 2, 1680, pillW, 96, 48);
  ctx.fill();
  ctx.restore();
  ctx.fillStyle = "#1a0e04";
  ctx.fillText(shown, cx, 1744);

  ctx.fillStyle = "rgba(255, 255, 255, 0.7)";
  ctx.font = `500 38px ${FONT}`;
  ctx.fillText("Тренируйся со мной в стае", cx, 1850);
}

/** Фото тренировки → картинка для холста; null, если браузер не умеет её открыть. */
async function loadBackground(photo: Blob | null | undefined) {
  if (!photo || typeof createImageBitmap !== "function") return null;
  try {
    return await createImageBitmap(photo);
  } catch {
    return null;
  }
}

/** Рисует карточку и отдаёт JPEG; null — холст недоступен (старый WebView, тесты). */
export async function renderShareCard(card: ShareCard, link: string): Promise<Blob | null> {
  if (typeof document === "undefined") return null;
  const canvas = document.createElement("canvas");
  canvas.width = SHARE_CARD_WIDTH;
  canvas.height = SHARE_CARD_HEIGHT;
  let ctx: CanvasRenderingContext2D | null = null;
  try {
    ctx = canvas.getContext("2d");
  } catch {
    ctx = null;
  }
  if (!ctx) return null;
  const background = await loadBackground(card.photo);
  drawShareCard(ctx, card, link, background);
  return new Promise((resolve) => {
    try {
      canvas.toBlob((b) => resolve(b), "image/jpeg", 0.9);
    } catch {
      resolve(null);
    }
  });
}

export type UploadedShareCard = { url: string; link: string };

/** Кладёт картинку на сервер: нужна публичная ссылка для сторис и превью в чате. */
export async function uploadShareCard(initData: string, image: Blob): Promise<UploadedShareCard | null> {
  if (!apiBase || !initData.trim()) return null;
  const fd = new FormData();
  fd.append("init_data", initData);
  fd.append("photo", image, "fat-leopard-card.jpg");
  try {
    const res = await fetch(`${apiBase}/api/miniapp/share/card`, { method: "POST", body: fd });
    const j = (await res.json().catch(() => ({}))) as { ok?: boolean; url?: unknown; link?: unknown };
    if (!res.ok || !j.ok || typeof j.url !== "string" || !/^https:\/\//.test(j.url)) return null;
    return { url: j.url, link: typeof j.link === "string" ? j.link : "" };
  } catch {
    return null;
  }
}

/** Сторис Telegram. false — клиент не умеет (старый Telegram или не мобильный). */
export function shareToTelegramStory(imageUrl: string, caption: string): boolean {
  const wa = window.Telegram?.WebApp;
  if (!wa?.shareToStory) return false;
  try {
    wa.shareToStory(imageUrl, { text: caption });
    return true;
  } catch {
    return false;
  }
}

/** Окно Telegram «Поделиться» в чат: превью картинки + подпись со ссылкой на бота. */
export function telegramChatShareUrl(imageUrl: string, caption: string): string {
  return `https://t.me/share/url?url=${encodeURIComponent(imageUrl)}&text=${encodeURIComponent(caption)}`;
}

/** Публикация во ВКонтакте: ссылка на бота с картинкой карточки. */
export function vkShareUrl(link: string, imageUrl: string, caption: string): string {
  const q = new URLSearchParams({ url: link || imageUrl, image: imageUrl, title: caption.split("\n")[0] ?? caption });
  return `https://vk.com/share.php?${q.toString()}`;
}

/** Открывает ссылку: t.me — внутри Telegram, остальное — во внешнем браузере. */
export function openShareLink(url: string): void {
  const wa = window.Telegram?.WebApp;
  if (url.startsWith("https://t.me/") && wa?.openTelegramLink) {
    wa.openTelegramLink(url);
    return;
  }
  if (wa?.openLink) {
    wa.openLink(url);
    return;
  }
  window.open(url, "_blank", "noopener");
}

export type SystemShareResult = "shared" | "cancelled" | "unsupported";

/** Системное «Поделиться» с файлом — так картинка попадает в Instagram, TikTok, VK. */
export async function shareImageViaSystem(image: Blob, caption: string): Promise<SystemShareResult> {
  const nav = navigator as Navigator & { canShare?: (d: ShareData) => boolean };
  if (typeof nav.share !== "function" || typeof File !== "function") return "unsupported";
  const file = new File([image], "fat-leopard.jpg", { type: image.type || "image/jpeg" });
  const data: ShareData = { files: [file], text: caption };
  if (typeof nav.canShare === "function" && !nav.canShare(data)) return "unsupported";
  try {
    await nav.share(data);
    return "shared";
  } catch (e) {
    return e instanceof Error && e.name === "AbortError" ? "cancelled" : "unsupported";
  }
}

/** Сохраняет картинку на устройство: Telegram downloadFile, иначе — открыть её в браузере. */
export function saveShareImage(imageUrl: string): void {
  const wa = window.Telegram?.WebApp;
  if (wa?.downloadFile) {
    try {
      wa.downloadFile({ url: imageUrl, file_name: "fat-leopard.jpg" });
      return;
    } catch {
      /* старый клиент — откроем картинку ссылкой */
    }
  }
  openShareLink(imageUrl);
}
