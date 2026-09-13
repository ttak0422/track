import { useEffect, useMemo, useRef, useState } from "react";
import { STATIC_MODE } from "../runtime";
import { useAgentsQuery, useAgentRequestsQuery, useCancelAgentRequestMutation, useCreateAgentRequestMutation, useRetryAgentRequestMutation } from "../queries";
import type { AgentRequest, RequestIntent, RequestNoteRef } from "../types";
import { IconX, RailIcon } from "./icons";
import "./agent-request.css";

export interface RequestTarget { vault?: string; title: string; quote?: string; note?: RequestNoteRef; intent?: RequestIntent }
export function openAgentRequest(target: RequestTarget) { window.dispatchEvent(new CustomEvent("track:open-agent-request", { detail: target })); }

const labels: Record<RequestIntent, string> = { explain: "説明", research: "調査", update: "更新" };
const statusLabels: Record<string, string> = { queued: "開始待ち", running: "実行中", applying: "反映中", completed: "完了", failed: "失敗", conflict: "競合", cancelled: "取消" };

export function AgentRequestPanel() {
  const [target, setTarget] = useState<RequestTarget | null>(null);
  const [intent, setIntent] = useState<RequestIntent>("explain");
  const [instruction, setInstruction] = useState("");
  const [agentID, setAgentID] = useState("");
  const [selected, setSelected] = useState<AgentRequest | null>(null);
  const [closed, setClosed] = useState(false);
  const returnFocus = useRef<HTMLElement | null>(null);
  const vault = target?.vault ?? "";
  const agents = useAgentsQuery(!!target);
  const requests = useAgentRequestsQuery(vault, !!target || !closed);
  const create = useCreateAgentRequestMutation(vault);
  const cancel = useCancelAgentRequestMutation(vault);
  const retry = useRetryAgentRequestMutation(vault);
  const agentList = agents.data?.agents ?? [];
  const available = useMemo(() => agentList.filter((a) => a.operations?.length === 0 || a.operations?.includes(intent)), [agentList, intent]);

  useEffect(() => {
    if (agentID === "" && available[0]) setAgentID(available[0].id);
  }, [available, agentID]);
  useEffect(() => {
    const open = (event: Event) => {
      const custom = event as CustomEvent<RequestTarget>;
      returnFocus.current = document.activeElement as HTMLElement;
      setTarget(custom.detail); setIntent(custom.detail.intent ?? "explain"); setInstruction(custom.detail.intent === "research" ? (custom.detail.quote ?? "調べたいことを入力") : custom.detail.quote ? `これを説明して: ${custom.detail.quote}` : "これを説明して"); setSelected(null); setClosed(false);
    };
    window.addEventListener("track:open-agent-request", open);
    return () => window.removeEventListener("track:open-agent-request", open);
  }, []);
  useEffect(() => { if (target && !closed) requests.refetch(); }, [target, closed]);
  useEffect(() => () => returnFocus.current?.focus(), []);
  if (STATIC_MODE) return null;

  const list = requests.data?.requests ?? [];
  const send = async () => {
    if (!target || !instruction.trim() || !agentID || (intent === "update" && !target.note)) return;
    const clientRequestID = crypto.randomUUID();
    const body: Record<string, unknown> = { client_request_id: clientRequestID, intent, instruction: instruction.trim(), agent_id: agentID, context: { quote: target.quote ?? "", note: target.note } };
    if (intent === "update") body.update_target = target.note;
    const result = await create.mutateAsync(body);
    setSelected(result.request); setClosed(false);
  };
  const close = () => { setClosed(true); returnFocus.current?.focus(); };
  return (
    <aside className={`agent-request-panel${target && !closed ? " open" : ""}`} aria-label="Agent request">
      <div className="agent-request-heading"><div><span className="label">AGENT REQUESTS</span><h2>エージェントに依頼</h2></div><button className="graph-reset agent-request-close" type="button" aria-label="Close" onClick={close}><RailIcon Icon={IconX} size={15} /></button></div>
      {target && !closed ? <>
        <div className="agent-request-target"><span className="label">対象</span><strong>{target.title}</strong>{target.quote ? <p>引用 「{target.quote}」</p> : null}</div>
        <div className="agent-request-tabs" role="tablist">{(Object.keys(labels) as RequestIntent[]).map((key) => <button key={key} type="button" role="tab" aria-selected={intent === key} className={intent === key ? "active" : ""} onClick={() => { setIntent(key); if (key === "explain") setInstruction(target.quote ? `これを説明して: ${target.quote}` : "これを説明して"); }}>{labels[key]}</button>)}</div>
        {intent === "update" ? <p className="agent-request-note">このノートの本文に反映します: {target.note?.title ?? "更新先を選択してください"}</p> : null}
        <textarea aria-label="Instruction" value={instruction} onChange={(e) => setInstruction(e.target.value)} placeholder="指示を入力" />
        <label className="agent-request-agent">依頼先<select aria-label="Agent" value={agentID} onChange={(e) => setAgentID(e.target.value)}><option value="">選択してください</option>{available.map((agent) => <option key={agent.id} value={agent.id} disabled={!agent.agmsg_available}>{agent.name || agent.id}{agent.agmsg_available ? "" : "（利用不可）"}</option>)}</select></label>
        <button className="primary-button" type="button" disabled={create.isPending || !instruction.trim() || !agentID || (intent === "update" && !target.note)} onClick={() => void send()}>{create.isPending ? "送信中…" : `${labels[intent]}を依頼`}</button>
      </> : null}
      <div className="agent-request-history"><div className="agent-request-history-title"><span className="label">依頼</span><button type="button" className="text-button" onClick={() => { setClosed(false); if (!target) setTarget({ title: "依頼履歴" }); }}>再表示</button></div>
        {list.map((request) => <RequestCard key={request.id} request={request} onSelect={() => { setSelected(request); setClosed(false); }} onCancel={() => void cancel.mutate(request.id)} onRetry={() => void retry.mutate(request.id)} />)}
      </div>
      {selected?.result ? <div className="agent-request-result"><span className="label">結果</span><p>{selected.result.answer_markdown || selected.result.proposed_body}</p></div> : null}
    </aside>
  );
}

function RequestCard({ request, onSelect, onCancel, onRetry }: { request: AgentRequest; onSelect: () => void; onCancel: () => void; onRetry: () => void }) {
  const pending = request.status === "queued" || request.status === "running" || request.status === "applying";
  return <article className="agent-request-card"><button type="button" className="request-card-main" onClick={onSelect}><strong>{labels[request.intent]}</strong><span>{statusLabels[request.status] ?? request.status}</span><p>{request.instruction}</p></button>{pending ? <button type="button" className="text-button" onClick={onCancel}>取り消す</button> : null}{request.status === "failed" || request.status === "conflict" ? <button type="button" className="text-button" onClick={onRetry}>再試行</button> : null}</article>;
}
