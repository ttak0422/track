import { PointerEvent, useEffect, useMemo, useRef, useState } from "react";
import { useThemeVersion } from "../hooks/useThemeVersion";
import type { Graph, GraphNode, NoteID } from "../types";
import { isZoomWheel, zoomDelta } from "./graphWheel";
import { layoutStatic, type StaticLayout } from "./graphStaticLayout";
import { radiusForNode } from "./nodeRadius";

export interface GraphOverviewCanvasProps {
  graph: Graph;
  onSelect: (noteID: NoteID) => void;
  resetToken: number;
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

interface DragState {
  pointerId: number;
  last: Point;
  moved: boolean;
}

// Room around the fitted layout, in CSS pixels.
const FIT_PADDING = 48;
// The automatic view fits the whole layout and never magnifies past life size: this canvas is an
// overview, where seeing every component at once is the point. Zooming in past the fit stays
// available by hand, at the same limits the force-drawn graphs use.
const MAX_FIT_SCALE = 1;
const MIN_SCALE = 0.015;
const MAX_SCALE = 4;

// GraphOverviewCanvas draws the whole-vault graph as a connection overview: the deterministic static
// layout (graphStaticLayout) rendered in a single pass — no force simulation, no per-tick repaint,
// no labels — because at thousands of notes the questions worth asking are structural (what clusters
// exist, what hangs off what), not positional. Hovering points at a node; clicking selects it.
export function GraphOverviewCanvas({ graph, onSelect, resetToken }: GraphOverviewCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const viewRef = useRef<GraphView>({ x: 0, y: 0, scale: 1 });
  const dragRef = useRef<DragState | null>(null);
  // Active pointers by id (canvas coords): two make a touch pinch, which zooms instead of panning.
  const pointersRef = useRef(new Map<number, Point>());
  const hoverRef = useRef<NoteID | null>(null);
  const userAdjustedRef = useRef(false);
  const onSelectRef = useRef(onSelect);
  onSelectRef.current = onSelect;

  // The layout is pure data derived from the graph, so it is computed exactly once per graph and
  // every later redraw just replays it onto the canvas.
  const layout = useMemo(() => layoutStatic(graph), [graph]);
  const centerID = graph.center_id;
  const [size, setSize] = useState({ width: 1, height: 1 });
  const themeVersion = useThemeVersion();

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

  // A new graph replaces whatever pan/zoom the previous one had: an overview the user must find
  // their way around again is worse than one that always starts showing everything.
  useEffect(() => {
    userAdjustedRef.current = false;
    hoverRef.current = null;
    viewRef.current = fitView(layout, size);
  }, [layout]);

  // One draw covers layout arrival, canvas resize, and theme recoloring; nothing else ever paints.
  useEffect(() => {
    resizeCanvas(size);
    if (!userAdjustedRef.current) {
      viewRef.current = fitView(layout, size);
    }
    drawGraph(size);
  }, [size, themeVersion, layout]);

  useEffect(() => {
    userAdjustedRef.current = false;
    viewRef.current = fitView(layout, size);
    drawGraph(size);
  }, [resetToken]);

  // The graph zooms only on ctrl+wheel (trackpad pinch) or shift+wheel; a bare wheel is left to
  // scroll whatever is under the cursor (see graphWheel.ts). Registered natively because React
  // attaches onWheel passively (React 17+), where preventDefault() is ignored.
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
      const scale = clamp(viewRef.current.scale * factor, MIN_SCALE, MAX_SCALE);
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

  function resizeCanvas(nextSize: { width: number; height: number }) {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ratio = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.floor(nextSize.width * ratio));
    canvas.height = Math.max(1, Math.floor(nextSize.height * ratio));
  }

