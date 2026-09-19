import { useEffect, useMemo, useRef, useState } from "react";
import { STATIC_MODE } from "../runtime";
import {
  useAgentsQuery,
  useAgentRequestsQuery,
  useCancelAgentRequestMutation,
  useCreateAgentRequestMutation,
  useRetryAgentRequestMutation,
  useRenderQuery,
  useSaveAgentRequestMutation,
} from "../queries";
import type { AgentRequest, RequestIntent, RequestNoteRef } from "../types";
import { MarkdownView } from "./MarkdownView";
import { IconX, RailIcon } from "./icons";
import "./agent-request.css";

export interface RequestTarget {
  vault?: string;
  title: string;
  quote?: string;
  note?: RequestNoteRef;
  intent?: RequestIntent;
}
export function openAgentRequest(target: RequestTarget) {
  window.dispatchEvent(new CustomEvent("track:open-agent-request", { detail: target }));
}

const labels: Record<RequestIntent, string> = { explain: "説明", research: "調査", update: "更新" };
const statusLabels: Record<string, string> = {
  queued: "開始待ち",
  running: "実行中",
  applying: "反映中",
  completed: "完了",
  failed: "失敗",
  conflict: "競合",
  cancelled: "取消",
};
const asList = (value: unknown): string[] => {
  if (Array.isArray(value)) return value.map(String);
  return typeof value === "string" && value.trim() ? [value] : [];
};
const terminalUpdate = (request: AgentRequest) =>
  request.intent === "update" &&
  (request.status === "completed" || request.status === "conflict" || request.status === "failed");
const readableDate = (value?: string) => {
  if (!value) return "";
  const date = new Date(value);
  return Number.isNaN(date.getTime()) ? value : date.toLocaleString();
};

