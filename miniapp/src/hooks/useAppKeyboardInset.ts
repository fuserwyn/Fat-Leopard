import { useEffect } from "react";

function isFeedThreadInput(el: Element | null): boolean {
  return el instanceof HTMLTextAreaElement && el.classList.contains("act-card__thread-input");
}

function isNavComposeInput(el: Element | null): boolean {
  return el instanceof HTMLInputElement && el.classList.contains("bottom-nav__compose-input");
}

/** Fixed/in-flow компоузеры сами поднимаются через bottom: var(--app-keyboard-bottom).
 *  Трекер сам докручивает поле над клавиатурой (useKeepFocusedFieldVisible в TrackerScreen). */
function shouldSkipScrollReveal(el: HTMLElement): boolean {
  return Boolean(
    el.closest(".bottom-nav, .chat__form, .admin__composer, .packroom__form, .sup .chat__form, .tracker"),
  );
}

function isTextEntry(el: Element | null): boolean {
  if (!el || !(el instanceof HTMLElement)) return false;
  const tag = el.tagName;
  if (tag === "TEXTAREA") return true;
  if (tag === "INPUT") {
    const type = (el as HTMLInputElement).type;
    return !["button", "submit", "reset", "checkbox", "radio", "file", "hidden", "image"].includes(
      type,
    );
  }
  return el.isContentEditable;
}

/**
 * Глобально: при фокусе в поле ввода таббар остаётся на bottom:0, но скрыт (не едет над клавиатурой).
 * --app-keyboard-bottom — для форм ввода (чат, поддержка), чтобы сидеть над клавиатурой.
 */
export function useAppKeyboardInset() {
  useEffect(() => {
    const root = document.documentElement;
    let debounce: ReturnType<typeof setTimeout> | undefined;
    let textFocused = false;

    const readKeyboard = () => {
      // Страховка: если сфокусированное поле удалили из DOM (например, закрыли
      // шторку тренировки свайпом вниз), focusout не стреляет — класс
      // app-keyboard-open зависал и навсегда прятал таббар с полем ввода ленты.
      // Сверяемся с реальным activeElement на каждом пересчёте вьюпорта.
      if (textFocused && !isTextEntry(document.activeElement)) {
        textFocused = false;
        root.classList.remove("feed-thread-composer-open");
        root.classList.remove("app-nav-compose-open");
        syncOpenClass();
      }
      const vv = window.visualViewport;
      const tg = window.Telegram?.WebApp;
      const tgStable =
        typeof tg?.viewportStableHeight === "number" && tg.viewportStableHeight > 200
          ? Math.floor(tg.viewportStableHeight)
          : 0;
      const tgCurrent =
        typeof tg?.viewportHeight === "number" && tg.viewportHeight > 100
          ? Math.floor(tg.viewportHeight)
          : 0;

      const layoutH = Math.max(320, tgStable || Math.floor(window.innerHeight || 320));
      const visualH = Math.floor(vv && vv.height > 0 ? vv.height : tgCurrent || layoutH);
      const offsetTop = Math.floor(vv?.offsetTop ?? 0);

      const kbVV = Math.max(0, layoutH - visualH - offsetTop);
      const kbTG = tgStable > 0 && tgCurrent > 0 ? Math.max(0, tgStable - tgCurrent) : 0;
      let kb = kbVV;
      if (textFocused) {
        // TG точнее после анимации; до обновления viewportHeight visualViewport
        // уже видит клавиатуру — иначе inset=0 и поле уходит под клавиатуру.
        // В стабильном состоянии TG часто завышает vv → зазор; берём tg, если он уже > 0.
        if (kbTG >= 48) kb = kbTG;
        else kb = Math.max(kbVV, kbTG);
      }

      const px = `${kb}px`;
      root.style.setProperty("--app-keyboard-bottom", px);
      root.style.setProperty("--feed-keyboard-bottom", px);
    };

    const scheduleRead = () => {
      if (debounce) window.clearTimeout(debounce);
      debounce = window.setTimeout(readKeyboard, 80);
    };

    const syncOpenClass = () => {
      root.classList.toggle("app-keyboard-open", textFocused);
    };

    const onFocusIn = (e: FocusEvent) => {
      if (!isTextEntry(e.target as Element)) return;
      if (isFeedThreadInput(e.target as Element)) {
        root.classList.add("feed-thread-composer-open");
      }
      if (isNavComposeInput(e.target as Element)) {
        root.classList.add("app-nav-compose-open");
      }
      textFocused = true;
      syncOpenClass();
      readKeyboard();
      window.setTimeout(readKeyboard, 0);
      window.setTimeout(readKeyboard, 100);
      window.setTimeout(readKeyboard, 280);
      window.setTimeout(readKeyboard, 480);
      const target = e.target;
      const reveal = () => {
        if (!(target instanceof HTMLElement) || document.activeElement !== target) return;
        if (shouldSkipScrollReveal(target)) return;
        const block: ScrollLogicalPosition = isFeedThreadInput(target) ? "end" : "nearest";
        target.scrollIntoView({ block, inline: "nearest", behavior: "smooth" });
      };
      window.setTimeout(reveal, 120);
      window.setTimeout(reveal, 320);
    };

    const onFocusOut = () => {
      window.setTimeout(() => {
        const active = document.activeElement;
        if (isTextEntry(active)) return;
        root.classList.remove("feed-thread-composer-open");
        root.classList.remove("app-nav-compose-open");
        textFocused = false;
        syncOpenClass();
      }, 80);
    };

    readKeyboard();
    syncOpenClass();

    document.addEventListener("focusin", onFocusIn);
    document.addEventListener("focusout", onFocusOut);

    const vv = window.visualViewport;
    vv?.addEventListener("resize", scheduleRead);
    vv?.addEventListener("scroll", scheduleRead);
    window.addEventListener("orientationchange", readKeyboard);

    const tg = window.Telegram?.WebApp as { onEvent?: (e: string, fn: () => void) => void } | undefined;
    tg?.onEvent?.("viewportChanged", scheduleRead);

    return () => {
      if (debounce) window.clearTimeout(debounce);
      document.removeEventListener("focusin", onFocusIn);
      document.removeEventListener("focusout", onFocusOut);
      vv?.removeEventListener("resize", scheduleRead);
      vv?.removeEventListener("scroll", scheduleRead);
      window.removeEventListener("orientationchange", readKeyboard);
      const tgOff = window.Telegram?.WebApp as { offEvent?: (e: string, fn: () => void) => void } | undefined;
      tgOff?.offEvent?.("viewportChanged", scheduleRead);
      root.classList.remove("app-keyboard-open");
      root.classList.remove("feed-thread-composer-open");
      root.classList.remove("app-nav-compose-open");
      root.style.removeProperty("--app-keyboard-bottom");
      root.style.removeProperty("--feed-keyboard-bottom");
    };
  }, []);
}
