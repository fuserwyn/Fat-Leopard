import { describe, expect, it } from "vitest";
import { langFromTelegram, levelName, t } from "./i18n";

describe("i18n", () => {
  it("picks ru from telegram codes", () => {
    expect(langFromTelegram("ru")).toBe("ru");
    expect(langFromTelegram("ru-RU")).toBe("ru");
    expect(langFromTelegram("en")).toBe("en");
    expect(langFromTelegram("")).toBe("en");
  });

  it("translates nav labels", () => {
    expect(t("en", "nav.pack")).toBe("Pack");
    expect(t("ru", "nav.pack")).toBe("Стая");
    expect(t("ru", "nav.packUnread", { n: 3 })).toBe("Стая, непрочитанных: 3");
  });

  it("level names follow lang", () => {
    expect(levelName("en", 1)).toBe("Meerkat");
    expect(levelName("ru", 1)).toBe("Сурикат");
    expect(levelName("ru", 6)).toBe("Слон");
  });
});
