import {
  RouterProvider,
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { STATIC_MODE } from "./runtime";
import { ActivityPanel } from "./components/ActivityPanel";
import { GraphFullView } from "./components/GraphFullView";
import { TrackLogo } from "./components/Logo";
import { NoteReader } from "./components/NoteReader";
import { SearchPanel } from "./components/SearchPanel";
import { Shell } from "./components/Shell";
import { Welcome } from "./components/Welcome";
import "katex/dist/katex.min.css";
import "./styles.css";

const queryClient = new QueryClient();

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

// Static sites are served from plain file hosts (GitHub Pages) that have no SPA fallback, so deep links
// would 404 under browser history. Hash history keeps every route inside index.html.
const router = createRouter({
  routeTree,
  ...(STATIC_MODE ? { history: createHashHistory() } : {}),
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

export function App() {
  return (
    <QueryClientProvider client={queryClient}>
      <RouterProvider router={router} />
    </QueryClientProvider>
  );
}

function HomeRoute() {
  // The published site has no heatmap home; it lands on a welcome screen (its own entry points), while
  // the live workspace shows the activity heatmap.
  if (STATIC_MODE) {
    return <Welcome />;
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
