import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { NoteMetaDialog } from "./NoteMetaDialog";

// The dialog reads/writes through the api module; stub it so the test drives the component alone.
const getNoteMeta = vi.fn();
const saveNoteMeta = vi.fn();
vi.mock("../api", () => ({
  getNoteMeta: (id: string) => getNoteMeta(id),
  saveNoteMeta: (id: string, req: unknown) => saveNoteMeta(id, req),
}));

function renderDialog(onClose = vi.fn()): { onClose: () => void } {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  const ui: ReactElement = (
    <QueryClientProvider client={client}>
      <NoteMetaDialog noteID="n1" onClose={onClose} />
    </QueryClientProvider>
  );
  render(ui);
  return { onClose };
}

describe("NoteMetaDialog", () => {
  beforeEach(() => {
    getNoteMeta.mockReset();
    saveNoteMeta.mockReset();
  });

  it("seeds the editor from the fetched document", async () => {
    getNoteMeta.mockResolvedValue({ doc: "title: Alpha\ntags:\n  - a\n" });
    renderDialog();
    const editor = await screen.findByRole("textbox");
    await waitFor(() => expect(editor).toHaveValue("title: Alpha\ntags:\n  - a\n"));
  });

  it("submits the edited document and closes on success", async () => {
    getNoteMeta.mockResolvedValue({ doc: "title: Alpha\n" });
    saveNoteMeta.mockResolvedValue({ doc: "title: Beta\n" });
    const { onClose } = renderDialog();
    const editor = await screen.findByRole("textbox");
    await waitFor(() => expect(editor).toHaveValue("title: Alpha\n"));

    fireEvent.change(editor, { target: { value: "title: Beta\n" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() => expect(saveNoteMeta).toHaveBeenCalledWith("n1", { doc: "title: Beta\n" }));
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it("surfaces a validation error and keeps the dialog open", async () => {
    getNoteMeta.mockResolvedValue({ doc: "title: Alpha\n" });
    saveNoteMeta.mockRejectedValue(new Error("image: assets/nope.png is not a vault asset"));
    const { onClose } = renderDialog();
    const editor = await screen.findByRole("textbox");
    await waitFor(() => expect(editor).toHaveValue("title: Alpha\n"));

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(/is not a vault asset/)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });
});
