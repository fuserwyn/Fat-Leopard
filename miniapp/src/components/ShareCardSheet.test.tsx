// @vitest-environment jsdom
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { act, cleanup, fireEvent, render, waitFor } from "@testing-library/react";
import { achievementShareCard } from "../lib/shareCard";

const fetchReferralState = vi.fn();
const renderShareCard = vi.fn();
const uploadShareCard = vi.fn();
const shareToTelegramStory = vi.fn();
const shareImageViaSystem = vi.fn();
const saveShareImage = vi.fn();
const openShareLink = vi.fn();

vi.mock("../lib/referral", () => ({ fetchReferralState: (...a: unknown[]) => fetchReferralState(...a) }));
vi.mock("../lib/shareCard", async (importOriginal) => {
  const orig = await importOriginal<typeof import("../lib/shareCard")>();
  return {
    ...orig,
    renderShareCard: (...a: unknown[]) => renderShareCard(...a),
    uploadShareCard: (...a: unknown[]) => uploadShareCard(...a),
    shareToTelegramStory: (...a: unknown[]) => shareToTelegramStory(...a),
    shareImageViaSystem: (...a: unknown[]) => shareImageViaSystem(...a),
    saveShareImage: (...a: unknown[]) => saveShareImage(...a),
    openShareLink: (...a: unknown[]) => openShareLink(...a),
  };
});

const { ShareCardSheet } = await import("./ShareCardSheet");

const LINK = "https://t.me/leo_bot?start=ref-1";
const IMG = "https://cdn.test/card.jpg";
const card = achievementShareCard("streak-30", "Аня");
const image = new Blob(["jpg"], { type: "image/jpeg" });

beforeEach(() => {
  fetchReferralState.mockResolvedValue({ link: LINK });
  renderShareCard.mockResolvedValue(image);
  uploadShareCard.mockResolvedValue({ url: IMG, link: LINK });
  shareToTelegramStory.mockReturnValue(true);
  shareImageViaSystem.mockResolvedValue("shared");
  URL.createObjectURL = vi.fn(() => "blob:card");
  URL.revokeObjectURL = vi.fn();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

async function open(onClose = vi.fn(), showAlert = vi.fn()) {
  const utils = render(<ShareCardSheet card={card} initData="init" showAlert={showAlert} onClose={onClose} />);
  await waitFor(() => expect((utils.getByText("Сторис Telegram").closest("button") as HTMLButtonElement).disabled).toBe(false));
  return { ...utils, onClose, showAlert };
}

const click = async (el: HTMLElement) => {
  await act(async () => {
    fireEvent.click(el);
  });
};

describe("ShareCardSheet", () => {
  it("рисует карточку со ссылкой на бота и сразу грузит её", async () => {
    const { container } = await open();
    expect(renderShareCard).toHaveBeenCalledWith(card, LINK);
    expect(uploadShareCard).toHaveBeenCalledWith("init", image);
    expect(container.querySelector("img")?.getAttribute("src")).toBe("blob:card");
  });

  it("сторис Telegram с подписью и ссылкой", async () => {
    const { getByText } = await open();
    await click(getByText("Сторис Telegram"));
    expect(shareToTelegramStory).toHaveBeenCalledWith(IMG, expect.stringContaining(LINK));
    expect(uploadShareCard).toHaveBeenCalledTimes(1);
  });

  it("старый Telegram без сторис — сохраняет картинку и подсказывает", async () => {
    shareToTelegramStory.mockReturnValue(false);
    const { getByText, showAlert } = await open();
    await click(getByText("Сторис Telegram"));
    expect(saveShareImage).toHaveBeenCalledWith(IMG);
    expect(showAlert).toHaveBeenCalledWith(expect.stringContaining("Сохрани картинку"));
  });

  it("чат Telegram и ВКонтакте открывают окна «Поделиться»", async () => {
    const { getByText } = await open();
    await click(getByText("В чат Telegram"));
    expect(openShareLink).toHaveBeenLastCalledWith(expect.stringMatching(/^https:\/\/t\.me\/share\/url\?url=https%3A%2F%2Fcdn\.test/));
    await click(getByText("ВКонтакте"));
    expect(openShareLink).toHaveBeenLastCalledWith(expect.stringMatching(/^https:\/\/vk\.com\/share\.php\?/));
  });

  it("Instagram — системное «Поделиться» с файлом", async () => {
    const { getByText, showAlert } = await open();
    await click(getByText("Instagram"));
    expect(shareImageViaSystem).toHaveBeenCalledWith(image, expect.any(String));
    expect(showAlert).not.toHaveBeenCalled();
  });

  it("TikTok без системного «Поделиться» — сохраняет картинку", async () => {
    shareImageViaSystem.mockResolvedValue("unsupported");
    const { getByText, showAlert } = await open();
    await click(getByText("TikTok"));
    expect(saveShareImage).toHaveBeenCalledWith(IMG);
    expect(showAlert).toHaveBeenCalledWith(expect.stringContaining("TikTok"));
  });

  it("загрузка не удалась — повторяет по нажатию, потом сообщает об ошибке", async () => {
    uploadShareCard.mockResolvedValue(null);
    shareImageViaSystem.mockResolvedValue("unsupported");
    const { getByText, showAlert } = await open();
    await click(getByText("Сторис Telegram"));
    expect(uploadShareCard).toHaveBeenCalledTimes(2);
    expect(showAlert).toHaveBeenLastCalledWith(expect.stringContaining("Не получилось"));
    await click(getByText("Instagram"));
    expect(showAlert).toHaveBeenCalledTimes(2);
    // В чат без картинки всё равно можно позвать по ссылке на бота.
    await click(getByText("В чат Telegram"));
    expect(openShareLink).toHaveBeenCalledWith(expect.stringContaining(encodeURIComponent(LINK)));
  });

  it("без холста показывает карточку разметкой и берёт ссылку из ответа сервера", async () => {
    renderShareCard.mockResolvedValue(null);
    fetchReferralState.mockResolvedValue(null);
    const { getByText, showAlert, container } = await open();
    expect(container.querySelector("img")).toBeNull();
    expect(getByText("30")).toBeTruthy();
    await click(getByText("В чат Telegram"));
    expect(showAlert).toHaveBeenCalledWith(expect.stringContaining("Не получилось"));
  });

  it("закрывается кнопкой, Escape и тапом по подложке", async () => {
    const { getByText, onClose, container } = await open();
    fireEvent.click(getByText("Закрыть"));
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.click(container.querySelector(".share-card-overlay")!);
    fireEvent.click(container.querySelector(".share-card-sheet")!);
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});
