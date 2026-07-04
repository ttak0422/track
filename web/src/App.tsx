import {
  type RouterHistory,
  RouterProvider,
  createBrowserHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider, hydrate } from "@tanstack/react-query";
import type { ReactNode } from "react";
import { START_PAGE_ID, STATIC_MODE } from "./runtime";
import { ActivityPanel } from "./components/ActivityPanel";
import { EmptyState } from "./components/EmptyState";
import { GraphFullView } from "./components/GraphFullView";
import { TrackLogo } from "./components/Logo";
import { NoteReader } from "./components/NoteReader";
import { SearchPanel } from "./components/SearchPanel";
import { Shell } from "./components/Shell";
import "./styles.css";

const rootRoute = createRootRoute({
  component: Shell,
});

const indexRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/",
  component: HomeRoute,
});

const noteRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/notes/$noteId",
  component: NoteRoute,
});

const graphRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/graph",
  component: GraphRoute,
});

// The static site's empty state (reached by closing every tab) has its own route so it is a real
// prerendered file, rather than sharing "/" — which is the start page.
const emptyRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/empty",
  component: () => <EmptyState />,
});

const routeTree = rootRoute.addChildren([indexRoute, noteRoute, graphRoute, emptyRoute]);

// The router basepath (and asset URLs) come from the build-time base (import.meta.env.BASE_URL), so the
// prerender and the hydrating client agree on link paths even under a GitHub Pages subpath. Trailing
// slash stripped: "/" → "", "/repo/" → "/repo".
const basepath = import.meta.env.BASE_URL.replace(/\/$/, "");

// createAppRouter builds a router over the shared route tree. The static site is path-routed and
// prerendered (every route is a real file), so it uses browser history like the live server; the
// prerender (entry-server) passes a memory history for the URL being rendered. Kept a factory so the
// client and each SSR render get their own instance.
export function createAppRouter(history?: RouterHistory, opts?: { isServer?: boolean }) {
  return createRouter({
    routeTree,
    basepath: STATIC_MODE ? basepath : undefined,
    // The prerender forces isServer so the router renders in server mode despite jsdom providing a
    // document (which would otherwise make it think it is on the client).
    ...(opts?.isServer ? { isServer: true } : {}),
    ...(history ? { history } : STATIC_MODE ? { history: createBrowserHistory() } : {}),
  });
}

declare module "@tanstack/react-router" {
  interface Register {
    router: ReturnType<typeof createAppRouter>;
  }
}

// The client's router is created lazily (not at module scope) so importing this module for the prerender
// (entry-server) does not run the browser/hash history, which touches window and would crash in Node.
let clientRouter: ReturnType<typeof createAppRouter> | null = null;

// AppTree is the provider stack shared by the live client (App), the prerender (entry-server via
// RouterServer), and the static client hydration (main.tsx via RouterClient). The router element is
// passed as children so each entry supplies the SSR-appropriate variant — RouterServer and RouterClient
// coordinate the same Suspense boundaries so prerender and hydration agree, which a plain RouterProvider
// on both sides does not.
export function AppTree({ queryClient, children }: { queryClient: QueryClient; children: ReactNode }) {
  return <QueryClientProvider client={queryClient}>{children}</QueryClientProvider>;
}

// The client's shared query client. On the static site the prerender inlines its dehydrated react-query
// cache as window.__TRACK_STATE__; hydrate it here so the client reuses the prerendered content instead
// of refetching and flashing.
export const queryClient = new QueryClient();
if (typeof window !== "undefined" && window.__TRACK_STATE__) {
  hydrate(queryClient, window.__TRACK_STATE__);
}

// clientAppRouter returns the client's singleton router (created lazily so importing this module for the
// prerender does not construct browser history in Node). main.tsx awaits its load() before hydrating the
// static site so the first client render matches the prerendered (already-loaded) markup.
export function clientAppRouter() {
  clientRouter ??= createAppRouter();
  return clientRouter;
}

export function App() {
  return (
    <AppTree queryClient={queryClient}>
      <RouterProvider router={clientAppRouter()} />
    </AppTree>
  );
}

function HomeRoute() {
  // The published site's "/" is the start page: it renders the configured root note directly, so the
  // prerendered index.html carries real content (fast FCP/LCP) instead of an empty shell that redirects.
  // The empty state lives at /empty (reached by closing every tab). The live workspace shows the heatmap
  // home here instead.
  if (STATIC_MODE) {
    return START_PAGE_ID ? <NoteReader noteID={START_PAGE_ID} /> : <EmptyState />;
  }
  return (
    <section className="home-hero">
      <TrackLogo className="home-logo" />
      <ActivityPanel variant="home" />
      <SearchPanel />
    </section>
  );
}

function NoteRoute() {
  const { noteId } = noteRoute.useParams();

  // Ids are opaque (numeric in live mode, base62 slugs in the static site), so just require a non-empty
  // param rather than parsing a number.
  if (!noteId) {
    return <p className="error">Invalid note id: {noteId}</p>;
  }

  return <NoteReader noteID={noteId} />;
}

function GraphRoute() {
  return <GraphFullView />;
}
