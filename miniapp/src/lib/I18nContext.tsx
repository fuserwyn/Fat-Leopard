import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { t as translate, type UILang } from "./i18n";
import { bootstrapUILang, getStoredUILang, persistUILangToServer, setUILang } from "./uiLang";

type I18nCtx = {
  lang: UILang;
  setLang: (lang: UILang) => void;
  t: (key: string, vars?: Record<string, string | number>) => string;
};

const Ctx = createContext<I18nCtx | null>(null);

export function I18nProvider({
  children,
  initData,
  telegramLanguageCode,
}: {
  children: ReactNode;
  initData?: string;
  telegramLanguageCode?: string | null;
}) {
  const [lang, setLangState] = useState<UILang>(() => bootstrapUILang(telegramLanguageCode));

  useEffect(() => {
    const onLang = (e: Event) => {
      const next = (e as CustomEvent<UILang>).detail;
      if (next === "en" || next === "ru") setLangState(next);
    };
    window.addEventListener("leo-ui-lang", onLang);
    return () => window.removeEventListener("leo-ui-lang", onLang);
  }, []);

  const value = useMemo<I18nCtx>(
    () => ({
      lang,
      setLang: (next) => {
        setUILang(next);
        setLangState(next);
        if (initData) void persistUILangToServer(initData, next);
      },
      t: (key, vars) => translate(lang, key, vars),
    }),
    [lang, initData],
  );

  return <Ctx.Provider value={value}>{children}</Ctx.Provider>;
}

export function useI18n(): I18nCtx {
  const ctx = useContext(Ctx);
  if (!ctx) {
    const lang = getStoredUILang();
    return {
      lang,
      setLang: setUILang,
      t: (key, vars) => translate(lang, key, vars),
    };
  }
  return ctx;
}
