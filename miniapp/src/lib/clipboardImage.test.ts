import { describe, expect, it } from "vitest";
import { clipboardImageFile } from "./clipboardImage";

function transfer(items: Array<{ kind: string; type: string; file?: File }>, files: File[] = []): DataTransfer {
  return {
    items: items.map((i) => ({ kind: i.kind, type: i.type, getAsFile: () => i.file ?? null })),
    files,
  } as unknown as DataTransfer;
}

describe("clipboardImageFile", () => {
  it("достаёт картинку из буфера", () => {
    const png = new File(["x"], "shot.png", { type: "image/png" });
    expect(clipboardImageFile(transfer([{ kind: "string", type: "text/plain" }, { kind: "file", type: "image/png", file: png }]))).toBe(png);
  });

  it("текст без картинки — null, вставка текста не перехватывается", () => {
    expect(clipboardImageFile(transfer([{ kind: "string", type: "text/plain" }]))).toBeNull();
    expect(clipboardImageFile(null)).toBeNull();
  });

  it("берёт картинку из files, если items пустые (Safari)", () => {
    const jpg = new File(["x"], "p.jpg", { type: "image/jpeg" });
    expect(clipboardImageFile(transfer([], [jpg]))).toBe(jpg);
  });
});
