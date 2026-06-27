import { useEffect } from "react";
import {
  RouterProvider,
  createHashHistory,
  createRootRoute,
  createRoute,
  createRouter,
  useNavigate,
} from "@tanstack/react-router";
import { QueryClient, QueryClientProvider, useQuery } from "@tanstack/react-query";
import { getSite } from "./api";
import { STATIC_MODE } from "./runtime";
import { ActivityPanel } from "./components/ActivityPanel";
import { GraphFullView } from "./components/GraphFullView";
import { TrackLogo } from "./components/Logo";
import { NoteReader } from "./components/NoteReader";
import { SearchPanel } from "./components/SearchPanel";
import { Shell } from "./components/Shell";
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
  // The published site has no heatmap home; its entry point is the root note, so redirect there.
  if (STATIC_MODE) {
    return <StaticHome />;
  }
  return (
    <section className="home-hero">
      <TrackLogo className="home-logo" />
      <ActivityPanel variant="home" />
      <SearchPanel />
    </section>
  );
}

function StaticHome() {
  const navigate = useNavigate();
  const { data, isError } = useQuery({ queryKey: ["site"], queryFn: getSite });

  useEffect(() => {
    if (data?.root) {
      void navigate({
        to: "/notes/$noteId",
        params: { noteId: String(data.root) },
        replace: true,
      });
    }
  }, [data, navigate]);

  if (isError) {
    return <p className="error">site data is missing</p>;
  }
  return (
    <section className="home-hero">
      <TrackLogo className="home-logo" />
    </section>
  );
}

function NoteRoute() {
  const { noteId } = noteRoute.useParams();
  const parsed = Number(noteId);

  if (!Number.isSafeInteger(parsed) || parsed <= 0) {
    return <p className="error">Invalid note id: {noteId}</p>;
  }

  return <NoteReader noteID={parsed} />;
}

function GraphRoute() {
  const navigate = useNavigate();
  return <GraphFullView onClose={() => void navigate({ to: "/" })} />;
}
