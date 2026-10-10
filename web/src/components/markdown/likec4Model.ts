import { createLanguageServices, NoFileSystem } from "@likec4/language-server/browser";
import { URI } from "langium";
import { maxLikeC4SourceLength, maxLikeC4ViewNodes, maxLikeC4Views, type LikeC4View } from "./likec4Types";

// Browser services have no filesystem, project configuration, watcher, or external renderer. Each
// fence is an isolated in-memory project. The worker is terminated when its result is delivered.
export async function renderLikeC4Model(text: string): Promise<LikeC4View[]> {
  if (text.length > maxLikeC4SourceLength) {
    throw new Error(`Source exceeds ${maxLikeC4SourceLength.toLocaleString("en-US")} characters.`);
  }
  const { shared, likec4 } = createLanguageServices({ ...NoFileSystem });
  const language = likec4.likec4.LanguageServices;
  try {
    const folder = { name: "diagram", uri: "file:///diagram" };
    shared.workspace.WorkspaceManager.initialize({
      capabilities: {}, processId: null, rootUri: folder.uri, workspaceFolders: [folder],
    });
    await shared.workspace.WorkspaceManager.initializeWorkspace([folder]);
    // Disable implicit per-element views; only the views declared by the note belong in its selector.
    await shared.workspace.ProjectsManager.registerProject({
      folderUri: URI.parse(folder.uri),
      config: { name: "diagram", implicitViews: false, exclude: [] },
    });
    const document = shared.workspace.LangiumDocumentFactory.fromString(text, URI.parse(`${folder.uri}/main.c4`));
    shared.workspace.LangiumDocuments.addDocument(document);
    await shared.workspace.DocumentBuilder.build([document], { validation: true });
    const errors = document.diagnostics?.filter((diagnostic) => diagnostic.severity === 1) ?? [];
    if (errors.length) {
      throw new Error(errors.slice(0, 5).map((error) => `Line ${error.range.start.line + 1}: ${error.message}`).join("\n"));
    }

    const model = await language.computedModel();
    // LikeC4 always supplies an index landscape even when implicitViews is false. Synthesized
    // views have no source path; keep only views declared in this document.
    const views = Object.values(model.$data.views).filter((view) => view.sourcePath !== undefined);
    if (views.length === 0) throw new Error("Define at least one view in a views block.");
    if (views.length > maxLikeC4Views) throw new Error(`A diagram supports at most ${maxLikeC4Views} views.`);
    const rendered: LikeC4View[] = [];
    for (const view of views) {
      if (view.nodes.length > maxLikeC4ViewNodes) {
        throw new Error(`View ${view.id} exceeds ${maxLikeC4ViewNodes} elements.`);
      }
      // Render sequentially and propagate every error. viewsAsGraphvizOut intentionally omits
      // failed views, which would make an incomplete architecture look successful here.
      // This SVG renderer has no icon assets; remove their placeholder padding as well. Icons and
      // node navigation belong to LikeC4's full app, not to Track's inert note diagrams.
      const svgView = { ...view, nodes: view.nodes.map((node) => ({ ...node, icon: undefined })) };
      const { svg } = await language.views.layouter.svg({ view: svgView, styles: model.$styles });
      rendered.push({ id: view.id, title: view.title || view.id, svg });
    }
    return rendered;
  } finally {
    await language.dispose();
  }
}