  function fitView(nextLayout: StaticLayout, nextSize: { width: number; height: number }): GraphView {
    // The layout origin sits at (0, 0), so fitting is a scale plus a half-extent shift; a
    // dimensionless layout (one node, one edge) still centres by standing in for a unit box.
    const extentW = Math.max(1, nextLayout.width);
    const extentH = Math.max(1, nextLayout.height);
    const availW = Math.max(1, nextSize.width - FIT_PADDING);
    const availH = Math.max(1, nextSize.height - FIT_PADDING);
    const scale = clamp(Math.min(availW / extentW, availH / extentH), MIN_SCALE, MAX_FIT_SCALE);
    return { x: -(extentW * scale) / 2, y: -(extentH * scale) / 2, scale };
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
    ctx.clearRect(0, 0, width, height);
    ctx.save();
    ctx.translate(width / 2 + view.x * ratio, height / 2 + view.y * ratio);
    ctx.scale(view.scale, view.scale);
    const lineWidth = (1 * ratio) / view.scale;

    // Edges first, then nodes, so a link never crosses over the dot it belongs to. The overview
    // carries no labels: at vault scale they would paint over each other into noise, and the shape
    // is the message — clicking a dot is how a position becomes a name again.
    ctx.globalAlpha = 0.62;
    ctx.lineWidth = lineWidth;
    ctx.strokeStyle = css("--line-strong");
    for (const edge of graph.edges || []) {
      const source = layout.positions.get(edge.source_id);
      const target = layout.positions.get(edge.target_id);
      if (!source || !target) continue;
      ctx.beginPath();
      ctx.moveTo(source.x * ratio, source.y * ratio);
      ctx.lineTo(target.x * ratio, target.y * ratio);
      ctx.stroke();
    }

    // Nodes draw at a fixed screen size like the force-drawn graphs do, so zooming reframes the
    // field without shrinking its dots away. At rest the field is rings on the page ground; the
    // centre note keeps the salient; hover is ink (design.md, Sidebar).
    const hoveredID = hoverRef.current;
    for (const node of graph.nodes) {
      const position = layout.positions.get(node.note_id);
      if (!position) continue;
      const center = node.center || node.note_id === centerID;
      const hovered = node.note_id === hoveredID;
      const radius = radiusForNode(node, centerID) / view.scale;
      ctx.beginPath();
      ctx.arc(position.x * ratio, position.y * ratio, radius, 0, Math.PI * 2);
      ctx.globalAlpha = 1;
      ctx.fillStyle = hovered ? css("--text") : center ? css("--mark") : css("--bg");
      ctx.fill();
      ctx.globalAlpha = hovered ? 1 : 0.92;
      ctx.strokeStyle = hovered ? css("--text") : center ? css("--mark") : css("--line-strong");
      ctx.stroke();
    }
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

  // nodeAt finds the nearest node within a few CSS pixels of slack on top of its drawn radius, so
  // hit-testing matches the dot the user actually sees (the same rule the force-drawn graph uses).
  function nodeAt(point: Point): GraphNode | undefined {
    const world = worldPoint(point);
    const scale = viewRef.current.scale;
    const pad = 5;
    let best: GraphNode | undefined;
    let bestD = Infinity;
    for (const node of graph.nodes) {
      const position = layout.positions.get(node.note_id);
      if (!position) continue;
      const dx = position.x - world.x;
      const dy = position.y - world.y;
      const distance = dx * dx + dy * dy;
      if (distance >= bestD) continue;
      const threshold = (radiusForNode(node, centerID) + pad) / scale;
      if (distance <= threshold * threshold) {
        bestD = distance;
        best = node;
      }
    }
    return best;
  }

  function updateHover(event: PointerEvent<HTMLCanvasElement>) {
    const id = nodeAt(canvasPoint(event))?.note_id ?? null;
    if (id === hoverRef.current) return;
    hoverRef.current = id;
    event.currentTarget.style.cursor = id !== null ? "pointer" : "";
    drawGraph(size);
  }

  function clearHover(canvas: HTMLCanvasElement) {
    if (hoverRef.current === null) return;
    hoverRef.current = null;
    canvas.style.cursor = "";
    drawGraph(size);
  }

  // pinchGeometry reduces the first two active pointers to the midpoint and distance a pinch is made of.
  function pinchGeometry(points: Point[]): { mid: Point; dist: number } {
    const [a, b] = points;
    return {
      mid: { x: (a.x + b.x) / 2, y: (a.y + b.y) / 2 },
      dist: Math.hypot(a.x - b.x, a.y - b.y),
    };
  }

  function pinchMove(event: PointerEvent<HTMLCanvasElement>) {
    event.preventDefault();
    const before = pinchGeometry([...pointersRef.current.values()]);
    pointersRef.current.set(event.pointerId, canvasPoint(event));
    const after = pinchGeometry([...pointersRef.current.values()]);
    if (before.dist <= 0 || after.dist <= 0) return;
    // Keep the world point that was under the previous midpoint pinned to the current one, so the
    // pinch pans and zooms together (the wheel handler is the single-pointer analogue).
    const world = worldPoint(before.mid);
    const scale = clamp(viewRef.current.scale * (after.dist / before.dist), MIN_SCALE, MAX_SCALE);
    viewRef.current = {
      x: after.mid.x - size.width / 2 - world.x * scale,
      y: after.mid.y - size.height / 2 - world.y * scale,
      scale,
    };
    userAdjustedRef.current = true;
    drawGraph(size);
  }

  function pointerDown(event: PointerEvent<HTMLCanvasElement>) {
    event.preventDefault();
    const point = canvasPoint(event);
    pointersRef.current.set(event.pointerId, point);
    // Pointer capture keeps a drag that leaves the canvas; called optionally because capture is
    // not implemented everywhere the component renders (jsdom ships without it).
    const canvas = event.currentTarget;
    canvas.setPointerCapture?.(event.pointerId);
    if (pointersRef.current.size >= 2) {
      // A second finger turns the gesture into a pinch: release the one-finger drag so pinchMove
      // owns the view until a finger lifts.
      dragRef.current = null;
      return;
    }
    dragRef.current = { pointerId: event.pointerId, last: point, moved: false };
    canvas.classList.add("dragging");
  }

  function pointerMove(event: PointerEvent<HTMLCanvasElement>) {
    if (pointersRef.current.size >= 2 && pointersRef.current.has(event.pointerId)) {
      pinchMove(event);
      return;
    }
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== event.pointerId) {
      updateHover(event);
      return;
    }
    event.preventDefault();
    const point = canvasPoint(event);
    if (Math.abs(point.x - drag.last.x) + Math.abs(point.y - drag.last.y) > 4) {
      drag.moved = true;
    }
    const view = viewRef.current;
    viewRef.current = {
      ...view,
      x: view.x + point.x - drag.last.x,
      y: view.y + point.y - drag.last.y,
    };
    drag.last = point;
    if (drag.moved) {
      userAdjustedRef.current = true;
      clearHover(event.currentTarget);
    }
    drawGraph(size);
  }

  function pointerUp(event: PointerEvent<HTMLCanvasElement>) {
    pointersRef.current.delete(event.pointerId);
    const drag = dragRef.current;
    const point = canvasPoint(event);
    dragRef.current = null;
    event.currentTarget.classList.remove("dragging");
    if (!drag || drag.pointerId !== event.pointerId) return;
    event.currentTarget.releasePointerCapture?.(event.pointerId);
    // A press without a drag selects the node under the pointer (navigation).
    if (drag.moved) return;
    const node = nodeAt(point);
    if (node) onSelectRef.current(node.note_id);
  }

  function pointerCancel(event: PointerEvent<HTMLCanvasElement>) {
    pointersRef.current.delete(event.pointerId);
    dragRef.current = null;
    event.currentTarget.classList.remove("dragging");
  }

  function pointerLeave(event: PointerEvent<HTMLCanvasElement>) {
    clearHover(event.currentTarget);
  }

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

function clamp(value: number, min: number, max: number): number {
  return Math.max(min, Math.min(max, value));
}
