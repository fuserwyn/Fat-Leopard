import { t, type UILang } from "./i18n";

const TRAINING_ACCEPTED_LINE_PREFIXES = [
  "✅ Workout logged",
  "✅ Report accepted",
  "✅ Отчёт принят",
  "✅ Тренировка",
] as const;

function lineLooksLikeTrainingAccepted(line: string): boolean {
  return TRAINING_ACCEPTED_LINE_PREFIXES.some((p) => line.startsWith(p));
}

/** Сырые ошибки ИИ/API не показываем пользователю в модалке успеха. */
export function isLeoAiFailureLine(line: string): boolean {
  const s = line.trim();
  if (!s) return false;
  if (/Ошибка при генерации ответа ИИ/i.test(s)) return true;
  if (/Failed to generate AI/i.test(s)) return true;
  if (/API error \(status \d+\)/i.test(s)) return true;
  if (/"code"\s*:\s*401/i.test(s) && /User not found/i.test(s)) return true;
  return false;
}

function stripAiFailures(parts: string[]): string[] {
  return parts
    .map((p) =>
      p
        .split("\n")
        .map((l) => l.trim())
        .filter((l) => l && !isLeoAiFailureLine(l))
        .join("\n")
        .trim(),
    )
    .filter(Boolean);
}

/** Compact workout confirmation for the done modal — localized, без сырых ошибок API. */
export function formatTrainingDoneAlert(replyParts: string[], lang: UILang = "en"): string {
  const cleaned = stripAiFailures(replyParts);
  const summary = cleaned.join("\n\n").trim();
  if (!summary) return "";

  const lines = summary
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  const titleLine = lines.find(lineLooksLikeTrainingAccepted);
  if (!titleLine && !lineLooksLikeTrainingAccepted(summary)) {
    return summary;
  }

  let title = lang === "ru" ? "✅ Тренировка записана!" : "✅ Workout logged!";
  if (titleLine?.startsWith("✅ Report accepted")) {
    title = titleLine;
  } else if (titleLine?.startsWith("✅ Workout logged") || titleLine?.startsWith("✅ Тренировка")) {
    title = titleLine;
  }

  const streakLineRaw =
    lines.find(
      (line) =>
        /(?:^🦁\s*)?(?:Streak|Series|Стрик|Серия):/i.test(line) ||
        /^🦁\s*Ты тренируешься \d+/i.test(line),
    ) ?? "";
  const cupsLinesRaw = lines.filter((line) => line.startsWith("🏆"));

  const streakLine =
    lang === "ru"
      ? streakLineRaw
          .replace(/^🦁\s*(?:Стрик|Серия|Streak|Series):\s*/i, "Стрик: ")
          .replace(/^🦁\s*Ты тренируешься (\d+)\s+.+?\s+подряд/i, "Стрик: $1 дн.")
          .replace(/\s+days?\b/gi, " дн.")
      : streakLineRaw
          .replace(/^🦁\s*(?:Стрик|Серия|Streak|Series):\s*/i, "Streak: ")
          .replace(/^🦁\s*Ты тренируешься (\d+)\s+.+?\s+подряд/i, "Streak: $1 days")
          .replace(/\s+дн[ьяеё]+/gi, " days")
          .replace(/^Streak:\s*(\d+)\s+day\b/i, "Streak: $1 days");

  const normalizeCupsLine = (raw: string) => {
    if (lang === "ru") {
      return raw
        .replace(/^🏆\s*/u, "")
        .replace(/(\d+)\s+cups?\b/gi, "$1 кубков")
        .replace(/Total cups:\s*/gi, "Всего кубков: ")
        .replace(/\+(\d+)\s+cup for this workout!?/gi, "+$1 кубок за тренировку")
        .replace(/\+(\d+)\s+cup for the extra workout!?/gi, "+$1 кубок за дополнительную тренировку");
    }
    return raw
      .replace(/^🏆\s*/u, "")
      .replace(/(\d+)\s+кубок\b/gi, "$1 cup")
      .replace(/(\d+)\s+кубка\b/gi, "$1 cups")
      .replace(/(\d+)\s+кубков\b/gi, "$1 cups")
      .replace(/Всего кубков:\s*/gi, "Total cups: ")
      .replace(/\+(\d+)\s+кубок за тренировку!?/gi, "+$1 cup for this workout")
      .replace(/\+(\d+)\s+кубок за дополнительную тренировку!?/gi, "+$1 cup for the extra workout");
  };

  const cupsLine = cupsLinesRaw.map(normalizeCupsLine).join("\n");

  const compact = [title, [streakLine, cupsLine].filter(Boolean).join("\n")].filter(Boolean).join("\n\n").trim();
  return compact || summary || t(lang, "workout.doneFallback");
}
