/** Отправка текста в личку бота через ms_leo (как ChatScreen). */

import { isModerationError, moderationUserMessage } from "./moderationMessages";
import { getStoredUILang } from "./uiLang";

const apiBase = (import.meta.env.VITE_MINIAPP_API_URL as string | undefined)?.replace(/\/$/, "") ?? "";

export type MiniappSendResult =
  | { ok: true; replyParts: string[]; trainingPhotoUrl?: string }
  | { ok: false; error: string };

export type MiniappSendOptions = {
  /**
   * Если false и сервер вернул pending без reply_text — не ждать ответ через poll
   * (быстрый UI; комментарий Лео появится в ленте позже).
   * По умолчанию true (как в чате: дождаться текста).
   */
  awaitReply?: boolean;
};

async function pollPendingReplies(initData: string): Promise<string[]> {
  const parts: string[] = [];
  const deadline = Date.now() + 4 * 60 * 1000;
  let gotAny = false;
  let idleEmpty = 0;
  while (Date.now() < deadline) {
    await new Promise((r) => setTimeout(r, 1500));
    const pr = await fetch(`${apiBase}/api/miniapp/personal-reply/poll`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData }),
    });
    const pj = (await pr.json().catch(() => ({}))) as { reply_text?: string; error?: string; ok?: boolean };
    if (!pr.ok) {
      throw new Error(pj.error ?? `Poll error ${pr.status}`);
    }
    const chunk = pj.reply_text?.trim();
    if (chunk) {
      gotAny = true;
      idleEmpty = 0;
      parts.push(chunk);
      continue;
    }
    idleEmpty++;
    if (gotAny && idleEmpty >= 2) break;
  }
  if (!gotAny) {
    parts.push("Reply is taking a while. Check your DM with the bot in Telegram.");
  }
  return parts;
}

/**
 * Собирает ответы бота (сразу или через poll), как в ChatScreen.
 */
export async function sendMiniappPrivateText(
  initData: string,
  text: string,
  opts?: MiniappSendOptions,
): Promise<MiniappSendResult> {
  const awaitReply = opts?.awaitReply !== false;
  if (!apiBase) {
    return { ok: false, error: "VITE_MINIAPP_API_URL is not set in the build." };
  }
  if (!initData.trim()) {
    return { ok: false, error: "initData required (open the app from Telegram)." };
  }
  let res: Response;
  try {
    res = await fetch(`${apiBase}/api/miniapp/messages`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ init_data: initData, text }),
    });
  } catch (e) {
    return {
      ok: false,
      error: `Network unavailable: ${e instanceof Error ? e.message : String(e)}`,
    };
  }
  const j = (await res.json().catch(() => ({}))) as {
    error?: string;
    message?: string;
    ok?: boolean;
    pending?: boolean;
    reply_text?: string;
  };
  if (!res.ok) {
    if (isModerationError(j.error)) {
      return { ok: false, error: moderationUserMessage(j.error, j.message, getStoredUILang()) };
    }
    return { ok: false, error: j.error ?? `Error ${res.status}` };
  }
  const replyNow = j.reply_text?.trim();
  if (replyNow) {
    return { ok: true, replyParts: [replyNow] };
  }
  if (j.pending) {
    if (!awaitReply) {
      return { ok: true, replyParts: [] };
    }
    try {
      const parts = await pollPendingReplies(initData);
      return { ok: true, replyParts: parts };
    } catch (e) {
      return { ok: false, error: e instanceof Error ? e.message : String(e) };
    }
  }
  return { ok: true, replyParts: ["Message sent to the bot. Open the bot chat in Telegram if you're waiting for a reply there."] };
}

/** То же для отчёта с фото: POST multipart /api/miniapp/workout. */
export async function sendMiniappTrainingWithPhoto(
  initData: string,
  text: string,
  photo: File,
  opts?: MiniappSendOptions,
): Promise<MiniappSendResult> {
  const awaitReply = opts?.awaitReply !== false;
  if (!apiBase) {
    return { ok: false, error: "VITE_MINIAPP_API_URL is not set in the build." };
  }
  if (!initData.trim()) {
    return { ok: false, error: "initData required (open the app from Telegram)." };
  }
  const fd = new FormData();
  fd.append("init_data", initData);
  fd.append("text", text);
  fd.append("photo", photo, photo.name || "photo.jpg");
  let res: Response;
  try {
    res = await fetch(`${apiBase}/api/miniapp/workout`, {
      method: "POST",
      body: fd,
    });
  } catch (e) {
    return {
      ok: false,
      error: `Network unavailable: ${e instanceof Error ? e.message : String(e)}`,
    };
  }
  const j = (await res.json().catch(() => ({}))) as {
    error?: string;
    message?: string;
    ok?: boolean;
    pending?: boolean;
    reply_text?: string;
    photo_url?: string;
  };
  if (!res.ok) {
    if (res.status === 400 && j.error === "media_not_configured") {
      return {
        ok: false,
        error: "Workout photos aren't configured on the server (MINIAPP_PUBLIC_BASE_URL / media directory).",
      };
    }
    if (res.status === 400 && j.error === "unsupported_image") {
      return { ok: false, error: "Couldn't read the photo. Try JPG, PNG, WEBP, or GIF." };
    }
    if (res.status === 400 && j.error === "photo_too_large") {
      return { ok: false, error: "Photo is too large. Maximum 6 MB." };
    }
    if (res.status === 400 && j.error === "invalid_multipart") {
      return { ok: false, error: "Couldn't upload the photo. Try choosing the image again." };
    }
    if (isModerationError(j.error)) {
      return { ok: false, error: moderationUserMessage(j.error, j.message, getStoredUILang()) };
    }
    return { ok: false, error: j.error ?? `Error ${res.status}` };
  }
  const photoURL = j.photo_url?.trim();
  const replyNow = j.reply_text?.trim();
  if (replyNow) {
    return { ok: true, replyParts: [replyNow], trainingPhotoUrl: photoURL || undefined };
  }
  if (j.pending) {
    if (!awaitReply) {
      return { ok: true, replyParts: [], trainingPhotoUrl: photoURL || undefined };
    }
    try {
      const parts = await pollPendingReplies(initData);
      return { ok: true, replyParts: parts, trainingPhotoUrl: photoURL || undefined };
    } catch (e) {
      return { ok: false, error: e instanceof Error ? e.message : String(e) };
    }
  }
  return {
    ok: true,
    replyParts: ["Message sent to the bot. Open the bot chat in Telegram if you're waiting for a reply there."],
    trainingPhotoUrl: photoURL || undefined,
  };
}