export function AgentRequestPanel() {
  const [target, setTarget] = useState<RequestTarget | null>(null);
  const [intent, setIntent] = useState<RequestIntent>("explain");
  const [instruction, setInstruction] = useState("");
  const [agentID, setAgentID] = useState("");
  const [selected, setSelected] = useState<AgentRequest | null>(null);
  const [closed, setClosed] = useState(false);
  const [parent, setParent] = useState<AgentRequest | null>(null);
  const [saveTitle, setSaveTitle] = useState("");
  const [saveVault, setSaveVault] = useState("");
  const [saveClientID, setSaveClientID] = useState("");
  const returnFocus = useRef<HTMLElement | null>(null);
  const panel = useRef<HTMLElement | null>(null);
  const vault = target?.vault ?? "";
  const agents = useAgentsQuery(!!target);
  const requests = useAgentRequestsQuery(vault, !!target || !closed);
  const create = useCreateAgentRequestMutation(vault);
  const cancel = useCancelAgentRequestMutation(vault);
  const retry = useRetryAgentRequestMutation(vault);
  // Keep this fallback for older embedders which mock the stage-1 hooks.
  const saveHook =
    typeof useSaveAgentRequestMutation === "function"
      ? useSaveAgentRequestMutation(vault)
      : {
          mutateAsync: async () => {
            throw new Error("save unavailable");
          },
          isPending: false,
          error: null,
        };
  const agentList = agents.data?.agents ?? [];
  const available = useMemo(
    () => agentList.filter((a) => a.operations?.length === 0 || a.operations?.includes(intent)),
    [agentList, intent],
  );
  const list = requests.data?.requests ?? [];

  useEffect(() => {
    if (agentID === "" && available[0]) setAgentID(available[0].id);
  }, [available, agentID]);
  useEffect(() => {
    if (selected) {
      const fresh = list.find((r) => r.id === selected.id);
      if (fresh) setSelected(fresh);
    }
  }, [list, selected]);
  useEffect(() => {
    const open = (event: Event) => {
      const detail = (event as CustomEvent<RequestTarget>).detail;
      returnFocus.current = document.activeElement as HTMLElement;
      setTarget(detail);
      setIntent(detail.intent ?? "explain");
      setInstruction(
        detail.intent === "research"
          ? (detail.quote ?? "調べたいことを入力")
          : detail.quote
            ? `これを説明して: ${detail.quote}`
            : "これを説明して",
      );
      setSelected(null);
      setParent(null);
      setSaveTitle("");
      setSaveVault(detail.vault ?? "");
      setSaveClientID("");
      setClosed(false);
    };
    window.addEventListener("track:open-agent-request", open);
    return () => window.removeEventListener("track:open-agent-request", open);
  }, []);
  useEffect(() => {
    if (target && !closed) void requests.refetch();
  }, [target, closed]);
  useEffect(() => () => returnFocus.current?.focus(), []);
  useEffect(() => {
    if (panel.current) panel.current.scrollTop = 0;
  }, [selected?.id]);
  if (STATIC_MODE) return null;

  const send = async () => {
    if (!target || !instruction.trim() || !agentID || (intent === "update" && !target.note)) return;
    const body: Record<string, unknown> = {
      client_request_id: crypto.randomUUID(),
      intent,
      instruction: instruction.trim(),
      agent_id: agentID,
      context: { quote: target.quote ?? "", note: target.note },
    };
    if (parent) {
      body.parent_request_id = parent.id;
      body.context = {
        ...(body.context as object),
        prior_answers: [
          {
            request_id: parent.id,
            intent: parent.intent,
            instruction: parent.instruction,
            answer_markdown: parent.result?.answer_markdown ?? "",
          },
        ],
      };
    }
    if (intent === "update") body.update_target = target.note;
    const result = await create.mutateAsync(body);
    setSelected(result.request);
    setParent(null);
    setClosed(false);
  };
  const openFollowUp = (request: AgentRequest) => {
    setParent(request);
    setSelected(null);
    setIntent(request.intent === "update" ? "explain" : request.intent);
    setInstruction("");
    setSaveClientID("");
    setClosed(false);
  };
  const save = async () => {
    if (!selected?.id || !saveTitle.trim()) return;
    const clientRequestID = saveClientID || crypto.randomUUID();
    if (!saveClientID) setSaveClientID(clientRequestID);
    const result = await saveHook.mutateAsync({
      id: selected.id,
      clientRequestID,
      title: saveTitle.trim(),
      targetVault: saveVault.trim() || undefined,
    });
    setSelected(result);
  };
  const close = () => {
    setClosed(true);
    returnFocus.current?.focus();
  };
  const saved = selected?.result?.saved;
  const answer = selected?.result?.answer_markdown;
  const unresolved = asList(selected?.result?.unresolved ?? selected?.result?.unresolved_questions);
  const uncertain = asList(selected?.result?.uncertain ?? selected?.result?.uncertain_points);
  return (
    <aside
      ref={panel}
      className={`agent-request-panel${target && !closed ? " open" : ""}${selected && (selected.result || terminalUpdate(selected)) ? " has-result" : ""}`}
      aria-label="Agent request"
    >
      <div className="agent-request-heading">
        <div>
          <span className="label">AGENT REQUESTS</span>
          <h2>エージェントに依頼</h2>
        </div>
        <button
          className="graph-reset agent-request-close"
          type="button"
          aria-label="Close"
          onClick={close}
        >
          <RailIcon Icon={IconX} size={15} />
        </button>
      </div>
      {selected && (selected.result || terminalUpdate(selected)) ? (
        <ResultView
          key={selected.id}
          vault={selected.vault ?? vault}
          request={selected}
          answer={answer}
          unresolved={unresolved}
          uncertain={uncertain}
          saved={saved}
          saveTitle={saveTitle}
          saveVault={saveVault}
          setSaveTitle={setSaveTitle}
          setSaveVault={setSaveVault}
          onSave={() => void save()}
          saving={saveHook.isPending}
          saveError={saveHook.error}
          onFollowUp={() => openFollowUp(selected)}
        />
      ) : null}
      {target && !closed ? (
        <>
          <div className="agent-request-target">
            <span className="label">対象</span>
            <strong>{target.title}</strong>
            {target.quote ? <p>引用 「{target.quote}」</p> : null}
          </div>
          <div className="agent-request-tabs" role="tablist">
            {(Object.keys(labels) as RequestIntent[]).map((key) => (
              <button
                key={key}
                type="button"
                role="tab"
                aria-selected={intent === key}
                className={intent === key ? "active" : ""}
                onClick={() => {
                  setIntent(key);
                  setParent(null);
                  if (key === "explain")
                    setInstruction(target.quote ? `これを説明して: ${target.quote}` : "これを説明して");
                }}
              >
                {labels[key]}
              </button>
            ))}
          </div>
          {parent ? (
            <p className="agent-request-followup">
              「{parent.instruction}」への追加依頼。前の回答をコンテキストとして渡します。
            </p>
          ) : null}
          {intent === "update" ? (
            <p className="agent-request-note">
              このノートの本文に反映します: {target.note?.title ?? "更新先を選択してください"}
            </p>
          ) : null}
          <textarea
            aria-label="Instruction"
            value={instruction}
            onChange={(e) => setInstruction(e.target.value)}
            placeholder="指示を入力"
          />
          <label className="agent-request-agent">
            依頼先
            <select aria-label="Agent" value={agentID} onChange={(e) => setAgentID(e.target.value)}>
              <option value="">選択してください</option>
              {available.map((agent) => (
                <option key={agent.id} value={agent.id} disabled={!agent.agmsg_available}>
                  {agent.name || agent.id}
                  {agent.agmsg_available ? "" : "（利用不可）"}
                </option>
              ))}
            </select>
          </label>
          <button
            className="primary-button"
            type="button"
            disabled={
              create.isPending ||
              !instruction.trim() ||
              !agentID ||
              (intent === "update" && !target.note)
            }
            onClick={() => void send()}
          >
            {create.isPending ? "送信中…" : `${labels[intent]}を依頼`}
          </button>
        </>
      ) : null}
      <div className="agent-request-history">
        <div className="agent-request-history-title">
          <span className="label">依頼</span>
          <button
            type="button"
            className="text-button"
            onClick={() => {
              setClosed(false);
              if (!target) setTarget({ title: "依頼履歴" });
            }}
          >
            再表示
          </button>
        </div>
        {list.map((request) => (
          <RequestCard
            key={request.id}
            request={request}
            onSelect={() => {
              setSelected(request);
              setClosed(false);
              if (panel.current) panel.current.scrollTop = 0;
            }}
            onCancel={() => void cancel.mutate(request.id)}
            onRetry={() => void retry.mutate(request.id)}
            onFollowUp={() => openFollowUp(request)}
          />
        ))}
      </div>
    </aside>
  );
}

