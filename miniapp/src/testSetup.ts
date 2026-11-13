import { beforeEach } from "vitest";
import { resetUILangRuntimeForTests } from "./lib/uiLang";

/**
 * Тесты писались под EN-строки. В проде bootstrap → ru, поэтому в jsdom
 * фиксируем en до каждого теста (и сбрасываем runtime-лок с сервера).
 */
beforeEach(() => {
  resetUILangRuntimeForTests();
  try {
    localStorage.setItem("leo-ui-lang-v2", "en");
    localStorage.removeItem("leo-ui-lang");
  } catch {
    /* */
  }
});
