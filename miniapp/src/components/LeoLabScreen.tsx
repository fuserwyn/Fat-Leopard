import { useCallback, useEffect, useState } from "react";
import { askLeoLab, fetchLeoMemory, fetchLeoPrompt, teachLeo, type LeoMemory } from "../lib/adminApi";
import "./AdminOpsScreen.css";

type Props = {
  initData: string;
  showAlert: (text: string) => void;
};

/**
 * Leo test lab: same trained Leo as in chat, without the pack. Training here is
 * system prompt (persona and rules) and RAG memory (pack facts); both are available here.
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
        fail(e, "Couldn’t load Leo settings");
      }
    })();
  }, [initData, fail]);

  const ask = async () => {
    if (!question.trim()) {
      showAlert("Ask something.");
      return;
    }
    setBusy("ask");
    try {
      const j = await askLeoLab(initData, system, question);
      setAnswer(j.answer);
      setUsedDefault(j.used_default);
    } catch (e) {
      fail(e, "Leo didn’t answer");
    } finally {
      setBusy("");
    }
  };

  const teach = async () => {
    if (!fact.trim()) {
      showAlert("Write what Leo should remember.");
      return;
    }
    setBusy("teach");
    try {
      await teachLeo(initData, fact);
      setFact("");
      showAlert("Remembered.");
      const m = await fetchLeoMemory(initData);
      setMemory(m.memory);
    } catch (e) {
      fail(e, "Couldn’t teach");
    } finally {
      setBusy("");
    }
  };

  return (
    <div className="ops">
      <div className="ops-card">
        <b>🐆 Ask</b>
        <textarea
          value={question}
          onChange={(e) => setQuestion(e.target.value)}
          placeholder="e.g. what do you say to someone who ghosted for a week?"
        />
        <div className="ops-row">
          <button type="button" className="ops-primary" disabled={busy !== ""} onClick={() => void ask()}>
            {busy === "ask" ? "Thinking…" : "Ask Leo"}
          </button>
        </div>
        {answer ? (
          <>
            <p className="ops-muted">{usedDefault ? "Used production persona" : "Used your prompt"}</p>
            <p className="leo-lab__answer">{answer}</p>
          </>
        ) : null}
      </div>

      <div className="ops-card">
        <b>⚙️ System prompt</b>
        <p className="ops-muted">
          Empty — Leo uses the production persona. Add your own to try another tone or rules; edits live only on
          this screen and don’t affect the pack.
        </p>
        <textarea
          value={system}
          onChange={(e) => setSystem(e.target.value)}
          placeholder="Custom system prompt for testing"
          spellCheck={false}
        />
        <div className="ops-row">
          <button type="button" disabled={!defaultPrompt} onClick={() => setSystem(defaultPrompt)}>
            Use production prompt
          </button>
          <button type="button" disabled={!system} onClick={() => setSystem("")}>
            Clear
          </button>
        </div>
      </div>

      <div className="ops-card">
        <b>🧠 Teach</b>
        <p className="ops-muted">
          Facts go into Leo’s memory and are pulled where he recalls pack chat. Phrase as a rule: “In the pack we
          don’t…”, “Cups are earned for…”.
        </p>
        <textarea value={fact} onChange={(e) => setFact(e.target.value)} placeholder="What Leo should remember" />
        <div className="ops-row">
          <button type="button" className="ops-primary" disabled={busy !== ""} onClick={() => void teach()}>
            {busy === "teach" ? "Remembering…" : "Remember"}
          </button>
        </div>
        {memory ? (
          <p className="ops-muted">
            {memory.total} fragments in memory now, {memory.old} older than six months.
          </p>
        ) : null}
      </div>
    </div>
  );
}