function ResultView({
  request,
  vault,
  answer,
  unresolved,
  uncertain,
  saved,
  saveTitle,
  saveVault,
  setSaveTitle,
  setSaveVault,
  onSave,
  saving,
  saveError,
  onFollowUp,
}: {
  request: AgentRequest;
  vault: string;
  answer?: string;
  unresolved: string[];
  uncertain: string[];
  saved?: NonNullable<NonNullable<AgentRequest["result"]>["saved"]>;
  saveTitle: string;
  saveVault: string;
  setSaveTitle: (v: string) => void;
  setSaveVault: (v: string) => void;
  onSave: () => void;
  saving: boolean;
  saveError: unknown;
  onFollowUp: () => void;
}) {
  const sources = request.result?.sources ?? [];
  const apply = request.result?.apply;
  const update = request.intent === "update";
  return (
    <section className="agent-request-result" aria-label="依頼結果">
      <div className="agent-request-result-heading">
        <span className="label">結果</span>
        <span className={`agent-request-status status-${request.status}`}>
          {statusLabels[request.status] ?? request.status}
        </span>
      </div>
      <p className="agent-request-result-instruction">{request.instruction}</p>
      {update ? (
        <>
          <section className="agent-request-proposal">
            <span className="label">提案本文</span>
            <pre>{request.result?.proposed_body || "（提案本文なし）"}</pre>
          </section>
          <section className="agent-request-update-result" aria-label="更新結果">
            <div className="agent-request-update-heading">
              <span className="label">反映結果</span>
              {apply?.applied_at ? (
                <time dateTime={apply.applied_at}>{readableDate(apply.applied_at)}</time>
              ) : null}
            </div>
            {apply?.reason || request.error ? (
              <p className="agent-request-update-reason">
                <strong>理由</strong>
                {apply?.reason || request.error}
              </p>
            ) : null}
            {apply?.before_body !== undefined || apply?.after_body !== undefined ? (
              <div className="agent-request-bodies">
                <div>
                  <span className="label">反映前</span>
                  <pre>{apply.before_body ?? "（本文なし）"}</pre>
                  {apply.before_etag ? <small>ETag: {apply.before_etag}</small> : null}
                </div>
                <div>
                  <span className="label">反映後</span>
                  <pre>{apply.after_body ?? "（反映されませんでした）"}</pre>
                  {apply.after_etag ? <small>ETag: {apply.after_etag}</small> : null}
                </div>
              </div>
            ) : (
              <p className="agent-request-no-apply">本文は反映されていません。</p>
            )}
          </section>
        </>
      ) : answer?.trim() ? (
        <AnswerView answer={answer} vault={vault} />
      ) : request.result?.proposed_body ? (
        <p>{request.result.proposed_body}</p>
      ) : null}
      {sources.length ? (
        <section>
          <span className="label">出典</span>
          <ul>
            {sources.map((source) => (
              <li key={source}>{source}</li>
            ))}
          </ul>
        </section>
      ) : null}
      {unresolved.length ? (
        <section>
          <span className="label">未解決</span>
          <ul>
            {unresolved.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </section>
      ) : null}
      {uncertain.length ? (
        <section>
          <span className="label">不確実な点</span>
          <ul>
            {uncertain.map((item) => (
              <li key={item}>{item}</li>
            ))}
          </ul>
        </section>
      ) : null}
      {request.status === "completed" && request.intent !== "update" ? (
        saved ? (
          <p className="agent-request-saved">
            ノートに保存済み:{" "}
            <a href={`/notes/${encodeURIComponent(String(saved.note_id))}`}>{saved.title}</a>
          </p>
        ) : (
          <div className="agent-request-save">
            <strong>回答をノートに保存</strong>
            <input
              aria-label="保存するノートのタイトル"
              value={saveTitle}
              onChange={(e) => setSaveTitle(e.target.value)}
              placeholder="ノートのタイトル"
            />
            <input
              aria-label="保存先 vault"
              value={saveVault}
              onChange={(e) => setSaveVault(e.target.value)}
              placeholder="保存先 vault（任意）"
            />
            <button
              className="primary-button"
              type="button"
              disabled={saving || !saveTitle.trim()}
              onClick={onSave}
            >
              {saving ? "保存中…" : "ノートに保存"}
            </button>
            {saveError ? (
              <p className="agent-request-error">保存できませんでした。もう一度お試しください。</p>
            ) : null}
          </div>
        )
      ) : null}
      <button
        className="text-button agent-request-follow-button"
        type="button"
        onClick={onFollowUp}
      >
        追加で依頼
      </button>
    </section>
  );
}

function AnswerView({ answer, vault }: { answer: string; vault: string }) {
  const rendered = useRenderQuery(answer, vault);
  if (rendered.isError) {
    return (
      <div role="alert" className="agent-request-error">
        回答を表示できませんでした。
        <button type="button" className="text-button" onClick={() => void rendered.refetch()}>
          表示を再試行
        </button>
      </div>
    );
  }
  if (!rendered.data || rendered.isPlaceholderData) {
    return <p role="status">回答を読み込み中…</p>;
  }
  return (
    <div className="agent-request-answer">
      <MarkdownView
        markdown={rendered.data.markdown}
        includes={rendered.data.includes}
        vault={vault}
      />
    </div>
  );
}

function RequestCard({
  request,
  onSelect,
  onCancel,
  onRetry,
  onFollowUp,
}: {
  request: AgentRequest;
  onSelect: () => void;
  onCancel: () => void;
  onRetry: () => void;
  onFollowUp: () => void;
}) {
  const cancellable = request.status === "queued" || request.status === "running";
  return (
    <article className="agent-request-card">
      <button type="button" className="request-card-main" onClick={onSelect}>
        <strong>{labels[request.intent]}</strong>
        <span>{statusLabels[request.status] ?? request.status}</span>
        <p>{request.instruction}</p>
      </button>
      {cancellable ? (
        <button type="button" className="text-button" onClick={onCancel}>
          取り消す
        </button>
      ) : null}
      {request.status === "failed" || request.status === "conflict" ? (
        <button type="button" className="text-button" onClick={onRetry}>
          再試行
        </button>
      ) : null}
      {request.result ? (
        <button type="button" className="text-button" onClick={onFollowUp}>
          追加で依頼
        </button>
      ) : null}
    </article>
  );
}
