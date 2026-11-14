import { useCallback, useEffect, useState } from "react";
import { askLeoLab, fetchLeoMemory, fetchLeoPrompt, teachLeo, type LeoMemory } from "../lib/adminApi";
import "./AdminOpsScreen.css";

type Props = {
  initData: string;
  showAlert: (text: string) => void;
};

/**
 * «Тест Лео»: тот же обученный Лео, что отвечает в чате, но здесь его можно
 * подёргать без стаи. Обучение у нас две вещи — системный промпт (характер и
 * правила) и память RAG (факты про стаю); обе доступны отсюда.
 */
export function LeoLabScreen({ initData, showAlert }: Props) {
  const [system, setSystem] = useState("");
  const [defaultPrompt, setDefaultPrompt] = useState("");
  const [question, setQuestion] = useState("");
  const [answer, setAnswer] = useState("");
  const [usedDefault, setUsedDefault] = useState(true);
  const [fact, setFact] = useState("");
  const [memory, setMemory] = useState<LeoMemory | null>(null);
  const [busy, setBusy] = useState<"" | "ask" | "teach">("");

  const fail = useCallback(
    (e: unknown, fallback: string) => showAlert(e instanceof Error ? e.message : fallback),
    [showAlert],
  );

  useEffect(() => {
    void (async () => {
      try {
        const [p, m] = await Promise.all([fetchLeoPrompt(initData), fetchLeoMemory(initData)]);
        setDefaultPrompt(p.prompt ?? "");
        setMemory(m.memory);
      } catch (e) {
        fail(e, "Не удалось получить настройки Лео");
      }
    })();
  }, [initData, fail]);

  const ask = async () => {
    if (!question.trim()) {
      showAlert("Спроси что-нибудь.");
      return;
    }
    setBusy("ask");
    try {
      const j = await askLeoLab(initData, system, question);
      setAnswer(j.answer);
      setUsedDefault(j.used_default);
    } catch (e) {
      fail(e, "Лео промолчал");
    } finally {
      setBusy("");
    }
  };

  const teach = async () => {
    if (!fact.trim()) {
      showAlert("Напиши, что Лео должен запомнить.");
      return;
    }
    setBusy("teach");
    try {
      await teachLeo(initData, fact);
      setFact("");
      showAlert("Запомнил.");
      const m = await fetchLeoMemory(initData);
      setMemory(m.memory);
    } catch (e) {
      fail(e, "Не удалось научить");
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="ops">
      <div className="ops-card">
        <b>🐆 Спросить</b>
        <textarea
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder="Например: что скажешь тому, кто пропал на неделю?"
        />
        <div className="ops-row">
          <button type="button" className="ops-primary" disabled={busy !== ""} onClick={() => void ask()}>
            {busy === "ask" ? "Думает…" : "Спросить Лео"}
          </button>
        </div>
        {answer ? (
          <>
            <p className="ops-muted">{usedDefault ? "Отвечал боевым характером" : "Отвечал твоим промптом"}</p>
            <p className="leo-lab__answer">{answer}</p>
          </>
        ) : null}
      </div>

      <div className="ops-card">
        <b>⚙️ Системный промпт</b>
        <p className="ops-muted">
          Пусто — Лео отвечает боевым характером. Впиши свой, чтобы попробовать другой тон или правила: правка живёт
          только в этом экране и на стаю не влияет.
        </p>
        <textarea
          value={system}
          onChange={(e) => setSystem(e.target.value)}
          placeholder="Свой системный промпт для проверки"
          spellCheck={false}
        />
        <div className="ops-row">
          <button type="button" disabled={!defaultPrompt} onClick={() => setSystem(defaultPrompt)}>
            Подставить боевой
          </button>
          <button type="button" disabled={!system} onClick={() => setSystem("")}>
            Очистить
          </button>
        </div>
      </div>

      <div className="ops-card">
        <b>🧠 Научить</b>
        <p className="ops-muted">
          Факт уходит в память Лео и подтягивается там же, где он вспоминает переписку стаи. Формулируй как правило:
          «В стае не принято…», «Кубки начисляются за…».
        </p>
        <textarea value={fact} onChange={(e) => setFact(e.target.value)} placeholder="Что Лео должен запомнить" />
        <div className="ops-row">
          <button type="button" className="ops-primary" disabled={busy !== ""} onClick={() => void teach()}>
            {busy === "teach" ? "Запоминает…" : "Запомнить"}
          </button>
        </div>
        {memory ? (
          <p className="ops-muted">
            В памяти сейчас {memory.total} фрагментов, из них старше полугода — {memory.old}.
          </p>
        ) : null}
      </div>
    </div>
  );
}
