import { useCallback, useEffect, useMemo, useState } from "react";
import {
  fetchAdminDbColumns,
  fetchAdminDbTable,
  fetchAdminDbTables,
  runAdminDbQuery,
  type AdminDbColumn,
  type AdminDbTable,
  type AdminQueryResult,
} from "../lib/adminApi";
import "./AdminOpsScreen.css";

type Props = {
  initData: string;
  showAlert: (text: string) => void;
};

const PAGE = 50;

/**
 * Вкладка «Данные»: список таблиц, просмотр строк с сортировкой и структурой,
 * плюс SQL-редактор. Пустые таблицы по умолчанию спрятаны — в базе стаи их
 * половина, и они только мешают искать нужную.
 */
export function AdminDataScreen({ initData, showAlert }: Props) {
  const [tables, setTables] = useState<AdminDbTable[]>([]);
  const [showEmpty, setShowEmpty] = useState(false);
  const [filter, setFilter] = useState("");

  const [table, setTable] = useState("");
  const [view, setView] = useState<"rows" | "columns">("rows");
  const [offset, setOffset] = useState(0);
  const [orderBy, setOrderBy] = useState("");
  const [desc, setDesc] = useState(false);
  const [result, setResult] = useState<AdminQueryResult | null>(null);
  const [columns, setColumns] = useState<AdminDbColumn[]>([]);

  const [sqlMode, setSqlMode] = useState(false);
  const [sql, setSql] = useState("");
  const [busy, setBusy] = useState(false);

  const fail = useCallback(
    (e: unknown, fallback: string) => showAlert(e instanceof Error ? e.message : fallback),
    [showAlert],
  );

  useEffect(() => {
    void (async () => {
      try {
        const j = await fetchAdminDbTables(initData);
        setTables(j.tables ?? []);
      } catch (e) {
        fail(e, "Не удалось получить список таблиц");
      }
    })();
  }, [initData, fail]);

  const emptyCount = useMemo(() => tables.filter((t) => t.rows === 0).length, [tables]);
  const visibleTables = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return tables.filter((t) => (showEmpty || t.rows > 0) && (!q || t.name.includes(q)));
  }, [tables, showEmpty, filter]);

  const openTable = useCallback(
    async (name: string, nextOffset: number, sortBy = "", sortDesc = false) => {
      setBusy(true);
      try {
        const j = await fetchAdminDbTable(initData, name, PAGE, nextOffset, sortBy, sortDesc);
        setTable(name);
        setOffset(nextOffset);
        setOrderBy(sortBy);
        setDesc(sortDesc);
        setResult(j.result);
        setView("rows");
        setSqlMode(false);
      } catch (e) {
        fail(e, "Не удалось открыть таблицу");
      } finally {
        setBusy(false);
      }
    },
    [initData, fail],
  );

  const openColumns = async () => {
    if (!table) return;
    setBusy(true);
    try {
      const j = await fetchAdminDbColumns(initData, table);
      setColumns(j.columns ?? []);
      setView("columns");
    } catch (e) {
      fail(e, "Не удалось получить структуру");
    } finally {
      setBusy(false);
    }
  };

  const sortBy = (column: string) => {
    if (!table) return;
    const nextDesc = orderBy === column ? !desc : false;
    void openTable(table, 0, column, nextDesc);
  };

  const runQuery = async () => {
    const q = sql.trim();
    if (!q) {
      showAlert("Напиши запрос.");
      return;
    }
    setBusy(true);
    try {
      const j = await runAdminDbQuery(initData, q);
      setResult(j.result);
      setTable("");
      setView("rows");
    } catch (e) {
      fail(e, "Запрос не выполнился");
    } finally {
      setBusy(false);
    }
  };

  const currentTable = tables.find((t) => t.name === table);

  return (
    <div className="ops">
      <div className="ops-periods">
        <button type="button" className={!sqlMode ? "on" : ""} onClick={() => setSqlMode(false)}>
          Таблицы
        </button>
        <button type="button" className={sqlMode ? "on" : ""} onClick={() => setSqlMode(true)}>
          SQL
        </button>
      </div>

      {sqlMode ? (
        <div className="ops-card">
          <textarea
            value={sql}
            onChange={(e) => setSql(e.target.value)}
            placeholder="SELECT user_id, streak FROM training_state ORDER BY streak DESC LIMIT 20"
            spellCheck={false}
          />
          <div className="ops-row">
            <button type="button" className="ops-primary" disabled={busy} onClick={() => void runQuery()}>
              Выполнить
            </button>
          </div>
          <p className="ops-muted">
            Только чтение: SELECT / WITH / EXPLAIN / SHOW / TABLE / VALUES, один запрос без «;». Не больше 200 строк,
            таймаут 20 секунд.
          </p>
        </div>
      ) : (
        <>
          <div className="ops-row">
            <input value={filter} onChange={(e) => setFilter(e.target.value)} placeholder="Поиск таблицы" />
            <button type="button" onClick={() => setShowEmpty((v) => !v)}>
              {showEmpty ? "Скрыть пустые" : `Пустые (${emptyCount})`}
            </button>
          </div>
          <div className="ops-chips">
            {visibleTables.map((t) => (
              <button
                key={t.name}
                type="button"
                className={`ops-chip${table === t.name ? " on" : ""}`}
                disabled={busy}
                onClick={() => void openTable(t.name, 0)}
              >
                {t.name}
                <small>{t.rows}</small>
              </button>
            ))}
            {visibleTables.length === 0 ? <p className="ops-muted">Ничего не нашлось</p> : null}
          </div>
        </>
      )}

      {table ? (
        <div className="ops-periods">
          <button type="button" className={view === "rows" ? "on" : ""} onClick={() => void openTable(table, offset, orderBy, desc)}>
            Данные
          </button>
          <button type="button" className={view === "columns" ? "on" : ""} onClick={() => void openColumns()}>
            Структура
          </button>
        </div>
      ) : null}

      {view === "columns" && table ? (
        <section className="ops-table">
          <h3 className="ops-table__title">{table}</h3>
          <p className="ops-table__subtitle">Колонок: {columns.length}</p>
          <div className="ops-table__scroll">
            <table>
              <thead>
                <tr>
                  <th>Колонка</th>
                  <th>Тип</th>
                  <th>NULL</th>
                  <th>По умолчанию</th>
                </tr>
              </thead>
              <tbody>
                {columns.map((c) => (
                  <tr key={c.name}>
                    <td>
                      {c.pk ? "🔑 " : ""}
                      {c.name}
                    </td>
                    <td>{c.type}</td>
                    <td>{c.nullable ? "да" : "нет"}</td>
                    <td title={c.default}>{c.default ? c.default.slice(0, 40) : "—"}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      ) : null}

      {view === "rows" && result ? (
        <section className="ops-table">
          <h3 className="ops-table__title">{table || "Результат"}</h3>
          <p className="ops-table__subtitle">
            {table && currentTable ? `Всего строк: ${currentTable.rows} · ` : ""}
            Показано: {result.rows.length}
            {result.truncated ? " (обрезано до 200)" : ""} · {result.took}
            {orderBy ? ` · сортировка: ${orderBy} ${desc ? "↓" : "↑"}` : ""}
          </p>
          <div className="ops-table__scroll">
            <table>
              <thead>
                <tr>
                  {result.columns.map((c) => (
                    <th key={c}>
                      {table ? (
                        <button type="button" className="ops-th" onClick={() => sortBy(c)}>
                          {c}
                          {orderBy === c ? (desc ? " ↓" : " ↑") : ""}
                        </button>
                      ) : (
                        c
                      )}
                    </th>
                  ))}
                </tr>
              </thead>
              <tbody>
                {result.rows.map((row, i) => (
                  <tr key={i}>
                    {row.map((cell, j) => (
                      <td key={j} title={cell}>
                        {cell.length > 80 ? `${cell.slice(0, 80)}…` : cell}
                      </td>
                    ))}
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          {result.rows.length === 0 ? <p className="ops-muted">Пусто</p> : null}
          {table ? (
            <div className="ops-row">
              <button
                type="button"
                disabled={busy || offset === 0}
                onClick={() => void openTable(table, Math.max(0, offset - PAGE), orderBy, desc)}
              >
                ◀ Назад
              </button>
              <span className="ops-muted">
                {offset + 1}–{offset + result.rows.length}
              </span>
              <button
                type="button"
                disabled={busy || result.rows.length < PAGE}
                onClick={() => void openTable(table, offset + PAGE, orderBy, desc)}
              >
                Вперёд ▶
              </button>
            </div>
          ) : null}
        </section>
      ) : null}
    </div>
  );
}
