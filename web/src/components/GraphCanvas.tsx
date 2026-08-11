import {
  forceLink,
  forceManyBody,
  forceSimulation,
  forceX,
  forceY,
  type Simulation,
  type SimulationLinkDatum,
  type SimulationNodeDatum,
} from "d3-force";
import { PointerEvent, useEffect, useRef, useState } from "react";
import { useThemeVersion } from "../hooks/useThemeVersion";
import type { Graph, GraphEdge, GraphNode, NoteID } from "../types";
import { isZoomWheel, zoomDelta } from "./graphWheel";

export interface GraphCanvasProps {
  graph: Graph;
  onSelect: (noteID: NoteID, point: Point) => void;
  // Fires when the hovered node changes (null when the pointer leaves all nodes). The point is in
  // viewport coordinates so callers can anchor a preview beside the cursor. Optional: only the full
  // graph view drives a hover preview.
  onHover?: (noteID: NoteID | null, viewportPoint: Point) => void;
  resetToken: number;
  // Background decoration: draw nodes/edges only, no labels or interaction.
  decorative?: boolean;
  // When set, only these nodes are drawn at full strength (accent); the rest dim in place. null draws
  // every node normally. Used by the home search to highlight matches without dropping the others.
  highlightIds?: ReadonlySet<NoteID> | null;
  // When set, the automatic initial/reset view follows this node instead of fitting the whole graph.
  focusNodeID?: NoteID;
}

// How long the layout may run inline before it owes the frame back. Half a 60fps frame: a note's
// local graph settles inside one slice and never blinks, and a big one costs no more than this
// before the browser gets the thread back.
const SETTLE_BUDGET_MS = 8;
// Ceiling on settling work, so a graph too large to converge still appears instead of staying blank.
const SETTLE_TICK_CAP = 400;
// While the view still follows the layout automatically. Reached the moment the layout settles, so
// the fit happens once rather than every tick — dragging a node later never yanks the camera.
const FIT_FOLLOW_TICKS = 150;

// requestIdleCallback is not everywhere (older Safari, jsdom). A macrotask is close enough for work
// nobody is waiting on; the deadline it fabricates just spends the same budget as the first slice.
function requestIdle(run: (deadline: IdleDeadline) => void): number {
  if (typeof requestIdleCallback === "function") return requestIdleCallback(run);
  return window.setTimeout(() => run({ didTimeout: true, timeRemaining: () => SETTLE_BUDGET_MS }), 0);
}

function cancelIdle(handle: number) {
  if (typeof cancelIdleCallback === "function") cancelIdleCallback(handle);
  else window.clearTimeout(handle);
}

interface SimNode extends GraphNode, SimulationNodeDatum {
  x: number;
  y: number;
  vx: number;
  vy: number;
  degree: number;
}

interface SimEdge extends SimulationLinkDatum<SimNode> {
  source: SimNode;
  target: SimNode;
}

interface GraphView {
  x: number;
  y: number;
  scale: number;
}

interface Point {
  x: number;
  y: number;
}

// pinchGeometry reduces the first two active pointers to the midpoint and distance a pinch is made of.
function pinchGeometry(points: Point[]): { mid: Point; dist: number } {
  const [a, b] = points;
  return {
    mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 },
    dist: Math.hypot(a.x - b.x, a.y - b.y),
  };
}

interface DragState {
  pointerId: number;
  start: Point;
  last: Point;
  moved: boolean;
  // When set, the drag moves this node (with elastic edges) instead of panning the view.
  node?: SimNode;
}

