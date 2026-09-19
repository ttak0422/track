import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { FloatingProvider } from "./preview/floatingStore";
import type { RenderResponse } from "../types";
import { AgentRequestPanel, openAgentRequest } from "./AgentRequestPanel";

const create = vi.hoisted(() =>
  vi.fn(async () => ({
    request: {
      id: "req-1",
      intent: "explain",
      instruction: "",
      agent_id: "research",
      status: "queued",
    },
  })),
);
const renderQuery = vi.hoisted(() => vi.fn());
const resolveQuery = vi.hoisted(() => vi.fn(() => ({ data: { found: false }, isPending: false })));
const refetch = vi.hoisted(() => vi.fn());
const requests = vi.hoisted(() => ({ requests: [] as Array<Record<string, unknown>> }));

vi.mock("@tanstack/react-router", () => ({ useRouterState: () => "/" }));
vi.mock("../runtime", () => ({ STATIC_MODE: false }));
vi.mock("../queries", () => ({
  useRenderQuery: renderQuery,
  useResolveQuery: resolveQuery,
  useAgentsQuery: () => ({
    data: {
      agents: [
        { id: "research", name: "Research", operations: ["explain"], agmsg_available: true },
      ],
    },
  }),
  useAgentRequestsQuery: () => ({ data: requests, refetch }),
  useCreateAgentRequestMutation: () => ({ mutateAsync: create, isPending: false }),
  useCancelAgentRequestMutation: () => ({ mutate: vi.fn() }),
  useRetryAgentRequestMutation: () => ({ mutate: vi.fn() }),
  useSaveAgentRequestMutation: () => ({ mutateAsync: vi.fn(), isPending: false, error: null }),
}));

