/** Ужимает фото тренировки для R2 / ленты (как в PhotoCropper). */
const MAX_SIDE = 1280;
const JPEG_QUALITY = 0.82;

function loadImage(url: string): Promise<HTMLImageElement> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => resolve(img);
    img.onerror = () => reject(new Error("image load failed"));
    img.src = url;
  });
}

/** Полное фото без обрезки: downscale + JPEG. */
export async function prepareWorkoutPhoto(file: File): Promise<File | null> {
  const url = URL.createObjectURL(file);
  try {
    const img = await loadImage(url);
    const nW = img.naturalWidth;
    const nH = img.naturalHeight;
    if (!nW || !nH) return null;
    const downscale = Math.min(1, MAX_SIDE / Math.max(nW, nH));
    const outW = Math.max(1, Math.round(nW * downscale));
    const outH = Math.max(1, Math.round(nH * downscale));
    const canvas = document.createElement("canvas");
    canvas.width = outW;
    canvas.height = outH;
    const ctx = canvas.getContext("2d");
    if (!ctx) return null;
    ctx.fillStyle = "#ffffff";
    ctx.fillRect(0, 0, outW, outH);
    ctx.imageSmoothingQuality = "high";
    ctx.drawImage(img, 0, 0, outW, outH);
    const blob: Blob | null = await new Promise((resolve) =>
      canvas.toBlob((b) => resolve(b), "image/jpeg", JPEG_QUALITY),
    );
    if (!blob) return null;
    const baseName = file.name.replace(/\.[^.]+$/, "") || "photo";
    return new File([blob], `${baseName}.jpg`, { type: "image/jpeg" });
  } finally {
    URL.revokeObjectURL(url);
  }
}
