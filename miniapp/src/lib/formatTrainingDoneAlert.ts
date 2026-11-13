const TRAINING_ACCEPTED_LINE_PREFIXES = [
  "✅ Workout logged",
  "✅ Report accepted",
  "✅ Отчёт принят",
  "✅ Тренировка",
] as const;

function lineLooksLikeTrainingAccepted(line: string): boolean {
  return TRAINING_ACCEPTED_LINE_PREFIXES.some((p) => line.startsWith(p));
}

/** Compact workout confirmation for tg.showAlert — English UI, legacy RU bot text ok. */
export function formatTrainingDoneAlert(replyParts: string[]): string {
  const summary = replyParts.filter(Boolean).join("\n\n").trim();
  if (!summary) return "";

  const lines = summary
    .split("\n")
    .map((line) => line.trim())
    .filter(Boolean);

  const titleLine = lines.find(lineLooksLikeTrainingAccepted);
  if (!titleLine && !lineLooksLikeTrainingAccepted(summary)) {
    return summary;
  }

  let title = "✅ Workout logged!";
  if (titleLine?.startsWith("✅ Report accepted")) {
    title = titleLine;
  } else if (titleLine?.startsWith("✅ Workout logged")) {
    title = titleLine;
  }

  const streakLineRaw =
    lines.find(
      (line) =>
        /(?:^🦁\s*)?(?:Streak|Series|Стрик|Серия):/i.test(line) ||
        /^🦁\s*Ты тренируешься \d+/i.test(line),
    ) ?? "";
  const cupsLinesRaw = lines.filter((line) => line.startsWith("🏆"));

  const streakLine = streakLineRaw
    .replace(/^🦁\s*(?:Стрик|Серия|Streak|Series):\s*/i, "Streak: ")
    .replace(/^🦁\s*Ты тренируешься (\d+)\s+.+?\s+подряд/i, "Streak: $1 days")
    .replace(/\s+дн[ьяеё]+/gi, " days")
    .replace(/^Streak:\s*(\d+)\s+day\b/i, "Streak: $1 days");

  const normalizeCupsLine = (raw: string) =>
    raw
      .replace(/^🏆\s*/u, "")
      .replace(/(\d+)\s+кубок\b/gi, "$1 cup")
      .replace(/(\d+)\s+кубка\b/gi, "$1 cups")
      .replace(/(\d+)\s+кубков\b/gi, "$1 cups")
      .replace(/Всего кубков:\s*/gi, "Total cups: ")
      .replace(/\+(\d+)\s+кубок за тренировку!?/gi, "+$1 cup for this workout")
      .replace(/\+(\d+)\s+кубок за дополнительную тренировку!?/gi, "+$1 cup for the extra workout");

  const cupsLine = cupsLinesRaw.map(normalizeCupsLine).join("\n");

  const compact = [title, [streakLine, cupsLine].filter(Boolean).join("\n")].filter(Boolean).join("\n\n").trim();
  return compact || summary;
}
