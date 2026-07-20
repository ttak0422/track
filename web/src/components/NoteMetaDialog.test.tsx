import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import type { ReactElement } from "react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { NoteMetaDialog } from "./NoteMetaDialog";

// The dialog reads/writes through the api module; stub it so the test drives the component alone.
const getNoteMeta = vi.fn();
const saveNoteMeta = vi.fn();
const uploadAsset = vi.fn();
vi.mock("../api", () => ({
  getNoteMeta: (id: string) => getNoteMeta(id),
  saveNoteMeta: (id: string, req: unknown) => saveNoteMeta(id, req),
  uploadAsset: (file: File) => uploadAsset(file),
}));

const seedMeta = {
  title: "Alpha",
  kind: "note",
  tags: ["project", "draft"],
  description: "A summary.",
  image: "assets/old.png",
  icon: "\u{1F4DA}",
  props: "status: draft\n",
};

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
    uploadAsset.mockReset();
  });

  it("seeds each field from the fetched metadata", async () => {
    getNoteMeta.mockResolvedValue(seedMeta);
    renderDialog();
    await waitFor(() => expect(screen.getByLabelText("Title")).toHaveValue("Alpha"));
    expect(screen.getByLabelText("Tags")).toHaveValue("project, draft");
    expect(screen.getByLabelText("Description")).toHaveValue("A summary.");
    expect(screen.getByLabelText("Cover image")).toHaveValue("assets/old.png");
    expect(screen.getByLabelText("Icon")).toHaveValue("\u{1F4DA}");
    expect(screen.getByLabelText("Properties")).toHaveValue("status: draft\n");
  });

  it("sends the edited fields and closes on success", async () => {
    getNoteMeta.mockResolvedValue(seedMeta);
    saveNoteMeta.mockResolvedValue(seedMeta);
    const { onClose } = renderDialog();
    await waitFor(() => expect(screen.getByLabelText("Title")).toHaveValue("Alpha"));

    fireEvent.change(screen.getByLabelText("Title"), { target: { value: "Beta" } });
    // The client only comma-splits and trims; blank entries drop out and the engine dedups.
    fireEvent.change(screen.getByLabelText("Tags"), { target: { value: "go,  lua ," } });
    fireEvent.change(screen.getByLabelText("Description"), { target: { value: "New." } });
    fireEvent.change(screen.getByLabelText("Icon"), { target: { value: "\u{1F525}" } });
    fireEvent.change(screen.getByLabelText("Properties"), { target: { value: "rating: 8\n" } });
    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    await waitFor(() =>
      expect(saveNoteMeta).toHaveBeenCalledWith("n1", {
        title: "Beta",
        tags: ["go", "lua"],
        description: "New.",
        image: "assets/old.png",
        icon: "\u{1F525}",
        props: "rating: 8\n",
      }),
    );
    await waitFor(() => expect(onClose).toHaveBeenCalled());
  });

  it("disables the title for a journal note", async () => {
    getNoteMeta.mockResolvedValue({ ...seedMeta, kind: "journal", title: "2026-07-12" });
    renderDialog();
    await waitFor(() => expect(screen.getByLabelText("Title")).toHaveValue("2026-07-12"));
    expect(screen.getByLabelText("Title")).toBeDisabled();
    // The other fields stay editable.
    expect(screen.getByLabelText("Tags")).not.toBeDisabled();
  });

  it("surfaces a validation error and keeps the dialog open", async () => {
    getNoteMeta.mockResolvedValue(seedMeta);
    saveNoteMeta.mockRejectedValue(new Error("image \"assets/nope.png\" not found in the vault assets"));
    const { onClose } = renderDialog();
    await waitFor(() => expect(screen.getByLabelText("Title")).toHaveValue("Alpha"));

    fireEvent.click(screen.getByRole("button", { name: "Save" }));

    expect(await screen.findByText(/not found in the vault assets/)).toBeInTheDocument();
    expect(onClose).not.toHaveBeenCalled();
    expect(screen.getByRole("dialog")).toBeInTheDocument();
  });

  it("uploads a picked image and sets the cover-image field to the returned ref", async () => {
    getNoteMeta.mockResolvedValue(seedMeta);
    uploadAsset.mockResolvedValue({ ref: "assets/new.png" });
    renderDialog();
    await waitFor(() => expect(screen.getByLabelText("Cover image")).toHaveValue("assets/old.png"));

    const file = new File(["png"], "new.png", { type: "image/png" });
    const picker = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(picker, { target: { files: [file] } });

    await waitFor(() => expect(uploadAsset).toHaveBeenCalledWith(file));
    await waitFor(() => expect(screen.getByLabelText("Cover image")).toHaveValue("assets/new.png"));
  });

  it("keeps the existing ref when an upload fails", async () => {
    getNoteMeta.mockResolvedValue(seedMeta);
    uploadAsset.mockRejectedValue(new Error("image must be one of .png .jpg"));
    renderDialog();
    await waitFor(() => expect(screen.getByLabelText("Cover image")).toHaveValue("assets/old.png"));

    const file = new File(["x"], "bad.svg", { type: "image/svg+xml" });
    const picker = document.querySelector('input[type="file"]') as HTMLInputElement;
    fireEvent.change(picker, { target: { files: [file] } });

    expect(await screen.findByText(/must be one of/)).toBeInTheDocument();
    expect(screen.getByLabelText("Cover image")).toHaveValue("assets/old.png");
  });
});
