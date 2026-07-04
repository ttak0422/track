import {
  type RouterHistory,
  RouterProvider,
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { STATIC_MODE } from "./runtime";
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

const routeTree = rootRoute.addChildren([indexRoute, noteRoute, graphRoute]);

// createAppRouter builds a router over the shared route tree. The client leaves history undefined (Tan
// Stack defaults to browser history) except the static site, which used hash history so deep links stay
// inside one index.html on a fallback-less host. The prerender (entry-server) passes a memory history for
// a specific URL. Kept a factory so client and SSR each get their own instance.
export function createAppRouter(history?: RouterHistory) {
  return createRouter({
    routeTree,
    ...(history ? { history } : STATIC_MODE ? { history: createHashHistory() } : {}),
  });
}

// The client's singleton router, and the type the Register interface binds to.
const router = createAppRouter();

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

// AppTree is the provider stack parameterized by a router and query client, shared by the client entry
// (App) and the prerender (entry-server), so both render an identical tree.
export function AppTree({
  router: r,
  queryClient,
}: {
  router: ReturnType<typeof createAppRouter>;
  queryClient: QueryClient;
}) {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={r} />
    </QueryClientProvider>
  );
}

const queryClient = new QueryClient();

export function App() {
  return <AppTree router={router} queryClient={queryClient} />;
}

function HomeRoute() {
  // The published site has no heatmap home. It opens the start page on launch (see Shell); this route is
  // only reached once every tab is closed, where the empty reader shows a faint mark and pointers to the
  // sidebar so the blank area reads as "nothing open".
  if (STATIC_MODE) {
    return <EmptyState />;
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