export function GraphCanvas({
  graph,
  onSelect,
  onHover,
  resetToken,
  decorative = false,
  highlightIds = null,
  focusNodeID,
}: GraphCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const nodesRef = useRef<SimNode[]>([]);
  const edgesRef = useRef<SimEdge[]>([]);
  const viewRef = useRef<GraphView>({ x: 0, y: 0, scale: 1 });
  const dragRef = useRef<DragState | null>(null);
  // Active pointers by id (canvas coords): two make a touch pinch, which zooms instead of dragging.
  const pointersRef = useRef(new Map<number, Point>());
  // The node currently held under the cursor. The simulation keeps it fixed while still letting it
  // pull its neighbours, so grabbing a node stretches its edges like Obsidian's graph.
  const pinnedRef = useRef<SimNode | null>(null);
  const simulationRef = useRef<Simulation<SimNode, SimEdge> | null>(null);
  const ticksRef = useRef(0);
  // In-flight settling pass: the idle handle to cancel, and the ticks it has spent against the cap.
  const settleIdleRef = useRef<number | null>(null);
  const settleTicksRef = useRef(0);
  // Only the very first layout hides the canvas while it settles. Later ones (a resize, another
  // note's graph) keep the previous drawing up and swap it out when the new one lands, so resizing a
  // window does not strobe.
  const firstLayoutRef = useRef(true);
  const hoverRef = useRef<NoteID | null>(null);
  const userAdjustedRef = useRef(false);
  const graphRef = useRef(graph);
  const onSelectRef = useRef(onSelect);
  const onHoverRef = useRef(onHover);
  const highlightRef = useRef<ReadonlySet<NoteID> | null>(highlightIds);
  const [size, setSize] = useState({ width: 1, height: 1 });
  const themeVersion = useThemeVersion();

  onSelectRef.current = onSelect;
  onHoverRef.current = onHover;
  highlightRef.current = highlightIds;

  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;

    const observer = new ResizeObserver(([entry]) => {
      const rect = entry.contentRect;
      setSize({ width: Math.max(1, rect.width), height: Math.max(1, rect.height) });
    });
    observer.observe(canvas);
    return () => observer.disconnect();
  }, []);

  useEffect(() => {
    graphRef.current = graph;
    initializeGraph(graph);
    userAdjustedRef.current = false;
    ticksRef.current = 0;
    viewRef.current = fitGraphView(size);
    startGraph();

    return () => stopGraph();
  }, [graph, size, focusNodeID]);

  useEffect(() => {
    resizeCanvas(size);
    if (!userAdjustedRef.current) {
      viewRef.current = fitGraphView(size);
    }
    drawGraph(size);
  }, [size, themeVersion]);

  useEffect(() => {
    dragRef.current = null;
    userAdjustedRef.current = false;
    viewRef.current = fitGraphView(size);
    drawGraph(size);
  }, [resetToken]);

  // Recolor in place when the highlight set changes (a settled graph is not otherwise redrawing).
  useEffect(() => {
    drawGraph(size);
  }, [highlightIds]);

  function initializeGraph(nextGraph: Graph) {
    const graphNodes = nextGraph.nodes || [];
    const nodes = graphNodes.map((node, index) => {
      const isolated = graphNodes.length === 1;
      const angle = (Math.PI * 2 * index) / Math.max(1, graphNodes.length);
      return {
        ...node,
        x: isolated ? 0 : Math.cos(angle) * 160,
        y: isolated ? 0 : Math.sin(angle) * 160,
        vx: 0,
        vy: 0,
        degree: 0,
      };
    });
    const byID = new Map(nodes.map((node) => [node.note_id, node]));
    const edges = (nextGraph.edges || [])
      .map((edge: GraphEdge) => {
        const source = byID.get(edge.source_id);
        const target = byID.get(edge.target_id);
        return source && target ? { source, target } : undefined;
      })
      .filter((edge): edge is SimEdge => edge !== undefined);
    edges.forEach((edge) => {
      edge.source.degree += 1;
      edge.target.degree += 1;
    });
    nodesRef.current = nodes;
    edgesRef.current = edges;
  }

  function startGraph() {
    stopGraph();
    resizeCanvas(size);
    if (nodesRef.current.length <= 1 || edgesRef.current.length === 0) {
      drawGraph(size);
      return;
    }

    // Lay the graph out at the size it will be seen at. Nodes and labels are drawn at a fixed
    // SCREEN size (radius and font are divided by the view scale), so a layout built for a
    // full-page canvas and then shrunk to fit a small one keeps its dots the same size while the
    // distances between them collapse — the note aside's 300x220 box turned the graph into a blob.
    // Scaling the world-space constants with the viewport instead lands the fit near 1:1, where the
    // separation matches what the dots are drawn at. Charge goes with the square: it falls off with
    // distance², so the equilibrium only holds its shape if it scales with area.
    const layout = layoutScale(size);
    const simulation = forceSimulation<SimNode, SimEdge>(nodesRef.current)
      .velocityDecay(0.18)
      .alpha(1)
      .alphaDecay(0.035)
      .force(
        "link",
        forceLink<SimNode, SimEdge>(edgesRef.current)
          .id((node) => String(node.note_id))
          .distance(110 * layout)
          .strength(0.12),
      )
      .force("charge", forceManyBody<SimNode>().strength(-1400 * layout * layout).distanceMin(9))
      .force("x", forceX<SimNode>(0).strength(0.002))
      .force("y", forceY<SimNode>(0).strength(0.002))
      .on("tick", () => {
        if (!userAdjustedRef.current && ticksRef.current < FIT_FOLLOW_TICKS) {
          viewRef.current = fitGraphView(size);
        }
        drawGraph(size);
        ticksRef.current += 1;
      });
    simulationRef.current = simulation;

    // Settle before the first paint. The trip from the seed ring to equilibrium is noise, not
    // information, and drawing it spends a canvas frame per tick — this vault's 194-node graph runs
    // 194 of them. simulation.tick() does not fire "tick", so nothing is drawn until the layout
    // lands; the ticks the drag handlers restart later still paint as they always did.
    simulation.stop();
    settleTicksRef.current = 0;
    settleGraph();
  }

  // settleGraph runs the layout to equilibrium off the page's critical path: the first slice is
  // inline on a frame budget (a small graph is done here, with no blank and no wait for an idle
  // callback that a busy page may hold for a hundred milliseconds), and a graph too big for that
  // hands the rest to requestIdleCallback rather than blocking a frame it does not deserve.
  function settleGraph(deadline?: IdleDeadline) {
    const simulation = simulationRef.current;
    if (!simulation) return;

    const until = performance.now() + SETTLE_BUDGET_MS;
    const affordable = () => (deadline ? deadline.timeRemaining() > 1 : performance.now() < until);
    while (
      simulation.alpha() > simulation.alphaMin() &&
      settleTicksRef.current < SETTLE_TICK_CAP &&
      affordable()
    ) {
      simulation.tick();
      settleTicksRef.current += 1;
    }

    if (simulation.alpha() > simulation.alphaMin() && settleTicksRef.current < SETTLE_TICK_CAP) {
      // Blank rather than show a layout mid-flight — but only the first time, so a resize keeps the
      // drawing it already has until the new one is ready.
      if (firstLayoutRef.current) canvasRef.current?.setAttribute("data-settling", "true");
      settleIdleRef.current = requestIdle(settleGraph);
      return;
    }

    settleIdleRef.current = null;
    firstLayoutRef.current = false;
    canvasRef.current?.removeAttribute("data-settling");
    // The layout is final, so the view is fitted once here instead of chased on every tick.
    ticksRef.current = FIT_FOLLOW_TICKS;
    if (!userAdjustedRef.current) viewRef.current = fitGraphView(size);
    drawGraph(size);
  }

  function stopGraph() {
    if (settleIdleRef.current !== null) {
      cancelIdle(settleIdleRef.current);
      settleIdleRef.current = null;
    }
    simulationRef.current?.stop();
    simulationRef.current = null;
  }

  function resizeCanvas(nextSize: { width: number; height: number }) {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ratio = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.floor(nextSize.width * ratio));
    canvas.height = Math.max(1, Math.floor(nextSize.height * ratio));
  }

  function fitGraphView(nextSize: { width: number; height: number }): GraphView {
    const focused = focusedGraphView(nextSize);
    if (focused) return focused;

    const nodes = nodesRef.current;
    if (nodes.length === 0) {
      return { x: 0, y: 0, scale: 1 };
    }

    let minX = Infinity;
    let maxX = -Infinity;
    let minY = Infinity;
    let maxY = -Infinity;
    nodes.forEach((node) => {
      minX = Math.min(minX, node.x);
      maxX = Math.max(maxX, node.x);
      minY = Math.min(minY, node.y);
      maxY = Math.max(maxY, node.y);
    });
    // The margin is a share of the box, not a flat 96px: in the aside's 220px-tall canvas a fixed
    // 96 ate nearly half the height, so the fit had to shrink the layout far past the point where
    // the fixed-size dots still had room between them.
    const padding = Math.min(96, Math.min(nextSize.width, nextSize.height) * 0.18);
    const graphW = Math.max(1, maxX - minX);
    const graphH = Math.max(1, maxY - minY);
    const availW = Math.max(1, nextSize.width - padding);
    const availH = Math.max(1, nextSize.height - padding);
    // Fitting everything in is only worth doing while the result stays readable. Below the floor the
    // dots — drawn at a fixed screen size — sit closer together than they are wide and the graph
    // reads as one blob, which is what a busy neighbourhood did to the note aside. Past that point a
    // small canvas stops zooming out and shows the graph at the floor, centred on the note itself so
    // the one node you came for is never the one pushed off; panning and zooming reach the rest.
    // A full-size canvas keeps the old near-zero floor: the whole-vault view is an overview, where
    // seeing everything at once is the point and a clipped graph would be the wrong answer.
    const floor = nextSize.width >= 420 ? 0.05 : 0.35;
    const fitted = Math.min(availW / graphW, availH / graphH);
    const scale = Math.max(floor, Math.min(0.65, fitted));
    const center =
      fitted < floor ? nodesRef.current.find((node) => node.center || node.note_id === graphRef.current.center_id) : undefined;
    const centerX = center ? center.x : (minX + maxX) / 2;
    const centerY = center ? center.y : (minY + maxY) / 2;
    return {
      x: -centerX * scale,
      y: -centerY * scale,
      scale,
    };
  }

  function focusedGraphView(nextSize: { width: number; height: number }): GraphView | null {
    if (focusNodeID === undefined) return null;
    const node = nodesRef.current.find((candidate) => candidate.note_id === focusNodeID);
    if (!node) return null;
    const scale = clamp(Math.min(nextSize.width, nextSize.height) / 640, 0.42, 0.65);
    return {
      x: -node.x * scale,
      y: -node.y * scale,
      scale,
    };
  }

  function drawGraph(nextSize: { width: number; height: number }) {
    const canvas = canvasRef.current;
    const ctx = canvas?.getContext("2d");
    if (!canvas || !ctx) return;

    const ratio = window.devicePixelRatio || 1;
    const width = Math.max(1, Math.floor(nextSize.width * ratio));
    const height = Math.max(1, Math.floor(nextSize.height * ratio));
    if (canvas.width !== width || canvas.height !== height) {
      canvas.width = width;
      canvas.height = height;
    }

    const view = viewRef.current;
    const graph = graphRef.current;
    ctx.clearRect(0, 0, width, height);
    ctx.save();
    ctx.translate(width / 2 + view.x * ratio, height / 2 + view.y * ratio);
    ctx.scale(view.scale, view.scale);
    const labelFontSize = 13;
    ctx.font = `${Math.floor((labelFontSize * ratio) / view.scale)}px ${
      css("--font-sans") || '"IBM Plex Sans JP", Inter, system-ui, sans-serif'
    }`;
    const baseLineWidth = (1 * ratio) / view.scale;
    const highlightLineWidth = (2.6 * ratio) / view.scale;
    ctx.lineWidth = baseLineWidth;
    ctx.strokeStyle = css("--line-strong");

    // Search and hover both keep the graph shape intact: active nodes/edges stay strong while the rest
    // dim in place instead of being removed from the graph.
    const searchHighlight = highlightRef.current;
    const hoverID = decorative ? null : hoverRef.current;
    const hoverHighlight = hoverID === null ? null : new Set<NoteID>([hoverID]);
    if (hoverHighlight) {
      edgesRef.current.forEach((edge) => {
        if (edge.source.note_id === hoverID || edge.target.note_id === hoverID) {
          hoverHighlight.add(edge.source.note_id);
          hoverHighlight.add(edge.target.note_id);
        }
      });
    }
    const hasActiveHighlight = searchHighlight !== null || hoverHighlight !== null;
    const nodeIsActive = (nodeID: NoteID): boolean =>
      !hasActiveHighlight || Boolean(searchHighlight?.has(nodeID) || hoverHighlight?.has(nodeID));
    const edgeIsActive = (edge: SimEdge): boolean =>
      Boolean(
        (searchHighlight?.has(edge.source.note_id) && searchHighlight.has(edge.target.note_id)) ||
          edge.source.note_id === hoverID ||
          edge.target.note_id === hoverID,
      );
    edgesRef.current.forEach((edge) => {
      if (hasActiveHighlight) {
        const active = edgeIsActive(edge);
        ctx.globalAlpha = active ? 0.86 : 0.08;
        ctx.lineWidth = active ? highlightLineWidth : baseLineWidth;
        // Ink, not the salient: emphasis here is contrast against edges that dim, and the vermilion
        // is spent on where you are (see the node pass below).
        ctx.strokeStyle = active ? css("--text") : css("--line-strong");
      } else {
        ctx.globalAlpha = 0.62;
        ctx.lineWidth = baseLineWidth;
        ctx.strokeStyle = css("--line-strong");
      }
      ctx.beginPath();
      ctx.moveTo(edge.source.x * ratio, edge.source.y * ratio);
      ctx.lineTo(edge.target.x * ratio, edge.target.y * ratio);
      ctx.stroke();
    });

    // Draw all node circles first, then all labels, so a label is never hidden behind a node drawn
    // later in the pass. (Edges are already drawn underneath above.)
    nodesRef.current.forEach((node) => {
      const center = node.center || node.note_id === graph.center_id;
      const active = nodeIsActive(node.note_id);
      const radius = (nodeRadius(node) * ratio) / view.scale;
      const x = node.x * ratio;
      const y = node.y * ratio;
      const nodeAlpha = active ? 0.92 : 0.18;
      ctx.lineWidth = hasActiveHighlight && active ? highlightLineWidth : baseLineWidth;
      ctx.beginPath();
      ctx.arc(x, y, radius, 0, Math.PI * 2);
      if (hasActiveHighlight && active) {
        // A match is ink: what marks it is the contrast with everything that dims in place, not a
        // colour of its own. The centre keeps the salient even while highlighted, so a frame frozen
        // mid-hover still says which note you are on rather than showing one undifferentiated set.
        ctx.fillStyle = center ? css("--mark") : css("--text");
        ctx.strokeStyle = center ? css("--mark") : css("--text");
      } else {
        // At rest the graph is ink and outline (design.md, Sidebar): the note you are on is the one
        // filled dot, its neighbours are rings on the page. Colour is reserved for the highlight
        // above, where it means "this is what you asked for".
        ctx.fillStyle = center && !hasActiveHighlight ? css("--mark") : css("--bg");
        ctx.strokeStyle = center ? css("--mark") : css("--line-node");
      }
      // Edges are painted first, so a translucent fill would let them show through every node. Keep
      // the interior opaque and use alpha only for the outline's inactive/active emphasis.
      ctx.globalAlpha = 1;
      ctx.fill();
      ctx.globalAlpha = nodeAlpha;
      ctx.stroke();
    });

    // Labels are drawn at a fixed screen size, so a canvas holds far fewer of them than it holds
    // nodes. On a roomy canvas everything past a readable zoom is named, and a well-connected node
    // is named even when zoomed out. A small one — the note aside — cannot afford either rule: in a
    // densely linked vault every neighbor clears any degree bar, and the box fills with overlapping
    // text. There, only the note itself, whatever is under the pointer, and a highlighted match are
    // named; the rest are dots to hover — and in the aside the backlinks and children lists right
    // above the graph already name the neighbourhood in text, so the graph is there for its shape.
    const roomy = nextSize.width >= 420;
    const showLabels = roomy && view.scale >= 0.26;
    nodesRef.current.forEach((node) => {
      if (decorative) return;
      const center = node.center || node.note_id === graph.center_id;
      const active = nodeIsActive(node.note_id);
      const hovered = node.note_id === hoverRef.current;
      const wellConnected = roomy && node.degree >= 4;
      if (!(showLabels || center || wellConnected || hovered || (hasActiveHighlight && active))) {
        return;
      }
      const radius = (nodeRadius(node) * ratio) / view.scale;
      const x = node.x * ratio;
      const y = node.y * ratio;
      const label = hovered
        ? node.title || `#${node.note_id}`
        : trim(node.title || `#${node.note_id}`, 24);
      const fontPx = (labelFontSize * ratio) / view.scale;
      const padX = (5 * ratio) / view.scale;
      const padY = (3 * ratio) / view.scale;
      const tx = x + radius + (7 * ratio) / view.scale;
      const ty = y;
      ctx.textAlign = "start";
      ctx.textBaseline = "middle";
      const textWidth = ctx.measureText(label).width;
      // A padded backdrop keeps the label legible where edges or other nodes pass behind it,
      // instead of the text sitting directly on a line.
      ctx.globalAlpha = center || hovered ? 0.92 : 0.78;
      ctx.fillStyle = css("--panel");
      fillRoundRect(
        ctx,
        tx - padX,
        ty - fontPx / 2 - padY,
        textWidth + padX * 2,
        fontPx + padY * 2,
        (4 * ratio) / view.scale,
      );
      ctx.globalAlpha = center || hovered ? 0.98 : 0.88;
      ctx.fillStyle = css("--text");
      ctx.fillText(label, tx, ty);
      ctx.globalAlpha = 0.9;
    });
    ctx.restore();
  }

  function canvasPoint(event: PointerEvent<HTMLCanvasElement>): Point {
    const rect = event.currentTarget.getBoundingClientRect();
    return {
      x: event.clientX - rect.left,
      y: event.clientY - rect.top,
    };
  }

  function worldPoint(point: Point): Point {
    const view = viewRef.current;
    return {
      x: (point.x - size.width / 2 - view.x) / view.scale,
      y: (point.y - size.height / 2 - view.y) / view.scale,
    };
  }

  // nodeRadius returns a node's drawn radius in CSS pixels (independent of zoom): larger for the center
  // node and for higher-degree nodes. Rendering and hit-testing both use it so the clickable area always
  // matches the dot the user actually sees.
  function nodeRadius(node: SimNode): number {
    const center = node.center || node.note_id === graphRef.current.center_id;
    const base = center ? 10 : 6;
    return base + Math.min(8, Math.sqrt(node.degree) * 2);
  }

  function graphNodeAt(point: Point): SimNode | undefined {
    const world = worldPoint(point);
    const scale = viewRef.current.scale;
    // A few CSS pixels of slack on top of the drawn radius keep small dots comfortably clickable,
    // without the old flat 34px hit area that selected nodes the pointer was nowhere near.
    const pad = 5;
    let best: SimNode | undefined;
    let bestD = Infinity;
    nodesRef.current.forEach((node) => {
      const dx = node.x - world.x;
      const dy = node.y - world.y;
      const distance = dx * dx + dy * dy;
      if (distance >= bestD) return;
      const threshold = (nodeRadius(node) + pad) / scale;
      if (distance <= threshold * threshold) {
        bestD = distance;
        best = node;
      }
    });
    return best;
  }

  function pointerDown(event: PointerEvent<HTMLCanvasElement>) {
    event.preventDefault();
    const point = canvasPoint(event);
    pointersRef.current.set(event.pointerId, point);
    if (pointersRef.current.size >= 2) {
      // A second finger turns the gesture into a pinch: release the one-finger drag (and any grabbed
      // node) so pinchMove owns the view until a finger lifts.
      event.currentTarget.setPointerCapture(event.pointerId);
      dragRef.current = null;
      if (pinnedRef.current) {
        pinnedRef.current.fx = null;
        pinnedRef.current.fy = null;
        simulationRef.current?.alphaTarget(0).restart();
        pinnedRef.current = null;
      }
      return;
    }
    // Grabbing a node drags it (edges stay elastic, pulling neighbours); grabbing empty space pans.
    const node = decorative ? undefined : graphNodeAt(point);
    dragRef.current = { pointerId: event.pointerId, start: point, last: point, moved: false, node };
    pinnedRef.current = node ?? null;
    // Grabbing/panning dismisses the transient hover preview so it never blocks the drag.
    if (!decorative && hoverRef.current !== null) {
      hoverRef.current = null;
      onHoverRef.current?.(null, { x: event.clientX, y: event.clientY });
    }
    if (node) {
      node.fx = node.x;
      node.fy = node.y;
      simulationRef.current?.alphaTarget(0.18).restart();
    }
    event.currentTarget.setPointerCapture(event.pointerId);
    event.currentTarget.classList.add("dragging");
  }

  function updateHover(event: PointerEvent<HTMLCanvasElement>) {
    const node = graphNodeAt(canvasPoint(event));
    const hoverID = node ? node.note_id : null;
    if (hoverID !== hoverRef.current) {
      hoverRef.current = hoverID;
      event.currentTarget.style.cursor = hoverID !== null ? "pointer" : "";
      if (!decorative) onHoverRef.current?.(hoverID, { x: event.clientX, y: event.clientY });
      drawGraph(size);
    }
  }

  function pointerMove(event: PointerEvent<HTMLCanvasElement>) {
    if (pointersRef.current.size >= 2 && pointersRef.current.has(event.pointerId)) {
      pinchMove(event);
      return;
    }
    if (pointersRef.current.has(event.pointerId)) {
      pointersRef.current.set(event.pointerId, canvasPoint(event));
    }
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) {
      updateHover(event);
      return;
    }
    event.preventDefault();
    const point = canvasPoint(event);
    if (Math.abs(point.x - drag.start.x) + Math.abs(point.y - drag.start.y) > 4) {
      drag.moved = true;
    }
    if (drag.node) {
      // Pin the grabbed node to the cursor; the running simulation keeps the edges springy so linked
      // nodes trail after it and settle elastically once it is released.
      const world = worldPoint(point);
      drag.node.x = world.x;
      drag.node.y = world.y;
      drag.node.vx = 0;
      drag.node.vy = 0;
      drag.node.fx = world.x;
      drag.node.fy = world.y;
    } else {
      const dx = point.x - drag.last.x;
      const dy = point.y - drag.last.y;
      viewRef.current = {
        ...viewRef.current,
        x: viewRef.current.x + dx,
        y: viewRef.current.y + dy,
      };
    }
    userAdjustedRef.current = true;
    drag.last = point;
    drawGraph(size);
  }

  // pinchMove zooms the view by the change in finger distance, keeping the world point that was under
  // the previous midpoint pinned to the current midpoint (so the pinch pans and zooms together, like
  // every touch map). The wheel handler is the single-pointer analogue of the same math.
  function pinchMove(event: PointerEvent<HTMLCanvasElement>) {
    event.preventDefault();
    const before = pinchGeometry([...pointersRef.current.values()]);
    pointersRef.current.set(event.pointerId, canvasPoint(event));
    const after = pinchGeometry([...pointersRef.current.values()]);
    if (before.dist <= 0 || after.dist <= 0) return;
    const world = worldPoint(before.mid);
    const scale = clamp(viewRef.current.scale * (after.dist / before.dist), 0.015, 4);
    viewRef.current = {
      x: after.mid.x - size.width / 2 - world.x * scale,
      y: after.mid.y - size.height / 2 - world.y * scale,
      scale,
    };
    userAdjustedRef.current = true;
    drawGraph(size);
  }

  function pointerUp(event: PointerEvent<HTMLCanvasElement>) {
    pointersRef.current.delete(event.pointerId);
    const drag = dragRef.current;
    const point = canvasPoint(event);
    dragRef.current = null;
    // Releasing unpins the node, so the simulation eases it back into equilibrium with its neighbours.
    if (pinnedRef.current) {
      pinnedRef.current.fx = null;
      pinnedRef.current.fy = null;
    }
    pinnedRef.current = null;
    const simulation = simulationRef.current;
    if (drag?.node && simulation) {
      simulation.alphaTarget(0).alpha(Math.max(simulation.alpha(), 0.25)).restart();
    }
    event.currentTarget.classList.remove("dragging");
    if (!drag || drag.pointerId !== event.pointerId) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
    if (drag.moved) return;
    // A click without a drag selects the node under the pointer (navigation).
    const node = drag.node ?? graphNodeAt(point);
    if (node) onSelectRef.current(node.note_id, point);
  }

  function pointerCancel(event: PointerEvent<HTMLCanvasElement>) {
    pointersRef.current.delete(event.pointerId);
    dragRef.current = null;
    if (pinnedRef.current) {
      pinnedRef.current.fx = null;
      pinnedRef.current.fy = null;
      simulationRef.current?.alphaTarget(0).restart();
    }
    pinnedRef.current = null;
    event.currentTarget.classList.remove("dragging");
  }

  function pointerLeave(event: PointerEvent<HTMLCanvasElement>) {
    if (hoverRef.current !== null) {
      hoverRef.current = null;
      event.currentTarget.style.cursor = "";
      if (!decorative) onHoverRef.current?.(null, { x: event.clientX, y: event.clientY });
      drawGraph(size);
    }
  }

  // The graph zooms only on ctrl+wheel (trackpad pinch) or shift+wheel; a bare wheel is left to
  // scroll whatever is under the cursor (see graphWheel.ts). Registered natively because React
  // attaches onWheel passively (React 17+), where preventDefault() is ignored — the zoom would
  // also scroll the note aside/page under the cursor, and a pinch would browser-zoom the whole page.
  useEffect(() => {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const wheel = (event: globalThis.WheelEvent) => {
      if (!isZoomWheel(event)) return;
      event.preventDefault();
      const rect = canvas.getBoundingClientRect();
      const point = { x: event.clientX - rect.left, y: event.clientY - rect.top };
      const before = worldPoint(point);
      const factor = Math.exp(-zoomDelta(event) * 0.001);
      const scale = clamp(viewRef.current.scale * factor, 0.015, 4);
      viewRef.current = {
        x: point.x - size.width / 2 - before.x * scale,
        y: point.y - size.height / 2 - before.y * scale,
        scale,
      };
      userAdjustedRef.current = true;
      drawGraph(size);
    };
    canvas.addEventListener("wheel", wheel, { passive: false });
    return () => canvas.removeEventListener("wheel", wheel);
  });

  return (
    <canvas
      ref={canvasRef}
      className="graph-canvas"
      onPointerDown={pointerDown}
      onPointerMove={pointerMove}
      onPointerUp={pointerUp}
      onPointerCancel={pointerCancel}
      onPointerLeave={pointerLeave}
    />
  );
}

function css(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

// layoutScale sizes the force layout to the canvas: 1 at the full-page graph's scale and smaller
// for a panel or the note aside, floored so a very short box still spreads its nodes rather than
// stacking them. 520 is roughly the smaller side of the floating panel, the size the constants
// above were tuned against.
function layoutScale(size: { width: number; height: number }): number {
  return clamp(Math.min(size.width, size.height) / 520, 0.42, 1);
}

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}

// fillRoundRect fills a rounded rectangle, falling back to a plain rectangle where the canvas API
// lacks roundRect (older engines). Used for the padded backdrop behind graph labels.
function fillRoundRect(
  ctx: CanvasRenderingContext2D,
  x: number,
  y: number,
  w: number,
  h: number,
  r: number,
): void {
  ctx.beginPath();
  if (typeof ctx.roundRect === "function") {
    ctx.roundRect(x, y, w, h, r);
  } else {
    ctx.rect(x, y, w, h);
  }
  ctx.fill();
}

function trim(text: string, max: number): string {
  return text.length <= max ? text : `${text.slice(0, max - 1)}...`;
}
