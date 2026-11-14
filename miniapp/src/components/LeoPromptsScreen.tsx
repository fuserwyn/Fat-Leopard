import { useCallback, useEffect, useRef, useState, type ChangeEvent } from "react";
import {
  fetchLeoPrompts,
  resetLeoPrompt,
  saveLeoPrompt,
  type LeoPromptSlot,
} from "../lib/adminApi";
import "./AdminOpsScreen.css";

type Props = {
  initData: string;
  showAlert: (text: string) => void;
};

export function LeoPromptsScreen({ initData, showAlert }: Props) {
  const [slots, setSlots] = useState<LeoPromptSlot[]>([]);
  const [drafts, setDrafts] = useState<Record<string, string>>({});
  const [busy, setBusy] = useState("");
  const fileFor = useRef<string>("");
  const fileInput = useRef<HTMLInputElement>(null);

  const fail = useCallback(
    (e: unknown, fallback: string) => showAlert(e instanceof Error ? e.message : fallback),
    [showAlert],
  );

  const load = useCallback(async () => {
    const j = await fetchLeoPrompts(initData);
    setSlots(j.prompts ?? []);
    const next: Record<string, string> = {};
    for (const p of j.prompts ?? []) next[p.key] = p.body;
    setDrafts(next);
  }, [initData]);

  useEffect(() => {
    void load().catch((e) => fail(e, "Не удалось загрузить промпты"));
  }, [load, fail]);

  const save = async (key: string, filename = "") => {
    const body = (drafts[key] ?? "").trim();
    if (!body) {
      showAlert("Текст пустой.");
      return;
    }
    setBusy(key);
    try {
      await saveLeoPrompt(initData, key, body, filename);
      await load();
      showAlert("Сохранили. Лео уже учится по новому тексту.");
    } catch (e) {
      fail(e, "Не удалось сохранить");
    } finally {
      setBusy("");
    }
  };

  const reset = async (key: string) => {
    setBusy(key);
    try {
      await resetLeoPrompt(initData, key);
      await load();
      showAlert("Вернули встроенный файл.");
    } catch (e) {
      fail(e, "Не удалось сбросить");
    } finally {
      setBusy("");
    }
  };

  const pickFile = (key: string) => {
    fileFor.current = key;
    fileInput.current?.click();
  };

  const onFile = async (e: ChangeEvent<HTMLInputElement>) => {
    const file = e.target.files?.[0];
    e.target.value = "";
    const key = fileFor.current;
    if (!file || !key) return;
    setBusy(key);
    try {
      const text = await file.text();
      if (!text.trim()) {
        showAlert("Файл пустой.");
        return;
      }
      setDrafts((d) => ({ ...d, [key]: text }));
      await saveLeoPrompt(initData, key, text, file.name);
      await load();
      showAlert("Файл поставили. Лео уже учится по нему.");
    } catch (err) {
      fail(err, "Файл не прочитался");
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="ops">
      <p className="ops-muted">
        Боевые промпты Леопарда лежат в <code>ms_leo/internal/prompts/data/*.txt</code>. Здесь их можно
        заменить текстом или файлом — Лео сразу отвечает по новой версии, пересборка не нужна. Сброс
        возвращает встроенный файл из репозитория.
      </p>
      <input
        ref={fileInput}
        type="file"
        accept=".txt,.md,text/plain"
        hidden
        onChange={(e) => void onFile(e)}
      />
      {slots.map((p) => (
        <div className="ops-card" key={p.key}>
          <b>
            {p.title} {p.overridden ? "· заменён" : ""}
          </b>
          <p className="ops-muted">
            {p.about}. Файл: {p.file}
            {p.filename ? ` · загружен ${p.filename}` : ""}
            {p.updated_at ? ` · ${p.updated_at}` : ""}
          </p>
          <textarea
            value={drafts[p.key] ?? ""}
            onChange={(e) => setDrafts((d) => ({ ...d, [p.key]: e.target.value }))}
            spellCheck={false}
            rows={8}
          />
          <div className="ops-row">
            <button
              type="button"
              className="ops-primary"
              disabled={busy !== ""}
              onClick={() => void save(p.key)}
            >
              {busy === p.key ? "Пишем…" : "Сохранить"}
            </button>
            <button type="button" disabled={busy !== ""} onClick={() => pickFile(p.key)}>
              Прикрепить файл
            </button>
            <button type="button" disabled={busy !== "" || !p.overridden} onClick={() => void reset(p.key)}>
              Сбросить
            </button>
          </div>
        </div>
      ))}
    </div>
  );
}
