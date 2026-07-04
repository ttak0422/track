import { RouterProvider, createMemoryHistory } from "@tanstack/react-router";
import { QueryClient, dehydrate } from "@tanstack/react-query";
import { renderToString } from "react-dom/server";
import { getNote, getSite, renderMarkdown } from "./api";
import { AppTree, createAppRouter } from "./App";
import { queryKeys } from "./queries";

export interface RenderedPage {
  html: string;
  // Dehydrated react-query cache, inlined into the page so the client hydrates without refetching.
  state: string;
}

// renderPage renders one route to static HTML with its above-the-fold data. react-query fetches from
// component effects, which renderToString does not run, so we can't rely on a render pass to trigger
// them — instead prefetch the queries a route needs into the cache first, then render once. Only the
// primary content (site + the note body + its sanitized render) is prefetched; secondary data (graph,
// backlinks lists) stays pending in the prerender and hydrates on the client, matching the client's own
// first render. The caller provides a global fetch resolving the app's `data/<file>` requests.
export async function renderPage(routePath: string): Promise<RenderedPage> {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false, gcTime: Number.POSITIVE_INFINITY } },
  });

  await prefetchForRoute(queryClient, routePath);

  const history = createMemoryHistory({ initialEntries: [routePath] });
  const router = createAppRouter(history, { isServer: true });
  await router.load();

  // The static client mounts with createRoot (not hydration — see main.tsx), so this HTML only needs to
  // paint the right content for a fast first paint; the client re-renders over it from the same
  // (dehydrated) data. That sidesteps TanStack Router's SSR hydration handshake, whose client-only
  // Suspense wrapper cannot be reproduced by a standalone prerender.
  const html = renderToString(
    <AppTree queryClient={queryClient}>
      <RouterProvider router={router} />
    </AppTree>,
  );

  return { html, state: JSON.stringify(dehydrate(queryClient)) };
}

// prefetchForRoute seeds the cache with the data a route renders above the fold. The site entry is
// always useful (the sidebar/start-page link reads it); a note route — and "/", which renders the start
// note — additionally prefetches the note and its rendered markdown so the reader paints the body on the
// first render.
async function prefetchForRoute(queryClient: QueryClient, routePath: string): Promise<void> {
  // The site query key Shell uses is a bare ["site"].
  await queryClient.prefetchQuery({ queryKey: ["site"], queryFn: getSite });

  const noteMatch = routePath.match(/^\/notes\/([^/?#]+)/);
  let noteID = noteMatch ? decodeURIComponent(noteMatch[1]) : "";
  if (routePath === "/") {
    // "/" renders the start note (see HomeRoute); prefetch it too.
    noteID = queryClient.getQueryData<{ root: string }>(["site"])?.root ?? "";
  }
  if (noteID === "") return;

  await queryClient.prefetchQuery({ queryKey: queryKeys.note(noteID), queryFn: () => getNote(noteID) });
  const body = queryClient.getQueryData<{ note: { body: string } }>(queryKeys.note(noteID))?.note.body ?? "";
  if (body.trim() !== "") {
    await queryClient.prefetchQuery({ queryKey: queryKeys.render(body), queryFn: () => renderMarkdown(body) });
  }
}