describe("AgentRequestPanel", () => {
  beforeEach(() => {
    create.mockClear();
    refetch.mockClear();
    requests.requests = [];
    renderQuery.mockReset();
    renderQuery.mockReturnValue({ data: undefined, isError: false });
    resolveQuery.mockClear();
  });

  it("sends only after the explicit action and fixes the opened quote", async () => {
    render(<AgentRequestPanel />);
    openAgentRequest({ title: "A note", quote: "the selected passage" });

    expect(await screen.findByRole("heading", { name: "エージェントに依頼" })).toBeInTheDocument();
    const instruction = screen.getByRole("textbox", { name: "Instruction" });
    expect(instruction).toHaveValue("これを説明して: the selected passage");

    fireEvent.change(instruction, { target: { value: "Explain this" } });
    fireEvent.compositionStart(instruction);
    fireEvent.compositionEnd(instruction);
    expect(create).not.toHaveBeenCalled();

    fireEvent.click(screen.getByRole("button", { name: "説明を依頼" }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(
        expect.objectContaining({
          intent: "explain",
          instruction: "Explain this",
          agent_id: "research",
          context: expect.objectContaining({ quote: "the selected passage" }),
        }),
      ),
    );
  });

  it("keeps an update proposal and shows the applied result without offering cancellation after applying", async () => {
    requests.requests = [
      {
        id: "update-1",
        intent: "update",
        instruction: "Rewrite it",
        agent_id: "writer",
        status: "conflict",
        error: "The note changed while the request was running.",
        result: {
          proposed_body: "# Proposed",
          apply: {
            before_body: "# Before",
            before_etag: "etag-a",
            reason: "ETag mismatch",
            applied_at: "2026-09-13T10:00:00Z",
          },
        },
      },
    ];
    render(<AgentRequestPanel />);
    openAgentRequest({ title: "A note" });

    fireEvent.click(await screen.findByText("Rewrite it"));
    expect(screen.getByText("# Proposed")).toBeInTheDocument();
    expect(screen.getByText("# Before")).toBeInTheDocument();
    expect(screen.getByText("ETag mismatch")).toBeInTheDocument();
    expect(screen.getByText(/反映されませんでした/)).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "取り消す" })).not.toBeInTheDocument();
  });

  it("reads sanitized Markdown before the composer and history, retaining the request vault", async () => {
    const answer = "# Raw answer\n[action](track:unsafe)";
    const sanitized = "## Findings\n\n**Evidence** and [[Reference]]\n\n| Source | Result |\n| --- | --- |\n| A | B |\n\n```text\nexample\n```\n\n- [ ] Review\n\n[Source](https://example.com)\n\naction";
    renderQuery.mockReturnValue({ data: { markdown: sanitized }, isError: false });
    requests.requests = [{
      id: "answer-1", vault: "research", intent: "research", status: "completed",
      instruction: "Compare the evidence", agent_id: "research",
      result: { answer_markdown: answer, sources: ["Source A"], unresolved: ["Open question"], uncertain: ["Limited sample"] },
    }];
    render(<FloatingProvider><AgentRequestPanel /></FloatingProvider>);
    openAgentRequest({ title: "A note", vault: "research" });
    fireEvent.click(await screen.findByText("Compare the evidence"));

    const result = screen.getByRole("region", { name: "依頼結果" });
    expect(within(result).getByRole("heading", { name: "Findings" })).toBeInTheDocument();
    expect(within(result).getByRole("table")).toHaveTextContent("SourceResultAB");
    expect(within(result).getByRole("button", { name: "Copy code" })).toBeInTheDocument();
    expect(within(result).getByRole("checkbox")).toBeDisabled();
    expect(within(result).getByRole("link", { name: "Source" })).toHaveAttribute("href", "https://example.com");
    expect(within(result).queryByRole("link", { name: "action" })).not.toBeInTheDocument();
    expect(renderQuery).toHaveBeenCalledWith(answer, "research");
    expect(resolveQuery).toHaveBeenCalledWith("Reference", "research");
    for (const text of ["出典", "Source A", "未解決", "Open question", "不確実な点", "Limited sample", "回答をノートに保存"]) {
      expect(within(result).getByText(text)).toBeInTheDocument();
    }
    const composer = screen.getByRole("textbox", { name: "Instruction" });
    const history = screen.getByText("再表示");
    expect(result.compareDocumentPosition(composer) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    expect(result.compareDocumentPosition(history) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
    const panel = screen.getByRole("complementary", { name: "Agent request" });
    expect(panel).toHaveClass("has-result");
    panel.scrollTop = 500;
    fireEvent.click(screen.getByRole("button", { name: /Compare the evidence/ }));
    expect(panel.scrollTop).toBe(0);
    fireEvent.click(within(result).getByRole("button", { name: "追加で依頼" }));
    expect(screen.queryByRole("region", { name: "依頼結果" })).not.toBeInTheDocument();
    expect(composer).toHaveValue("");
    expect(panel).not.toHaveClass("has-result");
  });

  it("shows loading and retry instead of raw or stale answers, then renders the response", async () => {
    const retryRender = vi.fn();
    const rendered: { data?: RenderResponse; isError: boolean; isPlaceholderData: boolean; refetch: typeof retryRender } = {
      data: undefined, isError: false, isPlaceholderData: false, refetch: retryRender,
    };
    renderQuery.mockImplementation(() => rendered);
    requests.requests = [{ id: "answer-2", intent: "explain", status: "completed", instruction: "Explain it", result: { answer_markdown: "# Raw answer" } }];
    const view = render(<AgentRequestPanel />);
    openAgentRequest({ title: "A note", vault: "research" });
    fireEvent.click(await screen.findByText("Explain it"));
    expect(screen.getByRole("status")).toHaveTextContent("回答を読み込み中");
    expect(screen.queryByText("# Raw answer")).not.toBeInTheDocument();
    expect(renderQuery).toHaveBeenCalledWith("# Raw answer", "research");

    rendered.data = { markdown: "# Previous answer" };
    rendered.isPlaceholderData = true;
    view.rerender(<AgentRequestPanel />);
    expect(screen.queryByRole("heading", { name: "Previous answer" })).not.toBeInTheDocument();
    expect(screen.getByRole("status")).toBeInTheDocument();

    rendered.isError = true;
    view.rerender(<AgentRequestPanel />);
    expect(screen.getByRole("alert")).toHaveTextContent("回答を表示できませんでした");
    fireEvent.click(screen.getByRole("button", { name: "表示を再試行" }));
    expect(retryRender).toHaveBeenCalledOnce();

    rendered.isError = false;
    rendered.isPlaceholderData = false;
    rendered.data = { markdown: "# Current answer" };
    view.rerender(<AgentRequestPanel />);
    expect(screen.getByRole("heading", { name: "Current answer" })).toBeInTheDocument();
    expect(screen.queryByRole("status")).not.toBeInTheDocument();
    expect(screen.queryByRole("alert")).not.toBeInTheDocument();
  });
});
