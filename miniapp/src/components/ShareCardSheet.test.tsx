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
const openShareApp = vi.fn();

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
    openShareApp: (...a: unknown[]) => openShareApp(...a),
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
  saveShareImage.mockImplementation((_url: string, onDone?: () => void) => {
    onDone?.();
    return true;
  });
  URL.createObjectURL = vi.fn(() => "blob:card");
  URL.revokeObjectURL = vi.fn();
});

afterEach(() => {
  cleanup();
  vi.clearAllMocks();
});

async function open(onClose = vi.fn(), showAlert = vi.fn()) {
  const utils = render(<ShareCardSheet card={card} initData="init" showAlert={showAlert} onClose={onClose} />);
  await waitFor(() => expect((utils.getByText("Telegram Story").closest("button") as HTMLButtonElement).disabled).toBe(false));
  return { ...utils, onClose, showAlert };
}

const click = async (el: HTMLElement) => {
  await act(async () => {
    fireEvent.click(el);
  });
};

describe("ShareCardSheet", () => {
  it("renders card with bot link and starts upload", async () => {
    const { container } = await open();
    expect(renderShareCard).toHaveBeenCalledWith(card, LINK);
    expect(uploadShareCard).toHaveBeenCalledWith("init", image);
    expect(container.querySelector("img")?.getAttribute("src")).toBe("blob:card");
  });

  it("Telegram Story with caption and link", async () => {
    const { getByText } = await open();
    await click(getByText("Telegram Story"));
    expect(shareToTelegramStory).toHaveBeenCalledWith(IMG, expect.stringContaining(LINK));
    expect(uploadShareCard).toHaveBeenCalledTimes(1);
  });

  it("legacy Telegram without stories — save image and hint", async () => {
    shareToTelegramStory.mockReturnValue(false);
    const { getByText, showAlert } = await open();
    await click(getByText("Telegram Story"));
    expect(saveShareImage).toHaveBeenCalledWith(IMG);
    expect(showAlert).toHaveBeenCalledWith(expect.stringContaining("Save the image"));
  });

  it("Telegram chat share dialog; VK opens app", async () => {
    const { getByText } = await open();
    await click(getByText("Telegram chat"));
    expect(openShareLink).toHaveBeenLastCalledWith(expect.stringMatching(/^https:\/\/t\.me\/share\/url\?url=https%3A%2F%2Fcdn\.test/));
    await click(getByText("VK"));
    expect(openShareApp).toHaveBeenLastCalledWith("vk", expect.stringMatching(/^https:\/\/vk\.com\/share\.php\?/));
  });

  it("Instagram opens app immediately without waiting for upload", async () => {
    let finishUpload: (v: unknown) => void = () => {};
    const { getByText, showAlert } = await open();
    uploadShareCard.mockReturnValue(new Promise((r) => (finishUpload = r)));
    await click(getByText("Instagram"));
    expect(openShareApp).toHaveBeenCalledWith("instagram");
    expect(saveShareImage).not.toHaveBeenCalled();
    expect(shareImageViaSystem).not.toHaveBeenCalled();
    expect(showAlert).not.toHaveBeenCalled();
    finishUpload(null);
  });

  it("TikTok opens app immediately", async () => {
    uploadShareCard.mockResolvedValue(null);
    const { getByText, showAlert } = await open();
    await click(getByText("TikTok"));
    expect(openShareApp).toHaveBeenCalledWith("tiktok");
    expect(saveShareImage).not.toHaveBeenCalled();
    expect(showAlert).not.toHaveBeenCalled();
  });

  it("Save image via Telegram", async () => {
    const { getByText, showAlert } = await open();
    await click(getByText("Save image"));
    expect(saveShareImage).toHaveBeenCalledWith(IMG);
    expect(openShareApp).not.toHaveBeenCalled();
    expect(showAlert).not.toHaveBeenCalled();
  });

  it("Save image without downloadFile — opens link and hints", async () => {
    saveShareImage.mockReturnValue(false);
    const { getByText, showAlert } = await open();
    await click(getByText("Save image"));
    expect(showAlert).toHaveBeenCalledWith(expect.stringContaining("long press"));
  });

  it("Save image without upload — system share", async () => {
    uploadShareCard.mockResolvedValue(null);
    const { getByText, showAlert } = await open();
    await click(getByText("Save image"));
    expect(saveShareImage).not.toHaveBeenCalled();
    expect(shareImageViaSystem).toHaveBeenCalledWith(image, expect.any(String));
    expect(showAlert).not.toHaveBeenCalled();
  });

  it("failed upload retries then shows error", async () => {
    uploadShareCard.mockResolvedValue(null);
    shareImageViaSystem.mockResolvedValue("unsupported");
    const { getByText, showAlert } = await open();
    await click(getByText("Telegram Story"));
    expect(uploadShareCard).toHaveBeenCalledTimes(2);
    expect(showAlert).toHaveBeenLastCalledWith(expect.stringContaining("Couldn't prepare"));
    await click(getByText("Save image"));
    expect(showAlert).toHaveBeenCalledTimes(2);
    await click(getByText("Telegram chat"));
    expect(openShareLink).toHaveBeenCalledWith(expect.stringContaining(encodeURIComponent(LINK)));
  });

  it("without canvas shows markup fallback and uses server link", async () => {
    renderShareCard.mockResolvedValue(null);
    fetchReferralState.mockResolvedValue(null);
    const { getByText, showAlert, container } = await open();
    expect(container.querySelector("img")).toBeNull();
    expect(getByText("30")).toBeTruthy();
    await click(getByText("Telegram chat"));
    expect(showAlert).toHaveBeenCalledWith(expect.stringContaining("Couldn't prepare"));
  });

  it("closes via button, Escape, and overlay tap", async () => {
    const { getByText, onClose, container } = await open();
    fireEvent.click(getByText("Close"));
    fireEvent.keyDown(window, { key: "Escape" });
    fireEvent.click(container.querySelector(".share-card-overlay")!);
    fireEvent.click(container.querySelector(".share-card-sheet")!);
    expect(onClose).toHaveBeenCalledTimes(3);
  });
});
