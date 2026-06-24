import { PointerEvent, WheelEvent, useEffect, useRef, useState } from "react";
import type { Graph, GraphEdge, GraphNode, NoteID } from "../types";

interface GraphCanvasProps {
  graph: Graph;
  onSelect: (noteID: NoteID) => void;
  resetToken: number;
  // Background decoration: draw nodes/edges only, no labels or interaction.
  decorative?: boolean;
  // When set, only these nodes are drawn at full strength (accent); the rest dim in place. null draws
  // every node normally. Used by the home search to highlight matches without dropping the others.
  highlightIds?: ReadonlySet<NoteID> | null;
}

interface SimNode extends GraphNode {
  x: number;
  y: number;
  vx: number;
  vy: number;
  degree: number;
}

interface SimEdge {
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
  resetToken,
  decorative = false,
  highlightIds = null,
}: GraphCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const nodesRef = useRef<SimNode[]>([]);
  const edgesRef = useRef<SimEdge[]>([]);
  const viewRef = useRef<GraphView>({ x: 0, y: 0, scale: 1 });
  const dragRef = useRef<DragState | null>(null);
  // The node currently held under the cursor. The simulation keeps it fixed while still letting it
  // pull its neighbours, so grabbing a node stretches its edges like Obsidian's graph.
  const pinnedRef = useRef<SimNode | null>(null);
  const animationRef = useRef<number | null>(null);
  const ticksRef = useRef(0);
  const hoverRef = useRef<NoteID | null>(null);
  const userAdjustedRef = useRef(false);
  const graphRef = useRef(graph);
  const onSelectRef = useRef(onSelect);
  const highlightRef = useRef<ReadonlySet<NoteID> | null>(highlightIds);
  const [size, setSize] = useState({ width: 1, height: 1 });

  onSelectRef.current = onSelect;
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
  }, [graph, size]);

  useEffect(() => {
    resizeCanvas(size);
    if (!userAdjustedRef.current) {
      viewRef.current = fitGraphView(size);
    }
    drawGraph(size);
  }, [size]);

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
        x: isolated ? 0 : Math.cos(angle) * 220,
        y: isolated ? 0 : Math.sin(angle) * 220,
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

    const frame = () => {
      stepGraph();
      if (!userAdjustedRef.current && ticksRef.current < 150) {
        viewRef.current = fitGraphView(size);
      }
      drawGraph(size);
      ticksRef.current += 1;
      animationRef.current = window.requestAnimationFrame(frame);
    };
    frame();
  }

  function stopGraph() {
    if (animationRef.current !== null) {
      window.cancelAnimationFrame(animationRef.current);
      animationRef.current = null;
    }
  }

  function resizeCanvas(nextSize: { width: number; height: number }) {
    const canvas = canvasRef.current;
    if (!canvas) return;
    const ratio = window.devicePixelRatio || 1;
    canvas.width = Math.max(1, Math.floor(nextSize.width * ratio));
    canvas.height = Math.max(1, Math.floor(nextSize.height * ratio));
  }

  function fitGraphView(nextSize: { width: number; height: number }): GraphView {
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
    const padding = 96;
    const graphW = Math.max(1, maxX - minX);
    const graphH = Math.max(1, maxY - minY);
    const availW = Math.max(1, nextSize.width - padding);
    const availH = Math.max(1, nextSize.height - padding);
    const scale = Math.max(0.05, Math.min(0.65, Math.min(availW / graphW, availH / graphH)));
    const centerX = (minX + maxX) / 2;
    const centerY = (minY + maxY) / 2;
    return {
      x: -centerX * scale,
      y: -centerY * scale,
      scale,
    };
  }

  function stepGraph() {
    const nodes = nodesRef.current;
    const edges = edgesRef.current;

    for (let i = 0; i < nodes.length; i += 1) {
      for (let j = i + 1; j < nodes.length; j += 1) {
        const a = nodes[i];
        const b = nodes[j];
        const dx = a.x - b.x;
        const dy = a.y - b.y;
        const d2 = Math.max(80, dx * dx + dy * dy);
        const force = 2200 / d2;
        a.vx += dx * force;
        a.vy += dy * force;
        b.vx -= dx * force;
        b.vy -= dy * force;
      }
    }

    edges.forEach((edge) => {
      const dx = edge.target.x - edge.source.x;
      const dy = edge.target.y - edge.source.y;
      const dist = Math.max(1, Math.sqrt(dx * dx + dy * dy));
      const force = (dist - 150) * 0.012;
      const fx = (dx / dist) * force;
      const fy = (dy / dist) * force;
      edge.source.vx += fx;
      edge.source.vy += fy;
      edge.target.vx -= fx;
      edge.target.vy -= fy;
    });

    const pinned = pinnedRef.current;
    nodes.forEach((node) => {
      if (node === pinned) {
        // The grabbed node stays where the pointer holds it: it still pushes and pulls its neighbours
        // through the forces above, but its own accumulated force is dropped so it doesn't drift off.
        node.vx = 0;
        node.vy = 0;
        return;
      }
      node.vx += -node.x * 0.002;
      node.vy += -node.y * 0.002;
      node.vx *= 0.82;
      node.vy *= 0.82;
      node.x += node.vx;
      node.y += node.vy;
    });
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
    ctx.font = `${Math.floor((12 * ratio) / view.scale)}px system-ui, sans-serif`;
    const baseLineWidth = (1 * ratio) / view.scale;
    const highlightLineWidth = (2.6 * ratio) / view.scale;
    ctx.lineWidth = baseLineWidth;
    ctx.strokeStyle = css("--line");

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
        ctx.strokeStyle = active ? css("--accent-strong") : css("--line");
      } else {
        ctx.globalAlpha = 0.62;
        ctx.lineWidth = baseLineWidth;
        ctx.strokeStyle = css("--line");
      }
      ctx.beginPath();
      ctx.moveTo(edge.source.x * ratio, edge.source.y * ratio);
      ctx.lineTo(edge.target.x * ratio, edge.target.y * ratio);
      ctx.stroke();
    });

    const showLabels = view.scale >= 0.4;
    nodesRef.current.forEach((node) => {
      const center = node.center || node.note_id === graph.center_id;
      const active = nodeIsActive(node.note_id);
      const base = center ? 10 : 6;
      const radius = ((base + Math.min(8, Math.sqrt(node.degree) * 2)) * ratio) / view.scale;
      const x = node.x * ratio;
      const y = node.y * ratio;
      ctx.globalAlpha = active ? 0.92 : 0.18;
      ctx.lineWidth = hasActiveHighlight && active ? highlightLineWidth : baseLineWidth;
      ctx.beginPath();
      ctx.arc(x, y, radius, 0, Math.PI * 2);
      if (hasActiveHighlight && active) {
        // Light active nodes up with the accent so they read as the current search or hover focus.
        ctx.fillStyle = css("--accent");
        ctx.strokeStyle = css("--accent-strong");
      } else {
        ctx.fillStyle = center ? css("--accent") : css("--panel-soft");
        ctx.strokeStyle = center ? css("--accent-strong") : css("--muted");
      }
      ctx.fill();
      ctx.stroke();

      const hovered = node.note_id === hoverRef.current;
      if (!decorative && (showLabels || center || node.degree >= 5 || hovered || (hasActiveHighlight && active))) {
        const label = trim(node.title || `#${node.note_id}`, 20);
        const fontPx = (12 * ratio) / view.scale;
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
      }
    });
    ctx.restore();
  }

  function canvasPoint(event: PointerEvent<HTMLCanvasElement> | WheelEvent<HTMLCanvasElement>): Point {
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

  function graphNodeAt(point: Point): SimNode | undefined {
    const world = worldPoint(point);
    let best: SimNode | undefined;
    let bestD = Infinity;
    nodesRef.current.forEach((node) => {
      const dx = node.x - world.x;
      const dy = node.y - world.y;
      const distance = dx * dx + dy * dy;
      if (distance < bestD) {
        bestD = distance;
        best = node;
      }
    });
    const threshold = 34 / viewRef.current.scale;
    return best && bestD <= threshold * threshold ? best : undefined;
  }

  function pointerDown(event: PointerEvent<HTMLCanvasElement>) {
    event.preventDefault();
    const point = canvasPoint(event);
    // Grabbing a node drags it (edges stay elastic, pulling neighbours); grabbing empty space pans.
    const node = decorative ? undefined : graphNodeAt(point);
    dragRef.current = { pointerId: event.pointerId, start: point, last: point, moved: false, node };
    pinnedRef.current = node ?? null;
    event.currentTarget.setPointerCapture(event.pointerId);
    event.currentTarget.classList.add("dragging");
  }

  function updateHover(event: PointerEvent<HTMLCanvasElement>) {
    const node = graphNodeAt(canvasPoint(event));
    const hoverID = node ? node.note_id : null;
    if (hoverID !== hoverRef.current) {
      hoverRef.current = hoverID;
      event.currentTarget.style.cursor = hoverID !== null ? "pointer" : "";
      drawGraph(size);
    }
  }

  function pointerMove(event: PointerEvent<HTMLCanvasElement>) {
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

  function pointerUp(event: PointerEvent<HTMLCanvasElement>) {
    const drag = dragRef.current;
    dragRef.current = null;
    // Releasing unpins the node, so the simulation eases it back into equilibrium with its neighbours.
    pinnedRef.current = null;
    event.currentTarget.classList.remove("dragging");
    if (!drag || drag.pointerId !== event.pointerId) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
    if (drag.moved) return;
    // A click without a drag selects the node under the pointer (navigation).
    const node = drag.node ?? graphNodeAt(canvasPoint(event));
    if (node) onSelectRef.current(node.note_id);
  }

  function pointerCancel(event: PointerEvent<HTMLCanvasElement>) {
    dragRef.current = null;
    pinnedRef.current = null;
    event.currentTarget.classList.remove("dragging");
  }

  function pointerLeave(event: PointerEvent<HTMLCanvasElement>) {
    if (hoverRef.current !== null) {
      hoverRef.current = null;
      event.currentTarget.style.cursor = "";
      drawGraph(size);
    }
  }

  function wheel(event: WheelEvent<HTMLCanvasElement>) {
    event.preventDefault();
    const point = canvasPoint(event);
    const before = worldPoint(point);
    const factor = Math.exp(-event.deltaY * 0.001);
    const scale = clamp(viewRef.current.scale * factor, 0.05, 4);
    viewRef.current = {
      x: point.x - size.width / 2 - before.x * scale,
      y: point.y - size.height / 2 - before.y * scale,
      scale,
    };
    userAdjustedRef.current = true;
    drawGraph(size);
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
      onWheel={wheel}
    />
  );
}

function css(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
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
