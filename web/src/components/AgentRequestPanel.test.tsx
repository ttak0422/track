import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { AgentRequestPanel, openAgentRequest } from "./AgentRequestPanel";

const create = vi.hoisted(() => vi.fn(async () => ({ request: { id: "req-1", intent: "explain", instruction: "", agent_id: "research", status: "queued" } })));
const refetch = vi.hoisted(() => vi.fn());

vi.mock("../runtime", () => ({ STATIC_MODE: false }));
vi.mock("../queries", () => ({
  useAgentsQuery: () => ({ data: { agents: [{ id: "research", name: "Research", operations: ["explain"], agmsg_available: true }] } }),
  useAgentRequestsQuery: () => ({ data: { requests: [] }, refetch }),
  useCreateAgentRequestMutation: () => ({ mutateAsync: create, isPending: false }),
  useCancelAgentRequestMutation: () => ({ mutate: vi.fn() }),
  useRetryAgentRequestMutation: () => ({ mutate: vi.fn() }),
  useSaveAgentRequestMutation: () => ({ mutateAsync: vi.fn(), isPending: false, error: null }),
}));

describe("AgentRequestPanel", () => {
  beforeEach(() => {
    create.mockClear();
    refetch.mockClear();
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
    await waitFor(() => expect(create).toHaveBeenCalledWith(expect.objectContaining({
      intent: "explain",
      instruction: "Explain this",
      agent_id: "research",
      context: expect.objectContaining({ quote: "the selected passage" }),
    })));
  });
});
