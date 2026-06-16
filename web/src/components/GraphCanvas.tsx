import { PointerEvent, WheelEvent, useEffect, useRef, useState } from "react";
import type { Graph, GraphEdge, GraphNode, NoteID } from "../types";

interface GraphCanvasProps {
  graph: Graph;
  onSelect: (noteID: NoteID) => void;
  resetToken: number;
  // Background decoration: draw nodes/edges only, no labels or interaction.
  decorative?: boolean;
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
}

export function GraphCanvas({ graph, onSelect, resetToken, decorative = false }: GraphCanvasProps) {
  const canvasRef = useRef<HTMLCanvasElement | null>(null);
  const nodesRef = useRef<SimNode[]>([]);
  const edgesRef = useRef<SimEdge[]>([]);
  const viewRef = useRef<GraphView>({ x: 0, y: 0, scale: 1 });
  const dragRef = useRef<DragState | null>(null);
  const animationRef = useRef<number | null>(null);
  const ticksRef = useRef(0);
  const hoverRef = useRef<NoteID | null>(null);
  const userAdjustedRef = useRef(false);
  const graphRef = useRef(graph);
  const onSelectRef = useRef(onSelect);
  const [size, setSize] = useState({ width: 1, height: 1 });

  onSelectRef.current = onSelect;

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
        const force = 1400 / d2;
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
      const force = (dist - 110) * 0.012;
      const fx = (dx / dist) * force;
      const fy = (dy / dist) * force;
      edge.source.vx += fx;
      edge.source.vy += fy;
      edge.target.vx -= fx;
      edge.target.vy -= fy;
    });

    nodes.forEach((node) => {
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
    ctx.lineWidth = (1 * ratio) / view.scale;
    ctx.globalAlpha = 0.62;
    ctx.strokeStyle = css("--line");

    edgesRef.current.forEach((edge) => {
      ctx.beginPath();
      ctx.moveTo(edge.source.x * ratio, edge.source.y * ratio);
      ctx.lineTo(edge.target.x * ratio, edge.target.y * ratio);
      ctx.stroke();
    });

    ctx.globalAlpha = 0.9;
    const showLabels = view.scale >= 0.4;
    nodesRef.current.forEach((node) => {
      const center = node.center || node.note_id === graph.center_id;
      const base = center ? 10 : 6;
      const radius = ((base + Math.min(8, Math.sqrt(node.degree) * 2)) * ratio) / view.scale;
      const x = node.x * ratio;
      const y = node.y * ratio;
      ctx.beginPath();
      ctx.arc(x, y, radius, 0, Math.PI * 2);
      ctx.fillStyle = center
        ? css("--accent")
        : css("--panel-soft");
      ctx.strokeStyle = center ? css("--accent-strong") : css("--muted");
      ctx.fill();
      ctx.stroke();

      const hovered = node.note_id === hoverRef.current;
      if (!decorative && (showLabels || center || node.degree >= 5 || hovered)) {
        ctx.globalAlpha = center || hovered ? 0.95 : 0.78;
        ctx.fillStyle = css("--text");
        ctx.textAlign = "start";
        ctx.fillText(trim(node.title || `#${node.note_id}`, 20), x + radius + 5, y + (4 * ratio) / view.scale);
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
    dragRef.current = { pointerId: event.pointerId, start: point, last: point, moved: false };
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
    const dx = point.x - drag.last.x;
    const dy = point.y - drag.last.y;
    viewRef.current = {
      ...viewRef.current,
      x: viewRef.current.x + dx,
      y: viewRef.current.y + dy,
    };
    userAdjustedRef.current = true;
    if (Math.abs(point.x - drag.start.x) + Math.abs(point.y - drag.start.y) > 4) {
      drag.moved = true;
    }
    drag.last = point;
    drawGraph(size);
  }

  function pointerUp(event: PointerEvent<HTMLCanvasElement>) {
    const drag = dragRef.current;
    dragRef.current = null;
    event.currentTarget.classList.remove("dragging");
    if (!drag || drag.pointerId !== event.pointerId) return;
    event.currentTarget.releasePointerCapture(event.pointerId);
    if (drag.moved) return;
    const node = graphNodeAt(canvasPoint(event));
    if (node) onSelectRef.current(node.note_id);
  }

  function pointerCancel(event: PointerEvent<HTMLCanvasElement>) {
    dragRef.current = null;
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

function trim(text: string, max: number): string {
  return text.length <= max ? text : `${text.slice(0, max - 1)}...`;
}
