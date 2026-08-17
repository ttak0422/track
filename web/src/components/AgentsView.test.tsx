import { render, screen } from "@testing-library/react";
import type { ReactNode } from "react";
import { describe, expect, it, vi } from "vitest";
import { AgentsView } from "./AgentsView";

const agents = vi.hoisted(() => vi.fn());
const agentLog = vi.hoisted(() => vi.fn());

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children, ...props }: { children?: ReactNode; [key: string]: unknown }) => <a {...props}>{children}</a>,
}));

vi.mock("../queries", () => ({
  useAgents: () => agents(),
  useAgentLog: () => agentLog(),
}));

vi.mock("./MarkdownView", () => ({
  MarkdownView: ({ markdown }: { markdown: string }) => <div data-testid="transcript">{markdown}</div>,
}));

describe("AgentsView", () => {
  it("lists session names, statuses, and branches", () => {
    agents.mockReturnValue({
      data: {
        sessions: [
          { sessionId: "wait", pid: 1, cwd: "/tmp/a", startedAt: 1, procStart: "", version: "", kind: "interactive", name: "track-f2", updatedAt: Date.now(), status: "waiting", statusUpdatedAt: 1, branch: "feat/agents" },
          { sessionId: "busy", pid: 2, cwd: "/tmp/b", startedAt: 1, procStart: "", version: "", kind: "bg", name: "builder", updatedAt: Date.now(), status: "busy", statusUpdatedAt: 1, branch: "main" },
          { sessionId: "idle", pid: 3, cwd: "/tmp/c", startedAt: 1, procStart: "", version: "", kind: "interactive", name: "reader", updatedAt: Date.now(), status: "idle", statusUpdatedAt: 1 },
        ],
      },
    });
    agentLog.mockReturnValue({ data: { messages: [] } });

    render(<AgentsView />);

    expect(screen.getAllByText("track-f2").length).toBeGreaterThan(0);
    expect(screen.getByText("feat/agents")).toBeTruthy();
    expect(screen.getByText("要対応").className).toContain("agent-state-waiting");
    expect(screen.getByText("稼働").className).toContain("agent-state-busy");
    expect(screen.getByText("待機").className).toContain("agent-state-idle");
  });
});
